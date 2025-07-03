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
}

// StatusChange represents a detected status change
type StatusChange struct {
	UserID          string                 `json:"user_id"`
	SourceID        string                 `json:"source_id"`
	AppName         string                 `json:"app_name"`
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
		"is_running":       scc.isRunning,
		"check_interval":   scc.checkInterval,
		"last_check_time":  scc.lastCheckTime,
		"check_count":      scc.checkCount,
		"correction_count": scc.correctionCount,
		"app_service_url":  fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", scc.appServiceHost, scc.appServicePort),
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

	glog.Infof("Fetched status for %d applications from app-service", len(latestStatus))

	// Get current status from cache
	cachedStatus := scc.getCachedStatus()
	if len(cachedStatus) == 0 {
		glog.Infof("No cached status found, skipping comparison")
		return
	}

	glog.Infof("Found cached status for %d applications", len(cachedStatus))

	// Compare and detect changes
	changes := scc.compareStatus(latestStatus, cachedStatus)

	if len(changes) > 0 {
		glog.Infof("Detected %d status changes, applying corrections", len(changes))
		scc.applyCorrections(changes, latestStatus)

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

	for _, app := range latestStatus {
		userID := app.Spec.Owner
		appName := app.Spec.Name

		// Find matching cached app by searching through all sources
		var cachedApp *types.AppStateLatestData
		var sourceID string

		for key, cached := range cachedStatus {
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
			glog.V(2).Infof("No cached status found for app: %s (user: %s)", appName, userID)
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
		// Find the corresponding app in latest status
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

		// Create app state data from the latest status
		appStateData := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID)
		if appStateData == nil {
			glog.Warningf("Failed to create app state data for app %s (user: %s)", change.AppName, change.UserID)
			continue
		}

		// Update the cache with the corrected status
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

		// Update the cache
		if err := scc.cacheManager.SetAppData(change.UserID, change.SourceID, AppStateLatest, stateData); err != nil {
			glog.Errorf("Failed to update cache with corrected status for app %s (user: %s, source: %s): %v",
				change.AppName, change.UserID, change.SourceID, err)
		} else {
			glog.Infof("Successfully updated cache with corrected status for app %s (user: %s, source: %s)",
				change.AppName, change.UserID, change.SourceID)
		}
	}
}

// createAppStateDataFromResponse creates AppStateLatestData from AppServiceResponse
func (scc *StatusCorrectionChecker) createAppStateDataFromResponse(app utils.AppServiceResponse, userID string) *types.AppStateLatestData {
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
	if userID != "" && app.Spec.Name != "" {
		if versionFromRecord, err := utils.GetAppVersionFromDownloadRecord(userID, app.Spec.Name); err == nil && versionFromRecord != "" {
			version = versionFromRecord
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
	}
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
