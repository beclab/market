package appinfo

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"market/internal/v2/appinfo/syncerfn"
	"market/internal/v2/db/models"
	"market/internal/v2/settings"
	"market/internal/v2/store/marketsource"
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
	settingsManager *settings.SettingsManager // Settings manager for data source information

	lastSyncExecuted time.Time // Last time a full sync cycle was actually executed

	lastKnownRemoteSourceIDs atomic.Value // string: sorted comma-joined remote source IDs from last sync
	lastKnownUserIDs         atomic.Value // string: sorted comma-joined user IDs from last sync

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
	tryOnce             atomic.Bool
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
		tryOnce:         atomic.Bool{},
	}
	// Initialize atomic values
	s.lastSyncTime.Store(time.Time{})
	s.lastSyncSuccess.Store(time.Time{})
	s.lastSyncError.Store("")
	s.currentStep.Store("")
	s.lastSyncDuration.Store(time.Duration(0))
	s.currentSource.Store("")
	s.lastKnownRemoteSourceIDs.Store("")
	s.lastKnownUserIDs.Store("")
	return s
}

// AddStep adds a step to the syncer
func (s *Syncer) AddStep(step syncerfn.SyncStep) {
	s.steps = append(s.steps, step)
}

// GetSteps returns a copy of all steps
func (s *Syncer) GetSteps() []syncerfn.SyncStep {
	steps := make([]syncerfn.SyncStep, len(s.steps))
	copy(steps, s.steps)
	return steps
}

// StartWithOptions starts the syncer with options.
// If enableSyncLoop is false, the periodic sync loop is not started (Pipeline handles scheduling).
func (s *Syncer) StartWithOptions(ctx context.Context) error {
	if s.isRunning.Load() {
		return fmt.Errorf("syncer is already running")
	}

	s.isRunning.Store(true)

	glog.V(2).Infof("Starting syncer with %d steps (passive mode, Pipeline handles scheduling)", len(s.steps))

	return nil
}

// SyncOnce executes one sync cycle if at least syncInterval has elapsed
// since the last execution, OR if the sync-relevant configuration has changed
// (e.g. a new source or user was added/removed), OR if a remote source's
// data hash has changed (lightweight probe). Called by Pipeline on every tick.
func (s *Syncer) SyncOnce(ctx context.Context) {
	if !s.isRunning.Load() {
		return
	}

	flag := s.tryOnce.Load()
	if flag {
		// return
	}

	configChanged, reason := s.hasSyncRelevantConfigChanged()
	throttled := !s.lastSyncExecuted.IsZero() && time.Since(s.lastSyncExecuted) < s.syncInterval

	if !configChanged && throttled {
		glog.V(3).Infof("SyncOnce: skipping, last sync was %v ago (interval: %v)", time.Since(s.lastSyncExecuted), s.syncInterval)
		return
	}
	if configChanged {
		glog.V(2).Infof("SyncOnce: %s, forcing sync cycle", reason)
	}
	s.lastSyncExecuted = time.Now()
	if err := s.executeSyncCycle(ctx); err != nil {
		glog.Errorf("SyncOnce: sync cycle failed: %v", err)
	}

	s.tryOnce.Store(true)
}


// hasSyncRelevantConfigChanged checks whether the remote source list or the
// user list has changed since the last sync cycle. Returns true with a
// human-readable reason when a change is detected.
func (s *Syncer) hasSyncRelevantConfigChanged() (changed bool, reason string) {
	// Check remote sources
	config := s.settingsManager.GetMarketSources()
	if config != nil && len(config.Sources) > 0 {
		var remoteIDs []string
		for _, src := range config.Sources {
			if src.Type == "remote" {
				remoteIDs = append(remoteIDs, src.ID)
			}
		}
		sort.Strings(remoteIDs)
		currentKey := strings.Join(remoteIDs, ",")

		lastKnown, _ := s.lastKnownRemoteSourceIDs.Load().(string)
		if currentKey != lastKnown {
			s.lastKnownRemoteSourceIDs.Store(currentKey)
			if lastKnown != "" {
				return true, "remote source configuration changed"
			}
		}
	}

	// Check user list
	if cm := s.cacheManager.Load(); cm != nil {
		userIDs := cm.GetUserIDs()
		sort.Strings(userIDs)
		currentKey := strings.Join(userIDs, ",")

		lastKnown, _ := s.lastKnownUserIDs.Load().(string)
		if currentKey != lastKnown {
			s.lastKnownUserIDs.Store(currentKey)
			if lastKnown != "" {
				return true, "user list changed"
			}
		}
	}

	return false, ""
}

// Stop stops the synchronization process
func (s *Syncer) Stop() {
	if !s.isRunning.CompareAndSwap(true, false) {
		return
	}

	glog.V(2).Info("Stopping syncer...")
	close(s.stopChan)
}

// IsRunning returns whether the syncer is currently running
func (s *Syncer) IsRunning() bool {
	return s.isRunning.Load()
}

// getVersionForSync returns the version to use for sync operations with fallback
func getVersionForSync() string {
	if version, err := utils.GetTerminusVersionValue(); err == nil {
		return version
	} else {
		glog.Errorf("Failed to get version, using fallback: %v", err)
		return "1.12.3" // fallback version
	}
}

// executeSyncCycle executes one complete synchronization cycle
func (s *Syncer) executeSyncCycle(ctx context.Context) error {
	glog.V(2).Info("==================== SYNC CYCLE STARTED ====================")
	glog.V(2).Info("Starting sync cycle")
	startTime := time.Now()

	// Update status: mark sync cycle started
	s.lastSyncTime.Store(time.Now())
	s.syncCount.Add(1)

	// Get all configured market sources (no IsActive filter - sync all configured sources)
	sources,err := marketsource.GetMarketSources()
	if err != nil || len(sources) == 0 {
		glog.Error("==================== SYNC CYCLE FAILED ====================")
		err := fmt.Errorf("no market sources configured, err: %v",err)
		s.updateSyncFailure(err, startTime)
		return err
	}


	// Log all configured sources for debugging
	glog.V(2).Infof("Found %d configured market sources:", len(sources))
	for _, src := range sources {
		glog.V(2).Infof("  - Source ID: %s, Name: %s, Type: %s, Priority: %d, BaseURL: %s",
			src.SourceID, src.SourceTitle, src.SourceType, src.Priority, src.SourceURL)
	}

	// Filter to only remote sources - syncer only processes remote sources
	var remoteSources []*models.MarketSource
	for _, source := range sources {
		if source.SourceType == "remote" {
			remoteSources = append(remoteSources, source)
		} else {
			glog.V(3).Infof("Skipping local source: %s (type: %s)", source.SourceID, source.SourceType)
		}
	}

	if len(remoteSources) == 0 {
		glog.V(2).Info("==================== SYNC CYCLE SKIPPED ====================")
		glog.V(2).Info("No remote market sources available for syncing")
		s.updateSyncSuccess(time.Since(startTime), startTime)
		return nil
	}

	glog.V(3).Infof("Found %d configured market sources (%d remote, %d local)", len(sources), len(remoteSources), len(sources)-len(remoteSources))

	// Process all remote sources in priority order - each source is synced independently
	var lastError error
	successCount := 0
	failureCount := 0
	var successSources []string
	var failedSources []string

	for _, source := range remoteSources {
		s.currentSource.Store(source.SourceID)
		glog.V(3).Infof("Trying market source: %s (%s)", source.SourceID, source.SourceURL)

		if err := s.executeSyncCycleWithSource(ctx, source); err != nil {
			glog.Errorf("Failed to sync with source %s: %v", source.SourceID, err)
			lastError = err
			failureCount++
			failedSources = append(failedSources, source.SourceID)
			continue
		}

		// Success with this source - continue to next source
		successCount++
		successSources = append(successSources, source.SourceID)
		glog.V(3).Infof("Sync cycle completed successfully with source %s", source.SourceID)
	}

	// Summary of sync cycle
	duration := time.Since(startTime)
	glog.V(3).Infof("Sync cycle summary: %d/%d remote sources succeeded, %d failed in %v",
		successCount, len(remoteSources), failureCount, duration)
	if len(successSources) > 0 {
		glog.V(3).Infof("Successfully synced sources: %v", successSources)
	}
	if len(failedSources) > 0 {
		glog.V(3).Infof("Failed to sync sources: %v", failedSources)
	}

	// Check if at least one source succeeded
	if successCount > 0 {
		s.updateSyncSuccess(duration, startTime)
		glog.V(3).Info("==================== SYNC CYCLE COMPLETED ====================")
		// If some sources failed, log a warning but still return success
		if failureCount > 0 {
			glog.V(3).Infof("WARNING: %d source(s) failed, but %d source(s) succeeded", failureCount, successCount)
		}
		return nil
	}

	// All remote sources failed
	glog.V(3).Info("==================== SYNC CYCLE FAILED ====================")
	err = fmt.Errorf("all remote market sources failed, last error: %w", lastError)
	s.updateSyncFailure(err, startTime)
	return err
}

// updateSyncSuccess updates status after a successful sync
func (s *Syncer) updateSyncSuccess(duration time.Duration, startTime time.Time) {
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
func (s *Syncer) executeSyncCycleWithSource(ctx context.Context, source *models.MarketSource) error {
	glog.V(2).Infof("-------------------- SOURCE SYNC STARTED: %s --------------------", source.SourceID)

	// Create sync context with CacheManager for unified lock strategy
	var cacheManager types.CacheManagerInterface
	if cm := s.cacheManager.Load(); cm != nil {
		cacheManager = cm
	}
	syncContext := syncerfn.NewSyncContextWithManager(s.cache, cacheManager)

	// Set version for API requests using utils function
	version := getVersionForSync()
	syncContext.SetVersion(version)
	glog.V(2).Infof("Set version for sync cycle: %s", version)

	// Set the current market source in sync context
	syncContext.SetMarketSource(source)

	// Create a timeout context for the entire sync cycle
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Minute) // 10 minute timeout for entire sync
	defer cancel()

	steps := s.GetSteps()
	s.totalSteps.Store(int32(len(steps)))

	for i, step := range steps {
		stepName := step.GetStepName()
		glog.V(2).Infof("======== SYNC STEP %d/%d STARTED: %s ========", i+1, len(steps), stepName)

		// Update current step status
		s.currentStep.Store(stepName)
		s.currentStepIndex.Store(int32(i))

		stepStartTime := time.Now()

		// Check if step can be skipped
		if step.CanSkip(syncCtx, syncContext) {
			glog.V(3).Infof("Skipping step %d: %s", i+1, stepName)
			glog.V(3).Infof("======== SYNC STEP %d/%d SKIPPED: %s ========", i+1, len(steps), stepName)
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
				glog.Errorf("Step %d (%s) failed: %v", i+1, step.GetStepName(), err)
				glog.Errorf("======== SYNC STEP %d/%d FAILED: %s ========", i+1, len(steps), step.GetStepName())
				glog.Errorf("-------------------- SOURCE SYNC FAILED: %s --------------------", source.SourceID)
				return fmt.Errorf("step %d failed: %w", i+1, err)
			}
		case <-stepTimer.C:
			glog.V(4).Infof("Step %d (%s) timed out after %v", i+1, step.GetStepName(), stepTimeout)
			glog.V(4).Infof("======== SYNC STEP %d/%d TIMEOUT: %s ========", i+1, len(steps), step.GetStepName())
			glog.V(4).Infof("-------------------- SOURCE SYNC TIMEOUT: %s --------------------", source.SourceID)
			return fmt.Errorf("step %d timed out after %v", i+1, stepTimeout)
		case <-syncCtx.Done():
			// Stop timer when context is cancelled
			if !stepTimer.Stop() {
				select {
				case <-stepTimer.C:
				default:
				}
			}
			glog.V(4).Infof("Step %d (%s) timed out or context cancelled", i+1, step.GetStepName())
			glog.V(4).Infof("======== SYNC STEP %d/%d TIMEOUT: %s ========", i+1, len(steps), step.GetStepName())
			glog.V(4).Infof("-------------------- SOURCE SYNC TIMEOUT: %s --------------------", source.SourceID)
			return fmt.Errorf("step %d timed out: %w", i+1, syncCtx.Err())
		}

		stepDuration := time.Since(stepStartTime)
		glog.V(2).Infof("Step %d (%s) completed in %v", i+1, step.GetStepName(), stepDuration)
		glog.V(2).Infof("======== SYNC STEP %d/%d COMPLETED: %s ========", i+1, len(steps), step.GetStepName())
	}

	// Report any errors collected during the process
	if syncContext.HasErrors() {
		errors := syncContext.GetErrors()
		glog.Errorf("Sync cycle completed with %d errors:", len(errors))
		for i, err := range errors {
			glog.Errorf("  Error %d: %v", i+1, err)
		}
	}

	// Store complete data to app-info-latest-pending after successful sync
	// Modified condition: Store data if we have LatestData, regardless of hash match status
	if syncContext.LatestData != nil {
		glog.V(3).Infof("Storing complete data to app-info-latest-pending for all users")
		glog.V(3).Infof("Sync context status - HashMatches: %t, RemoteHash: %s, LocalHash: %s",
			syncContext.HashMatches, syncContext.RemoteHash, syncContext.LocalHash)

		// Count apps synced
		var appCount int64
		if apps := syncContext.LatestData.Data.Apps; apps != nil {
			appCount = int64(len(apps))
		}
		s.lastSyncedAppCount.Store(appCount)

		sourceID := source.SourceID // Use market source name as source ID
		glog.V(3).Infof("Using source ID: %s for data storage", sourceID)
	} else {
		glog.V(3).Info("WARNING: No LatestData available in sync context, skipping data storage")
		glog.V(3).Infof("Sync context status - HashMatches: %t, RemoteHash: %s, LocalHash: %s",
			syncContext.HashMatches, syncContext.RemoteHash, syncContext.LocalHash)
	}

	glog.V(2).Infof("-------------------- SOURCE SYNC COMPLETED: %s --------------------", source.SourceID)
	return nil
}

// CreateDefaultSyncer creates a syncer with default steps configured
func CreateDefaultSyncer(cache *CacheData, config SyncerConfig, settingsManager *settings.SettingsManager) *Syncer {
	syncer := NewSyncer(cache, config.SyncInterval, settingsManager)

	// Get version for API requests using utils function
	version := getVersionForSync()
	glog.V(3).Infof("Using version for syncer steps: %s", version)

	// Get API endpoints configuration
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints == nil {
		glog.V(3).Infof("Warning: no API endpoints configuration found, using defaults")
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

	glog.V(2).Infof("Created syncer with API endpoints - Hash: %s, Data: %s, Detail: %s",
		endpoints.HashPath, endpoints.DataPath, endpoints.DetailPath)

	return syncer
}

// SyncerConfig holds configuration for the syncer
type SyncerConfig struct {
	SyncInterval time.Duration `json:"sync_interval"`
}

// SetCacheManager sets the cache manager for hydration notifications
func (s *Syncer) SetCacheManager(cacheManager *CacheManager) {
	s.cacheManager.Store(cacheManager)
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
