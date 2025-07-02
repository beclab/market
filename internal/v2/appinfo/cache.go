package appinfo

import (
	"fmt"
	"market/internal/v2/types"
	"sync"
	"time"

	"market/internal/v2/utils"

	"github.com/golang/glog"
)

// HydrationNotifier interface for notifying hydrator about pending data updates
type HydrationNotifier interface {
	NotifyPendingDataUpdate(userID, sourceID string, pendingData map[string]interface{})
}

// CacheManager manages the in-memory cache and Redis synchronization
type CacheManager struct {
	cache             *CacheData
	redisClient       *RedisClient
	userConfig        *UserConfig
	hydrationNotifier HydrationNotifier   // Notifier for hydration updates
	stateMonitor      *utils.StateMonitor // State monitor for change detection
	mutex             sync.RWMutex
	syncChannel       chan SyncRequest
	stopChannel       chan bool
	isRunning         bool

	// Lock monitoring
	lockStats struct {
		sync.Mutex
		lastLockTime   time.Time
		lastUnlockTime time.Time
		lockDuration   time.Duration
		lockCount      int64
		unlockCount    int64
	}
}

// SyncRequest represents a request to sync data to Redis
type SyncRequest struct {
	UserID   string
	SourceID string
	Type     SyncType
}

// SyncType represents the type of sync operation
type SyncType int

const (
	SyncUser   SyncType = iota // Sync entire user data
	SyncSource                 // Sync specific source data
	DeleteUser                 // Delete user data
)

// NewCacheManager creates a new cache manager
func NewCacheManager(redisClient *RedisClient, userConfig *UserConfig) *CacheManager {
	// Initialize state monitor
	dataSender, err := NewDataSender()
	var stateMonitor *utils.StateMonitor
	if err != nil {
		glog.Warningf("Failed to initialize DataSender for state monitor: %v", err)
		// Continue without state monitor
	} else {
		stateMonitor = utils.NewStateMonitor(dataSender)
	}

	return &CacheManager{
		cache:        NewCacheData(),
		redisClient:  redisClient,
		userConfig:   userConfig,
		stateMonitor: stateMonitor,
		syncChannel:  make(chan SyncRequest, 1000), // Buffer for async sync requests
		stopChannel:  make(chan bool, 1),
		isRunning:    false,
	}
}

// Start initializes the cache by loading data from Redis and starts the sync worker
func (cm *CacheManager) Start() error {
	glog.Infof("Starting cache manager")

	// Load cache data from Redis if ClearCache is false
	if !cm.userConfig.ClearCache {
		cache, err := cm.redisClient.LoadCacheFromRedis()
		if err != nil {
			glog.Errorf("Failed to load cache from Redis: %v", err)
			return err
		}
		glog.Infof("[LOCK] cm.mutex.Lock() @81 Start")
		cm.mutex.Lock()
		cm.cache = cache
		cm.mutex.Unlock()
	} else {
		glog.Infof("ClearCache is enabled, clearing Redis data and starting with empty cache")

		// Clear all Redis data
		if err := cm.redisClient.ClearAllData(); err != nil {
			glog.Errorf("Failed to clear Redis data: %v", err)
			return err
		}

		glog.Infof("[LOCK] cm.mutex.Lock() @81 Start")
		cm.mutex.Lock()
		cm.cache = NewCacheData()
		cm.mutex.Unlock()
	}

	// Ensure all users from userConfig.UserList have their data structures initialized
	if cm.userConfig != nil && len(cm.userConfig.UserList) > 0 {
		glog.Infof("Initializing data structures for configured users")

		glog.Infof("[LOCK] cm.mutex.Lock() @102 Start")
		cm.mutex.Lock()
		for _, userID := range cm.userConfig.UserList {
			if _, exists := cm.cache.Users[userID]; !exists {
				glog.Infof("Creating data structure for new user: %s", userID)
				cm.cache.Users[userID] = NewUserData()
			}
		}
		cm.mutex.Unlock()

		glog.Infof("User data structure initialization completed for %d users", len(cm.userConfig.UserList))
	}

	glog.Infof("[LOCK] cm.mutex.Lock() @114 Start")
	cm.mutex.Lock()
	cm.isRunning = true
	cm.mutex.Unlock()

	// Start sync worker goroutine
	go cm.syncWorker()

	glog.Infof("Cache manager started successfully")
	return nil
}

// Stop stops the cache manager and sync worker
func (cm *CacheManager) Stop() {
	glog.Infof("Stopping cache manager")

	glog.Infof("[LOCK] cm.mutex.Lock() @129 Start")
	cm.mutex.Lock()
	if cm.isRunning {
		cm.isRunning = false
		cm.stopChannel <- true
	}
	cm.mutex.Unlock()

	// Close state monitor
	if cm.stateMonitor != nil {
		cm.stateMonitor.Close()
		glog.Infof("State monitor closed")
	}

	glog.Infof("Cache manager stopped")
}

// syncWorker processes sync requests in the background
func (cm *CacheManager) syncWorker() {
	glog.Infof("Sync worker started")

	for {
		select {
		case syncReq := <-cm.syncChannel:
			cm.processSyncRequest(syncReq)
		case <-cm.stopChannel:
			glog.Infof("Sync worker stopped")
			return
		}
	}
}

// processSyncRequest handles individual sync requests
func (cm *CacheManager) processSyncRequest(req SyncRequest) {
	switch req.Type {
	case SyncUser:
		if userData := cm.getUserData(req.UserID); userData != nil {
			if err := cm.redisClient.SaveUserDataToRedis(req.UserID, userData); err != nil {
				glog.Errorf("Failed to sync user data to Redis: %v", err)
			}
		}
	case SyncSource:
		if sourceData := cm.getSourceData(req.UserID, req.SourceID); sourceData != nil {
			if err := cm.redisClient.SaveSourceDataToRedis(req.UserID, req.SourceID, sourceData); err != nil {
				glog.Errorf("Failed to sync source data to Redis: %v", err)
			}
		}
	case DeleteUser:
		if err := cm.redisClient.DeleteUserDataFromRedis(req.UserID); err != nil {
			glog.Errorf("Failed to delete user data from Redis: %v", err)
		}
	}
}

// GetUserData retrieves user data from cache
func (cm *CacheManager) GetUserData(userID string) *UserData {
	glog.Infof("[LOCK] cm.mutex.RLock() @184 Start")
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	return cm.cache.Users[userID]
}

// getUserData internal method to get user data without external locking
func (cm *CacheManager) getUserData(userID string) *UserData {
	return cm.cache.Users[userID]
}

// GetSourceData retrieves source data from cache
func (cm *CacheManager) GetSourceData(userID, sourceID string) *SourceData {
	glog.Infof("[LOCK] cm.mutex.RLock() @197 Start")
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if userData, exists := cm.cache.Users[userID]; exists {
		return userData.Sources[sourceID]
	}
	return nil
}

// getSourceData internal method to get source data without external locking
func (cm *CacheManager) getSourceData(userID, sourceID string) *SourceData {
	if userData, exists := cm.cache.Users[userID]; exists {
		return userData.Sources[sourceID]
	}
	return nil
}

// SetHydrationNotifier sets the hydration notifier for real-time updates
func (cm *CacheManager) SetHydrationNotifier(notifier HydrationNotifier) {
	glog.Infof("[LOCK] cm.mutex.Lock() @216 Start")
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.hydrationNotifier = notifier
	glog.Infof("Hydration notifier set successfully")
}

// updateAppStateLatest updates or adds a single app state based on name matching
func (cm *CacheManager) updateAppStateLatest(sourceData *SourceData, newAppState *types.AppStateLatestData) {
	if newAppState == nil {
		glog.Errorf("Invalid app state data: app state is nil")
		return
	}

	if newAppState.Status.Name == "" {
		glog.Errorf("Invalid app state data: missing name field - app state will be rejected")
		return
	}

	// Check if any running entrance has empty URL (only running entrances require URLs)
	hasRunningWithEmptyUrl := false
	for _, entrance := range newAppState.Status.EntranceStatuses {
		if entrance.State == "running" && entrance.Url == "" {
			hasRunningWithEmptyUrl = true
			break
		}
	}

	if hasRunningWithEmptyUrl {
		glog.Warningf("App state data has running entrance with empty URL for app %s - app state will be rejected", newAppState.Status.Name)
		return
	}

	// Try to find existing app state with the same name
	found := false
	for i, existingAppState := range sourceData.AppStateLatest {
		if existingAppState != nil && existingAppState.Status.Name == newAppState.Status.Name {
			// Update existing app state
			sourceData.AppStateLatest[i] = newAppState
			glog.V(2).Infof("Updated existing app state for app: %s", newAppState.Status.Name)
			found = true
			break
		}
	}

	// If not found, add new app state
	if !found {
		sourceData.AppStateLatest = append(sourceData.AppStateLatest, newAppState)
		glog.V(2).Infof("Added new app state for app: %s", newAppState.Status.Name)
	}
}

// SetAppData sets app data in cache using single global lock
func (cm *CacheManager) SetAppData(userID, sourceID string, dataType AppDataType, data map[string]interface{}) error {
	glog.Infof("[LOCK] cm.mutex.Lock() @269 Start")
	cm.updateLockStats("lock")
	cm.mutex.Lock()
	defer func() {
		cm.mutex.Unlock()
		cm.updateLockStats("unlock")
		glog.Infof("[LOCK] cm.mutex.Unlock() @269 End")
	}()

	if !cm.isRunning {
		return fmt.Errorf("cache manager is not running")
	}

	if cm.cache == nil {
		return fmt.Errorf("cache is not initialized")
	}

	// Ensure user exists
	if _, exists := cm.cache.Users[userID]; !exists {
		cm.cache.Users[userID] = NewUserData()
	}

	// Check source limit for user
	userData := cm.cache.Users[userID]
	// No nested locks needed since we already hold the global lock

	// Check if we're adding a new source and if it exceeds the limit
	if _, exists := userData.Sources[sourceID]; !exists {
		if cm.userConfig != nil {
			maxSources := cm.userConfig.MaxSourcesPerUser

			// Give admin users double the limit
			for _, admin := range cm.userConfig.AdminList {
				if admin == userID {
					maxSources *= 2
					break
				}
			}

			if len(userData.Sources) >= maxSources {
				glog.Warningf("User '%s' has reached maximum sources limit (%d)", userID, maxSources)
				return fmt.Errorf("user '%s' has reached maximum sources limit (%d)", userID, maxSources)
			}
		}

		userData.Sources[sourceID] = NewSourceData()
	}

	// Set the data
	sourceData := userData.Sources[sourceID]
	// No nested locks needed since we already hold the global lock

	// Log image analysis information if present
	if imageAnalysis, hasImageAnalysis := data["image_analysis"]; hasImageAnalysis {
		glog.Infof("Setting app data with image analysis for user=%s, source=%s, type=%s", userID, sourceID, dataType)
		if analysisMap, ok := imageAnalysis.(map[string]interface{}); ok {
			if totalImages, ok := analysisMap["total_images"].(int); ok {
				glog.Infof("App data includes %d Docker images", totalImages)
			}
		}
	}

	switch dataType {
	case AppInfoHistory:
		appData := NewAppInfoHistoryData(data)
		appData.Timestamp = time.Now().Unix()
		sourceData.AppInfoHistory = append(sourceData.AppInfoHistory, appData)
	case AppStateLatest:
		// Check if this is a list of app states
		if appStatesData, hasAppStates := data["app_states"].([]*types.AppStateLatestData); hasAppStates {
			// Check for state changes and send notifications for each app state
			if cm.stateMonitor != nil {
				for _, appState := range appStatesData {
					if appState != nil {
						// Extract app name from app state for state monitoring
						appName := appState.Status.Name

						if appName != "" {
							// Check state changes and send notifications
							if err := cm.stateMonitor.CheckAndNotifyStateChange(
								userID, sourceID, appName,
								appState,
								sourceData.AppStateLatest,
								sourceData.AppInfoLatest,
							); err != nil {
								glog.Warningf("Failed to check and notify state change for app %s: %v", appName, err)
							}
						}
					}
				}
			}

			// Update each app state individually using name matching
			for _, appState := range appStatesData {
				if appState != nil {
					cm.updateAppStateLatest(sourceData, appState)
				}
			}
			glog.Infof("Updated %d app states for user=%s, source=%s", len(appStatesData), userID, sourceID)
		} else {
			// Fallback to old logic for backward compatibility
			// Check if entrance URLs are missing and fetch them if needed
			enhancedData := cm.enhanceAppStateDataWithUrls(data)

			appData := types.NewAppStateLatestData(enhancedData, userID, utils.GetAppVersionFromDownloadRecord)

			// Validate that the created app state has a name field
			if appData == nil {
				glog.Errorf("Failed to create AppStateLatestData from data for user=%s, source=%s - data may be invalid", userID, sourceID)
				return fmt.Errorf("invalid app state data: NewAppStateLatestData returned nil")
			}

			if appData.Status.Name == "" {
				glog.Errorf("Invalid app state data: missing name field for user=%s, source=%s - app state will be rejected", userID, sourceID)
				return fmt.Errorf("invalid app state data: missing name field")
			}

			// Check for state changes and send notifications
			if cm.stateMonitor != nil {
				// Extract app name from app state for state monitoring
				appName := appData.Status.Name

				if appName != "" {
					// Check state changes and send notifications
					if err := cm.stateMonitor.CheckAndNotifyStateChange(
						userID, sourceID, appName,
						appData,
						sourceData.AppStateLatest,
						sourceData.AppInfoLatest,
					); err != nil {
						glog.Warningf("Failed to check and notify state change for app %s: %v", appName, err)
					}
				}
			}

			// Update or add the app state using name matching
			cm.updateAppStateLatest(sourceData, appData)
			glog.Infof("Updated single app state for user=%s, source=%s", userID, sourceID)
		}
	case AppInfoLatest:
		appData := NewAppInfoLatestData(data)
		if appData == nil {
			glog.Warningf("Failed to create AppInfoLatestData from data for user=%s, source=%s - data may be invalid", userID, sourceID)
			return fmt.Errorf("invalid app data: NewAppInfoLatestData returned nil")
		}
		appData.Timestamp = time.Now().Unix()
		sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, appData)
	case AppInfoLatestPending:
		// Clear existing AppInfoLatestPending list before adding new data
		// This ensures we don't accumulate old data when hash doesn't match
		originalCount := len(sourceData.AppInfoLatestPending)
		sourceData.AppInfoLatestPending = sourceData.AppInfoLatestPending[:0] // Clear the slice
		glog.Infof("Cleared %d existing AppInfoLatestPending entries for user=%s, source=%s", originalCount, userID, sourceID)

		// Check if this is a complete market data structure
		if appsData, hasApps := data["apps"].(map[string]interface{}); hasApps {
			// This is complete market data, extract individual apps
			glog.Infof("Processing complete market data with %d apps for user=%s, source=%s", len(appsData), userID, sourceID)

			// Also store the "others" data (hash, version, topics, etc.)
			others := &types.Others{}
			if version, ok := data["version"].(string); ok {
				others.Version = version
			}
			if hash, ok := data["hash"].(string); ok {
				others.Hash = hash
			}

			// Extract topics, recommends, pages if present
			if topics, ok := data["topics"].(map[string]interface{}); ok {
				for _, topicData := range topics {
					if topicMap, ok := topicData.(map[string]interface{}); ok {
						topic := &types.Topic{}
						if name, ok := topicMap["name"].(string); ok {
							topic.Name = name
						}
						if apps, ok := topicMap["apps"].(string); ok {
							topic.Apps = apps
						}
						// ... extract other topic fields as needed
						others.Topics = append(others.Topics, topic)
					}
				}
			}

			// Store others data in source
			sourceData.Others = others

			// Process each individual app
			for appID, appDataInterface := range appsData {
				if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
					glog.Infof("DEBUG: CALL POINT 1 - Processing app %s for user=%s, source=%s", appID, userID, sourceID)
					glog.Infof("DEBUG: CALL POINT 1 - App data before calling NewAppInfoLatestPendingDataFromLegacyData: %+v", appDataMap)
					appData := NewAppInfoLatestPendingDataFromLegacyData(appDataMap)
					if appData != nil {
						appData.Timestamp = time.Now().Unix()
						sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
						glog.V(2).Infof("Added app %s for user=%s, source=%s", appID, userID, sourceID)
					} else {
						glog.Warningf("Failed to create app data for app %s (user=%s, source=%s)", appID, userID, sourceID)
					}
				}
			}

			glog.Infof("Successfully processed %d apps from market data for user=%s, source=%s", len(sourceData.AppInfoLatestPending), userID, sourceID)
		} else {
			// This might be market data with nested apps structure, try to extract apps
			glog.Infof("DEBUG: CALL POINT 2 - Processing potential market data for user=%s, source=%s", userID, sourceID)
			glog.Infof("DEBUG: CALL POINT 2 - Data before processing: %+v", data)

			// Check if this is market data with nested structure
			if dataSection, hasData := data["data"].(map[string]interface{}); hasData {
				if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
					// This is market data with apps - process each app individually
					glog.Infof("DEBUG: CALL POINT 2 - Found nested apps structure with %d apps", len(appsData))
					for appID, appDataInterface := range appsData {
						if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
							glog.Infof("DEBUG: CALL POINT 2 - Processing app %s", appID)
							appData := NewAppInfoLatestPendingDataFromLegacyData(appDataMap)
							if appData != nil {
								appData.Timestamp = time.Now().Unix()
								sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
								glog.V(2).Infof("Added app %s for user=%s, source=%s", appID, userID, sourceID)
							} else {
								glog.Warningf("Failed to create app data for app %s (user=%s, source=%s)", appID, userID, sourceID)
							}
						}
					}
					glog.Infof("Successfully processed %d apps from nested market data for user=%s, source=%s", len(sourceData.AppInfoLatestPending), userID, sourceID)
				} else {
					glog.Warningf("Market data found but no apps section for user=%s, source=%s", userID, sourceID)
				}
			} else {
				// This might be actual single app data, try to process directly
				glog.Infof("DEBUG: CALL POINT 2 - Trying as single app data for user=%s, source=%s", userID, sourceID)
				appData := NewAppInfoLatestPendingDataFromLegacyData(data)
				if appData == nil {
					glog.Warningf("Failed to create AppInfoLatestPendingData from data for user=%s, source=%s - not recognized as app data or market data", userID, sourceID)
					return fmt.Errorf("invalid app data: missing required identifiers (id, name, or appID)")
				}

				appData.Timestamp = time.Now().Unix()
				sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
				glog.Infof("Successfully processed single app data for user=%s, source=%s", userID, sourceID)
			}
		}

		glog.Infof("Updated AppInfoLatestPending list with %d new entries for user=%s, source=%s",
			len(sourceData.AppInfoLatestPending), userID, sourceID)

		// Notify hydrator about pending data update for immediate task creation
		if cm.hydrationNotifier != nil && len(sourceData.AppInfoLatestPending) > 0 {
			glog.Infof("Notifying hydrator about pending data update for user=%s, source=%s", userID, sourceID)
			go cm.hydrationNotifier.NotifyPendingDataUpdate(userID, sourceID, data)
		}
	case types.AppRenderFailed:
		// Handle render failed data - this is typically set by the hydrator when tasks fail
		if failedAppData, hasFailedApp := data["failed_app"].(*types.AppRenderFailedData); hasFailedApp {
			sourceData.AppRenderFailed = append(sourceData.AppRenderFailed, failedAppData)
			glog.Infof("Added render failed app for user=%s, source=%s, app=%s, reason=%s",
				userID, sourceID, failedAppData.RawData.AppID, failedAppData.FailureReason)
		} else {
			glog.Warningf("Invalid render failed data format for user=%s, source=%s", userID, sourceID)
			return fmt.Errorf("invalid render failed data: missing failed_app field")
		}
	}

	// Trigger async sync to Redis
	cm.requestSync(SyncRequest{
		UserID:   userID,
		SourceID: sourceID,
		Type:     SyncSource,
	})

	glog.Infof("Set app data for user=%s, source=%s, type=%s", userID, sourceID, dataType)
	return nil
}

// GetAppData retrieves app data from cache using single global lock
func (cm *CacheManager) GetAppData(userID, sourceID string, dataType AppDataType) interface{} {
	glog.Infof("[LOCK] cm.mutex.RLock() @543 Start")
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if userData, exists := cm.cache.Users[userID]; exists {
		if sourceData, exists := userData.Sources[sourceID]; exists {
			// No nested locks needed since we already hold the global lock
			switch dataType {
			case AppInfoHistory:
				return sourceData.AppInfoHistory
			case AppStateLatest:
				return sourceData.AppStateLatest
			case AppInfoLatest:
				return sourceData.AppInfoLatest
			case AppInfoLatestPending:
				return sourceData.AppInfoLatestPending
			case types.AppRenderFailed:
				return sourceData.AppRenderFailed
			}
		}
	}
	return nil
}

// RemoveUserData removes user data from cache and Redis
func (cm *CacheManager) RemoveUserData(userID string) error {
	glog.Infof("[LOCK] cm.mutex.Lock() @568 Start")
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Remove from cache
	delete(cm.cache.Users, userID)

	// Trigger async deletion from Redis
	cm.requestSync(SyncRequest{
		UserID: userID,
		Type:   DeleteUser,
	})

	glog.Infof("Removed user data for user=%s", userID)
	return nil
}

// GetCacheStats returns cache statistics using single global lock
func (cm *CacheManager) GetCacheStats() map[string]interface{} {
	glog.Infof("[LOCK] cm.mutex.RLock() @586 Start")
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_users"] = len(cm.cache.Users)
	stats["is_running"] = cm.isRunning

	totalSources := 0
	for _, userData := range cm.cache.Users {
		// No nested locks needed since we already hold the global lock
		totalSources += len(userData.Sources)
	}
	stats["total_sources"] = totalSources

	return stats
}

// requestSync sends a sync request to the sync worker
func (cm *CacheManager) requestSync(req SyncRequest) {
	if cm.isRunning {
		select {
		case cm.syncChannel <- req:
			// Request queued successfully
		default:
			glog.Warningf("Sync channel is full, dropping sync request")
		}
	}
}

// ForceSync forces immediate synchronization of all data to Redis
func (cm *CacheManager) ForceSync() error {
	glog.Infof("[LOCK] cm.mutex.RLock() @617 Start")
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	glog.Infof("Force syncing all cache data to Redis")

	for userID, userData := range cm.cache.Users {
		if err := cm.redisClient.SaveUserDataToRedis(userID, userData); err != nil {
			glog.Errorf("Failed to force sync user data: %v", err)
			return err
		}
	}

	glog.Infof("Force sync completed successfully")
	return nil
}

// GetAllUsersData returns all users data from cache using single global lock with timeout
func (cm *CacheManager) GetAllUsersData() map[string]*UserData {
	// Use timeout to prevent blocking indefinitely
	timeout := 10 * time.Second
	done := make(chan map[string]*UserData, 1)

	go func() {
		glog.Infof("[LOCK] cm.mutex.RLock() @635 Start")
		cm.updateLockStats("lock")
		cm.mutex.RLock()
		defer func() {
			cm.mutex.RUnlock()
			cm.updateLockStats("unlock")
			glog.Infof("[LOCK] cm.mutex.RUnlock() @635 End")
		}()

		// Return shallow copy of data directly without nested locks
		result := make(map[string]*UserData)
		for userID, userData := range cm.cache.Users {
			// Create shallow copy
			userDataCopy := &UserData{
				Sources: make(map[string]*SourceData),
				Hash:    userData.Hash,
			}

			// Copy source data references
			for sourceID, sourceData := range userData.Sources {
				userDataCopy.Sources[sourceID] = sourceData
			}

			result[userID] = userDataCopy
		}

		done <- result
	}()

	select {
	case result := <-done:
		return result
	case <-time.After(timeout):
		glog.Errorf("GetAllUsersData: Timeout after %v, returning empty result", timeout)
		return make(map[string]*UserData)
	}
}

// UpdateUserConfig updates the user configuration and ensures all users have data structures
func (cm *CacheManager) UpdateUserConfig(newUserConfig *UserConfig) error {
	glog.Infof("[LOCK] cm.mutex.Lock() @660 Start")
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if newUserConfig == nil {
		return fmt.Errorf("user config cannot be nil")
	}

	glog.Infof("Updating user configuration")

	// oldUserConfig := cm.userConfig // Commented out as it's only used in optional removal logic
	cm.userConfig = newUserConfig

	// Initialize data structures for new users in the updated configuration
	if len(newUserConfig.UserList) > 0 {
		for _, userID := range newUserConfig.UserList {
			if _, exists := cm.cache.Users[userID]; !exists {
				glog.Infof("Creating data structure for newly configured user: %s", userID)
				cm.cache.Users[userID] = NewUserData()

				// Trigger sync to Redis for the new user
				if cm.isRunning {
					cm.requestSync(SyncRequest{
						UserID: userID,
						Type:   SyncUser,
					})
				}
			}
		}
	}

	// Optionally remove users that are no longer in the configuration
	// (This is commented out by default to preserve data)
	/*
		oldUserConfig := cm.userConfig // Uncomment this line if enabling user removal logic
		if oldUserConfig != nil {
			for _, oldUserID := range oldUserConfig.UserList {
				found := false
				for _, newUserID := range newUserConfig.UserList {
					if oldUserID == newUserID {
						found = true
						break
					}
				}
				if !found {
					glog.Infof("User %s is no longer in configuration, but data is preserved", oldUserID)
					// Uncomment the line below if you want to remove data for users not in the new configuration
					// delete(cm.cache.Users, oldUserID)
				}
			}
		}
	*/

	glog.Infof("User configuration updated successfully")
	return nil
}

// SyncUserListToCache ensures all users from current userConfig have initialized data structures
func (cm *CacheManager) SyncUserListToCache() error {
	glog.Infof("[LOCK] cm.mutex.Lock() @718 Start")
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if cm.userConfig == nil || len(cm.userConfig.UserList) == 0 {
		glog.Warningf("No user configuration available for syncing")
		return nil
	}

	glog.Infof("Syncing user list to cache")

	newUsersCount := 0
	for _, userID := range cm.userConfig.UserList {
		if _, exists := cm.cache.Users[userID]; !exists {
			glog.Infof("Adding missing user to cache: %s", userID)
			cm.cache.Users[userID] = NewUserData()
			newUsersCount++

			// Trigger sync to Redis for the new user
			if cm.isRunning {
				cm.requestSync(SyncRequest{
					UserID: userID,
					Type:   SyncUser,
				})
			}
		}
	}

	glog.Infof("User list sync completed, added %d new users", newUsersCount)
	return nil
}

// CleanupInvalidPendingData removes invalid pending data entries that lack required identifiers
func (cm *CacheManager) CleanupInvalidPendingData() int {
	glog.Infof("[LOCK] cm.mutex.Lock() @751 Start")
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	totalCleaned := 0

	for userID, userData := range cm.cache.Users {
		// No nested locks needed since we already hold the global lock
		for sourceID, sourceData := range userData.Sources {
			// No nested locks needed since we already hold the global lock

			originalCount := len(sourceData.AppInfoLatestPending)
			cleanedPendingData := make([]*AppInfoLatestPendingData, 0, originalCount)

			for _, pendingData := range sourceData.AppInfoLatestPending {
				// Check if this pending data has valid identifiers
				isValid := false

				if pendingData.RawData != nil {
					// Check for valid ID or AppID
					if (pendingData.RawData.ID != "" && pendingData.RawData.ID != "0") ||
						(pendingData.RawData.AppID != "" && pendingData.RawData.AppID != "0") ||
						(pendingData.RawData.Name != "" && pendingData.RawData.Name != "unknown") {
						isValid = true
					}
				}

				if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
					// Also check AppInfo.AppEntry for valid identifiers
					if (pendingData.AppInfo.AppEntry.ID != "" && pendingData.AppInfo.AppEntry.ID != "0") ||
						(pendingData.AppInfo.AppEntry.AppID != "" && pendingData.AppInfo.AppEntry.AppID != "0") ||
						(pendingData.AppInfo.AppEntry.Name != "" && pendingData.AppInfo.AppEntry.Name != "unknown") {
						isValid = true
					}
				}

				if isValid {
					cleanedPendingData = append(cleanedPendingData, pendingData)
				} else {
					glog.Infof("Removing invalid pending data entry for user=%s, source=%s (missing identifiers)", userID, sourceID)
					totalCleaned++
				}
			}

			// Update the source data with cleaned list
			sourceData.AppInfoLatestPending = cleanedPendingData

			if originalCount != len(cleanedPendingData) {
				glog.Infof("Cleaned %d invalid pending data entries for user=%s, source=%s",
					originalCount-len(cleanedPendingData), userID, sourceID)

				// Trigger sync to Redis to persist the cleanup
				if cm.isRunning {
					cm.requestSync(SyncRequest{
						UserID:   userID,
						SourceID: sourceID,
						Type:     SyncSource,
					})
				}
			}
		}
	}

	if totalCleaned > 0 {
		glog.Infof("Cleanup completed: removed %d invalid pending data entries across all users", totalCleaned)
	}

	return totalCleaned
}

// enhanceAppStateDataWithUrls enhances app state data with entrance URLs
func (cm *CacheManager) enhanceAppStateDataWithUrls(data map[string]interface{}) map[string]interface{} {
	// Create a copy of the data to avoid modifying the original
	enhancedData := make(map[string]interface{})
	for k, v := range data {
		enhancedData[k] = v
	}

	// Add debug logging for input data
	if entranceStatusesVal, ok := data["entranceStatuses"]; ok {
		glog.Infof("DEBUG: enhanceAppStateDataWithUrls - input entranceStatuses type: %T, value: %+v", entranceStatusesVal, entranceStatusesVal)
	} else {
		glog.Infof("DEBUG: enhanceAppStateDataWithUrls - no entranceStatuses found in input data")
	}

	// Extract app name for URL fetching
	var appName string
	if name, ok := data["name"].(string); ok && name != "" {
		appName = name
	} else if appNameVal, ok := data["appName"].(string); ok && appNameVal != "" {
		appName = appNameVal
	} else if appIDVal, ok := data["appID"].(string); ok && appIDVal != "" {
		appName = appIDVal
	} else if idVal, ok := data["id"].(string); ok && idVal != "" {
		appName = idVal
	}

	if appName == "" {
		glog.Warningf("Cannot determine app name for URL enhancement")
		return enhancedData
	}

	// Process entrance statuses
	if entranceStatusesVal, ok := data["entranceStatuses"]; ok {
		if entranceStatuses, ok := entranceStatusesVal.([]interface{}); ok {
			// Check if any running entrance has empty URL
			hasRunningWithEmptyUrl := false
			runningEntrancesWithoutUrl := make([]string, 0)

			for _, entranceVal := range entranceStatuses {
				if entranceMap, ok := entranceVal.(map[string]interface{}); ok {
					state, _ := entranceMap["state"].(string)
					url, hasUrl := entranceMap["url"].(string)
					name, _ := entranceMap["name"].(string)

					// Only require URL for running entrances
					if state == "running" && (!hasUrl || url == "") {
						hasRunningWithEmptyUrl = true
						if name != "" {
							runningEntrancesWithoutUrl = append(runningEntrancesWithoutUrl, name)
						}
					}
				}
			}

			// If running entrances have empty URLs, try to fetch them from app-service
			if hasRunningWithEmptyUrl {
				glog.Infof("Running entrances %v have empty URLs for app %s - attempting to fetch URLs from app-service", runningEntrancesWithoutUrl, appName)

				// Fetch entrance URLs from app-service
				entranceUrls, err := utils.FetchAppEntranceUrls(appName)
				if err != nil {
					glog.Warningf("Failed to fetch entrance URLs for app %s: %v - returning empty entrance statuses", appName, err)
					enhancedData["entranceStatuses"] = []interface{}{}
					return enhancedData
				}

				// Update entrance statuses with fetched URLs
				updatedEntrances := make([]interface{}, 0, len(entranceStatuses))
				for _, entranceVal := range entranceStatuses {
					if entranceMap, ok := entranceVal.(map[string]interface{}); ok {
						name, _ := entranceMap["name"].(string)
						state, _ := entranceMap["state"].(string)
						url, hasUrl := entranceMap["url"].(string)

						// If this is a running entrance without URL, try to get it from fetched URLs
						if state == "running" && (!hasUrl || url == "") {
							if fetchedUrl, exists := entranceUrls[name]; exists && fetchedUrl != "" {
								entranceMap["url"] = fetchedUrl
								glog.Infof("Updated entrance %s URL for app %s: %s", name, appName, fetchedUrl)
							} else {
								glog.Warningf("Running entrance %s for app %s still has no URL after fetching - skipping", name, appName)
								continue // Skip this entrance
							}
						}

						updatedEntrances = append(updatedEntrances, entranceMap)
					}
				}

				enhancedData["entranceStatuses"] = updatedEntrances
				glog.Infof("DEBUG: enhanceAppStateDataWithUrls - output entranceStatuses type: %T, value: %+v", enhancedData["entranceStatuses"], enhancedData["entranceStatuses"])
				return enhancedData
			}

			// If no running entrances with empty URLs, return as is
			enhancedData["entranceStatuses"] = entranceStatuses
			glog.Infof("DEBUG: enhanceAppStateDataWithUrls - output entranceStatuses type: %T, value: %+v", enhancedData["entranceStatuses"], enhancedData["entranceStatuses"])
			return enhancedData
		}
	}

	return enhancedData
}

// GetLockStats returns current lock statistics for monitoring
func (cm *CacheManager) GetLockStats() map[string]interface{} {
	glog.V(2).Infof("[LOCK] cm.lockStats.Lock() GetLockStats Start")
	cm.lockStats.Lock()
	defer func() {
		cm.lockStats.Unlock()
		glog.V(2).Infof("[LOCK] cm.lockStats.Unlock() GetLockStats End")
	}()

	stats := make(map[string]interface{})
	stats["last_lock_time"] = cm.lockStats.lastLockTime
	stats["last_unlock_time"] = cm.lockStats.lastUnlockTime
	stats["lock_duration"] = cm.lockStats.lockDuration
	stats["lock_count"] = cm.lockStats.lockCount
	stats["unlock_count"] = cm.lockStats.unlockCount

	// Check for potential lock issues
	if cm.lockStats.lockCount > cm.lockStats.unlockCount {
		stats["lock_imbalance"] = cm.lockStats.lockCount - cm.lockStats.unlockCount
		stats["potential_deadlock"] = true
	} else {
		stats["lock_imbalance"] = 0
		stats["potential_deadlock"] = false
	}

	// Check if lock has been held for too long
	if !cm.lockStats.lastLockTime.IsZero() && cm.lockStats.lockDuration > 30*time.Second {
		stats["long_lock_duration"] = true
		stats["current_lock_duration"] = time.Since(cm.lockStats.lastLockTime)
	} else {
		stats["long_lock_duration"] = false
	}

	return stats
}

// updateLockStats updates lock statistics
func (cm *CacheManager) updateLockStats(lockType string) {
	glog.V(2).Infof("[LOCK] cm.lockStats.Lock() Start")
	cm.lockStats.Lock()
	defer func() {
		cm.lockStats.Unlock()
		glog.V(2).Infof("[LOCK] cm.lockStats.Unlock() End")
	}()

	now := time.Now()
	if lockType == "lock" {
		cm.lockStats.lastLockTime = now
		cm.lockStats.lockCount++
		glog.V(2).Infof("[LOCK] Lock stats updated - lock count: %d", cm.lockStats.lockCount)
	} else if lockType == "unlock" {
		cm.lockStats.lastUnlockTime = now
		cm.lockStats.unlockCount++
		if !cm.lockStats.lastLockTime.IsZero() {
			cm.lockStats.lockDuration = now.Sub(cm.lockStats.lastLockTime)
		}
		glog.V(2).Infof("[LOCK] Lock stats updated - unlock count: %d, duration: %v", cm.lockStats.unlockCount, cm.lockStats.lockDuration)
	}
}
