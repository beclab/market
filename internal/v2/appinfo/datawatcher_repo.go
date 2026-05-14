package appinfo

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"market/internal/v2/appinfo/uploadevent"
	"market/internal/v2/chartrepo"
	"market/internal/v2/helper"
	"market/internal/v2/store"
	"market/internal/v2/types"
	"market/internal/v2/watchers"

	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
)

// StateChange represents a single state change record from the API
type StateChange struct {
	ID        int64                 `json:"id"`                   // Auto-increment ID
	Type      string                `json:"type"`                 // Type of state change
	AppData   *StateChangeAppData   `json:"app_data,omitempty"`   // App upload data
	ImageData *StateChangeImageData `json:"image_data,omitempty"` // Image update data
	Timestamp time.Time             `json:"timestamp"`            // When the change occurred
}

// StateChangeAppData represents data for app upload completed state
type StateChangeAppData struct {
	Source  string `json:"source"`   // Data source
	AppName string `json:"app_name"` // Application name
	UserID  string `json:"user_id"`  // User ID
}

// StateChangeImageData represents data for image info updated state
type StateChangeImageData struct {
	ImageName string `json:"image_name"` // Image name
}

// StateChangesResponse represents the response from /state-changes API
type StateChangesResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    *StateChangesData `json:"data,omitempty"`
}

// StateChangesData represents the data field in the response
type StateChangesData struct {
	AfterID        int64          `json:"after_id"`
	Limit          int            `json:"limit"`
	TypeFilter     string         `json:"type_filter"`
	Count          int            `json:"count"`
	TotalAvailable int            `json:"total_available"`
	StateChanges   []*StateChange `json:"state_changes"`
}

// DataWatcherRepo polls chart-repo's /state-changes endpoint and projects
// the two event types it carries into PG:
//
//   - "app_upload_completed" → uploadevent.FetchAndHandle on the
//     applications + user_applications tables, plus a new_app_ready
//     NATS push to the upload's user. See the uploadevent package for
//     the projection contract; this loop only owns dispatch.
//
//   - "image_info_updated" → store.PatchImageAnalysisImage on
//     user_applications.image_analysis JSONB; the patched users get a
//     direct "image_state_change" push so the frontend updates without
//     waiting for a full hydration cycle.
//
// The Redis cursor (datawatcher:last_processed_id) is the only Redis
// state this module still owns; it is a small kv used to make polling
// resumable across process restarts and is left in Redis for now to
// keep the PG migration scoped.
type DataWatcherRepo struct {
	redisClient     *RedisClient
	lastProcessedID int64
	apiBaseURL      string
	dataWatcher     *DataWatcher // hash recalculation hook (unused on PG path; retained for callers)
	dataSender      *DataSender  // NATS publisher for image_state_change pushes
	ticker          *time.Ticker
	stopChannel     chan bool
	isRunning       bool
	stopOnce        sync.Once
}

// NewDataWatcherRepo creates a new data watcher repository instance.
// The cacheManager argument that previously fed the in-memory write
// paths has been dropped: app_upload_completed now writes through
// uploadevent.FetchAndHandle and image_info_updated through
// store.PatchImageAnalysisImage, neither of which needs the cache.
func NewDataWatcherRepo(redisClient *RedisClient, dataWatcher *DataWatcher, dataSender *DataSender) *DataWatcherRepo {
	apiBaseURL := os.Getenv("CHART_REPO_SERVICE_HOST")
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:8080" // Default fallback
		glog.V(2).Infof("CHART_REPO_SERVICE_HOST not set, using default: %s", apiBaseURL)
	}

	repo := &DataWatcherRepo{
		redisClient: redisClient,
		apiBaseURL:  apiBaseURL,
		dataWatcher: dataWatcher,
		dataSender:  dataSender,
		stopChannel: make(chan bool),
	}

	repo.initializeLastProcessedID()
	return repo
}

// initializeLastProcessedID retrieves the last processed ID from Redis
func (dwr *DataWatcherRepo) initializeLastProcessedID() error {
	ctx := context.Background()

	lastIDStr, err := dwr.redisClient.client.Get(ctx, "datawatcher:last_processed_id").Result()
	if err != nil {
		if err == redis.Nil {
			dwr.lastProcessedID = 0
			glog.Error("No previous state changes found, starting from ID 0")
			return nil
		}
		glog.Errorf("Error retrieving last processed ID from Redis: %v", err)
		return err
	}

	lastID, err := strconv.ParseInt(lastIDStr, 10, 64)
	if err != nil {
		glog.Errorf("Error parsing last processed ID from Redis: %v", err)
		dwr.lastProcessedID = 0
		return nil
	}

	dwr.lastProcessedID = lastID
	glog.V(2).Infof("Initialized last processed ID from Redis: %d", dwr.lastProcessedID)
	return nil
}

// StartWithOptions starts in passive mode; the serial pipeline drives
// processing via ProcessOnce.
func (dwr *DataWatcherRepo) StartWithOptions() error {
	if dwr.isRunning {
		return fmt.Errorf("DataWatcherRepo is already running")
	}

	dwr.isRunning = true
	glog.V(3).Info("Starting DataWatcherRepo in passive mode (serial pipeline handles processing)")
	return nil
}

// ProcessOnce executes one round of state change processing, called by
// Pipeline Phase 2 (after Syncer, before Hydrate). Returns the set of
// user_ids whose user_applications rows were touched, so Pipeline can
// fold them into the affected-users set fed to phaseHashAndSync.
func (dwr *DataWatcherRepo) ProcessOnce() map[string]bool {
	return dwr.processStateChanges()
}

// Stop stops the periodic state checking process
func (dwr *DataWatcherRepo) Stop() error {
	if dwr.ticker != nil {
		dwr.ticker.Stop()
	}

	dwr.stopOnce.Do(func() {
		close(dwr.stopChannel)
	})
	dwr.isRunning = false

	glog.V(3).Info("Data watcher stopped")
	return nil
}

// processStateChanges fetches and processes new state changes. Returns
// the set of user_ids affected by any of the changes processed in this
// invocation — empty map (never nil) when nothing happens.
func (dwr *DataWatcherRepo) processStateChanges() map[string]bool {
	glog.V(2).Infof("Processing state changes after ID: %d", dwr.lastProcessedID)
	affectedUsers := make(map[string]bool)

	stateChanges, err := dwr.fetchStateChanges(dwr.lastProcessedID)
	if err != nil {
		glog.Errorf("Failed to fetch state changes: %v", err)
		return affectedUsers
	}

	if len(stateChanges) == 0 {
		glog.V(2).Info("No new state changes found")
		return affectedUsers
	}

	glog.V(2).Infof("Found %d new state changes", len(stateChanges))

	sort.Slice(stateChanges, func(i, j int) bool {
		return stateChanges[i].ID < stateChanges[j].ID
	})

	glog.V(2).Info("State changes sorted by ID, processing in order...")

	// Inherit the current cursor so a batch where every change fails
	// does not overwrite dwr.lastProcessedID (and Redis) with 0, which
	// would force the next tick to replay the full history from ID 0.
	lastProcessedID := dwr.lastProcessedID
	for _, change := range stateChanges {
		users, err := dwr.processStateChange(change)
		if err != nil {
			glog.Errorf("Error processing state change ID %d: %v", change.ID, err)
			continue
		}
		for _, u := range users {
			if u != "" {
				affectedUsers[u] = true
			}
		}
		lastProcessedID = change.ID
	}

	if lastProcessedID == dwr.lastProcessedID {
		// No change advanced the cursor; skip the Redis write to avoid
		// pointless traffic and keep the existing value as a tombstone
		// of the last successful progress.
		return affectedUsers
	}

	dwr.lastProcessedID = lastProcessedID

	ctx := context.Background()
	if err = dwr.redisClient.client.Set(ctx, "datawatcher:last_processed_id", strconv.FormatInt(lastProcessedID, 10), 0).Err(); err != nil {
		glog.Errorf("Failed to update last processed ID in Redis: %v", err)
	}

	return affectedUsers
}

// fetchStateChanges calls the /state-changes API to get new state changes
func (dwr *DataWatcherRepo) fetchStateChanges(afterID int64) ([]*chartrepo.StateChange, error) {
	c, err := chartrepo.NewClient(chartrepo.WithBaseURL(dwr.apiBaseURL))
	if err != nil {
		return nil, err
	}

	scd, err := c.GetStateChanges(context.Background(), afterID, 1000)
	if err != nil {
		return nil, err
	}

	glog.V(3).Infof("Fetched state changes payload: %s", helper.ParseJson(scd))
	glog.V(2).Infof("Successfully fetched %d state changes, afterId: %d", scd.Count, scd.AfterID)

	return scd.StateChanges, nil
}

// processStateChange dispatches a single state change to the matching
// handler. Returns the list of user_ids the handler reports as affected
// (empty for handlers that don't have user-level fan-out, or for events
// that no user currently observes).
func (dwr *DataWatcherRepo) processStateChange(change *chartrepo.StateChange) ([]string, error) {
	glog.V(3).Infof("Processing state change ID %d, type: %s", change.ID, change.Type)

	switch change.Type {
	case "app_upload_completed":
		return dwr.handleAppUploadCompleted(change)
	case "image_info_updated":
		return dwr.handleImageInfoUpdated(change)
	default:
		glog.Warningf("Unknown state change type: %s, skipping", change.Type)
		return nil, nil
	}
}

// handleAppUploadCompleted projects an app_upload_completed event onto
// PG via the dedicated uploadevent package. The two-table write
// (applications + user_applications), the canonical 8-char app_id
// derivation (md5(metadata.appid)[:8]), and the new_app_ready NATS
// push all live there — see internal/v2/appinfo/uploadevent/handler.go
// for the orchestration. Keeping that logic out of this file means
// the chart-repo state-change loop owns no event-specific decoding,
// no PG schema knowledge, and no NATS payload composition.
//
// The returned slice is the canonical "affected users" set the
// pipeline phaseDataWatcherRepo loop merges into its per-cycle
// affected-user map. Today that is exactly [event.UserID] (the upload
// is per-user from the event's perspective) — users who already
// observe the same (source, app_id) will be refreshed by the next
// phaseHydrateApps pass via store.ListRenderCandidates' version-drift
// branch, the same way they were before this handler existed.
func (dwr *DataWatcherRepo) handleAppUploadCompleted(change *chartrepo.StateChange) ([]string, error) {
	if change.AppData == nil {
		return nil, fmt.Errorf("app_upload_completed without app_data")
	}
	glog.V(2).Infof("Handling app upload completed for app: %s, source: %s, user: %s",
		change.AppData.AppName, change.AppData.Source, change.AppData.UserID)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := uploadevent.FetchAndHandle(ctx, dwr.apiBaseURL, uploadevent.EventData{
		UserID:  change.AppData.UserID,
		Source:  change.AppData.Source,
		AppName: change.AppData.AppName,
	}, uploadevent.HandleDeps{
		Sender:     dwr.dataSender,
		UserLookup: watchers.GetUserFromCache,
	})
	if err != nil {
		return nil, err
	}

	if res.AppID != "" {
		glog.V(2).Infof("Handled upload event: source=%s app_id=%s affected_users=%d",
			change.AppData.Source, res.AppID, len(res.AffectedUsers))
	}
	return res.AffectedUsers, nil
}

// handleImageInfoUpdated fetches the updated image info from chart-repo
// and patches every user_applications.image_analysis row that already
// references the image. Each affected user gets a direct
// image_state_change push so the frontend can refresh without waiting
// for the next pipeline cycle's hash recalc.
func (dwr *DataWatcherRepo) handleImageInfoUpdated(change *chartrepo.StateChange) ([]string, error) {
	if change.ImageData == nil || change.ImageData.ImageName == "" {
		return nil, fmt.Errorf("image_info_updated without image_name")
	}
	imageName := change.ImageData.ImageName
	glog.V(3).Infof("Handling image info updated for image: %s", imageName)

	rawInfo, err := dwr.fetchImageInfoFromAPI(imageName)
	if err != nil {
		return nil, fmt.Errorf("fetch image info: %w", err)
	}

	info := dwr.convertMapToImageInfo(imageName, rawInfo)
	if info == nil {
		return nil, fmt.Errorf("convert image info to ImageInfo struct for %s yielded nil", imageName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	affected, err := store.PatchImageAnalysisImage(ctx, imageName, info)
	if err != nil {
		return nil, fmt.Errorf("patch image_analysis for %s: %w", imageName, err)
	}

	glog.V(3).Infof("Patched image_analysis for image %s, affected users=%d", imageName, len(affected))

	if dwr.dataSender != nil {
		for _, userID := range affected {
			dwr.sendImageStateChangeToUser(userID, imageName, info)
		}
	}

	return affected, nil
}

// SetDataWatcher sets the DataWatcher reference for hash calculation
func (dwr *DataWatcherRepo) SetDataWatcher(dataWatcher *DataWatcher) {
	dwr.dataWatcher = dataWatcher
}

// GetDataWatcher returns the DataWatcher reference
func (dwr *DataWatcherRepo) GetDataWatcher() *DataWatcher {
	return dwr.dataWatcher
}

// SetDataSender sets the DataSender reference for NATS communication
func (dwr *DataWatcherRepo) SetDataSender(dataSender *DataSender) {
	dwr.dataSender = dataSender
}

// GetDataSender returns the DataSender reference
func (dwr *DataWatcherRepo) GetDataSender() *DataSender {
	return dwr.dataSender
}

// GetLastProcessedID returns the last processed ID
func (dwr *DataWatcherRepo) GetLastProcessedID() int64 {
	return dwr.lastProcessedID
}

// GetApiBaseURL returns the current API base URL
func (dwr *DataWatcherRepo) GetApiBaseURL() string {
	return dwr.apiBaseURL
}

// SetApiBaseURL updates the API base URL
func (dwr *DataWatcherRepo) SetApiBaseURL(url string) {
	dwr.apiBaseURL = url
	glog.V(3).Infof("API base URL updated to: %s", url)
}

// fetchImageInfoFromAPI fetches image information from the /images API endpoint with query parameter
func (dwr *DataWatcherRepo) fetchImageInfoFromAPI(imageName string) (map[string]interface{}, error) {
	c, err := chartrepo.NewClient(chartrepo.WithBaseURL(dwr.apiBaseURL))
	if err != nil {
		return nil, err
	}

	resp, err := c.GetImageInfo(context.Background(), imageName)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, fmt.Errorf("invalid API response format: missing image_info data")
	}

	return resp, nil
}

// sendImageStateChangeToUser sends image state change message to a specific user
func (dwr *DataWatcherRepo) sendImageStateChangeToUser(userID, imageName string, imageInfo *types.ImageInfo) {
	if imageInfo == nil {
		glog.V(3).Infof("sendImageStateChangeToUser: nil image info for user %s, image %s; skipping push", userID, imageName)
		return
	}

	update := types.ImageInfoUpdate{
		ImageInfo:  imageInfo,
		Timestamp:  time.Now().Unix(),
		User:       userID,
		NotifyType: "image_state_change",
	}

	if err := dwr.dataSender.SendImageInfoUpdate(update); err != nil {
		glog.Errorf("Failed to send image state change message to user %s: %v", userID, err)
	} else {
		glog.V(3).Infof("Successfully sent image state change message to user: %s for image: %s", userID, imageName)
	}
}

// convertMapToImageInfo converts map[string]interface{} (the chart-repo
// /images wire response) to *types.ImageInfo so the value can be passed
// to store.PatchImageAnalysisImage and to the NATS push payload without
// the caller having to do another marshal hop.
func (dwr *DataWatcherRepo) convertMapToImageInfo(imageName string, imageData map[string]interface{}) *types.ImageInfo {
	if imageData == nil {
		return nil
	}

	imageInfo := &types.ImageInfo{
		Name: imageName,
	}

	if tag, ok := imageData["tag"].(string); ok {
		imageInfo.Tag = tag
	}
	if architecture, ok := imageData["architecture"].(string); ok {
		imageInfo.Architecture = architecture
	}
	if totalSize, ok := imageData["total_size"].(float64); ok {
		imageInfo.TotalSize = int64(totalSize)
	}
	if downloadedSize, ok := imageData["downloaded_size"].(float64); ok {
		imageInfo.DownloadedSize = int64(downloadedSize)
	}
	if downloadProgress, ok := imageData["download_progress"].(float64); ok {
		imageInfo.DownloadProgress = downloadProgress
	}
	if layerCount, ok := imageData["layer_count"].(float64); ok {
		imageInfo.LayerCount = int(layerCount)
	}
	if downloadedLayers, ok := imageData["downloaded_layers"].(float64); ok {
		imageInfo.DownloadedLayers = int(downloadedLayers)
	}
	if status, ok := imageData["status"].(string); ok {
		imageInfo.Status = status
	}
	if errorMessage, ok := imageData["error_message"].(string); ok {
		imageInfo.ErrorMessage = errorMessage
	}

	imageInfo.CreatedAt = time.Now()
	imageInfo.AnalyzedAt = time.Now()

	if nodesData, ok := imageData["nodes"].([]interface{}); ok {
		imageInfo.Nodes = dwr.convertNodesData(nodesData)
	}

	return imageInfo
}

// convertNodesData converts nodes data from API response to NodeInfo slice
func (dwr *DataWatcherRepo) convertNodesData(nodesData []interface{}) []*types.NodeInfo {
	nodes := make([]*types.NodeInfo, 0, len(nodesData))

	for _, nodeData := range nodesData {
		if nodeMap, ok := nodeData.(map[string]interface{}); ok {
			nodeInfo := &types.NodeInfo{}

			if nodeName, ok := nodeMap["node_name"].(string); ok {
				nodeInfo.NodeName = nodeName
			}
			if architecture, ok := nodeMap["architecture"].(string); ok {
				nodeInfo.Architecture = architecture
			}
			if variant, ok := nodeMap["variant"].(string); ok {
				nodeInfo.Variant = variant
			}
			if os, ok := nodeMap["os"].(string); ok {
				nodeInfo.OS = os
			}
			if totalSize, ok := nodeMap["total_size"].(float64); ok {
				nodeInfo.TotalSize = int64(totalSize)
			}
			if layerCount, ok := nodeMap["layer_count"].(float64); ok {
				nodeInfo.LayerCount = int(layerCount)
			}

			if layersData, ok := nodeMap["layers"].([]interface{}); ok {
				nodeInfo.Layers = dwr.convertLayersData(layersData)
			}

			nodes = append(nodes, nodeInfo)
		}
	}

	return nodes
}

// convertLayersData converts layers data from API response to LayerInfo slice
func (dwr *DataWatcherRepo) convertLayersData(layersData []interface{}) []*types.LayerInfo {
	layers := make([]*types.LayerInfo, 0, len(layersData))

	for _, layerData := range layersData {
		if layerMap, ok := layerData.(map[string]interface{}); ok {
			layerInfo := &types.LayerInfo{}

			if digest, ok := layerMap["digest"].(string); ok {
				layerInfo.Digest = digest
			}
			if size, ok := layerMap["size"].(float64); ok {
				layerInfo.Size = int64(size)
			}
			if mediaType, ok := layerMap["media_type"].(string); ok {
				layerInfo.MediaType = mediaType
			}
			if offset, ok := layerMap["offset"].(float64); ok {
				layerInfo.Offset = int64(offset)
			}
			if downloaded, ok := layerMap["downloaded"].(bool); ok {
				layerInfo.Downloaded = downloaded
			}
			if progress, ok := layerMap["progress"].(float64); ok {
				layerInfo.Progress = int(progress)
			}
			if localPath, ok := layerMap["local_path"].(string); ok {
				layerInfo.LocalPath = localPath
			}

			layers = append(layers, layerInfo)
		}
	}

	return layers
}
