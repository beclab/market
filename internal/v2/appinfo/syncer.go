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

	"github.com/golang/glog"
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

	// Status tracking fields
	lastSyncTime        atomic.Value // time.Time
	lastSyncSuccess     atomic.Value // time.Time
	lastSyncError       atomic.Value // error (stored as string)
	currentStep         atomic.Value // string
	currentStepIndex    atomic.Int32 // int32
	totalSteps          atomic.Int32 // int32
	syncCount           atomic.Int64
	successCount        atomic.Int64
	failureCount        atomic.Int64
	consecutiveFailures atomic.Int64
	lastSyncDuration    atomic.Value // time.Duration
	currentSource       atomic.Value // string
	lastSyncedAppCount  atomic.Int64
	lastSyncDetails     atomic.Value // *SyncDetails
	statusMutex         sync.RWMutex // Mutex for complex status updates
}

// NewSyncer creates a new syncer with the given steps
func NewSyncer(cache *CacheData, syncInterval time.Duration, settingsManager *settings.SettingsManager) *Syncer {
	s := &Syncer{
		steps:           make([]syncerfn.SyncStep, 0),
		cache:           cache,
		cacheManager:    atomic.Pointer[CacheManager]{}, // Initialize with nil
		syncInterval:    syncInterval,
		stopChan:        make(chan struct{}),
		isRunning:       atomic.Bool{}, // Initialize with false
		settingsManager: settingsManager,
	}
	// Initialize atomic values
	s.lastSyncTime.Store(time.Time{})
	s.lastSyncSuccess.Store(time.Time{})
	s.lastSyncError.Store("")
	s.currentStep.Store("")
	s.lastSyncDuration.Store(time.Duration(0))
	s.currentSource.Store("")
	return s
}

// AddStep adds a step to the syncer
func (s *Syncer) AddStep(step syncerfn.SyncStep) {
	if !s.mutex.TryLock() {
		log.Printf("Failed to acquire lock for AddStep, skipping")
		return
	}
	defer s.mutex.Unlock()
	s.steps = append(s.steps, step)
}

// RemoveStep removes a step by index
func (s *Syncer) RemoveStep(index int) error {
	if !s.mutex.TryLock() {
		return fmt.Errorf("failed to acquire lock for RemoveStep")
	}
	defer s.mutex.Unlock()

	if index < 0 || index >= len(s.steps) {
		return fmt.Errorf("step index %d out of range", index)
	}

	s.steps = append(s.steps[:index], s.steps[index+1:]...)
	return nil
}

// GetSteps returns a copy of all steps
func (s *Syncer) GetSteps() []syncerfn.SyncStep {
	if !s.mutex.TryRLock() {
		log.Printf("Failed to acquire read lock for GetSteps, returning empty slice")
		return make([]syncerfn.SyncStep, 0)
	}
	defer s.mutex.RUnlock()

	steps := make([]syncerfn.SyncStep, len(s.steps))
	copy(steps, s.steps)
	return steps
}

// Start begins the synchronization process
func (s *Syncer) Start(ctx context.Context) error {
	if !s.mutex.TryLock() {
		return fmt.Errorf("failed to acquire lock for Start")
	}
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
	if !s.mutex.TryLock() {
		log.Printf("Failed to acquire lock for Stop, skipping")
		return
	}
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
		// Use TryLock for cleanup to avoid blocking
		if s.mutex.TryLock() {
			s.isRunning.Store(false)
			s.mutex.Unlock()
		}
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

	// Update status: mark sync cycle started
	s.lastSyncTime.Store(time.Now())
	s.syncCount.Add(1)

	// Get available data sources
	activeSources := s.settingsManager.GetActiveMarketSources()
	if len(activeSources) == 0 {
		log.Println("==================== SYNC CYCLE FAILED ====================")
		err := fmt.Errorf("no active market sources available")
		s.updateSyncFailure(err, startTime)
		return err
	}

	// Filter to only remote sources - syncer only processes remote sources
	var remoteSources []*settings.MarketSource
	for _, source := range activeSources {
		if source.Type == "remote" {
			remoteSources = append(remoteSources, source)
		} else {
			log.Printf("Skipping local source: %s (type: %s)", source.ID, source.Type)
		}
	}

	if len(remoteSources) == 0 {
		log.Println("==================== SYNC CYCLE SKIPPED ====================")
		log.Println("No remote market sources available for syncing")
		s.updateSyncSuccess(time.Since(startTime), startTime)
		return nil
	}

	log.Printf("Found %d active market sources (%d remote, %d local)", len(activeSources), len(remoteSources), len(activeSources)-len(remoteSources))

	// Process all remote sources in priority order - each source is synced independently
	var lastError error
	successCount := 0
	failureCount := 0
	var successSources []string
	var failedSources []string

	for _, source := range remoteSources {
		s.currentSource.Store(source.ID)
		log.Printf("Trying market source: %s (%s)", source.ID, source.BaseURL)

		if err := s.executeSyncCycleWithSource(ctx, source); err != nil {
			log.Printf("Failed to sync with source %s: %v", source.ID, err)
			lastError = err
			failureCount++
			failedSources = append(failedSources, source.ID)
			continue
		}

		// Success with this source - continue to next source
		successCount++
		successSources = append(successSources, source.ID)
		log.Printf("Sync cycle completed successfully with source %s", source.ID)
	}

	// Summary of sync cycle
	duration := time.Since(startTime)
	log.Printf("Sync cycle summary: %d/%d remote sources succeeded, %d failed in %v",
		successCount, len(remoteSources), failureCount, duration)
	if len(successSources) > 0 {
		log.Printf("Successfully synced sources: %v", successSources)
	}
	if len(failedSources) > 0 {
		log.Printf("Failed to sync sources: %v", failedSources)
	}

	// Check if at least one source succeeded
	if successCount > 0 {
		s.updateSyncSuccess(duration, startTime)
		log.Println("==================== SYNC CYCLE COMPLETED ====================")
		// If some sources failed, log a warning but still return success
		if failureCount > 0 {
			log.Printf("WARNING: %d source(s) failed, but %d source(s) succeeded", failureCount, successCount)
		}
		return nil
	}

	// All remote sources failed
	log.Println("==================== SYNC CYCLE FAILED ====================")
	err := fmt.Errorf("all remote market sources failed, last error: %w", lastError)
	s.updateSyncFailure(err, startTime)
	return err
}

// updateSyncSuccess updates status after a successful sync
func (s *Syncer) updateSyncSuccess(duration time.Duration, startTime time.Time) {
	if !s.statusMutex.TryLock() {
		log.Printf("Failed to acquire lock for updateSyncSuccess, skipping status update")
		return
	}
	defer s.statusMutex.Unlock()

	s.lastSyncSuccess.Store(time.Now())
	s.lastSyncDuration.Store(duration)
	s.lastSyncError.Store("")
	s.consecutiveFailures.Store(0)
	s.successCount.Add(1)
	s.currentStep.Store("")
	s.currentStepIndex.Store(0)
	s.totalSteps.Store(0)
}

// updateSyncFailure updates status after a failed sync
func (s *Syncer) updateSyncFailure(err error, startTime time.Time) {
	if !s.statusMutex.TryLock() {
		log.Printf("Failed to acquire lock for updateSyncFailure, skipping status update")
		return
	}
	defer s.statusMutex.Unlock()

	duration := time.Since(startTime)
	s.lastSyncDuration.Store(duration)
	if err != nil {
		s.lastSyncError.Store(err.Error())
	}
	s.consecutiveFailures.Add(1)
	s.failureCount.Add(1)
	s.currentStep.Store("")
	s.currentStepIndex.Store(0)
	s.totalSteps.Store(0)
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
	s.totalSteps.Store(int32(len(steps)))

	for i, step := range steps {
		stepName := step.GetStepName()
		log.Printf("======== SYNC STEP %d/%d STARTED: %s ========", i+1, len(steps), stepName)

		// Update current step status
		s.currentStep.Store(stepName)
		s.currentStepIndex.Store(int32(i))

		stepStartTime := time.Now()

		// Check if step can be skipped
		if step.CanSkip(syncCtx, syncContext) {
			log.Printf("Skipping step %d: %s", i+1, stepName)
			log.Printf("======== SYNC STEP %d/%d SKIPPED: %s ========", i+1, len(steps), stepName)
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

		select {
		case err := <-stepErr:
			// Stop timer immediately when step completes to prevent resource leak
			if !stepTimer.Stop() {
				// If timer already fired, drain the channel
				select {
				case <-stepTimer.C:
				default:
				}
			}
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
			// Stop timer when context is cancelled
			if !stepTimer.Stop() {
				select {
				case <-stepTimer.C:
				default:
				}
			}
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

		// Count apps synced
		var appCount int64
		if apps := syncContext.LatestData.Data.Apps; apps != nil {
			appCount = int64(len(apps))
		}
		s.lastSyncedAppCount.Store(appCount)

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
			if !cacheManager.mutex.TryRLock() {
				glog.Warningf("Syncer: CacheManager read lock not available, skipping user ID collection")
				return fmt.Errorf("read lock not available")
			}
			for userID := range s.cache.Users {
				userIDs = append(userIDs, userID)
			}
			cacheManager.mutex.RUnlock()

			// If no users exist, create a system user as fallback
			if len(userIDs) == 0 {
				log.Printf("[LOCK] cacheManager.mutex.TryLock() @syncer:createSystemUser Start")
				if !cacheManager.mutex.TryLock() {
					glog.Warningf("Syncer: CacheManager write lock not available for system user creation, skipping")
					return fmt.Errorf("write lock not available")
				}
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
		log.Printf("[LOCK] cacheManager.mutex.TryLock() @syncer:storeDataDirectly Start")
		if !cacheManager.mutex.TryLock() {
			glog.Warningf("Syncer: CacheManager write lock not available for data storage, skipping")
			return
		}
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
									if mobileDetailImg, ok := topicDataMap["mobileDetailImg"].(string); ok {
										topicData.MobileDetailImg = mobileDetailImg
									}
									if mobileRichText, ok := topicDataMap["mobileRichtext"].(string); ok {
										topicData.MobileRichText = mobileRichText
									}
									if backgroundColor, ok := topicDataMap["backgroundColor"].(string); ok {
										topicData.BackgroundColor = backgroundColor
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
			if !cacheManager.mutex.TryRLock() {
				glog.Warningf("Syncer.storeDataViaCacheManager: CacheManager read lock not available for user %s, source %s, skipping", userID, sourceID)
				continue
			}
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
	if !s.mutex.TryLock() {
		log.Printf("Failed to acquire lock for SetCacheManager, skipping")
		return
	}
	defer s.mutex.Unlock()
	s.cacheManager.Store(cacheManager) // Use atomic.Store to set the pointer
}

// SyncDetails contains detailed information about a sync operation
type SyncDetails struct {
	SourceID      string          `json:"source_id"`
	SyncTime      time.Time       `json:"sync_time"`
	SucceededApps []string        `json:"succeeded_apps"` // List of app IDs/names that succeeded
	FailedApps    []FailedAppInfo `json:"failed_apps"`    // List of apps that failed with reasons
	TotalApps     int             `json:"total_apps"`
	SuccessCount  int             `json:"success_count"`
	FailureCount  int             `json:"failure_count"`
}

// FailedAppInfo contains information about a failed app sync
type FailedAppInfo struct {
	AppID   string `json:"app_id"`
	AppName string `json:"app_name,omitempty"`
	Reason  string `json:"reason"`
}

// SyncerMetrics contains metrics for the syncer
type SyncerMetrics struct {
	IsRunning           bool          `json:"is_running"`
	SyncInterval        time.Duration `json:"sync_interval"`
	LastSyncTime        time.Time     `json:"last_sync_time"`
	LastSyncSuccess     time.Time     `json:"last_sync_success"`
	LastSyncError       string        `json:"last_sync_error,omitempty"`
	CurrentStep         string        `json:"current_step,omitempty"`
	CurrentStepIndex    int32         `json:"current_step_index"`
	TotalSteps          int32         `json:"total_steps"`
	CurrentSource       string        `json:"current_source,omitempty"`
	TotalSyncs          int64         `json:"total_syncs"`
	SuccessCount        int64         `json:"success_count"`
	FailureCount        int64         `json:"failure_count"`
	ConsecutiveFailures int64         `json:"consecutive_failures"`
	LastSyncDuration    time.Duration `json:"last_sync_duration"`
	LastSyncedAppCount  int64         `json:"last_synced_app_count"`
	SuccessRate         float64       `json:"success_rate"` // 0-100
	NextSyncTime        time.Time     `json:"next_sync_time,omitempty"`
	LastSyncDetails     *SyncDetails  `json:"last_sync_details,omitempty"`
}

// GetMetrics returns syncer metrics
func (s *Syncer) GetMetrics() SyncerMetrics {
	lastSyncTime := s.lastSyncTime.Load()
	lastSyncSuccess := s.lastSyncSuccess.Load()
	lastSyncError := s.lastSyncError.Load()
	currentStep := s.currentStep.Load()
	lastSyncDuration := s.lastSyncDuration.Load()
	currentSource := s.currentSource.Load()

	var lastSyncTimeVal time.Time
	var lastSyncSuccessVal time.Time
	var lastSyncErrorVal string
	var currentStepVal string
	var lastSyncDurationVal time.Duration
	var currentSourceVal string

	if t, ok := lastSyncTime.(time.Time); ok {
		lastSyncTimeVal = t
	}
	if t, ok := lastSyncSuccess.(time.Time); ok {
		lastSyncSuccessVal = t
	}
	if err, ok := lastSyncError.(string); ok {
		lastSyncErrorVal = err
	}
	if step, ok := currentStep.(string); ok {
		currentStepVal = step
	}
	if d, ok := lastSyncDuration.(time.Duration); ok {
		lastSyncDurationVal = d
	}
	if src, ok := currentSource.(string); ok {
		currentSourceVal = src
	}

	totalSyncs := s.syncCount.Load()
	successCount := s.successCount.Load()
	failureCount := s.failureCount.Load()

	// Calculate success rate
	var successRate float64
	if totalSyncs > 0 {
		successRate = float64(successCount) * 100.0 / float64(totalSyncs)
	}

	// Calculate next sync time
	var nextSyncTime time.Time
	if s.isRunning.Load() && !lastSyncTimeVal.IsZero() {
		nextSyncTime = lastSyncTimeVal.Add(s.syncInterval)
	} else if !lastSyncSuccessVal.IsZero() {
		nextSyncTime = lastSyncSuccessVal.Add(s.syncInterval)
	}

	var lastSyncDetails *SyncDetails
	if details := s.lastSyncDetails.Load(); details != nil {
		if d, ok := details.(*SyncDetails); ok {
			lastSyncDetails = d
		}
	}

	return SyncerMetrics{
		IsRunning:           s.isRunning.Load(),
		SyncInterval:        s.syncInterval,
		LastSyncTime:        lastSyncTimeVal,
		LastSyncSuccess:     lastSyncSuccessVal,
		LastSyncError:       lastSyncErrorVal,
		CurrentStep:         currentStepVal,
		CurrentStepIndex:    s.currentStepIndex.Load(),
		TotalSteps:          s.totalSteps.Load(),
		CurrentSource:       currentSourceVal,
		TotalSyncs:          totalSyncs,
		SuccessCount:        successCount,
		FailureCount:        failureCount,
		ConsecutiveFailures: s.consecutiveFailures.Load(),
		LastSyncDuration:    lastSyncDurationVal,
		LastSyncedAppCount:  s.lastSyncedAppCount.Load(),
		SuccessRate:         successRate,
		NextSyncTime:        nextSyncTime,
		LastSyncDetails:     lastSyncDetails,
	}
}

// RecordSyncDetails records detailed information about a sync operation
func (s *Syncer) RecordSyncDetails(details *SyncDetails) {
	if details != nil {
		s.lastSyncDetails.Store(details)
	}
}

// IsHealthy returns whether the syncer is healthy
func (s *Syncer) IsHealthy() bool {
	if !s.isRunning.Load() {
		return false
	}

	// Check for too many consecutive failures
	if s.consecutiveFailures.Load() >= 3 {
		return false
	}

	// Check if last sync was too long ago
	lastSuccess := s.lastSyncSuccess.Load()
	if t, ok := lastSuccess.(time.Time); ok && !t.IsZero() {
		// If last success was more than 3 sync intervals ago, consider unhealthy
		if time.Since(t) > s.syncInterval*3 {
			return false
		}
	} else {
		// If never had a successful sync, check if we've been running for a while
		lastSync := s.lastSyncTime.Load()
		if t, ok := lastSync.(time.Time); ok && !t.IsZero() {
			// If running but never succeeded and it's been more than 3 intervals, unhealthy
			if time.Since(t) > s.syncInterval*3 && s.syncCount.Load() > 0 {
				return false
			}
		}
	}

	return true
}
