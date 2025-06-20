package appinfo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"

	"github.com/golang/glog"
)

// DataWatcher monitors pending app data and moves completed hydration apps to latest
type DataWatcher struct {
	cacheManager *CacheManager
	hydrator     *Hydrator
	interval     time.Duration
	isRunning    bool
	stopChan     chan struct{}
	mutex        sync.RWMutex

	// Processing mutex to ensure only one cycle runs at a time
	processingMutex sync.Mutex

	// Active hash calculations tracking
	activeHashCalculations map[string]bool
	hashMutex              sync.Mutex

	// Metrics
	totalAppsProcessed int64
	totalAppsMoved     int64
	lastRunTime        time.Time
	metricsMutex       sync.RWMutex

	// Logging configuration
	logLevel           int // 0=Error, 1=Warning, 2=Info, 3=Debug
	enableVerboseLogs  bool
	lastSummaryLogTime time.Time
	summaryInterval    time.Duration
}

// DataWatcherConfig contains configuration options for DataWatcher
type DataWatcherConfig struct {
	Interval          time.Duration `json:"interval"`
	LogLevel          int           `json:"log_level"`          // 0=Error, 1=Warning, 2=Info, 3=Debug
	SummaryInterval   time.Duration `json:"summary_interval"`   // How often to log summaries when no activity
	BatchSize         int           `json:"batch_size"`         // Number of users to process in each batch
	ProcessingTimeout time.Duration `json:"processing_timeout"` // Timeout for processing cycle
}

// DefaultDataWatcherConfig returns default configuration
func DefaultDataWatcherConfig() *DataWatcherConfig {
	return &DataWatcherConfig{
		Interval:          30 * time.Second,
		LogLevel:          1, // Warning level by default
		SummaryInterval:   5 * time.Minute,
		BatchSize:         5,
		ProcessingTimeout: 25 * time.Second,
	}
}

// NewDataWatcher creates a new DataWatcher instance with configuration
func NewDataWatcher(cacheManager *CacheManager, hydrator *Hydrator, config *DataWatcherConfig) *DataWatcher {
	if config == nil {
		config = DefaultDataWatcherConfig()
	}

	// Validate configuration
	if config.Interval < time.Second {
		config.Interval = time.Second
	}
	if config.LogLevel < 0 || config.LogLevel > 3 {
		config.LogLevel = 1
	}
	if config.SummaryInterval < time.Minute {
		config.SummaryInterval = time.Minute
	}
	if config.BatchSize < 1 {
		config.BatchSize = 5
	}
	if config.ProcessingTimeout < 5*time.Second {
		config.ProcessingTimeout = 5 * time.Second
	}

	return &DataWatcher{
		cacheManager:           cacheManager,
		hydrator:               hydrator,
		interval:               config.Interval,
		stopChan:               make(chan struct{}),
		isRunning:              false,
		activeHashCalculations: make(map[string]bool),
		logLevel:               config.LogLevel,
		enableVerboseLogs:      config.LogLevel >= 2,
		summaryInterval:        config.SummaryInterval,
	}
}

// NewDataWatcherLegacy creates a new DataWatcher instance (legacy constructor)
func NewDataWatcherLegacy(cacheManager *CacheManager, hydrator *Hydrator) *DataWatcher {
	return NewDataWatcher(cacheManager, hydrator, DefaultDataWatcherConfig())
}

// SetLogLevel sets the logging level for DataWatcher
// 0=Error only, 1=Warning, 2=Info, 3=Debug
func (dw *DataWatcher) SetLogLevel(level int) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if level < 0 {
		level = 0
	} else if level > 3 {
		level = 3
	}

	dw.logLevel = level
	dw.enableVerboseLogs = level >= 2
	glog.Infof("DataWatcher log level set to: %d", level)
}

// logInfo logs info messages based on log level
func (dw *DataWatcher) logInfo(format string, args ...interface{}) {
	if dw.logLevel >= 2 {
		glog.Infof(format, args...)
	}
}

// logWarning logs warning messages based on log level
func (dw *DataWatcher) logWarning(format string, args ...interface{}) {
	if dw.logLevel >= 1 {
		glog.Warningf(format, args...)
	}
}

// logDebug logs debug messages based on log level
func (dw *DataWatcher) logDebug(format string, args ...interface{}) {
	if dw.logLevel >= 3 {
		glog.V(2).Infof(format, args...)
	}
}

// logError always logs error messages
func (dw *DataWatcher) logError(format string, args ...interface{}) {
	glog.Errorf(format, args...)
}

// shouldLogSummary checks if we should log a summary based on time interval
func (dw *DataWatcher) shouldLogSummary() bool {
	return time.Since(dw.lastSummaryLogTime) >= dw.summaryInterval
}

// Start begins the data watching process
func (dw *DataWatcher) Start(ctx context.Context) error {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if dw.isRunning {
		return fmt.Errorf("DataWatcher is already running")
	}

	if dw.cacheManager == nil {
		return fmt.Errorf("CacheManager is required for DataWatcher")
	}

	if dw.hydrator == nil {
		return fmt.Errorf("Hydrator is required for DataWatcher")
	}

	dw.isRunning = true
	dw.logInfo("Starting DataWatcher with interval: %v", dw.interval)

	// Start the monitoring goroutine
	go dw.watchLoop(ctx)

	return nil
}

// Stop stops the data watching process
func (dw *DataWatcher) Stop() {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if !dw.isRunning {
		return
	}

	dw.logInfo("Stopping DataWatcher...")
	close(dw.stopChan)
	dw.isRunning = false
}

// IsRunning returns whether the DataWatcher is currently running
func (dw *DataWatcher) IsRunning() bool {
	dw.mutex.RLock()
	defer dw.mutex.RUnlock()
	return dw.isRunning
}

// watchLoop is the main monitoring loop
func (dw *DataWatcher) watchLoop(ctx context.Context) {
	dw.logInfo("DataWatcher monitoring loop started")
	defer dw.logInfo("DataWatcher monitoring loop stopped")

	ticker := time.NewTicker(dw.interval)
	defer ticker.Stop()

	// Run once immediately
	dw.processCompletedApps()

	for {
		select {
		case <-ctx.Done():
			dw.logInfo("DataWatcher stopped due to context cancellation")
			return
		case <-dw.stopChan:
			dw.logInfo("DataWatcher stopped due to explicit stop")
			return
		case <-ticker.C:
			dw.processCompletedApps()
		}
	}
}

// processCompletedApps checks for completed hydration apps and moves them
func (dw *DataWatcher) processCompletedApps() {
	// Ensure only one processing cycle runs at a time
	if !dw.processingMutex.TryLock() {
		dw.logWarning("DataWatcher: Previous processing cycle still running, skipping this cycle")
		return
	}
	defer dw.processingMutex.Unlock()

	processingStart := time.Now()
	dw.metricsMutex.Lock()
	dw.lastRunTime = time.Now()
	dw.metricsMutex.Unlock()

	dw.logInfo("DataWatcher: Starting to process completed apps")

	// Create timeout context for entire processing cycle
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Get all users data from cache manager with timeout
	var allUsersData map[string]*types.UserData

	userDataChan := make(chan map[string]*types.UserData, 1)
	go func() {
		data := dw.cacheManager.GetAllUsersData()
		userDataChan <- data
	}()

	select {
	case allUsersData = <-userDataChan:
		// Successfully got user data
	case <-ctx.Done():
		dw.logError("DataWatcher: Timeout getting all users data")
		return
	}

	if len(allUsersData) == 0 {
		dw.logInfo("DataWatcher: No users data found, processing cycle completed in %v", time.Since(processingStart))
		return
	}

	dw.logInfo("DataWatcher: Found %d users to process", len(allUsersData))
	totalProcessed := int64(0)
	totalMoved := int64(0)

	// Process users in batches to avoid holding locks too long
	const batchSize = 5
	userCount := 0
	userBatch := make([]string, 0, batchSize)
	userDataBatch := make(map[string]*types.UserData)

	// Batch processing statistics
	batchStats := struct {
		batchesProcessed int
		usersWithMoves   int
		usersProcessed   int
	}{}

	for userID, userData := range allUsersData {
		userBatch = append(userBatch, userID)
		userDataBatch[userID] = userData
		userCount++

		// Process batch when it's full or we've reached the end
		if len(userBatch) >= batchSize || userCount == len(allUsersData) {
			batchProcessed, batchMoved := dw.processUserBatch(ctx, userBatch, userDataBatch)
			totalProcessed += batchProcessed
			totalMoved += batchMoved
			batchStats.batchesProcessed++
			batchStats.usersProcessed += len(userBatch)

			if batchMoved > 0 {
				batchStats.usersWithMoves++
			}

			// Clear batch for next iteration
			userBatch = userBatch[:0]
			userDataBatch = make(map[string]*types.UserData)

			// Check timeout between batches
			select {
			case <-ctx.Done():
				dw.logError("DataWatcher: Timeout during batch processing after processing %d users", userCount)
				return
			default:
			}
		}
	}

	// Update metrics
	dw.metricsMutex.Lock()
	dw.totalAppsProcessed += totalProcessed
	dw.totalAppsMoved += totalMoved
	dw.metricsMutex.Unlock()

	processingDuration := time.Since(processingStart)

	// Log summary based on activity level and time interval
	if totalMoved > 0 {
		dw.logInfo("DataWatcher: Processing cycle completed in %v - %d apps processed, %d moved to AppInfoLatest (batches: %d, users with moves: %d/%d)",
			processingDuration, totalProcessed, totalMoved, batchStats.batchesProcessed, batchStats.usersWithMoves, batchStats.usersProcessed)
	} else if dw.shouldLogSummary() {
		// Log summary even when no moves, but only periodically
		dw.logInfo("DataWatcher: Processing cycle completed in %v - %d apps processed, no moves needed (batches: %d, users: %d)",
			processingDuration, totalProcessed, batchStats.batchesProcessed, batchStats.usersProcessed)
		dw.lastSummaryLogTime = time.Now()
	} else {
		// Only log debug level for normal cycles with no activity
		dw.logDebug("DataWatcher: Processing cycle completed in %v - %d apps processed, no moves needed", processingDuration, totalProcessed)
	}
}

// processUserBatch processes a batch of users
func (dw *DataWatcher) processUserBatch(ctx context.Context, userIDs []string, userDataMap map[string]*types.UserData) (int64, int64) {
	totalProcessed := int64(0)
	totalMoved := int64(0)

	// Track users with activity for summary logging
	usersWithActivity := make([]string, 0)

	for i, userID := range userIDs {
		// Check timeout during batch processing
		select {
		case <-ctx.Done():
			dw.logError("DataWatcher: Timeout during user batch processing (user %d/%d)", i+1, len(userIDs))
			return totalProcessed, totalMoved
		default:
		}

		userData := userDataMap[userID]
		if userData == nil {
			continue
		}

		// Only log user processing at debug level unless there's activity
		dw.logDebug("DataWatcher: Processing user %d/%d in batch: %s", i+1, len(userIDs), userID)
		processed, moved := dw.processUserData(userID, userData)
		totalProcessed += processed
		totalMoved += moved

		// Track users with activity for summary
		if processed > 0 || moved > 0 {
			usersWithActivity = append(usersWithActivity, userID)
		}

		dw.logDebug("DataWatcher: User %s completed: %d processed, %d moved", userID, processed, moved)
	}

	// Log batch summary if there was activity
	if len(usersWithActivity) > 0 {
		dw.logInfo("DataWatcher: Batch completed - %d users processed, %d apps moved, active users: %v",
			len(userIDs), totalMoved, usersWithActivity)
	}

	return totalProcessed, totalMoved
}

// processUserData processes a single user's data
func (dw *DataWatcher) processUserData(userID string, userData *types.UserData) (int64, int64) {
	if userData == nil {
		return 0, 0
	}

	// Step 1: Collect source data references under minimal lock
	sourceRefs := make(map[string]*SourceData)
	for sourceID, sourceData := range userData.Sources {
		sourceRefs[sourceID] = sourceData
	}

	// Step 2: Process each source without holding user lock
	totalProcessed := int64(0)
	totalMoved := int64(0)

	for sourceID, sourceData := range sourceRefs {
		processed, moved := dw.processSourceData(userID, sourceID, sourceData)
		totalProcessed += processed
		totalMoved += moved
	}

	// Step 3: Calculate hash if apps were moved OR if hash is empty
	shouldCalculateHash := totalMoved > 0 || userData.Hash == ""

	if shouldCalculateHash {
		if totalMoved > 0 {
			dw.logInfo("%d apps moved for user %s, scheduling hash calculation", totalMoved, userID)
		} else {
			dw.logInfo("Hash is empty for user %s, scheduling hash calculation", userID)
		}

		// Check if hash calculation is already in progress for this user
		dw.hashMutex.Lock()
		if dw.activeHashCalculations[userID] {
			dw.hashMutex.Unlock()
			dw.logWarning("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
			return totalProcessed, totalMoved
		}
		dw.activeHashCalculations[userID] = true
		dw.hashMutex.Unlock()

		// Schedule hash calculation with a small delay to ensure all locks are released
		go func() {
			defer func() {
				// Clean up tracking when done
				dw.hashMutex.Lock()
				delete(dw.activeHashCalculations, userID)
				dw.hashMutex.Unlock()
				dw.logInfo("DataWatcher: Hash calculation tracking cleaned up for user %s", userID)
			}()

			// Wait a short time to ensure all source processing locks are released
			time.Sleep(100 * time.Millisecond)
			dw.logInfo("DataWatcher: Starting hash calculation for user %s", userID)

			// Call the hash calculation function directly without additional tracking
			dw.calculateAndSetUserHashDirect(userID, userData)
		}()
	} else {
		dw.logDebug("DataWatcher: No apps moved and hash exists for user %s, skipping hash calculation", userID)
	}

	return totalProcessed, totalMoved
}

// calculateAndSetUserHash calculates and sets the hash for user data (with tracking)
func (dw *DataWatcher) calculateAndSetUserHash(userID string, userData *types.UserData) {
	// Check if hash calculation is already in progress for this user
	dw.hashMutex.Lock()
	if dw.activeHashCalculations[userID] {
		dw.hashMutex.Unlock()
		dw.logInfo("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
		return
	}
	dw.activeHashCalculations[userID] = true
	dw.hashMutex.Unlock()

	defer func() {
		// Clean up tracking when done
		dw.hashMutex.Lock()
		delete(dw.activeHashCalculations, userID)
		dw.hashMutex.Unlock()
		dw.logInfo("DataWatcher: Hash calculation tracking cleaned up for user %s", userID)
	}()

	// Call the direct calculation function
	dw.calculateAndSetUserHashDirect(userID, userData)
}

// calculateAndSetUserHashDirect calculates hash without tracking (used internally by goroutines)
func (dw *DataWatcher) calculateAndSetUserHashDirect(userID string, userData *types.UserData) {
	dw.logInfo("DataWatcher: Starting direct hash calculation for user %s", userID)

	// Get the original user data from cache manager to ensure we have the latest reference
	originalUserData := dw.cacheManager.GetUserData(userID)
	if originalUserData == nil {
		dw.logError("DataWatcher: Failed to get user data from cache manager for user %s", userID)
		return
	}

	// Use global lock with timeout to avoid deadlock
	lockTimeout := 5 * time.Second
	lockAcquired := make(chan bool, 1)
	lockError := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.logError("DataWatcher: Panic during lock acquisition for user %s: %v", userID, r)
				lockError <- fmt.Errorf("panic during lock acquisition: %v", r)
			}
		}()

		dw.logInfo("DataWatcher: Attempting to acquire global cache lock for user %s", userID)
		dw.cacheManager.mutex.RLock()
		dw.logInfo("DataWatcher: Global cache read lock acquired for user %s", userID)
		lockAcquired <- true
	}()

	select {
	case <-lockAcquired:
		// Lock acquired successfully
		dw.logInfo("DataWatcher: Global cache lock acquired for user %s", userID)
	case err := <-lockError:
		dw.logError("DataWatcher: Error acquiring global cache lock for user %s: %v", userID, err)
		return
	case <-time.After(lockTimeout):
		dw.logError("DataWatcher: Timeout acquiring global cache lock for hash calculation, user %s", userID)
		return
	}

	// Ensure we release the read lock when done with read operations
	defer func() {
		dw.logInfo("DataWatcher: Releasing global cache read lock for user %s", userID)
		dw.cacheManager.mutex.RUnlock()
	}()

	dw.logInfo("DataWatcher: Creating user data snapshot for user %s", userID)
	// Create snapshot for hash calculation
	snapshot, err := dw.createUserDataSnapshot(userID, originalUserData)
	if err != nil {
		dw.logError("DataWatcher: Failed to create user data snapshot for user %s: %v", userID, err)
		return
	}

	dw.logInfo("DataWatcher: Calculating hash for user %s", userID)
	// Calculate hash using the snapshot
	newHash, err := utils.CalculateUserDataHash(snapshot)
	if err != nil {
		dw.logError("DataWatcher: Failed to calculate hash for user %s: %v", userID, err)
		return
	}

	// Get current hash for comparison
	currentHash := originalUserData.Hash
	dw.logInfo("DataWatcher: Hash comparison for user %s - current: '%s', new: '%s'", userID, currentHash, newHash)

	if currentHash == newHash {
		dw.logDebug("DataWatcher: Hash unchanged for user %s: %s", userID, newHash)
		return
	}

	dw.logInfo("DataWatcher: Hash changed for user %s: %s -> %s", userID, currentHash, newHash)

	// Release read lock before acquiring write lock to avoid deadlock
	dw.logInfo("DataWatcher: Releasing read lock to acquire write lock for user %s", userID)
	dw.cacheManager.mutex.RUnlock()

	// Acquire write lock for hash update
	writeTimeout := 3 * time.Second
	writeLockAcquired := make(chan bool, 1)
	writeLockError := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.logError("DataWatcher: Panic during write lock acquisition for user %s: %v", userID, r)
				writeLockError <- fmt.Errorf("panic during write lock acquisition: %v", r)
			}
		}()

		dw.logInfo("DataWatcher: Attempting to acquire write lock for user %s", userID)
		dw.cacheManager.mutex.Lock()
		dw.logInfo("DataWatcher: Write lock acquired for user %s", userID)
		writeLockAcquired <- true
	}()

	select {
	case <-writeLockAcquired:
		// Write lock acquired successfully
		dw.logInfo("DataWatcher: Write lock acquired for hash update, user %s", userID)

		// Update hash and release lock immediately
		originalUserData.Hash = newHash
		dw.logInfo("DataWatcher: Hash updated in memory for user %s", userID)

		dw.cacheManager.mutex.Unlock()
		dw.logInfo("DataWatcher: Write lock released for user %s", userID)

	case err := <-writeLockError:
		dw.logError("DataWatcher: Error acquiring write lock for user %s: %v", userID, err)
		// Re-acquire read lock for the defer statement since we released it earlier
		dw.cacheManager.mutex.RLock()
		return

	case <-time.After(writeTimeout):
		dw.logError("DataWatcher: Timeout acquiring write lock for hash update, user %s", userID)
		// Re-acquire read lock for the defer statement since we released it earlier
		dw.cacheManager.mutex.RLock()
		return
	}

	// Re-acquire read lock for the defer statement since we released it earlier
	dw.cacheManager.mutex.RLock()

	dw.logInfo("DataWatcher: Hash updated for user %s", userID)

	// Verification: Check if the hash was actually updated
	if dw.logLevel >= 2 {
		verifyUserData := dw.cacheManager.GetUserData(userID)
		if verifyUserData != nil {
			verifyHash := verifyUserData.Hash
			dw.logInfo("DataWatcher: Verification - hash = '%s' for user %s", verifyHash, userID)
		} else {
			dw.logError("DataWatcher: Verification failed - CacheManager.GetUserData returned nil for user %s", userID)
		}
	}

	// Trigger force sync to persist the hash change
	dw.logInfo("DataWatcher: Starting force sync for user %s", userID)
	if err := dw.cacheManager.ForceSync(); err != nil {
		dw.logError("DataWatcher: Failed to force sync after hash update for user %s: %v", userID, err)
	} else {
		dw.logInfo("DataWatcher: Force sync completed after hash update for user %s", userID)
	}
}

// calculateAndSetUserHashAsync calculates and sets hash for user data asynchronously
func (dw *DataWatcher) calculateAndSetUserHashAsync(userID string, userData *types.UserData) {
	dw.logInfo("DataWatcher: Starting async hash calculation for user %s", userID)

	// Add timeout to prevent hanging
	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.logError("DataWatcher: Panic during hash calculation for user %s: %v", userID, r)
			}
		}()
		dw.logInfo("DataWatcher: Hash calculation goroutine started for user %s", userID)
		dw.calculateAndSetUserHash(userID, userData)
		dw.logInfo("DataWatcher: Hash calculation goroutine completed for user %s", userID)
		done <- true
	}()

	select {
	case <-done:
		// Hash calculation completed successfully
		dw.logInfo("DataWatcher: Hash calculation finished successfully for user %s", userID)
	case <-time.After(10 * time.Second):
		dw.logError("DataWatcher: Hash calculation timeout for user %s after 10 seconds", userID)
	}
}

// createUserDataSnapshot creates a snapshot of user data for hash calculation
func (dw *DataWatcher) createUserDataSnapshot(userID string, userData *types.UserData) (*UserDataSnapshot, error) {
	// Create a lightweight snapshot for hash calculation
	snapshot := &UserDataSnapshot{
		Hash:    userData.Hash,
		Sources: make(map[string]*SourceDataSnapshot),
	}

	// Convert each source data to snapshot format
	for sourceID, sourceData := range userData.Sources {
		if sourceData == nil {
			continue
		}

		sourceSnapshot := &SourceDataSnapshot{
			AppStateLatest: make([]interface{}, len(sourceData.AppStateLatest)),
			AppInfoLatest:  make([]interface{}, len(sourceData.AppInfoLatest)),
		}

		// Convert AppStateLatest
		for i, data := range sourceData.AppStateLatest {
			sourceSnapshot.AppStateLatest[i] = data
		}

		// Convert AppInfoLatest
		for i, data := range sourceData.AppInfoLatest {
			sourceSnapshot.AppInfoLatest[i] = data
		}

		// Convert Others data
		if sourceData.Others != nil {
			othersSnapshot := &OthersSnapshot{
				Topics:     make([]interface{}, len(sourceData.Others.Topics)),
				TopicLists: make([]interface{}, len(sourceData.Others.TopicLists)),
				Recommends: make([]interface{}, len(sourceData.Others.Recommends)),
				Pages:      make([]interface{}, len(sourceData.Others.Pages)),
			}

			// Convert each Others field
			for i, topic := range sourceData.Others.Topics {
				othersSnapshot.Topics[i] = topic
			}
			for i, topicList := range sourceData.Others.TopicLists {
				othersSnapshot.TopicLists[i] = topicList
			}
			for i, recommend := range sourceData.Others.Recommends {
				othersSnapshot.Recommends[i] = recommend
			}
			for i, page := range sourceData.Others.Pages {
				othersSnapshot.Pages[i] = page
			}

			sourceSnapshot.Others = othersSnapshot
		}

		snapshot.Sources[sourceID] = sourceSnapshot
	}

	return snapshot, nil
}

// Snapshot data structures for lock-free hash calculation

type UserDataSnapshot struct {
	Hash    string
	Sources map[string]*SourceDataSnapshot
}

func (s *UserDataSnapshot) GetSources() map[string]utils.SourceDataInterface {
	result := make(map[string]utils.SourceDataInterface)
	for sourceID, sourceData := range s.Sources {
		result[sourceID] = sourceData
	}
	return result
}

func (s *UserDataSnapshot) GetHash() string {
	return s.Hash
}

func (s *UserDataSnapshot) SetHash(hash string) {
	s.Hash = hash
}

type SourceDataSnapshot struct {
	AppStateLatest []interface{}
	AppInfoLatest  []interface{}
	Others         *OthersSnapshot
}

func (s *SourceDataSnapshot) GetAppStateLatest() []interface{} {
	return s.AppStateLatest
}

func (s *SourceDataSnapshot) GetAppInfoLatest() []interface{} {
	return s.AppInfoLatest
}

func (s *SourceDataSnapshot) GetOthers() utils.OthersInterface {
	if s.Others == nil {
		return nil
	}
	return s.Others
}

type OthersSnapshot struct {
	Topics     []interface{}
	TopicLists []interface{}
	Recommends []interface{}
	Pages      []interface{}
}

func (s *OthersSnapshot) GetTopics() []interface{} {
	return s.Topics
}

func (s *OthersSnapshot) GetTopicLists() []interface{} {
	return s.TopicLists
}

func (s *OthersSnapshot) GetRecommends() []interface{} {
	return s.Recommends
}

func (s *OthersSnapshot) GetPages() []interface{} {
	return s.Pages
}

// processSourceData processes a single source's data for completed hydration
func (dw *DataWatcher) processSourceData(userID, sourceID string, sourceData *types.SourceData) (int64, int64) {
	if sourceData == nil {
		return 0, 0
	}

	var pendingApps []*types.AppInfoLatestPendingData
	var appInfoLatest []*types.AppInfoLatestData

	// Step 1: Quick check and data copy with minimal lock time
	func() {
		dw.cacheManager.mutex.RLock()
		defer dw.cacheManager.mutex.RUnlock()

		// Quick check - if no pending apps, exit early
		if len(sourceData.AppInfoLatestPending) == 0 {
			return
		}

		// Copy references to pending apps for processing
		pendingApps = make([]*types.AppInfoLatestPendingData, len(sourceData.AppInfoLatestPending))
		copy(pendingApps, sourceData.AppInfoLatestPending)

		// Copy references to existing AppInfoLatest
		appInfoLatest = make([]*types.AppInfoLatestData, len(sourceData.AppInfoLatest))
		copy(appInfoLatest, sourceData.AppInfoLatest)
	}()

	// Early exit if no pending apps
	if len(pendingApps) == 0 {
		return 0, 0
	}

	dw.logInfo("DataWatcher: Processing %d pending apps for user=%s, source=%s", len(pendingApps), userID, sourceID)

	// Step 2: Lock-free processing - Check hydration completion status
	var completedApps []*types.AppInfoLatestPendingData
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i, pendingApp := range pendingApps {
		if pendingApp == nil {
			continue
		}

		if isDevEnvironment() {
			dw.logDebug("DataWatcher: Checking app %d/%d: %s", i+1, len(pendingApps), dw.getAppID(pendingApp))
		}

		if dw.isAppHydrationCompletedWithTimeout(ctx, pendingApp) {
			completedApps = append(completedApps, pendingApp)
			dw.logInfo("DataWatcher: App hydration completed: %s", dw.getAppID(pendingApp))
		}
	}

	if len(completedApps) == 0 {
		dw.logDebug("DataWatcher: No completed apps found for user=%s, source=%s", userID, sourceID)
		return int64(len(pendingApps)), 0
	}

	dw.logInfo("DataWatcher: Found %d completed apps out of %d pending for user=%s, source=%s",
		len(completedApps), len(pendingApps), userID, sourceID)

	// Step 3: Acquire write lock and move completed apps
	lockStartTime := time.Now()
	writeLockChan := make(chan bool, 1)
	go func() {
		dw.cacheManager.mutex.Lock()
		writeLockChan <- true
	}()

	select {
	case <-writeLockChan:
		dw.logDebug("DataWatcher: Write lock acquired for user=%s, source=%s", userID, sourceID)

		defer func() {
			dw.cacheManager.mutex.Unlock()
			totalLockTime := time.Since(lockStartTime)
			dw.logDebug("DataWatcher: Write lock released after %v for user=%s, source=%s", totalLockTime, userID, sourceID)
		}()

		// Move completed apps from pending to latest
		movedCount := int64(0)
		movedApps := make([]string, 0)

		for _, completedApp := range completedApps {
			// Convert to AppInfoLatestData
			latestData := dw.convertPendingToLatest(completedApp)
			if latestData != nil {
				// Check if app with same name already exists in AppInfoLatest
				appName := dw.getAppName(completedApp)
				existingIndex := -1

				// Find existing app with same name
				for i, existingApp := range sourceData.AppInfoLatest {
					if existingApp != nil {
						existingAppName := dw.getAppNameFromLatest(existingApp)
						if existingAppName == appName {
							existingIndex = i
							break
						}
					}
				}

				if existingIndex >= 0 {
					// Replace existing app with same name
					sourceData.AppInfoLatest[existingIndex] = latestData
					dw.logDebug("DataWatcher: Replaced existing app with same name: %s (index: %d)", appName, existingIndex)
				} else {
					// Add new app if no existing app with same name
					sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, latestData)
					dw.logDebug("DataWatcher: Added new app to latest: %s", appName)
				}

				movedCount++
				movedApps = append(movedApps, appName)
			}
		}

		// Remove completed apps from pending list
		if movedCount > 0 {
			newPendingList := make([]*types.AppInfoLatestPendingData, 0, len(sourceData.AppInfoLatestPending)-int(movedCount))
			completedAppIDs := make(map[string]bool)

			// Create a map of completed app IDs for efficient lookup
			for _, completedApp := range completedApps {
				appID := dw.getAppID(completedApp)
				if appID != "" {
					completedAppIDs[appID] = true
				}
			}

			// Filter out completed apps from pending list
			for _, pendingApp := range sourceData.AppInfoLatestPending {
				appID := dw.getAppID(pendingApp)
				if !completedAppIDs[appID] {
					newPendingList = append(newPendingList, pendingApp)
				}
			}

			sourceData.AppInfoLatestPending = newPendingList
			dw.logInfo("DataWatcher: Moved %d apps to latest for user=%s, source=%s: %v",
				movedCount, userID, sourceID, movedApps)
		}

		return int64(len(pendingApps)), movedCount

	case <-time.After(10 * time.Second):
		dw.logError("DataWatcher: Timeout acquiring write lock for user=%s, source=%s", userID, sourceID)
		return int64(len(pendingApps)), 0
	}
}

// isAppHydrationCompletedWithTimeout checks if app hydration is completed with timeout protection
func (dw *DataWatcher) isAppHydrationCompletedWithTimeout(ctx context.Context, pendingApp *types.AppInfoLatestPendingData) bool {
	if pendingApp == nil {
		dw.logWarning("DataWatcher: isAppHydrationCompletedWithTimeout called with nil pendingApp")
		return false
	}
	if dw.hydrator == nil {
		dw.logError("DataWatcher: Hydrator is nil, cannot check hydration completion")
		return false
	}

	// Create a channel to receive the result
	resultChan := make(chan bool, 1)

	// Run hydration check in a goroutine with timeout
	go func() {
		defer func() {
			if r := recover(); r != nil {
				dw.logError("DataWatcher: Panic in hydration check: %v", r)
				resultChan <- false
			}
		}()

		result := dw.hydrator.isAppHydrationComplete(pendingApp)
		resultChan <- result
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result
	case <-ctx.Done():
		appID := dw.getAppID(pendingApp)
		dw.logWarning("DataWatcher: Timeout checking hydration completion for app=%s", appID)
		return false
	}
}

// getAppID extracts app ID from pending app data
func (dw *DataWatcher) getAppID(pendingApp *types.AppInfoLatestPendingData) string {
	if pendingApp == nil {
		return "unknown"
	}

	if pendingApp.RawData != nil {
		if pendingApp.RawData.AppID != "" {
			return pendingApp.RawData.AppID
		}
		if pendingApp.RawData.ID != "" {
			return pendingApp.RawData.ID
		}
		if pendingApp.RawData.Name != "" {
			return pendingApp.RawData.Name
		}
	}

	if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil {
		if pendingApp.AppInfo.AppEntry.AppID != "" {
			return pendingApp.AppInfo.AppEntry.AppID
		}
		if pendingApp.AppInfo.AppEntry.ID != "" {
			return pendingApp.AppInfo.AppEntry.ID
		}
		if pendingApp.AppInfo.AppEntry.Name != "" {
			return pendingApp.AppInfo.AppEntry.Name
		}
	}

	return "unknown"
}

// getAppName extracts app name from pending app data for deduplication
func (dw *DataWatcher) getAppName(pendingApp *types.AppInfoLatestPendingData) string {
	if pendingApp == nil {
		return "unknown"
	}

	// Try to get name from RawData first
	if pendingApp.RawData != nil {
		if pendingApp.RawData.Name != "" {
			return pendingApp.RawData.Name
		}
	}

	// Try to get name from AppInfo
	if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil {
		if pendingApp.AppInfo.AppEntry.Name != "" {
			return pendingApp.AppInfo.AppEntry.Name
		}
	}

	return "unknown"
}

// getAppNameFromLatest extracts app name from latest app data for deduplication
func (dw *DataWatcher) getAppNameFromLatest(latestApp *types.AppInfoLatestData) string {
	if latestApp == nil {
		return "unknown"
	}

	// Try to get name from RawData first
	if latestApp.RawData != nil {
		if latestApp.RawData.Name != "" {
			return latestApp.RawData.Name
		}
	}

	// Try to get name from AppInfo
	if latestApp.AppInfo != nil && latestApp.AppInfo.AppEntry != nil {
		if latestApp.AppInfo.AppEntry.Name != "" {
			return latestApp.AppInfo.AppEntry.Name
		}
	}

	// Try to get name from AppSimpleInfo
	if latestApp.AppSimpleInfo != nil {
		if latestApp.AppSimpleInfo.AppName != "" {
			return latestApp.AppSimpleInfo.AppName
		}
	}

	return "unknown"
}

// convertPendingToLatest converts AppInfoLatestPendingData to AppInfoLatestData
func (dw *DataWatcher) convertPendingToLatest(pendingApp *types.AppInfoLatestPendingData) *types.AppInfoLatestData {
	if pendingApp == nil {
		dw.logWarning("DataWatcher: convertPendingToLatest called with nil pendingApp")
		return nil
	}

	// Validate that the pending app has essential data
	hasRawData := pendingApp.RawData != nil
	hasAppInfo := pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil
	hasPackageInfo := pendingApp.RawPackage != "" || pendingApp.RenderedPackage != ""

	// Return nil if no essential data is present
	if !hasRawData && !hasAppInfo && !hasPackageInfo {
		appID := dw.getAppID(pendingApp)
		dw.logWarning("DataWatcher: Skipping conversion of pending app %s - no essential data found", appID)
		return nil
	}

	// Additional validation for data integrity
	if hasRawData && (pendingApp.RawData.AppID == "" && pendingApp.RawData.ID == "" && pendingApp.RawData.Name == "") {
		dw.logWarning("DataWatcher: Skipping conversion - RawData exists but lacks identifying information")
		return nil
	}

	if hasAppInfo && (pendingApp.AppInfo.AppEntry.AppID == "" && pendingApp.AppInfo.AppEntry.ID == "" && pendingApp.AppInfo.AppEntry.Name == "") {
		dw.logWarning("DataWatcher: Skipping conversion - AppInfo exists but lacks identifying information")
		return nil
	}

	// Create the latest app data structure
	latestApp := &types.AppInfoLatestData{
		Type:      types.AppInfoLatest,
		Timestamp: time.Now().Unix(),
	}

	// Copy relevant data from pending to latest
	if pendingApp.AppInfo != nil {
		latestApp.AppInfo = pendingApp.AppInfo
	}

	// Copy RawData directly (same type: *ApplicationInfoEntry)
	if pendingApp.RawData != nil {
		latestApp.RawData = pendingApp.RawData

		// Validate and fix AppLabels if needed
		types.ValidateAndFixAppLabels(nil, latestApp.RawData)
	}

	// Copy package information
	latestApp.RawPackage = pendingApp.RawPackage
	latestApp.RenderedPackage = pendingApp.RenderedPackage

	// Copy Values if present
	if pendingApp.Values != nil {
		latestApp.Values = pendingApp.Values
	}

	// Copy version information
	latestApp.Version = pendingApp.Version

	// Create AppSimpleInfo from available data
	latestApp.AppSimpleInfo = dw.createAppSimpleInfo(pendingApp)

	return latestApp
}

// GetMetrics returns DataWatcher metrics
func (dw *DataWatcher) GetMetrics() DataWatcherMetrics {
	dw.metricsMutex.RLock()
	defer dw.metricsMutex.RUnlock()

	dw.mutex.RLock()
	isRunning := dw.isRunning
	dw.mutex.RUnlock()

	return DataWatcherMetrics{
		IsRunning:          isRunning,
		TotalAppsProcessed: dw.totalAppsProcessed,
		TotalAppsMoved:     dw.totalAppsMoved,
		LastRunTime:        dw.lastRunTime,
		Interval:           dw.interval,
	}
}

// DataWatcherMetrics contains metrics for the DataWatcher
type DataWatcherMetrics struct {
	IsRunning          bool          `json:"is_running"`
	TotalAppsProcessed int64         `json:"total_apps_processed"`
	TotalAppsMoved     int64         `json:"total_apps_moved"`
	LastRunTime        time.Time     `json:"last_run_time"`
	Interval           time.Duration `json:"interval"`
}

// SetInterval sets the monitoring interval
func (dw *DataWatcher) SetInterval(interval time.Duration) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if interval < time.Second {
		interval = time.Second // Minimum 1 second
	}

	dw.interval = interval
	dw.logInfo("DataWatcher interval set to: %v", interval)
}

// createAppSimpleInfo creates an AppSimpleInfo from pending app data
func (dw *DataWatcher) createAppSimpleInfo(pendingApp *types.AppInfoLatestPendingData) *types.AppSimpleInfo {
	if pendingApp == nil {
		return nil
	}

	appSimpleInfo := &types.AppSimpleInfo{
		AppDescription: make(map[string]string),
		AppTitle:       make(map[string]string),
	}

	// Extract information from RawData if available
	if pendingApp.RawData != nil {
		// Use AppID as the primary identifier
		if pendingApp.RawData.AppID != "" {
			appSimpleInfo.AppID = pendingApp.RawData.AppID
		} else if pendingApp.RawData.ID != "" {
			appSimpleInfo.AppID = pendingApp.RawData.ID
		}

		// Use Name for AppName
		if pendingApp.RawData.Name != "" {
			appSimpleInfo.AppName = pendingApp.RawData.Name
		} else if len(pendingApp.RawData.Title) > 0 {
			// Fallback to first available title if name is empty
			appSimpleInfo.AppName = dw.getLocalizedStringValue(pendingApp.RawData.Title, "en-US")
		}

		// Use Icon for AppIcon
		appSimpleInfo.AppIcon = pendingApp.RawData.Icon

		// Copy multilingual Description to AppDescription
		if len(pendingApp.RawData.Description) > 0 {
			appSimpleInfo.AppDescription = dw.copyMultilingualMap(pendingApp.RawData.Description)
		}

		// Copy multilingual Title to AppTitle
		if len(pendingApp.RawData.Title) > 0 {
			appSimpleInfo.AppTitle = dw.copyMultilingualMap(pendingApp.RawData.Title)
		}

		// Use Version for AppVersion
		appSimpleInfo.AppVersion = pendingApp.RawData.Version
	}

	// Fallback to AppInfo data if RawData is insufficient
	if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil {
		entry := pendingApp.AppInfo.AppEntry

		// Fill missing AppID
		if appSimpleInfo.AppID == "" {
			if entry.AppID != "" {
				appSimpleInfo.AppID = entry.AppID
			} else if entry.ID != "" {
				appSimpleInfo.AppID = entry.ID
			}
		}

		// Fill missing AppName
		if appSimpleInfo.AppName == "" {
			if entry.Name != "" {
				appSimpleInfo.AppName = entry.Name
			} else if len(entry.Title) > 0 {
				appSimpleInfo.AppName = dw.getLocalizedStringValue(entry.Title, "en-US")
			}
		}

		// Fill missing AppIcon
		if appSimpleInfo.AppIcon == "" {
			appSimpleInfo.AppIcon = entry.Icon
		}

		// Fill missing AppDescription
		if len(appSimpleInfo.AppDescription) == 0 && len(entry.Description) > 0 {
			appSimpleInfo.AppDescription = dw.copyMultilingualMap(entry.Description)
		}

		// Fill missing AppTitle
		if len(appSimpleInfo.AppTitle) == 0 && len(entry.Title) > 0 {
			appSimpleInfo.AppTitle = dw.copyMultilingualMap(entry.Title)
		}

		// Fill missing AppVersion
		if appSimpleInfo.AppVersion == "" {
			appSimpleInfo.AppVersion = entry.Version
		}
	}

	// Use pendingApp version if still empty
	if appSimpleInfo.AppVersion == "" && pendingApp.Version != "" {
		appSimpleInfo.AppVersion = pendingApp.Version
	}

	// Return nil if no essential information is available
	if appSimpleInfo.AppID == "" && appSimpleInfo.AppName == "" {
		dw.logWarning("DataWatcher: createAppSimpleInfo - no essential app information available")
		return nil
	}

	return appSimpleInfo
}

// getLocalizedStringValue gets localized string from multilingual map with fallback logic
func (dw *DataWatcher) getLocalizedStringValue(multiLangMap map[string]string, preferredLang string) string {
	if len(multiLangMap) == 0 {
		return ""
	}

	// First try preferred language
	if value, exists := multiLangMap[preferredLang]; exists && value != "" {
		return value
	}

	// Try common fallback languages in order
	fallbackLanguages := []string{"en-US", "en", "zh-CN", "zh"}
	for _, lang := range fallbackLanguages {
		if value, exists := multiLangMap[lang]; exists && value != "" {
			return value
		}
	}

	// Return first available value
	for _, value := range multiLangMap {
		if value != "" {
			return value
		}
	}

	return ""
}

// copyMultilingualMap creates a deep copy of a multilingual map
func (dw *DataWatcher) copyMultilingualMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return make(map[string]string)
	}

	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// ForceCalculateUserHash forces hash calculation for a user regardless of app movement
func (dw *DataWatcher) ForceCalculateUserHash(userID string) error {
	dw.logInfo("DataWatcher: Force calculating hash for user %s", userID)

	// Get user data from cache manager
	userData := dw.cacheManager.GetUserData(userID)
	if userData == nil {
		return fmt.Errorf("user data not found for user %s", userID)
	}

	// Call hash calculation directly
	dw.calculateAndSetUserHash(userID, userData)
	return nil
}

// ForceCalculateAllUsersHash forces hash calculation for all users
func (dw *DataWatcher) ForceCalculateAllUsersHash() error {
	dw.logInfo("DataWatcher: Force calculating hash for all users")

	// Get all users data
	allUsersData := dw.cacheManager.GetAllUsersData()
	if len(allUsersData) == 0 {
		return fmt.Errorf("no users found in cache")
	}

	for userID, userData := range allUsersData {
		if userData != nil {
			dw.logInfo("DataWatcher: Force calculating hash for user: %s", userID)
			dw.calculateAndSetUserHash(userID, userData)
		}
	}

	return nil
}
