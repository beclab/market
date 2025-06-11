package appinfo

import (
	"fmt"
	"market/internal/v2/types"
	"sync"
	"time"

	"github.com/golang/glog"
)

// HydrationNotifier interface for notifying hydrator about pending data updates
// HydrationNotifier 接口用于通知水合器关于待处理数据更新
type HydrationNotifier interface {
	NotifyPendingDataUpdate(userID, sourceID string, pendingData map[string]interface{})
}

// CacheManager manages the in-memory cache and Redis synchronization
// CacheManager 管理内存缓存和 Redis 同步
type CacheManager struct {
	cache             *CacheData
	redisClient       *RedisClient
	userConfig        *UserConfig
	hydrationNotifier HydrationNotifier // Notifier for hydration updates
	mutex             sync.RWMutex
	syncChannel       chan SyncRequest
	stopChannel       chan bool
	isRunning         bool
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
	return &CacheManager{
		cache:       NewCacheData(),
		redisClient: redisClient,
		userConfig:  userConfig,
		syncChannel: make(chan SyncRequest, 1000), // Buffer for async sync requests
		stopChannel: make(chan bool, 1),
		isRunning:   false,
	}
}

// Start initializes the cache by loading data from Redis and starts the sync worker
func (cm *CacheManager) Start() error {
	glog.Infof("Starting cache manager")

	// Load cache data from Redis
	cache, err := cm.redisClient.LoadCacheFromRedis()
	if err != nil {
		glog.Errorf("Failed to load cache from Redis: %v", err)
		return err
	}

	cm.mutex.Lock()
	cm.cache = cache

	// Ensure all users from userConfig.UserList have their data structures initialized
	// 确保userConfig.UserList中的所有用户都有其数据结构初始化
	if cm.userConfig != nil && len(cm.userConfig.UserList) > 0 {
		glog.Infof("Initializing data structures for configured users")

		for _, userID := range cm.userConfig.UserList {
			if _, exists := cm.cache.Users[userID]; !exists {
				glog.Infof("Creating data structure for new user: %s", userID)
				cm.cache.Users[userID] = NewUserData()
			}
		}

		glog.Infof("User data structure initialization completed for %d users", len(cm.userConfig.UserList))
	}

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

	cm.mutex.Lock()
	if cm.isRunning {
		cm.isRunning = false
		cm.stopChannel <- true
	}
	cm.mutex.Unlock()

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
// SetHydrationNotifier 设置水合通知器以进行实时更新
func (cm *CacheManager) SetHydrationNotifier(notifier HydrationNotifier) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.hydrationNotifier = notifier
	glog.Infof("Hydration notifier set successfully")
}

// SetAppData sets app data in cache and triggers sync to Redis
func (cm *CacheManager) SetAppData(userID, sourceID string, dataType AppDataType, data map[string]interface{}) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Validate user configuration if available
	if cm.userConfig != nil {
		// Check if user is valid
		isValidUser := false
		for _, validUser := range cm.userConfig.UserList {
			if validUser == userID {
				isValidUser = true
				break
			}
		}

		// Allow guest users if enabled, otherwise reject invalid users
		if !isValidUser && !cm.userConfig.GuestEnabled {
			glog.Warningf("User '%s' is not authorized to set app data", userID)
			return fmt.Errorf("user '%s' is not authorized", userID)
		}
	}

	// Ensure user exists
	if _, exists := cm.cache.Users[userID]; !exists {
		cm.cache.Users[userID] = NewUserData()
	}

	// Check source limit for user
	userData := cm.cache.Users[userID]
	userData.Mutex.Lock()

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
				userData.Mutex.Unlock()
				glog.Warningf("User '%s' has reached maximum sources limit (%d)", userID, maxSources)
				return fmt.Errorf("user '%s' has reached maximum sources limit (%d)", userID, maxSources)
			}
		}

		userData.Sources[sourceID] = NewSourceData()
	}
	userData.Mutex.Unlock()

	// Set the data
	sourceData := userData.Sources[sourceID]
	sourceData.Mutex.Lock()

	// Log image analysis information if present
	// 如果存在镜像分析信息则记录
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
		appData := NewAppStateLatestData(data)
		appData.Timestamp = time.Now().Unix()
		sourceData.AppStateLatest = append(sourceData.AppStateLatest, appData)
	case AppInfoLatest:
		appData := NewAppInfoLatestData(data)
		if appData == nil {
			glog.Warningf("Failed to create AppInfoLatestData from data for user=%s, source=%s - data may be invalid", userID, sourceID)
			return fmt.Errorf("invalid app data: NewAppInfoLatestData returned nil")
		}
		appData.Timestamp = time.Now().Unix()
		sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, appData)
	case AppInfoLatestPending:
		// Check if this is a complete market data structure
		// 检查这是否是完整的市场数据结构
		if appsData, hasApps := data["apps"].(map[string]interface{}); hasApps {
			// This is complete market data, extract individual apps
			// 这是完整的市场数据，提取单个应用
			glog.Infof("Processing complete market data with %d apps for user=%s, source=%s", len(appsData), userID, sourceID)

			// Also store the "others" data (hash, version, topics, etc.)
			// 同时存储"others"数据（hash、version、topics等）
			others := &types.Others{}
			if version, ok := data["version"].(string); ok {
				others.Version = version
			}
			if hash, ok := data["hash"].(string); ok {
				others.Hash = hash
			}

			// Extract topics, recommends, pages if present
			// 提取topics、recommends、pages（如果存在）
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
			// 在源数据中存储others数据
			sourceData.Others = others

			// Process each individual app
			// 处理每个单独的应用
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
			// 这可能是包含嵌套应用结构的市场数据，尝试提取应用
			glog.Infof("DEBUG: CALL POINT 2 - Processing potential market data for user=%s, source=%s", userID, sourceID)
			glog.Infof("DEBUG: CALL POINT 2 - Data before processing: %+v", data)

			// Check if this is market data with nested structure
			// 检查是否是具有嵌套结构的市场数据
			if dataSection, hasData := data["data"].(map[string]interface{}); hasData {
				if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
					// This is market data with apps - process each app individually
					// 这是包含应用的市场数据 - 逐个处理每个应用
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
				// 这可能是实际的单个应用数据，尝试直接处理
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

		// Notify hydrator about pending data update for immediate task creation
		// 通知水合器关于待处理数据更新以立即创建任务
		if cm.hydrationNotifier != nil && len(sourceData.AppInfoLatestPending) > 0 {
			glog.Infof("Notifying hydrator about pending data update for user=%s, source=%s", userID, sourceID)
			go cm.hydrationNotifier.NotifyPendingDataUpdate(userID, sourceID, data)
		}
		// Note: Other data type is now part of AppInfoLatestPendingData.Others field
		// 注意：Other数据类型现在是AppInfoLatestPendingData.Others字段的一部分
		// case Other:
		//     return sourceData.Other // This field no longer exists
	}

	sourceData.Mutex.Unlock()

	// Trigger async sync to Redis
	cm.requestSync(SyncRequest{
		UserID:   userID,
		SourceID: sourceID,
		Type:     SyncSource,
	})

	glog.Infof("Set app data for user=%s, source=%s, type=%s", userID, sourceID, dataType)
	return nil
}

// GetAppData retrieves specific app data from cache
func (cm *CacheManager) GetAppData(userID, sourceID string, dataType AppDataType) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if userData, exists := cm.cache.Users[userID]; exists {
		if sourceData, exists := userData.Sources[sourceID]; exists {
			sourceData.Mutex.RLock()
			defer sourceData.Mutex.RUnlock()

			switch dataType {
			case AppInfoHistory:
				return sourceData.AppInfoHistory
			case AppStateLatest:
				return sourceData.AppStateLatest
			case AppInfoLatest:
				return sourceData.AppInfoLatest
			case AppInfoLatestPending:
				return sourceData.AppInfoLatestPending
				// Note: Other data type is now part of AppInfoLatestPendingData.Others field
				// 注意：Other数据类型现在是AppInfoLatestPendingData.Others字段的一部分
				// case Other:
				//     return sourceData.Other // This field no longer exists
			}
		}
	}
	return nil
}

// RemoveUserData removes user data from cache and Redis
func (cm *CacheManager) RemoveUserData(userID string) error {
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

// GetCacheStats returns cache statistics
func (cm *CacheManager) GetCacheStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_users"] = len(cm.cache.Users)
	stats["is_running"] = cm.isRunning

	totalSources := 0
	for _, userData := range cm.cache.Users {
		userData.Mutex.RLock()
		totalSources += len(userData.Sources)
		userData.Mutex.RUnlock()
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

// GetAllUsersData returns all users data from cache for inspection/debugging
func (cm *CacheManager) GetAllUsersData() map[string]*UserData {
	// First, get the list of user IDs with minimal locking
	// 首先，用最小锁定获取用户ID列表
	cm.mutex.RLock()
	userIDs := make([]string, 0, len(cm.cache.Users))
	for userID := range cm.cache.Users {
		userIDs = append(userIDs, userID)
	}
	cm.mutex.RUnlock()

	// Then, get each user's data individually to avoid nested locking
	// 然后，逐个获取每个用户的数据以避免嵌套锁定
	result := make(map[string]*UserData)
	for _, userID := range userIDs {
		cm.mutex.RLock()
		if userData, exists := cm.cache.Users[userID]; exists {
			// Make a shallow copy to avoid holding locks too long
			// 进行浅拷贝以避免长时间持有锁
			userDataCopy := &UserData{
				Sources: make(map[string]*SourceData),
			}

			userData.Mutex.RLock()
			// Copy Hash field and source data references
			// 复制Hash字段和源数据引用
			userDataCopy.Hash = userData.Hash
			for sourceID, sourceData := range userData.Sources {
				userDataCopy.Sources[sourceID] = sourceData
			}
			userData.Mutex.RUnlock()

			result[userID] = userDataCopy
		}
		cm.mutex.RUnlock()
	}

	return result
}

// UpdateUserConfig updates the user configuration and ensures all users have data structures
// UpdateUserConfig 更新用户配置并确保所有用户都有数据结构
func (cm *CacheManager) UpdateUserConfig(newUserConfig *UserConfig) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if newUserConfig == nil {
		return fmt.Errorf("user config cannot be nil")
	}

	glog.Infof("Updating user configuration")

	// oldUserConfig := cm.userConfig // Commented out as it's only used in optional removal logic
	cm.userConfig = newUserConfig

	// Initialize data structures for new users in the updated configuration
	// 为更新配置中的新用户初始化数据结构
	if len(newUserConfig.UserList) > 0 {
		for _, userID := range newUserConfig.UserList {
			if _, exists := cm.cache.Users[userID]; !exists {
				glog.Infof("Creating data structure for newly configured user: %s", userID)
				cm.cache.Users[userID] = NewUserData()

				// Trigger sync to Redis for the new user
				// 为新用户触发Redis同步
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
	// 可选择移除不再在配置中的用户（默认注释以保留数据）
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
// SyncUserListToCache 确保当前userConfig中的所有用户都有初始化的数据结构
func (cm *CacheManager) SyncUserListToCache() error {
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
			// 为新用户触发Redis同步
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
// CleanupInvalidPendingData 移除缺少必需标识符的无效待处理数据条目
func (cm *CacheManager) CleanupInvalidPendingData() int {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	totalCleaned := 0

	for userID, userData := range cm.cache.Users {
		userData.Mutex.Lock()
		for sourceID, sourceData := range userData.Sources {
			sourceData.Mutex.Lock()

			originalCount := len(sourceData.AppInfoLatestPending)
			cleanedPendingData := make([]*AppInfoLatestPendingData, 0, originalCount)

			for _, pendingData := range sourceData.AppInfoLatestPending {
				// Check if this pending data has valid identifiers
				// 检查此待处理数据是否有有效标识符
				isValid := false

				if pendingData.RawData != nil {
					// Check for valid ID or AppID
					// 检查有效的ID或AppID
					if (pendingData.RawData.ID != "" && pendingData.RawData.ID != "0") ||
						(pendingData.RawData.AppID != "" && pendingData.RawData.AppID != "0") ||
						(pendingData.RawData.Name != "" && pendingData.RawData.Name != "unknown") {
						isValid = true
					}
				}

				if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
					// Also check AppInfo.AppEntry for valid identifiers
					// 也检查AppInfo.AppEntry中的有效标识符
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
			// 用清理后的列表更新源数据
			sourceData.AppInfoLatestPending = cleanedPendingData

			if originalCount != len(cleanedPendingData) {
				glog.Infof("Cleaned %d invalid pending data entries for user=%s, source=%s",
					originalCount-len(cleanedPendingData), userID, sourceID)

				// Trigger sync to Redis to persist the cleanup
				// 触发Redis同步以持久化清理
				if cm.isRunning {
					cm.requestSync(SyncRequest{
						UserID:   userID,
						SourceID: sourceID,
						Type:     SyncSource,
					})
				}
			}

			sourceData.Mutex.Unlock()
		}
		userData.Mutex.Unlock()
	}

	if totalCleaned > 0 {
		glog.Infof("Cleanup completed: removed %d invalid pending data entries across all users", totalCleaned)
	}

	return totalCleaned
}
