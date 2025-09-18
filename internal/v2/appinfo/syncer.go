package appinfo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
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
	cacheManager    atomic.Pointer[CacheManager] // Use atomic.Pointer for thread-safe pointer assignment
	syncInterval    time.Duration
	stopChan        chan struct{}
	isRunning       atomic.Bool               // Use atomic.Bool for thread-safe boolean operations
	mutex           sync.RWMutex              // Keep mutex for steps slice operations
	settingsManager *settings.SettingsManager // Settings manager for data source information
}

// NewSyncer creates a new syncer with the given steps
func NewSyncer(cache *CacheData, syncInterval time.Duration, settingsManager *settings.SettingsManager) *Syncer {
	return &Syncer{
		steps:           make([]syncerfn.SyncStep, 0),
		cache:           cache,
		cacheManager:    atomic.Pointer[CacheManager]{}, // Initialize with nil
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
		isRunning:       atomic.Bool{}, // Initialize with false
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
	if s.isRunning.Load() {
		s.mutex.Unlock()
		return fmt.Errorf("syncer is already running")
	}
	s.isRunning.Store(true)
	s.mutex.Unlock()

	log.Printf("Starting syncer with %d steps, sync interval: %v", len(s.steps), s.syncInterval)

	go s.syncLoop(ctx)
	return nil
}

// Stop stops the synchronization process
func (s *Syncer) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isRunning.Load() {
		return
	}

	log.Println("Stopping syncer...")
	close(s.stopChan)
	s.isRunning.Store(false)
}

// IsRunning returns whether the syncer is currently running
func (s *Syncer) IsRunning() bool {
	return s.isRunning.Load()
}

// syncLoop runs the main synchronization loop
func (s *Syncer) syncLoop(ctx context.Context) {
	defer func() {
		s.mutex.Lock()
		s.isRunning.Store(false)
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
	log.Println("==================== SYNC CYCLE STARTED ====================")
	log.Println("Starting sync cycle")
	startTime := time.Now()

	// Get available data sources
	activeSources := s.settingsManager.GetActiveMarketSources()
	if len(activeSources) == 0 {
		log.Println("==================== SYNC CYCLE FAILED ====================")
		return fmt.Errorf("no active market sources available")
	}

	log.Printf("Found %d active market sources", len(activeSources))

	// Try each source in priority order until one succeeds
	var lastError error
	for _, source := range activeSources {
		log.Printf("Trying market source: %s (%s)", source.ID, source.BaseURL)

		if err := s.executeSyncCycleWithSource(ctx, source); err != nil {
			log.Printf("Failed to sync with source %s: %v", source.ID, err)
			lastError = err
			continue
		}

		// Success with this source
		duration := time.Since(startTime)
		log.Printf("Sync cycle completed successfully with source %s in %v", source.ID, duration)

	}
	log.Println("==================== SYNC CYCLE COMPLETED ====================")

	// All sources failed
	log.Println("==================== SYNC CYCLE FAILED ====================")
	return fmt.Errorf("all market sources failed, last error: %w", lastError)
}

// executeSyncCycleWithSource executes sync cycle with a specific market source
func (s *Syncer) executeSyncCycleWithSource(ctx context.Context, source *settings.MarketSource) error {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in executeSyncCycleWithSource: %v", r)
		}
	}()

	log.Printf("-------------------- SOURCE SYNC STARTED: %s --------------------", source.ID)

	// Create sync context with CacheManager for unified lock strategy
	var cacheManager types.CacheManagerInterface
	if cm := s.cacheManager.Load(); cm != nil {
		cacheManager = cm
	}
	syncContext := syncerfn.NewSyncContextWithManager(s.cache, cacheManager)

	// Set version for API requests using utils function
	version := getVersionForSync()
	syncContext.SetVersion(version)
	log.Printf("Set version for sync cycle: %s", version)

	// Set the current market source in sync context
	syncContext.SetMarketSource(source)

	// Create a timeout context for the entire sync cycle
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Minute) // 10 minute timeout for entire sync
	defer cancel()

	steps := s.GetSteps()
	for i, step := range steps {
		log.Printf("======== SYNC STEP %d/%d STARTED: %s ========", i+1, len(steps), step.GetStepName())
		stepStartTime := time.Now()

		// Check if step can be skipped
		if step.CanSkip(syncCtx, syncContext) {
			log.Printf("Skipping step %d: %s", i+1, step.GetStepName())
			log.Printf("======== SYNC STEP %d/%d SKIPPED: %s ========", i+1, len(steps), step.GetStepName())
			continue
		}

		// Execute step with timeout check
		stepErr := make(chan error, 1)
		go func() {
			stepErr <- step.Execute(syncCtx, syncContext)
		}()

		// Wait for step completion or timeout with progress monitoring
		stepTimeout := 15 * time.Minute // 15 minute timeout per step
		stepTimer := time.NewTimer(stepTimeout)
		defer stepTimer.Stop()

		select {
		case err := <-stepErr:
			if err != nil {
				log.Printf("Step %d (%s) failed: %v", i+1, step.GetStepName(), err)
				log.Printf("======== SYNC STEP %d/%d FAILED: %s ========", i+1, len(steps), step.GetStepName())
				log.Printf("-------------------- SOURCE SYNC FAILED: %s --------------------", source.ID)
				return fmt.Errorf("step %d failed: %w", i+1, err)
			}
		case <-stepTimer.C:
			log.Printf("Step %d (%s) timed out after %v", i+1, step.GetStepName(), stepTimeout)
			log.Printf("======== SYNC STEP %d/%d TIMEOUT: %s ========", i+1, len(steps), step.GetStepName())
			log.Printf("-------------------- SOURCE SYNC TIMEOUT: %s --------------------", source.ID)
			return fmt.Errorf("step %d timed out after %v", i+1, stepTimeout)
		case <-syncCtx.Done():
			log.Printf("Step %d (%s) timed out or context cancelled", i+1, step.GetStepName())
			log.Printf("======== SYNC STEP %d/%d TIMEOUT: %s ========", i+1, len(steps), step.GetStepName())
			log.Printf("-------------------- SOURCE SYNC TIMEOUT: %s --------------------", source.ID)
			return fmt.Errorf("step %d timed out: %w", i+1, syncCtx.Err())
		}

		stepDuration := time.Since(stepStartTime)
		log.Printf("Step %d (%s) completed in %v", i+1, step.GetStepName(), stepDuration)
		log.Printf("======== SYNC STEP %d/%d COMPLETED: %s ========", i+1, len(steps), step.GetStepName())
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
	// Modified condition: Store data if we have LatestData, regardless of hash match status
	if syncContext.LatestData != nil {
		log.Printf("Storing complete data to app-info-latest-pending for all users")
		log.Printf("Sync context status - HashMatches: %t, RemoteHash: %s, LocalHash: %s",
			syncContext.HashMatches, syncContext.RemoteHash, syncContext.LocalHash)

		// Convert LatestData to the format expected by cache
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

		sourceID := source.ID // Use market source name as source ID
		log.Printf("Using source ID: %s for data storage", sourceID)

		// Get all existing user IDs with minimal locking
		var userIDs []string
		// Use CacheManager if available, otherwise use direct cache access
		if cacheManager := s.cacheManager.Load(); cacheManager != nil {
			// Use CacheManager's lock
			cacheManager.mutex.RLock()
			for userID := range s.cache.Users {
				userIDs = append(userIDs, userID)
			}
			cacheManager.mutex.RUnlock()

			// If no users exist, create a system user as fallback
			if len(userIDs) == 0 {
				log.Printf("[LOCK] cacheManager.mutex.Lock() @syncer:createSystemUser Start")
				__lockStartCreate := time.Now()
				cacheManager.mutex.Lock()
				log.Printf("[LOCK] cacheManager.mutex.Lock() @syncer:createSystemUser Success (wait=%v)", time.Since(__lockStartCreate))
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
				cacheManager.mutex.Unlock()
			}
		} else {
			// Fallback to direct cache access without lock (not recommended)
			log.Printf("Warning: CacheManager not available, using direct cache access")
			for userID := range s.cache.Users {
				userIDs = append(userIDs, userID)
			}
		}

		log.Printf("Storing data for %d users: %v", len(userIDs), userIDs)

		// Determine storage method based on CacheManager availability
		if cacheManager := s.cacheManager.Load(); cacheManager != nil {
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

	log.Printf("-------------------- SOURCE SYNC COMPLETED: %s --------------------", source.ID)
	return nil
}

// storeDataDirectly stores data directly to cache without going through CacheManager
func (s *Syncer) storeDataDirectly(userID, sourceID string, completeData map[string]interface{}) {
	// Use CacheManager's lock if available
	if cacheManager := s.cacheManager.Load(); cacheManager != nil {
		log.Printf("[LOCK] cacheManager.mutex.Lock() @syncer:storeDataDirectly Start")
		__lockStartStore := time.Now()
		cacheManager.mutex.Lock()
		log.Printf("[LOCK] cacheManager.mutex.Lock() @syncer:storeDataDirectly Success (wait=%v)", time.Since(__lockStartStore))
		defer cacheManager.mutex.Unlock()
	} else {
		// Fallback: no lock protection (not recommended)
		log.Printf("Warning: CacheManager not available for storeDataDirectly")
	}

	userData := s.cache.Users[userID]

	// Ensure source data exists for this user
	if _, exists := userData.Sources[sourceID]; !exists {
		userData.Sources[sourceID] = NewSourceData()
		log.Printf("Created new source data for user: %s, source: %s", userID, sourceID)
	}

	sourceData := userData.Sources[sourceID]

	// Check if this is a local source - skip syncer storage for local sources
	if sourceData.Type == types.SourceDataTypeLocal {
		log.Printf("Skipping syncer data storage for local source: user=%s, source=%s", userID, sourceID)
		return
	}

	// Extract Others data from complete data
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

						// Handle Data field
						if dataField, ok := recMap["data"].(map[string]interface{}); ok {
							recommend.Data = &types.RecommendData{}

							if title, ok := dataField["title"].(map[string]interface{}); ok {
								recommend.Data.Title = make(map[string]string)
								for k, v := range title {
									if str, ok := v.(string); ok {
										recommend.Data.Title[k] = str
									}
								}
							}

							if description, ok := dataField["description"].(map[string]interface{}); ok {
								recommend.Data.Description = make(map[string]string)
								for k, v := range description {
									if str, ok := v.(string); ok {
										recommend.Data.Description[k] = str
									}
								}
							}
						}

						// Handle Source field
						if source, ok := recMap["source"].(string); ok {
							recommend.Source = source
						}

						// Handle time fields
						if createdAt, ok := recMap["createdAt"].(string); ok {
							if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
								recommend.CreatedAt = t
							}
						}
						if updatedAt, ok := recMap["updated_at"].(string); ok {
							if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
								recommend.UpdatedAt = t
							}
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
						if data, ok := topicMap["data"].(map[string]interface{}); ok {
							topicObj.Data = make(map[string]*types.TopicData)
							for lang, topicDataInterface := range data {
								if topicDataMap, ok := topicDataInterface.(map[string]interface{}); ok {
									topicData := &types.TopicData{}

									if group, ok := topicDataMap["group"].(string); ok {
										topicData.Group = group
									}
									if title, ok := topicDataMap["title"].(string); ok {
										topicData.Title = title
									}
									if des, ok := topicDataMap["des"].(string); ok {
										topicData.Des = des
									}
									if iconImg, ok := topicDataMap["iconimg"].(string); ok {
										topicData.IconImg = iconImg
									}
									if detailImg, ok := topicDataMap["detailimg"].(string); ok {
										topicData.DetailImg = detailImg
									}
									if richText, ok := topicDataMap["richtext"].(string); ok {
										topicData.RichText = richText
									}
									if apps, ok := topicDataMap["apps"].(string); ok {
										topicData.Apps = apps
									}
									if isDelete, ok := topicDataMap["isdelete"].(bool); ok {
										topicData.IsDelete = isDelete
									}

									topicObj.Data[lang] = topicData
								}
							}
						}
						if source, ok := topicMap["source"].(string); ok {
							topicObj.Source = source
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
						if title, ok := topicListMap["title"].(map[string]interface{}); ok {
							topicListObj.Title = make(map[string]string)
							for k, v := range title {
								if str, ok := v.(string); ok {
									topicListObj.Title[k] = str
								}
							}
						}
						if source, ok := topicListMap["source"].(string); ok {
							topicListObj.Source = source
						}
						others.TopicLists[i] = topicListObj
					}
				}
			}
		}

		// Store Others data in source
		sourceData.Others = others

		// Process each app individually
		if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
			// Clear existing AppInfoLatestPending list before adding new data
			// This ensures we don't accumulate old data when hash doesn't match
			originalCount := len(sourceData.AppInfoLatestPending)
			sourceData.AppInfoLatestPending = sourceData.AppInfoLatestPending[:0] // Clear the slice
			log.Printf("Cleared %d existing AppInfoLatestPending entries for user: %s, source: %s", originalCount, userID, sourceID)

			for appID, appDataInterface := range appsData {
				if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
					// Create AppInfoLatestPendingData for this specific app using the basic function
					log.Printf("DEBUG: CALL POINT 3 - Processing app %s for user %s, source %s", appID, userID, sourceID)
					log.Printf("DEBUG: CALL POINT 3 - App data before calling NewAppInfoLatestPendingDataFromLegacyData: %+v", appDataMap)
					appData := NewAppInfoLatestPendingDataFromLegacyData(appDataMap)
					// Check if app data creation was successful
					if appData == nil {
						log.Printf("Warning: Skipping app %s for user %s, source %s - not recognized as valid app data", appID, userID, sourceID)
						// Log available keys for debugging
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

					// Store app data directly without label filtering (moved to detail_fetch_step.go)
					sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
					log.Printf("Successfully stored app data for app: %s, user: %s, source: %s", appID, userID, sourceID)
				}
			}

			log.Printf("Updated AppInfoLatestPending list with %d new entries for user: %s, source: %s",
				len(sourceData.AppInfoLatestPending), userID, sourceID)
		} else {
			log.Printf("No apps data found in complete data for user: %s, source: %s", userID, sourceID)
		}
	} else {
		log.Printf("No data section found in complete data for user: %s, source: %s", userID, sourceID)
	}
}

// storeDataDirectlyBatch stores data directly to cache without going through CacheManager
func (s *Syncer) storeDataDirectlyBatch(userIDs []string, sourceID string, completeData map[string]interface{}) {
	for _, userID := range userIDs {
		s.storeDataDirectly(userID, sourceID, completeData)
	}
}

// storeDataViaCacheManager stores data via CacheManager
func (s *Syncer) storeDataViaCacheManager(userIDs []string, sourceID string, completeData map[string]interface{}) {
	for _, userID := range userIDs {
		// Check if the source is local type - skip syncer operations for local sources
		if cacheManager := s.cacheManager.Load(); cacheManager != nil {
			cacheManager.mutex.RLock()
			userData, userExists := s.cache.Users[userID]
			if userExists {
				sourceData, sourceExists := userData.Sources[sourceID]
				if sourceExists {
					sourceType := sourceData.Type
					if sourceType == types.SourceDataTypeLocal {
						log.Printf("Skipping syncer CacheManager operation for local source: user=%s, source=%s", userID, sourceID)
						cacheManager.mutex.RUnlock()
						continue
					}
				}
			}
			cacheManager.mutex.RUnlock()
		}

		// Use CacheManager.SetAppData to trigger hydration notifications if available
		if cacheManager := s.cacheManager.Load(); cacheManager != nil {
			log.Printf("Using CacheManager to store data for user: %s, source: %s", userID, sourceID)
			err := cacheManager.SetAppData(userID, sourceID, AppInfoLatestPending, completeData)
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
	version := getVersionForSync()
	log.Printf("Using version for syncer steps: %s", version)

	// Get API endpoints configuration
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
func (s *Syncer) SetCacheManager(cacheManager *CacheManager) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.cacheManager.Store(cacheManager) // Use atomic.Store to set the pointer
}
