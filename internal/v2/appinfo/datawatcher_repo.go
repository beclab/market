package appinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"market/internal/v2/store"
	"market/internal/v2/types"

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
//   - "app_upload_completed" → store.UpsertApplicationFromChartRepo on
//     the applications table; the next pipeline cycle's hydration step
//     picks up the row (it is a fresh / version-drift candidate).
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
// store.UpsertApplicationFromChartRepo and image_info_updated through
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
	if !dwr.isRunning {
		return nil
	}
	return dwr.processStateChanges()
}

// Stop stops the periodic state checking process
func (dwr *DataWatcherRepo) Stop() error {
	if !dwr.isRunning {
		return fmt.Errorf("data watcher is not running")
	}

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

// IsRunning returns whether the data watcher is currently running
func (dwr *DataWatcherRepo) IsRunning() bool {
	return dwr.isRunning
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

	var lastProcessedID int64
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

	dwr.lastProcessedID = lastProcessedID

	ctx := context.Background()
	if err = dwr.redisClient.client.Set(ctx, "datawatcher:last_processed_id", strconv.FormatInt(lastProcessedID, 10), 0).Err(); err != nil {
		glog.Errorf("Failed to update last processed ID in Redis: %v", err)
	}

	return affectedUsers
}

// fetchStateChanges calls the /state-changes API to get new state changes
func (dwr *DataWatcherRepo) fetchStateChanges(afterID int64) ([]*StateChange, error) {
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/state-changes?after_id=%d&limit=1000", dwr.apiBaseURL, afterID)

	glog.V(2).Infof("Fetching state changes from: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var apiResponse StateChangesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	if !apiResponse.Success {
		return nil, fmt.Errorf("API request failed: %s", apiResponse.Message)
	}

	if apiResponse.Data == nil {
		return nil, fmt.Errorf("API response data is nil")
	}

	glog.V(2).Infof("Successfully fetched %d state changes", apiResponse.Data.Count)
	return apiResponse.Data.StateChanges, nil
}

// processStateChange dispatches a single state change to the matching
// handler. Returns the list of user_ids the handler reports as affected
// (empty for handlers that don't have user-level fan-out, or for events
// that no user currently observes).
func (dwr *DataWatcherRepo) processStateChange(change *StateChange) ([]string, error) {
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

// handleAppUploadCompleted fetches the freshly uploaded app's manifest
// from chart-repo and upserts the applications row via the store layer.
// User-level re-hydration is intentionally NOT triggered here: the
// pipeline's next phaseHydrateApps cycle will pick the row up as a
// version-drift / fresh candidate via store.ListRenderCandidates, which
// is the canonical entry point.
//
// The returned slice contains every user_id who already has a
// user_applications row on (source_id, app_id) — those users observe
// the app today and should be flagged for the pipeline's affected-user
// fan-out (hash recalc / push). Users who have not yet rendered the
// app (no row) will discover it on the next hydration cycle anyway.
func (dwr *DataWatcherRepo) handleAppUploadCompleted(change *StateChange) ([]string, error) {
	if change.AppData == nil {
		return nil, fmt.Errorf("app_upload_completed without app_data")
	}
	glog.V(2).Infof("Handling app upload completed for app: %s, source: %s, user: %s",
		change.AppData.AppName, change.AppData.Source, change.AppData.UserID)

	appInfo, err := dwr.fetchAppInfoFromAPI(change.AppData.UserID, change.AppData.Source, change.AppData.AppName)
	if err != nil {
		return nil, fmt.Errorf("fetch app info: %w", err)
	}

	appID := pickAppIDFromPayload(appInfo, change.AppData.AppName)
	if appID == "" {
		return nil, fmt.Errorf("app payload missing usable id (source=%s appName=%s)", change.AppData.Source, change.AppData.AppName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	affected, err := store.UpsertApplicationFromChartRepo(ctx, change.AppData.Source, appID, appInfo)
	if err != nil {
		return nil, fmt.Errorf("upsert application (source=%s app=%s): %w", change.AppData.Source, appID, err)
	}

	glog.V(2).Infof("Upserted application from chart-repo: source=%s app=%s, affected users=%d",
		change.AppData.Source, appID, len(affected))
	return affected, nil
}

// handleImageInfoUpdated fetches the updated image info from chart-repo
// and patches every user_applications.image_analysis row that already
// references the image. Each affected user gets a direct
// image_state_change push so the frontend can refresh without waiting
// for the next pipeline cycle's hash recalc.
func (dwr *DataWatcherRepo) handleImageInfoUpdated(change *StateChange) ([]string, error) {
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

// pickAppIDFromPayload mirrors the legacy "ID > AppID > Name" fallback
// chain used by checkAppInCache: the chart-repo payload can carry the
// id under any of three field names depending on origin. Returns an
// empty string only when none of them are present.
func pickAppIDFromPayload(appPayload map[string]interface{}, fallbackName string) string {
	if appPayload == nil {
		return fallbackName
	}
	if v, ok := appPayload["id"].(string); ok && v != "" {
		return v
	}
	if v, ok := appPayload["appID"].(string); ok && v != "" {
		return v
	}
	if v, ok := appPayload["app_id"].(string); ok && v != "" {
		return v
	}
	if v, ok := appPayload["name"].(string); ok && v != "" {
		return v
	}
	return fallbackName
}

// fetchAppInfoFromAPI fetches app information from the /apps API endpoint
func (dwr *DataWatcherRepo) fetchAppInfoFromAPI(userID, sourceID, appName string) (map[string]interface{}, error) {
	requestPayload := map[string]interface{}{
		"apps": []map[string]string{
			{
				"appid":          appName,
				"sourceDataName": sourceID,
			},
		},
		"userid": userID,
	}

	jsonData, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	url := fmt.Sprintf("http://%s/chart-repo/api/v2/apps", dwr.apiBaseURL)
	glog.V(3).Infof("Fetching app info from API: %s for app: %s, user: %s, source: %s", url, appName, userID, sourceID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d for app: %s", resp.StatusCode, appName)
	}

	var apiResponse struct {
		Success bool                   `json:"success"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response for app %s: %w", appName, err)
	}

	if !apiResponse.Success {
		return nil, fmt.Errorf("API request failed for app %s: %s", appName, apiResponse.Message)
	}

	if appsData, ok := apiResponse.Data["apps"]; ok {
		if apps, ok := appsData.([]interface{}); ok && len(apps) > 0 {
			if appInfo, ok := apps[0].(map[string]interface{}); ok {
				glog.V(3).Infof("Successfully fetched app info from API for app: %s", appName)
				return appInfo, nil
			}
		}
	}

	return nil, fmt.Errorf("no app data found in API response for app: %s", appName)
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
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/images?imageName=%s", dwr.apiBaseURL, imageName)
	glog.V(3).Infof("Fetching image info from API: %s for image: %s", url, imageName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var apiResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	if success, ok := apiResponse["success"].(bool); !ok || !success {
		message := "unknown error"
		if msg, ok := apiResponse["message"].(string); ok {
			message = msg
		}
		return nil, fmt.Errorf("API request failed: %s", message)
	}

	if data, ok := apiResponse["data"].(map[string]interface{}); ok {
		if imageInfo, ok := data["image_info"].(map[string]interface{}); ok {
			return imageInfo, nil
		}
	}

	return nil, fmt.Errorf("invalid API response format: missing image_info data")
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
