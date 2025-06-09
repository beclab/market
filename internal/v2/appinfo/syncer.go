package appinfo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"market/internal/v2/appinfo/syncerfn"
	"market/internal/v2/settings"
)

// Syncer manages the synchronization process with multiple steps
type Syncer struct {
	steps           []syncerfn.SyncStep
	cache           *CacheData
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
	if version, err := GetTerminusVersionValue(); err == nil {
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
			"data":    syncContext.LatestData.Data,
		}

		sourceID := source.Name // Use market source name as source ID
		log.Printf("Using source ID: %s for data storage", sourceID)

		// Store to cache for all users
		// 为所有用户存储到缓存
		s.cache.Mutex.Lock()
		defer s.cache.Mutex.Unlock()

		// Get all existing user IDs
		// 获取所有现有的用户ID
		var userIDs []string
		for userID := range s.cache.Users {
			userIDs = append(userIDs, userID)
		}

		// If no users exist, create a system user as fallback
		// 如果没有用户存在，创建系统用户作为回退
		if len(userIDs) == 0 {
			systemUserID := "system"
			s.cache.Users[systemUserID] = NewUserData()
			userIDs = append(userIDs, systemUserID)
			log.Printf("No existing users found, created system user as fallback")
		}

		log.Printf("Storing data for %d users: %v", len(userIDs), userIDs)

		// Store data for each user
		// 为每个用户存储数据
		for _, userID := range userIDs {
			userData := s.cache.Users[userID]
			userData.Mutex.Lock()

			// Ensure source data exists for this user
			// 确保此用户的源数据存在
			if _, exists := userData.Sources[sourceID]; !exists {
				userData.Sources[sourceID] = NewSourceData()
				log.Printf("Created new source data for user: %s, source: %s", userID, sourceID)
			}

			sourceData := userData.Sources[sourceID]
			sourceData.Mutex.Lock()

			// Create AppData for app-info-latest-pending
			appData := NewAppData(AppInfoLatestPending, completeData)
			sourceData.AppInfoLatestPending = appData

			sourceData.Mutex.Unlock()
			userData.Mutex.Unlock()

			log.Printf("Successfully stored data to app-info-latest-pending for user: %s, source: %s", userID, sourceID)
		}

		log.Printf("Data synchronization completed for all %d users", len(userIDs))
	} else {
		log.Printf("WARNING: No LatestData available in sync context, skipping data storage")
		log.Printf("Sync context status - HashMatches: %t, RemoteHash: %s, LocalHash: %s",
			syncContext.HashMatches, syncContext.RemoteHash, syncContext.LocalHash)
	}

	return nil
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
