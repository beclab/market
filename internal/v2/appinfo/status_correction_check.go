package appinfo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"market/internal/v2/history"
	"market/internal/v2/task"
	"market/internal/v2/types"
	"market/internal/v2/utils"

	"github.com/golang/glog"
)

// StatusCorrectionChecker manages periodic status checking and correction
type StatusCorrectionChecker struct {
	cacheManager    *CacheManager
	checkInterval   time.Duration
	appServiceHost  string
	appServicePort  string
	stopChan        chan struct{}
	isRunning       bool
	mutex           sync.RWMutex
	lastCheckTime   time.Time
	checkCount      int64
	correctionCount int64

	historyModule *history.HistoryModule
	taskModule    *task.TaskModule
}

// StatusChange represents a detected status change
type StatusChange struct {
	UserID          string                 `json:"user_id"`
	SourceID        string                 `json:"source_id"`
	AppName         string                 `json:"app_name"`
	ChangeType      string                 `json:"change_type"` // "state_change", "app_disappeared", "app_appeared", "state_inconsistency"
	OldState        string                 `json:"old_state"`
	NewState        string                 `json:"new_state"`
	EntranceChanges []EntranceStatusChange `json:"entrance_changes,omitempty"`
	Timestamp       time.Time              `json:"timestamp"`
}

// EntranceStatusChange represents a change in entrance status
type EntranceStatusChange struct {
	EntranceName string `json:"entrance_name"`
	OldState     string `json:"old_state"`
	NewState     string `json:"new_state"`
}

// NewStatusCorrectionChecker creates a new status correction checker
func NewStatusCorrectionChecker(cacheManager *CacheManager) *StatusCorrectionChecker {
	// Get app service configuration from environment variables
	host := os.Getenv("APP_SERVICE_SERVICE_HOST")
	if host == "" {
		host = "localhost" // Default fallback
	}

	port := os.Getenv("APP_SERVICE_SERVICE_PORT")
	if port == "" {
		port = "80" // Default fallback
	}

	return &StatusCorrectionChecker{
		cacheManager:   cacheManager,
		checkInterval:  2 * time.Minute, // Check every 5 minutes
		appServiceHost: host,
		appServicePort: port,
		stopChan:       make(chan struct{}),
		isRunning:      false,
	}
}

// Start begins the periodic status checking
func (scc *StatusCorrectionChecker) Start() error {
	scc.mutex.Lock()
	defer scc.mutex.Unlock()

	if scc.isRunning {
		return fmt.Errorf("status correction checker is already running")
	}

	if scc.cacheManager == nil {
		return fmt.Errorf("cache manager is required")
	}

	scc.isRunning = true
	scc.lastCheckTime = time.Time{} // Zero time indicates no checks yet
	scc.checkCount = 0
	scc.correctionCount = 0
	scc.stopChan = make(chan struct{}) // Recreate stopChan for each start

	glog.Infof("Starting status correction checker with interval: %v", scc.checkInterval)
	glog.Infof("App service endpoint: http://%s:%s/app-service/v1/all/apps", scc.appServiceHost, scc.appServicePort)
	glog.Infof("Middleware service endpoint: http://%s:%s/app-service/v1/middlewares/status", scc.appServiceHost, scc.appServicePort)

	// Start the periodic checking goroutine
	go scc.runPeriodicCheck()

	return nil
}

// Stop stops the periodic status checking
func (scc *StatusCorrectionChecker) Stop() {
	scc.mutex.Lock()
	defer scc.mutex.Unlock()

	if !scc.isRunning {
		return
	}

	scc.isRunning = false
	select {
	case <-scc.stopChan:
		// Channel already closed, do nothing
	default:
		close(scc.stopChan)
	}
	glog.Infof("Status correction checker stopped")
}

// IsRunning returns whether the checker is currently running
func (scc *StatusCorrectionChecker) IsRunning() bool {
	scc.mutex.RLock()
	defer scc.mutex.RUnlock()
	return scc.isRunning
}

// GetStats returns statistics about the checker
func (scc *StatusCorrectionChecker) GetStats() map[string]interface{} {
	scc.mutex.RLock()
	defer scc.mutex.RUnlock()

	return map[string]interface{}{
		"is_running":             scc.isRunning,
		"check_interval":         scc.checkInterval,
		"last_check_time":        scc.lastCheckTime,
		"check_count":            scc.checkCount,
		"correction_count":       scc.correctionCount,
		"app_service_url":        fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", scc.appServiceHost, scc.appServicePort),
		"middleware_service_url": fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/status", scc.appServiceHost, scc.appServicePort),
	}
}

// runPeriodicCheck runs the periodic status checking loop
func (scc *StatusCorrectionChecker) runPeriodicCheck() {
	ticker := time.NewTicker(scc.checkInterval)
	defer ticker.Stop()

	glog.Infof("Status correction checker periodic loop started")

	// Perform initial check immediately
	scc.performStatusCheck()

	for {
		select {
		case <-ticker.C:
			scc.performStatusCheck()
		case <-scc.stopChan:
			glog.Infof("Status correction checker periodic loop stopped")
			return
		}
	}
}

// performStatusCheck performs a single status check cycle
func (scc *StatusCorrectionChecker) performStatusCheck() {
	startTime := time.Now()

	scc.mutex.Lock()
	scc.lastCheckTime = startTime
	scc.checkCount++
	scc.mutex.Unlock()

	glog.Infof("Starting status check cycle #%d", scc.checkCount)

	// Fetch latest status from app-service
	latestStatus, err := scc.fetchLatestStatus()
	if err != nil {
		glog.Errorf("Failed to fetch latest status from app-service: %v", err)
		return
	}

	glog.Infof("Fetched status for %d applications and middlewares from app-service", len(latestStatus))

	// Get current status from cache
	cachedStatus := scc.getCachedStatus()
	if len(cachedStatus) == 0 {
		glog.Infof("No cached status found, skipping comparison")
		return
	}

	glog.Infof("Found cached status for %d applications and middlewares", len(cachedStatus))

	// Compare and detect changes
	changes := scc.compareStatus(latestStatus, cachedStatus)

	if len(changes) > 0 {
		glog.Infof("Detected %d status changes, applying corrections", len(changes))
		scc.applyCorrections(changes, latestStatus)

		// After applying corrections, recalculate and update user data hash for all affected users.
		// This ensures the hash stays consistent with the latest user data state.
		// The hash calculation logic is consistent with DataWatcher (see datawatcher_app.go).
		affectedUsers := make(map[string]struct{})
		for _, change := range changes {
			affectedUsers[change.UserID] = struct{}{}
		}
		for userID := range affectedUsers {
			userData := scc.cacheManager.GetUserData(userID)
			if userData == nil {
				glog.Warningf("StatusCorrectionChecker: userData not found for user %s, skip hash calculation", userID)
				continue
			}
			// Generate snapshot for hash calculation (reuse logic from DataWatcher)
			snapshot, err := utils.CreateUserDataSnapshot(userID, userData)
			if err != nil {
				glog.Errorf("StatusCorrectionChecker: failed to create snapshot for user %s: %v", userID, err)
				continue
			}
			newHash, err := utils.CalculateUserDataHash(snapshot)
			if err != nil {
				glog.Errorf("StatusCorrectionChecker: failed to calculate hash for user %s: %v", userID, err)
				continue
			}
			// Write back hash with lock
			glog.Infof("[LOCK] scc.cacheManager.mutex.TryLock() @status_correction:updateHash Start")
			if !scc.cacheManager.mutex.TryLock() {
				glog.Warningf("StatusCorrectionChecker: CacheManager write lock not available for hash update, skipping")
				continue
			}
			userData.Hash = newHash
			scc.cacheManager.mutex.Unlock()
			glog.Infof("StatusCorrectionChecker: user %s hash updated to %s", userID, newHash)
		}
		// Force sync after hash update
		if err := scc.cacheManager.ForceSync(); err != nil {
			glog.Errorf("StatusCorrectionChecker: ForceSync failed after hash update: %v", err)
		}

		scc.mutex.Lock()
		scc.correctionCount += int64(len(changes))
		scc.mutex.Unlock()
	} else {
		glog.Infof("No status changes detected")
	}

	glog.Infof("Status check cycle #%d completed in %v", scc.checkCount, time.Since(startTime))
}

// fetchLatestStatus fetches the latest status from app-service
func (scc *StatusCorrectionChecker) fetchLatestStatus() ([]utils.AppServiceResponse, error) {
	// Fetch apps status
	appsStatus, err := scc.fetchLatestAppsStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch apps status: %v", err)
	}

	// Fetch middlewares status
	middlewaresStatus, err := scc.fetchLatestMiddlewaresStatus()
	if err != nil {
		// Log warning but don't fail the entire operation
		glog.Warningf("Failed to fetch middlewares status: %v", err)
		// Return only apps status if middlewares fetch fails
		return appsStatus, nil
	}

	// Combine apps and middlewares status
	// Convert middlewares to AppServiceResponse format and merge with apps
	allStatus := make([]utils.AppServiceResponse, 0, len(appsStatus)+len(middlewaresStatus))

	// Add apps status
	allStatus = append(allStatus, appsStatus...)

	// Add middlewares status (already converted to AppServiceResponse format)
	allStatus = append(allStatus, middlewaresStatus...)

	glog.Infof("Combined status: %d apps + %d middlewares = %d total",
		len(appsStatus), len(middlewaresStatus), len(allStatus))

	return allStatus, nil
}

// fetchLatestAppsStatus fetches the latest apps status from app-service
func (scc *StatusCorrectionChecker) fetchLatestAppsStatus() ([]utils.AppServiceResponse, error) {
	url := fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", scc.appServiceHost, scc.appServicePort)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from app-service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("app-service returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var apps []utils.AppServiceResponse
	if err := json.Unmarshal(data, &apps); err != nil {
		return nil, fmt.Errorf("failed to parse app-service response: %v", err)
	}

	return apps, nil
}

// MiddlewareStatusResponse represents the response structure from middleware status endpoint
type MiddlewareStatusResponse struct {
	Code int `json:"code"`
	Data []struct {
		UUID           string `json:"uuid"`
		Namespace      string `json:"namespace"`
		User           string `json:"user"`
		ResourceStatus string `json:"resourceStatus"`
		ResourceType   string `json:"resourceType"`
		CreateTime     string `json:"createTime"`
		UpdateTime     string `json:"updateTime"`
		Metadata       struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Version string `json:"version"`
		Title   string `json:"title"`
	} `json:"data"`
}

// fetchLatestMiddlewaresStatus fetches the latest middlewares status from app-service
func (scc *StatusCorrectionChecker) fetchLatestMiddlewaresStatus() ([]utils.AppServiceResponse, error) {
	url := fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/status", scc.appServiceHost, scc.appServicePort)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch middlewares from app-service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("app-service middleware endpoint returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read middleware response body: %v", err)
	}

	var middlewareResp MiddlewareStatusResponse
	if err := json.Unmarshal(data, &middlewareResp); err != nil {
		return nil, fmt.Errorf("failed to parse middleware response: %v", err)
	}

	// Convert middleware response to AppServiceResponse format for compatibility
	// This allows us to reuse existing comparison logic
	var convertedResponses []utils.AppServiceResponse

	for _, middleware := range middlewareResp.Data {
		// Convert middleware to AppServiceResponse format
		converted := utils.AppServiceResponse{
			Metadata: struct {
				Name      string `json:"name"`
				UID       string `json:"uid"`
				Namespace string `json:"namespace"`
			}{
				Name:      middleware.Metadata.Name,
				UID:       middleware.UUID,
				Namespace: middleware.Namespace,
			},
			Spec: struct {
				Name      string `json:"name"`
				AppID     string `json:"appid"`
				Owner     string `json:"owner"`
				Icon      string `json:"icon"`
				Title     string `json:"title"`
				Source    string `json:"source"`
				Entrances []struct {
					Name      string `json:"name"`
					Url       string `json:"url"`
					Invisible bool   `json:"invisible"`
				} `json:"entrances"`
			}{
				Name:   middleware.Metadata.Name,
				AppID:  middleware.Metadata.Name,
				Owner:  middleware.User,
				Icon:   "",
				Title:  middleware.Title,
				Source: "middleware",
				Entrances: []struct {
					Name      string `json:"name"`
					Url       string `json:"url"`
					Invisible bool   `json:"invisible"`
				}{},
			},
			Status: struct {
				State              string `json:"state"`
				UpdateTime         string `json:"updateTime"`
				StatusTime         string `json:"statusTime"`
				LastTransitionTime string `json:"lastTransitionTime"`
				EntranceStatuses   []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					State      string `json:"state"`
					StatusTime string `json:"statusTime"`
					Reason     string `json:"reason"`
					Url        string `json:"url"`
				} `json:"entranceStatuses"`
			}{
				State:              middleware.ResourceStatus,
				UpdateTime:         middleware.UpdateTime,
				StatusTime:         middleware.UpdateTime, // Use UpdateTime as StatusTime for middlewares
				LastTransitionTime: middleware.UpdateTime, // Use UpdateTime as LastTransitionTime for middlewares
				EntranceStatuses: []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					State      string `json:"state"`
					StatusTime string `json:"statusTime"`
					Reason     string `json:"reason"`
					Url        string `json:"url"`
				}{},
			},
		}

		convertedResponses = append(convertedResponses, converted)
	}

	return convertedResponses, nil
}

// getCachedStatus retrieves current status from cache
func (scc *StatusCorrectionChecker) getCachedStatus() map[string]*types.AppStateLatestData {
	// Get all users data from cache
	allUsersData := scc.cacheManager.GetAllUsersData()

	cachedStatus := make(map[string]*types.AppStateLatestData)

	for userID, userData := range allUsersData {
		for sourceID, sourceData := range userData.Sources {
			for _, appState := range sourceData.AppStateLatest {
				if appState != nil && appState.Status.Name != "" {
					// Create a unique key: user:source:app
					key := fmt.Sprintf("%s:%s:%s", userID, sourceID, appState.Status.Name)
					cachedStatus[key] = appState
				}
			}
		}
	}

	return cachedStatus
}

// compareStatus compares latest status with cached status and returns changes
func (scc *StatusCorrectionChecker) compareStatus(latestStatus []utils.AppServiceResponse, cachedStatus map[string]*types.AppStateLatestData) []StatusChange {
	var changes []StatusChange

	// Create maps for easier lookup
	latestApps := make(map[string]*utils.AppServiceResponse)
	for i := range latestStatus {
		app := &latestStatus[i]
		key := fmt.Sprintf("%s:%s", app.Spec.Owner, app.Spec.Name)
		latestApps[key] = app
	}

	cachedApps := make(map[string]*types.AppStateLatestData)
	var cachedAppKeys []string
	for key, cached := range cachedStatus {
		cachedApps[key] = cached
		cachedAppKeys = append(cachedAppKeys, key)
	}

	// 1. Check for new apps (appeared in latest but not in cache)
	// Build a set of all user+appName combinations in cache
	cacheAppKeys := make(map[string]struct{})
	for key := range cachedApps {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) == 3 {
			cacheAppKeys[parts[0]+":"+parts[2]] = struct{}{}
		}
	}

	for _, app := range latestStatus {
		userID := app.Spec.Owner
		appName := app.Spec.Name
		key := userID + ":" + appName

		if _, exists := cacheAppKeys[key]; !exists {
			// New app appeared, SourceID will be determined during correction
			change := StatusChange{
				UserID:     userID,
				SourceID:   "",
				AppName:    appName,
				ChangeType: "app_appeared",
				OldState:   "unknown",
				NewState:   app.Status.State,
				Timestamp:  time.Now(),
			}
			changes = append(changes, change)
			glog.Infof("New app appeared: %s (user: %s, state: %s)", appName, userID, app.Status.State)
		}
	}

	// 2. Check for disappeared apps (in cache but not in latest)
	for key, cached := range cachedApps {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		userID := parts[0]
		sourceID := parts[1]
		appName := cached.Status.Name

		// Check if this app still exists in latest status
		foundInLatest := false
		for _, app := range latestStatus {
			if app.Spec.Owner == userID && app.Spec.Name == appName {
				foundInLatest = true
				break
			}
		}

		if !foundInLatest {
			// App disappeared
			change := StatusChange{
				UserID:     userID,
				SourceID:   sourceID,
				AppName:    appName,
				ChangeType: "app_disappeared",
				OldState:   cached.Status.State,
				NewState:   "unknown",
				Timestamp:  time.Now(),
			}
			changes = append(changes, change)

			glog.Infof("App disappeared: %s (user: %s, source: %s, last state: %s)",
				appName, userID, sourceID, cached.Status.State)
		}
	}

	// 3. Check for state changes in existing apps
	for _, app := range latestStatus {
		userID := app.Spec.Owner
		appName := app.Spec.Name

		// Find matching cached app by searching through all sources
		var cachedApp *types.AppStateLatestData
		var sourceID string

		for key, cached := range cachedApps {
			if cached.Status.Name == appName {
				// Extract user and source from key
				parts := strings.SplitN(key, ":", 3)
				if len(parts) == 3 && parts[0] == userID {
					cachedApp = cached
					sourceID = parts[1]
					break
				}
			}
		}

		if cachedApp == nil {
			// This case is already handled in step 1 (new app)
			continue
		}

		// Compare main state
		stateChanged := cachedApp.Status.State != app.Status.State

		// Compare entrance statuses
		entranceChanges := scc.compareEntranceStatuses(cachedApp.Status.EntranceStatuses, app.Status.EntranceStatuses)

		if stateChanged || len(entranceChanges) > 0 {
			change := StatusChange{
				UserID:          userID,
				SourceID:        sourceID,
				AppName:         appName,
				ChangeType:      "state_change",
				OldState:        cachedApp.Status.State,
				NewState:        app.Status.State,
				EntranceChanges: entranceChanges,
				Timestamp:       time.Now(),
			}
			changes = append(changes, change)

			glog.Infof("Status change detected for app %s (user: %s, source: %s): %s -> %s",
				appName, userID, sourceID, cachedApp.Status.State, app.Status.State)

			if len(entranceChanges) > 0 {
				for _, entranceChange := range entranceChanges {
					glog.Infof("  Entrance %s: %s -> %s",
						entranceChange.EntranceName, entranceChange.OldState, entranceChange.NewState)
				}
			}
		}
	}

	// 4. Check for state inconsistency (all entrances are running but app state is not running)
	for _, app := range latestStatus {
		userID := app.Spec.Owner
		appName := app.Spec.Name

		// Find matching cached app by searching through all sources
		var cachedApp *types.AppStateLatestData
		// var sourceID string

		for key, cached := range cachedApps {
			if cached.Status.Name == appName {
				// Extract user and source from key
				parts := strings.SplitN(key, ":", 3)
				if len(parts) == 3 && parts[0] == userID {
					cachedApp = cached
					// sourceID = parts[1]
					break
				}
			}
		}

		if cachedApp == nil {
			// This case is already handled in step 1 (new app)
			continue
		}

		// // Check for state inconsistency
		// if scc.isStateInconsistent(app) {
		// 	change := StatusChange{
		// 		UserID:     userID,
		// 		SourceID:   sourceID,
		// 		AppName:    appName,
		// 		ChangeType: "state_inconsistency",
		// 		OldState:   cachedApp.Status.State,
		// 		NewState:   "running", // Should be corrected to running
		// 		Timestamp:  time.Now(),
		// 	}
		// 	changes = append(changes, change)

		// 	glog.Infof("State inconsistency detected for app %s (user: %s, source: %s): app state is %s but all entrances are running",
		// 		appName, userID, sourceID, app.Status.State)
		// }
	}

	return changes
}

// compareEntranceStatuses compares entrance statuses and returns changes
func (scc *StatusCorrectionChecker) compareEntranceStatuses(cachedEntrances []struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	StatusTime string `json:"statusTime"`
	Reason     string `json:"reason"`
	Url        string `json:"url"`
	Invisible  bool   `json:"invisible"`
}, latestEntrances []struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	StatusTime string `json:"statusTime"`
	Reason     string `json:"reason"`
	Url        string `json:"url"`
}) []EntranceStatusChange {

	var changes []EntranceStatusChange

	// Create maps for easier lookup
	cachedMap := make(map[string]string)
	for _, entrance := range cachedEntrances {
		cachedMap[entrance.Name] = entrance.State
	}

	latestMap := make(map[string]string)
	for _, entrance := range latestEntrances {
		latestMap[entrance.Name] = entrance.State
	}

	// Check for changes in existing entrances
	for name, latestState := range latestMap {
		if cachedState, exists := cachedMap[name]; exists && cachedState != latestState {
			changes = append(changes, EntranceStatusChange{
				EntranceName: name,
				OldState:     cachedState,
				NewState:     latestState,
			})
		}
	}

	return changes
}

// applyCorrections applies the detected status changes to the cache
func (scc *StatusCorrectionChecker) applyCorrections(changes []StatusChange, latestStatus []utils.AppServiceResponse) {
	for _, change := range changes {
		switch change.ChangeType {
		case "app_disappeared":
			glog.Infof("App disappeared detected: %s (user: %s, source: %s, last state: %s) - fixing in cache",
				change.AppName, change.UserID, change.SourceID, change.OldState)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App disappeared: %s (user: %s, source: %s, last state: %s)", change.AppName, change.UserID, change.SourceID, change.OldState),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Warningf("Failed to store app disappeared history record: %v", err)
				}
			}
			if err := scc.cacheManager.RemoveAppStateData(change.UserID, change.SourceID, change.AppName); err != nil {
				glog.Errorf("Failed to remove disappeared app %s from cache (user: %s, source: %s): %v",
					change.AppName, change.UserID, change.SourceID, err)
			} else {
				glog.Infof("Successfully removed disappeared app %s from cache (user: %s, source: %s)",
					change.AppName, change.UserID, change.SourceID)
			}
			if scc.taskModule != nil {
				if err := scc.taskModule.UninstallTaskSucceed("", change.AppName, change.UserID); err != nil {
					glog.Warningf("Failed to mark uninstall task as succeeded for app %s (user: %s): %v", change.AppName, change.UserID, err)
				}
			}

		case "app_appeared":
			var appToUpdate *utils.AppServiceResponse
			for _, app := range latestStatus {
				if app.Spec.Owner == change.UserID && app.Spec.Name == change.AppName {
					appToUpdate = &app
					break
				}
			}
			if appToUpdate == nil {
				glog.Warningf("Could not find appeared app %s for user %s in latest status", change.AppName, change.UserID)
				continue
			}
			glog.Infof("New app appeared detected: %s (user: %s, state: %s) - fixing in cache",
				change.AppName, change.UserID, appToUpdate.Status.State)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App appeared: %s (user: %s, state: %s)", change.AppName, change.UserID, appToUpdate.Status.State),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Warningf("Failed to store app appeared history record: %v", err)
				}
			}
			// Dynamically determine sourceID
			sourceID := ""
			// 1. Check cache
			if scc.cacheManager != nil {
				userData := scc.cacheManager.GetUserData(change.UserID)
				if userData != nil {
					for srcID, srcData := range userData.Sources {
						if srcData == nil || srcData.AppStateLatest == nil {
							continue
						}
						for _, appState := range srcData.AppStateLatest {
							if appState != nil && appState.Status.Name == change.AppName {
								sourceID = srcID
								break
							}
						}
						if sourceID != "" {
							break
						}
					}
				}
			}

			if scc.taskModule != nil {
				if err := scc.taskModule.InstallTaskSucceed("", change.AppName, change.UserID); err != nil {
					glog.Warningf("Failed to mark install task as succeeded for app %s (user: %s): %v", change.AppName, change.UserID, err)
				}
			}

			appStateData, sourceID := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID)
			if appStateData == nil {
				glog.Warningf("Failed to create app state data for appeared app %s (user: %s)", change.AppName, change.UserID)
				continue
			}
			stateData := scc.createStateDataFromAppStateData(appStateData)
			if err := scc.cacheManager.SetAppData(change.UserID, sourceID, AppStateLatest, stateData); err != nil {
				glog.Errorf("Failed to add appeared app %s to cache (user: %s, source: %s): %v",
					change.AppName, change.UserID, sourceID, err)
			} else {
				glog.Infof("Successfully added appeared app %s to cache (user: %s, source: %s)",
					change.AppName, change.UserID, sourceID)
			}

		case "state_change":
			var appToUpdate *utils.AppServiceResponse
			for _, app := range latestStatus {
				if app.Spec.Owner == change.UserID && app.Spec.Name == change.AppName {
					appToUpdate = &app
					break
				}
			}
			if appToUpdate == nil {
				glog.Warningf("Could not find app %s for user %s in latest status", change.AppName, change.UserID)
				continue
			}
			glog.Infof("State change detected for app %s (user: %s, source: %s): %s -> %s - fixing in cache",
				change.AppName, change.UserID, change.SourceID, change.OldState, change.NewState)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App state changed: %s (user: %s, source: %s): %s -> %s", change.AppName, change.UserID, change.SourceID, change.OldState, change.NewState),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Warningf("Failed to store state change history record: %v", err)
				}
			}

			appStateData, sourceID := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID)
			if appStateData == nil {
				glog.Warningf("Failed to create app state data for app %s (user: %s)", change.AppName, change.UserID)
				continue
			}
			stateData := scc.createStateDataFromAppStateData(appStateData)
			if err := scc.cacheManager.SetAppData(change.UserID, sourceID, AppStateLatest, stateData); err != nil {
				glog.Errorf("Failed to update cache with corrected status for app %s (user: %s, source: %s): %v",
					change.AppName, change.UserID, sourceID, err)
			} else {
				glog.Infof("Successfully updated cache with corrected status for app %s (user: %s, source: %s)",
					change.AppName, change.UserID, sourceID)
			}

		case "state_inconsistency":
			var appToUpdate *utils.AppServiceResponse
			for _, app := range latestStatus {
				if app.Spec.Owner == change.UserID && app.Spec.Name == change.AppName {
					appToUpdate = &app
					break
				}
			}
			if appToUpdate == nil {
				glog.Warningf("Could not find app %s for user %s in latest status", change.AppName, change.UserID)
				continue
			}
			glog.Infof("State inconsistency detected for app %s (user: %s, source: %s): app state is %s but all entrances are running - fixing in cache",
				change.AppName, change.UserID, change.SourceID, appToUpdate.Status.State)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App state inconsistency: %s (user: %s, source: %s), app state: %s, all entrances are running", change.AppName, change.UserID, change.SourceID, appToUpdate.Status.State),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Warningf("Failed to store state inconsistency history record: %v", err)
				}
			}

			appStateData, sourceID := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID)
			if appStateData == nil {
				glog.Warningf("Failed to create app state data for app %s (user: %s)", change.AppName, change.UserID)
				continue
			}
			appStateData.Status.State = "running"
			stateData := scc.createStateDataFromAppStateData(appStateData)
			if err := scc.cacheManager.SetAppData(change.UserID, sourceID, AppStateLatest, stateData); err != nil {
				glog.Errorf("Failed to update cache with corrected state for inconsistent app %s (user: %s, source: %s): %v",
					change.AppName, change.UserID, sourceID, err)
			} else {
				glog.Infof("Successfully corrected inconsistent state for app %s (user: %s, source: %s): %s -> running",
					change.AppName, change.UserID, sourceID, change.OldState)
			}

		default:
			glog.Warningf("Unknown change type: %s for app %s (user: %s)", change.ChangeType, change.AppName, change.UserID)
		}
	}
}

// createAppStateDataFromResponse creates AppStateLatestData from AppServiceResponse
func (scc *StatusCorrectionChecker) createAppStateDataFromResponse(app utils.AppServiceResponse, userID string) (*types.AppStateLatestData, string) {
	// Create entrance statuses
	entranceStatuses := make([]struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
		Url        string `json:"url"`
		Invisible  bool   `json:"invisible"`
	}, len(app.Status.EntranceStatuses))

	for i, entrance := range app.Status.EntranceStatuses {
		entranceStatuses[i] = struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			State      string `json:"state"`
			StatusTime string `json:"statusTime"`
			Reason     string `json:"reason"`
			Url        string `json:"url"`
			Invisible  bool   `json:"invisible"`
		}{
			ID:         entrance.ID,
			Name:       entrance.Name,
			State:      entrance.State,
			StatusTime: entrance.StatusTime,
			Reason:     entrance.Reason,
			Url:        entrance.Url,
			Invisible:  false, // Default value, will be updated from spec.entrances if available
		}

		// Try to get invisible flag from spec.entrances
		for _, specEntrance := range app.Spec.Entrances {
			if specEntrance.Name == entrance.Name {
				entranceStatuses[i].Invisible = specEntrance.Invisible
				break
			}
		}
	}

	// Get version from download record
	version := ""
	source := ""
	if userID != "" && app.Spec.Name != "" {
		if versionFromRecord, sourceFromRecord, err := utils.GetAppInfoLastInstalled(userID, app.Spec.Name); err == nil && versionFromRecord != "" {
			version = versionFromRecord
			source = sourceFromRecord
		}
	}

	return &types.AppStateLatestData{
		Type:    types.AppStateLatest,
		Version: version,
		Status: struct {
			Name               string `json:"name"`
			State              string `json:"state"`
			UpdateTime         string `json:"updateTime"`
			StatusTime         string `json:"statusTime"`
			LastTransitionTime string `json:"lastTransitionTime"`
			Progress           string `json:"progress"`
			EntranceStatuses   []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				State      string `json:"state"`
				StatusTime string `json:"statusTime"`
				Reason     string `json:"reason"`
				Url        string `json:"url"`
				Invisible  bool   `json:"invisible"`
			} `json:"entranceStatuses"`
		}{
			Name:               app.Spec.Name,
			State:              app.Status.State,
			UpdateTime:         app.Status.UpdateTime,
			StatusTime:         app.Status.StatusTime,
			LastTransitionTime: app.Status.LastTransitionTime,
			Progress:           "",
			EntranceStatuses:   entranceStatuses,
		},
	}, source
}

// createStateDataFromAppStateData creates a state data from AppStateLatestData
func (scc *StatusCorrectionChecker) createStateDataFromAppStateData(appStateData *types.AppStateLatestData) map[string]interface{} {
	stateData := map[string]interface{}{
		"name":               appStateData.Status.Name,
		"state":              appStateData.Status.State,
		"updateTime":         appStateData.Status.UpdateTime,
		"statusTime":         appStateData.Status.StatusTime,
		"lastTransitionTime": appStateData.Status.LastTransitionTime,
		"progress":           appStateData.Status.Progress,
	}

	// Convert entrance statuses to interface{} slice
	entranceStatuses := make([]interface{}, len(appStateData.Status.EntranceStatuses))
	for i, entrance := range appStateData.Status.EntranceStatuses {
		entranceStatuses[i] = map[string]interface{}{
			"id":         entrance.ID,
			"name":       entrance.Name,
			"state":      entrance.State,
			"statusTime": entrance.StatusTime,
			"reason":     entrance.Reason,
			"url":        entrance.Url,
			"invisible":  entrance.Invisible,
		}
	}
	stateData["entranceStatuses"] = entranceStatuses

	return stateData
}

// ForceCheck performs an immediate status check
func (scc *StatusCorrectionChecker) ForceCheck() error {
	if !scc.IsRunning() {
		return fmt.Errorf("status correction checker is not running")
	}

	glog.Infof("Forcing immediate status check")
	scc.performStatusCheck()
	return nil
}

// SetCheckInterval sets the check interval
func (scc *StatusCorrectionChecker) SetCheckInterval(interval time.Duration) {
	scc.mutex.Lock()
	defer scc.mutex.Unlock()
	scc.checkInterval = interval
	glog.Infof("Status correction check interval updated to: %v", interval)
}

// isStateInconsistent checks if the app state is inconsistent with the entrance statuses
// Returns true if all entrances are running but the app state is not running
func (scc *StatusCorrectionChecker) isStateInconsistent(app utils.AppServiceResponse) bool {
	// If app state is already running, no inconsistency
	if app.Status.State == "running" {
		return false
	}

	// If no entrances, no inconsistency
	if len(app.Status.EntranceStatuses) == 0 {
		return false
	}

	// Check if all entrances are running
	for _, entrance := range app.Status.EntranceStatuses {
		if entrance.State != "running" {
			return false
		}
	}

	// All entrances are running but app state is not running - this is inconsistent
	return true
}

// SetHistoryModule sets the history module for status correction checker
func (scc *StatusCorrectionChecker) SetHistoryModule(hm *history.HistoryModule) {
	scc.mutex.Lock()
	defer scc.mutex.Unlock()
	scc.historyModule = hm
}

// SetTaskModule sets the task module for status correction checker
func (scc *StatusCorrectionChecker) SetTaskModule(tm *task.TaskModule) {
	scc.mutex.Lock()
	defer scc.mutex.Unlock()
	scc.taskModule = tm
}
