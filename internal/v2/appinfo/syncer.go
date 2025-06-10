package appinfo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"market/internal/v2/appinfo/syncerfn"
	"market/internal/v2/settings"
	"market/internal/v2/types"
	"market/internal/v2/utils"
)

// Syncer manages the synchronization process with multiple steps
type Syncer struct {
	steps           []syncerfn.SyncStep
	cache           *CacheData
	cacheManager    *CacheManager // 添加CacheManager引用用于通知
	syncInterval    time.Duration
	stopChan        chan struct{}
	isRunning       bool
	mutex           sync.RWMutex
	settingsManager *settings.SettingsManager // 设置管理器用于获取数据源信息
}

// NewSyncer creates a new syncer with the given steps
func NewSyncer(cache *CacheData, syncInterval time.Duration, settingsManager *settings.SettingsManager) *Syncer {
	return &Syncer{
		steps:           make([]syncerfn.SyncStep, 0),
		cache:           cache,
		cacheManager:    nil, // 将在模块初始化时设置
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
		isRunning:       false,
		settingsManager: settingsManager,
	}
}

// AddStep adds a step to the syncer
func (s *Syncer) AddStep(step syncerfn.SyncStep) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.steps = append(s.steps, step)
}

// RemoveStep removes a step by index
func (s *Syncer) RemoveStep(index int) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if index < 0 || index >= len(s.steps) {
		return fmt.Errorf("step index %d out of range", index)
	}

	s.steps = append(s.steps[:index], s.steps[index+1:]...)
	return nil
}

// GetSteps returns a copy of all steps
func (s *Syncer) GetSteps() []syncerfn.SyncStep {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	steps := make([]syncerfn.SyncStep, len(s.steps))
	copy(steps, s.steps)
	return steps
}

// Start begins the synchronization process
func (s *Syncer) Start(ctx context.Context) error {
	s.mutex.Lock()
	if s.isRunning {
		s.mutex.Unlock()
		return fmt.Errorf("syncer is already running")
	}
	s.isRunning = true
	s.mutex.Unlock()

	log.Printf("Starting syncer with %d steps, sync interval: %v", len(s.steps), s.syncInterval)

	go s.syncLoop(ctx)
	return nil
}

// Stop stops the synchronization process
func (s *Syncer) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isRunning {
		return
	}

	log.Println("Stopping syncer...")
	close(s.stopChan)
	s.isRunning = false
}

// IsRunning returns whether the syncer is currently running
func (s *Syncer) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.isRunning
}

// syncLoop runs the main synchronization loop
func (s *Syncer) syncLoop(ctx context.Context) {
	defer func() {
		s.mutex.Lock()
		s.isRunning = false
		s.mutex.Unlock()
		log.Println("Syncer stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			log.Println("Context cancelled, stopping syncer")
			return
		case <-s.stopChan:
			log.Println("Stop signal received, stopping syncer")
			return
		default:
			// Execute sync cycle
			if err := s.executeSyncCycle(ctx); err != nil {
				log.Printf("Sync cycle failed: %v", err)
			}

			// Wait for next cycle or stop signal
			select {
			case <-ctx.Done():
				return
			case <-s.stopChan:
				return
			case <-time.After(s.syncInterval):
				// Continue to next cycle
			}
		}
	}
}

// getVersionForSync returns the version to use for sync operations with fallback
// 返回用于同步操作的版本号，包含回退逻辑
func getVersionForSync() string {
	if version, err := utils.GetTerminusVersionValue(); err == nil {
		return version
	} else {
		log.Printf("Failed to get version, using fallback: %v", err)
		return "1.12.0" // fallback version
	}
}

// executeSyncCycle executes one complete synchronization cycle
func (s *Syncer) executeSyncCycle(ctx context.Context) error {
	log.Println("Starting sync cycle")
	startTime := time.Now()

	// Get available data sources
	// 获取可用的数据源
	activeSources := s.settingsManager.GetActiveMarketSources()
	if len(activeSources) == 0 {
		return fmt.Errorf("no active market sources available")
	}

	log.Printf("Found %d active market sources", len(activeSources))

	// Try each source in priority order until one succeeds
	// 按优先级顺序尝试每个源，直到有一个成功
	var lastError error
	for _, source := range activeSources {
		log.Printf("Trying market source: %s (%s)", source.Name, source.BaseURL)

		if err := s.executeSyncCycleWithSource(ctx, source); err != nil {
			log.Printf("Failed to sync with source %s: %v", source.Name, err)
			lastError = err
			continue
		}

		// Success with this source
		// 使用此源成功
		duration := time.Since(startTime)
		log.Printf("Sync cycle completed successfully with source %s in %v", source.Name, duration)
		return nil
	}

	// All sources failed
	// 所有源都失败了
	return fmt.Errorf("all market sources failed, last error: %w", lastError)
}

// executeSyncCycleWithSource executes sync cycle with a specific market source
// 使用特定市场源执行同步周期
func (s *Syncer) executeSyncCycleWithSource(ctx context.Context, source *settings.MarketSource) error {
	syncContext := syncerfn.NewSyncContext(s.cache)

	// Set version for API requests using utils function
	// 使用utils函数为API请求设置版本
	version := getVersionForSync()
	syncContext.SetVersion(version)
	log.Printf("Set version for sync cycle: %s", version)

	// Set the current market source in sync context
	// 在同步上下文中设置当前市场源
	syncContext.SetMarketSource(source)

	steps := s.GetSteps()
	for i, step := range steps {
		stepStartTime := time.Now()

		// Check if step can be skipped
		if step.CanSkip(ctx, syncContext) {
			log.Printf("Skipping step %d: %s", i+1, step.GetStepName())
			continue
		}

		// Execute step
		if err := step.Execute(ctx, syncContext); err != nil {
			log.Printf("Step %d (%s) failed: %v", i+1, step.GetStepName(), err)
			return fmt.Errorf("step %d failed: %w", i+1, err)
		}

		stepDuration := time.Since(stepStartTime)
		log.Printf("Step %d (%s) completed in %v", i+1, step.GetStepName(), stepDuration)
	}

	// Report any errors collected during the process
	if syncContext.HasErrors() {
		errors := syncContext.GetErrors()
		log.Printf("Sync cycle completed with %d errors:", len(errors))
		for i, err := range errors {
			log.Printf("  Error %d: %v", i+1, err)
		}
	}

	// Store complete data to app-info-latest-pending after successful sync
	// 成功同步后将完整数据存储到所有用户的app-info-latest-pending
	// Modified condition: Store data if we have LatestData, regardless of hash match status
	// 修改条件：如果有LatestData就存储数据，不管hash是否匹配
	if syncContext.LatestData != nil {
		log.Printf("Storing complete data to app-info-latest-pending for all users")
		log.Printf("Sync context status - HashMatches: %t, RemoteHash: %s, LocalHash: %s",
			syncContext.HashMatches, syncContext.RemoteHash, syncContext.LocalHash)

		// Convert LatestData to the format expected by cache
		// 将LatestData转换为缓存期望的格式
		completeData := map[string]interface{}{
			"version": syncContext.LatestData.Version,
			"data": map[string]interface{}{
				"apps":        syncContext.LatestData.Data.Apps,
				"recommends":  syncContext.LatestData.Data.Recommends,
				"pages":       syncContext.LatestData.Data.Pages,
				"topics":      syncContext.LatestData.Data.Topics,
				"topic_lists": syncContext.LatestData.Data.TopicLists,
			},
		}

		sourceID := source.Name // Use market source name as source ID
		log.Printf("Using source ID: %s for data storage", sourceID)

		// Get all existing user IDs with minimal locking
		// 用最小锁定获取所有现有的用户ID
		s.cache.Mutex.RLock()
		var userIDs []string
		for userID := range s.cache.Users {
			userIDs = append(userIDs, userID)
		}
		s.cache.Mutex.RUnlock()

		// If no users exist, create a system user as fallback
		// 如果没有用户存在，创建系统用户作为回退
		if len(userIDs) == 0 {
			s.cache.Mutex.Lock()
			// Double-check after acquiring write lock
			if len(s.cache.Users) == 0 {
				systemUserID := "system"
				s.cache.Users[systemUserID] = NewUserData()
				userIDs = append(userIDs, systemUserID)
				log.Printf("No existing users found, created system user as fallback")
			} else {
				// Users were added by another goroutine
				for userID := range s.cache.Users {
					userIDs = append(userIDs, userID)
				}
			}
			s.cache.Mutex.Unlock()
		}

		log.Printf("Storing data for %d users: %v", len(userIDs), userIDs)

		// Determine storage method based on CacheManager availability
		// 根据CacheManager的可用性确定存储方法
		if s.cacheManager != nil {
			log.Printf("Using CacheManager for data storage with hydration notifications")
			s.storeDataViaCacheManager(userIDs, sourceID, completeData)
		} else {
			log.Printf("CacheManager not available, using direct cache storage")
			s.storeDataDirectlyBatch(userIDs, sourceID, completeData)
		}
	} else {
		log.Printf("WARNING: No LatestData available in sync context, skipping data storage")
		log.Printf("Sync context status - HashMatches: %t, RemoteHash: %s, LocalHash: %s",
			syncContext.HashMatches, syncContext.RemoteHash, syncContext.LocalHash)
	}

	return nil
}

// storeDataDirectly stores data directly to cache without going through CacheManager
// storeDataDirectly 直接存储数据到缓存，不通过CacheManager
func (s *Syncer) storeDataDirectly(userID, sourceID string, completeData map[string]interface{}) {
	userData := s.cache.Users[userID]
	userData.Mutex.Lock()
	defer userData.Mutex.Unlock()

	// Ensure source data exists for this user
	// 确保此用户的源数据存在
	if _, exists := userData.Sources[sourceID]; !exists {
		userData.Sources[sourceID] = NewSourceData()
		log.Printf("Created new source data for user: %s, source: %s", userID, sourceID)
	}

	sourceData := userData.Sources[sourceID]
	sourceData.Mutex.Lock()
	defer sourceData.Mutex.Unlock()

	// Extract Others data from complete data
	// 从完整数据中提取Others数据
	others := &types.Others{}

	// Extract version and hash
	if version, ok := completeData["version"].(string); ok {
		others.Version = version
	}

	if dataSection, ok := completeData["data"].(map[string]interface{}); ok {
		// Convert recommends data
		if recommendsData, hasRecommends := dataSection["recommends"]; hasRecommends {
			if recommendsList, ok := recommendsData.([]interface{}); ok {
				others.Recommends = make([]*types.Recommend, len(recommendsList))
				for i, rec := range recommendsList {
					if recMap, ok := rec.(map[string]interface{}); ok {
						recommend := &types.Recommend{}
						if name, ok := recMap["name"].(string); ok {
							recommend.Name = name
						}
						if desc, ok := recMap["description"].(string); ok {
							recommend.Description = desc
						}
						if content, ok := recMap["content"].(string); ok {
							recommend.Content = content
						}
						others.Recommends[i] = recommend
					}
				}
			}
		}

		// Convert pages data
		if pagesData, hasPages := dataSection["pages"]; hasPages {
			if pagesList, ok := pagesData.([]interface{}); ok {
				others.Pages = make([]*types.Page, len(pagesList))
				for i, page := range pagesList {
					if pageMap, ok := page.(map[string]interface{}); ok {
						pageObj := &types.Page{}
						if category, ok := pageMap["category"].(string); ok {
							pageObj.Category = category
						}
						if content, ok := pageMap["content"].(string); ok {
							pageObj.Content = content
						}
						others.Pages[i] = pageObj
					}
				}
			}
		}

		// Convert topics data
		if topicsData, hasTopics := dataSection["topics"]; hasTopics {
			if topicsList, ok := topicsData.([]interface{}); ok {
				others.Topics = make([]*types.Topic, len(topicsList))
				for i, topic := range topicsList {
					if topicMap, ok := topic.(map[string]interface{}); ok {
						topicObj := &types.Topic{}
						if name, ok := topicMap["name"].(string); ok {
							topicObj.Name = name
						}
						if intro, ok := topicMap["introduction"].(string); ok {
							topicObj.Introduction = intro
						}
						if desc, ok := topicMap["des"].(string); ok {
							topicObj.Des = desc
						}
						if iconImg, ok := topicMap["iconimg"].(string); ok {
							topicObj.IconImg = iconImg
						}
						if detailImg, ok := topicMap["detailimg"].(string); ok {
							topicObj.DetailImg = detailImg
						}
						if richText, ok := topicMap["richtext"].(string); ok {
							topicObj.RichText = richText
						}
						if apps, ok := topicMap["apps"].(string); ok {
							topicObj.Apps = apps
						}
						if isDelete, ok := topicMap["isdelete"].(bool); ok {
							topicObj.IsDelete = isDelete
						}
						others.Topics[i] = topicObj
					}
				}
			}
		}

		// Convert topic_lists data
		if topicListsData, hasTopicLists := dataSection["topic_lists"]; hasTopicLists {
			if topicListsList, ok := topicListsData.([]interface{}); ok {
				others.TopicLists = make([]*types.TopicList, len(topicListsList))
				for i, topicList := range topicListsList {
					if topicListMap, ok := topicList.(map[string]interface{}); ok {
						topicListObj := &types.TopicList{}
						if name, ok := topicListMap["name"].(string); ok {
							topicListObj.Name = name
						}
						if listType, ok := topicListMap["type"].(string); ok {
							topicListObj.Type = listType
						}
						if desc, ok := topicListMap["description"].(string); ok {
							topicListObj.Description = desc
						}
						if content, ok := topicListMap["content"].(string); ok {
							topicListObj.Content = content
						}
						others.TopicLists[i] = topicListObj
					}
				}
			}
		}

		// Store Others data in source
		// 在源数据中存储Others数据
		sourceData.Others = others

		// Process each app individually
		// 单独处理每个应用
		if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
			for appID, appDataInterface := range appsData {
				if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
					// Create AppInfoLatestPendingData for this specific app using the basic function
					// 使用基础函数为这个特定应用创建AppInfoLatestPendingData
					log.Printf("DEBUG: CALL POINT 3 - Processing app %s for user %s, source %s", appID, userID, sourceID)
					log.Printf("DEBUG: CALL POINT 3 - App data before calling NewAppInfoLatestPendingDataFromLegacyData: %+v", appDataMap)
					appData := NewAppInfoLatestPendingDataFromLegacyData(appDataMap)
					// Check if app data creation was successful
					// 检查应用数据创建是否成功
					if appData == nil {
						log.Printf("Warning: Skipping app %s for user %s, source %s - not recognized as valid app data", appID, userID, sourceID)
						// Log available keys for debugging
						// 记录可用键以供调试
						if appDataMap != nil {
							keys := make([]string, 0, len(appDataMap))
							for k := range appDataMap {
								keys = append(keys, k)
							}
							log.Printf("Available app data keys for %s: %v", appID, keys)
						}
						continue // Skip this app and continue with next one
					}
					appData.Version = others.Version

					sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
					log.Printf("Successfully stored app data for app: %s, user: %s, source: %s", appID, userID, sourceID)
				}
			}
		} else {
			log.Printf("No apps data found in complete data for user: %s, source: %s", userID, sourceID)
		}
	} else {
		log.Printf("No data section found in complete data for user: %s, source: %s", userID, sourceID)
	}
}

// storeDataDirectlyBatch stores data directly to cache without going through CacheManager
// storeDataDirectlyBatch 直接存储数据到缓存，不通过CacheManager
func (s *Syncer) storeDataDirectlyBatch(userIDs []string, sourceID string, completeData map[string]interface{}) {
	for _, userID := range userIDs {
		s.storeDataDirectly(userID, sourceID, completeData)
	}
}

// storeDataViaCacheManager stores data via CacheManager
// storeDataViaCacheManager 通过CacheManager存储数据
func (s *Syncer) storeDataViaCacheManager(userIDs []string, sourceID string, completeData map[string]interface{}) {
	for _, userID := range userIDs {
		// Use CacheManager.SetAppData to trigger hydration notifications if available
		// 使用CacheManager.SetAppData来触发水合通知（如果可用）
		if s.cacheManager != nil {
			log.Printf("Using CacheManager to store data for user: %s, source: %s", userID, sourceID)
			err := s.cacheManager.SetAppData(userID, sourceID, AppInfoLatestPending, completeData)
			if err != nil {
				log.Printf("Failed to store data via CacheManager for user: %s, source: %s, error: %v", userID, sourceID, err)
				// Fall back to direct cache access
				s.storeDataDirectly(userID, sourceID, completeData)
			} else {
				log.Printf("Successfully stored data via CacheManager for user: %s, source: %s", userID, sourceID)
			}
		} else {
			log.Printf("CacheManager not available, storing data directly for user: %s, source: %s", userID, sourceID)
			s.storeDataDirectly(userID, sourceID, completeData)
		}
	}
}

// CreateDefaultSyncer creates a syncer with default steps configured
func CreateDefaultSyncer(cache *CacheData, config SyncerConfig, settingsManager *settings.SettingsManager) *Syncer {
	syncer := NewSyncer(cache, config.SyncInterval, settingsManager)

	// Get version for API requests using utils function
	// 获取API请求版本号
	version := getVersionForSync()
	log.Printf("Using version for syncer steps: %s", version)

	// Get API endpoints configuration
	// 获取API端点配置
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints == nil {
		log.Printf("Warning: no API endpoints configuration found, using defaults")
		endpoints = &settings.APIEndpointsConfig{
			HashPath:   "/api/v1/appstore/hash",
			DataPath:   "/api/v1/appstore/info",
			DetailPath: "/api/v1/applications/info",
		}
	}

	// Add default steps with endpoint paths instead of full URLs
	// 使用端点路径而不是完整URL添加默认步骤
	syncer.AddStep(syncerfn.NewHashComparisonStep(endpoints.HashPath, settingsManager))
	syncer.AddStep(syncerfn.NewDataFetchStep(endpoints.DataPath, settingsManager))
	syncer.AddStep(syncerfn.NewDetailFetchStep(endpoints.DetailPath, version, settingsManager))

	log.Printf("Created syncer with API endpoints - Hash: %s, Data: %s, Detail: %s",
		endpoints.HashPath, endpoints.DataPath, endpoints.DetailPath)

	return syncer
}

// SyncerConfig holds configuration for the syncer
type SyncerConfig struct {
	SyncInterval time.Duration `json:"sync_interval"`
}

// DefaultSyncerConfig returns a default configuration
func DefaultSyncerConfig() SyncerConfig {
	return SyncerConfig{
		SyncInterval: 5 * time.Minute,
	}
}

// SetCacheManager sets the cache manager for hydration notifications
// SetCacheManager 设置缓存管理器以进行水合通知
func (s *Syncer) SetCacheManager(cacheManager *CacheManager) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cacheManager = cacheManager
}
