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
	dataSender   *DataSender
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
}

// NewDataWatcher creates a new DataWatcher instance
func NewDataWatcher(cacheManager *CacheManager, hydrator *Hydrator, dataSender *DataSender) *DataWatcher {
	return &DataWatcher{
		cacheManager:           cacheManager,
		hydrator:               hydrator,
		dataSender:             dataSender,
		interval:               30 * time.Second, // Run every 30 seconds
		stopChan:               make(chan struct{}),
		isRunning:              false,
		activeHashCalculations: make(map[string]bool),
	}
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
	glog.Infof("Starting DataWatcher with interval: %v", dw.interval)

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

	glog.Infof("Stopping DataWatcher...")
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
	glog.Infof("DataWatcher monitoring loop started")
	defer glog.Infof("DataWatcher monitoring loop stopped")

	ticker := time.NewTicker(dw.interval)
	defer ticker.Stop()

	// Run once immediately
	dw.processCompletedApps()

	for {
		select {
		case <-ctx.Done():
			glog.Infof("DataWatcher stopped due to context cancellation")
			return
		case <-dw.stopChan:
			glog.Infof("DataWatcher stopped due to explicit stop")
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
		glog.Warningf("DataWatcher: Previous processing cycle still running, skipping this cycle")
		return
	}
	defer dw.processingMutex.Unlock()

	processingStart := time.Now()
	dw.metricsMutex.Lock()
	dw.lastRunTime = time.Now()
	dw.metricsMutex.Unlock()

	glog.Infof("DataWatcher: Starting to process completed apps")

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
		glog.Errorf("DataWatcher: Timeout getting all users data")
		return
	}

	if len(allUsersData) == 0 {
		glog.Infof("DataWatcher: No users data found, processing cycle completed in %v", time.Since(processingStart))
		return
	}

	glog.Infof("DataWatcher: Found %d users to process", len(allUsersData))
	totalProcessed := int64(0)
	totalMoved := int64(0)

	// Process users in batches to avoid holding locks too long
	const batchSize = 5
	userCount := 0
	userBatch := make([]string, 0, batchSize)
	userDataBatch := make(map[string]*types.UserData)

	for userID, userData := range allUsersData {
		userBatch = append(userBatch, userID)
		userDataBatch[userID] = userData
		userCount++

		// Process batch when it's full or we've reached the end
		if len(userBatch) >= batchSize || userCount == len(allUsersData) {
			batchProcessed, batchMoved := dw.processUserBatch(ctx, userBatch, userDataBatch)
			totalProcessed += batchProcessed
			totalMoved += batchMoved

			// Clear batch for next iteration
			userBatch = userBatch[:0]
			userDataBatch = make(map[string]*types.UserData)

			// Check timeout between batches
			select {
			case <-ctx.Done():
				glog.Errorf("DataWatcher: Timeout during batch processing after processing %d users", userCount)
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
	if totalMoved > 0 {
		glog.Infof("DataWatcher: Processing cycle completed in %v - %d apps processed, %d moved to AppInfoLatest",
			processingDuration, totalProcessed, totalMoved)
	} else {
		glog.Infof("DataWatcher: Processing cycle completed in %v - %d apps processed, no moves needed",
			processingDuration, totalProcessed)
	}
}

// processUserBatch processes a batch of users
func (dw *DataWatcher) processUserBatch(ctx context.Context, userIDs []string, userDataMap map[string]*types.UserData) (int64, int64) {
	totalProcessed := int64(0)
	totalMoved := int64(0)

	for i, userID := range userIDs {
		// Check timeout during batch processing
		select {
		case <-ctx.Done():
			glog.Errorf("DataWatcher: Timeout during user batch processing (user %d/%d)", i+1, len(userIDs))
			return totalProcessed, totalMoved
		default:
		}

		userData := userDataMap[userID]
		if userData == nil {
			continue
		}

		glog.Infof("DataWatcher: Processing user %d/%d in batch: %s", i+1, len(userIDs), userID)
		processed, moved := dw.processUserData(userID, userData)
		totalProcessed += processed
		totalMoved += moved
		glog.V(2).Infof("DataWatcher: User %s completed: %d processed, %d moved", userID, processed, moved)
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
			glog.Infof("DataWatcher: %d apps moved for user %s, scheduling hash calculation", totalMoved, userID)
		} else {
			glog.Infof("DataWatcher: Hash is empty for user %s, scheduling hash calculation", userID)
		}

		// Check if hash calculation is already in progress for this user
		dw.hashMutex.Lock()
		if dw.activeHashCalculations[userID] {
			dw.hashMutex.Unlock()
			glog.Warningf("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
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
				glog.Infof("DataWatcher: Hash calculation tracking cleaned up for user %s", userID)
			}()

			// Wait a short time to ensure all source processing locks are released
			time.Sleep(100 * time.Millisecond)
			glog.Infof("DataWatcher: Starting hash calculation for user %s", userID)

			// Call the hash calculation function directly without additional tracking
			dw.calculateAndSetUserHashWithRetry(userID, userData)
		}()
	} else {
		glog.V(2).Infof("DataWatcher: No apps moved and hash exists for user %s, skipping hash calculation", userID)
	}

	return totalProcessed, totalMoved
}

// calculateAndSetUserHash calculates and sets the hash for user data (with tracking)
func (dw *DataWatcher) calculateAndSetUserHash(userID string, userData *types.UserData) {
	// Add a per-user calculation flag to prevent concurrent execution
	var isCalculatingKey = "isCalculating_" + userID

	// Use a map in DataWatcher to track per-user calculation state
	if dw.activeHashCalculations[isCalculatingKey] {
		glog.Infof("DataWatcher: Hash calculation already in progress for user %s (isCalculating), skipping", userID)
		return
	}

	dw.activeHashCalculations[isCalculatingKey] = true
	dw.hashMutex.Lock()
	// Also keep the original tracking for compatibility
	if dw.activeHashCalculations[userID] {
		dw.hashMutex.Unlock()
		glog.Infof("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
		return
	}
	dw.activeHashCalculations[userID] = true
	dw.hashMutex.Unlock()

	defer func() {
		// Clean up tracking when done
		dw.hashMutex.Lock()
		delete(dw.activeHashCalculations, userID)
		delete(dw.activeHashCalculations, isCalculatingKey)
		dw.hashMutex.Unlock()
		glog.Infof("DataWatcher: Hash calculation tracking cleaned up for user %s", userID)
	}()

	// Call the direct calculation function
	_ = dw.calculateAndSetUserHashDirect(userID, userData)
}

// calculateAndSetUserHashWithRetry calculates hash with retry mechanism for data consistency
func (dw *DataWatcher) calculateAndSetUserHashWithRetry(userID string, userData *types.UserData) {
	maxRetries := 3
	retryDelay := 200 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		glog.Infof("DataWatcher: Hash calculation attempt %d/%d for user %s", attempt, maxRetries, userID)

		// Check if calculation is already in progress
		dw.hashMutex.Lock()
		if dw.activeHashCalculations[userID] {
			dw.hashMutex.Unlock()
			glog.Infof("DataWatcher: Hash calculation already in progress for user %s, waiting...", userID)

			// Wait for current calculation to complete
			for i := 0; i < 50; i++ { // Wait up to 5 seconds
				time.Sleep(100 * time.Millisecond)
				dw.hashMutex.Lock()
				if !dw.activeHashCalculations[userID] {
					dw.hashMutex.Unlock()
					break
				}
				dw.hashMutex.Unlock()
			}

			// Verify if we need to recalculate after waiting
			currentUserData := dw.cacheManager.GetUserData(userID)
			if currentUserData != nil && currentUserData.Hash != "" {
				glog.Infof("DataWatcher: Hash calculation completed by another process for user %s", userID)
				return
			}
		}

		// Mark calculation as in progress
		dw.activeHashCalculations[userID] = true
		dw.hashMutex.Unlock()

		// Perform hash calculation
		success := dw.calculateAndSetUserHashDirect(userID, userData)

		// Clean up tracking
		dw.hashMutex.Lock()
		delete(dw.activeHashCalculations, userID)
		dw.hashMutex.Unlock()

		if success {
			glog.Infof("DataWatcher: Hash calculation completed successfully for user %s", userID)
			return
		}

		if attempt < maxRetries {
			glog.Warningf("DataWatcher: Hash calculation failed for user %s, retrying in %v", userID, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff
		}
	}

	glog.Errorf("DataWatcher: Hash calculation failed after %d attempts for user %s", maxRetries, userID)
}

// calculateAndSetUserHashDirect calculates hash without tracking (used internally by goroutines)
func (dw *DataWatcher) calculateAndSetUserHashDirect(userID string, userData *types.UserData) bool {
	glog.Infof("DataWatcher: Starting direct hash calculation for user %s", userID)

	// Get the original user data from cache manager to ensure we have the latest reference
	originalUserData := dw.cacheManager.GetUserData(userID)
	if originalUserData == nil {
		glog.Errorf("DataWatcher: Failed to get user data from cache manager for user %s", userID)
		return false
	}

	// Create snapshot for hash calculation without holding any locks
	glog.Infof("DataWatcher: Creating user data snapshot for user %s", userID)
	snapshot, err := utils.CreateUserDataSnapshot(userID, originalUserData)
	if err != nil {
		glog.Errorf("DataWatcher: Failed to create user data snapshot for user %s: %v", userID, err)
		return false
	}

	glog.Infof("DataWatcher: Calculating hash for user %s", userID)
	// Calculate hash using the snapshot
	newHash, err := utils.CalculateUserDataHash(snapshot)
	if err != nil {
		glog.Errorf("DataWatcher: Failed to calculate hash for user %s: %v", userID, err)
		return false
	}

	// Get current hash for comparison
	currentHash := originalUserData.Hash
	glog.Infof("DataWatcher: Hash comparison for user %s - current: '%s', new: '%s'", userID, currentHash, newHash)

	if currentHash == newHash {
		glog.V(2).Infof("DataWatcher: Hash unchanged for user %s: %s", userID, newHash)
		return true
	}

	glog.Infof("DataWatcher: Hash changed for user %s: %s -> %s", userID, currentHash, newHash)

	// Use a single write lock acquisition with timeout to avoid deadlock
	writeTimeout := 5 * time.Second
	writeLockAcquired := make(chan bool, 1)
	writeLockError := make(chan error, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("DataWatcher: Panic during write lock acquisition for user %s: %v", userID, r)
				writeLockError <- fmt.Errorf("panic during write lock acquisition: %v", r)
			}
		}()

		glog.Infof("DataWatcher: Attempting to acquire write lock for user %s", userID)
		glog.Infof("[LOCK] dw.cacheManager.mutex.Lock() @439 Start")
		dw.cacheManager.mutex.Lock()
		glog.Infof("DataWatcher: Write lock acquired for user %s", userID)
		glog.Infof("[LOCK] dw.cacheManager.mutex.Lock() @439 Success")
		writeLockAcquired <- true
	}()

	select {
	case <-writeLockAcquired:
		// Write lock acquired successfully
		glog.Infof("DataWatcher: Write lock acquired for hash update, user %s", userID)

		// Update hash and release lock immediately
		originalUserData.Hash = newHash
		glog.Infof("DataWatcher: Hash updated in memory for user %s", userID)

		glog.Infof("[LOCK] dw.cacheManager.mutex.Unlock() @453 Start")
		dw.cacheManager.mutex.Unlock()
		glog.Infof("DataWatcher: Write lock released for user %s", userID)

	case err := <-writeLockError:
		glog.Errorf("DataWatcher: Error acquiring write lock for user %s: %v", userID, err)
		return false

	case <-time.After(writeTimeout):
		glog.Errorf("DataWatcher: Timeout acquiring write lock for hash update, user %s", userID)
		return false
	}

	glog.Infof("DataWatcher: Hash updated for user %s", userID)

	// Verification: Check if the hash was actually updated
	if glog.V(2) {
		verifyUserData := dw.cacheManager.GetUserData(userID)
		if verifyUserData != nil {
			verifyHash := verifyUserData.Hash
			glog.Infof("DataWatcher: Verification - hash = '%s' for user %s", verifyHash, userID)
		} else {
			glog.Errorf("DataWatcher: Verification failed - CacheManager.GetUserData returned nil for user %s", userID)
		}
	}

	// Trigger force sync to persist the hash change
	glog.Infof("DataWatcher: Starting force sync for user %s", userID)
	if err := dw.cacheManager.ForceSync(); err != nil {
		glog.Errorf("DataWatcher: Failed to force sync after hash update for user %s: %v", userID, err)
		return false
	} else {
		glog.Infof("DataWatcher: Force sync completed after hash update for user %s", userID)
	}

	return true
}

// calculateAndSetUserHashAsync calculates and sets hash for user data asynchronously
func (dw *DataWatcher) calculateAndSetUserHashAsync(userID string, userData *types.UserData) {
	glog.Infof("DataWatcher: Starting async hash calculation for user %s", userID)

	// Add timeout to prevent hanging
	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("DataWatcher: Panic during hash calculation for user %s: %v", userID, r)
			}
		}()
		glog.Infof("DataWatcher: Hash calculation goroutine started for user %s", userID)
		dw.calculateAndSetUserHash(userID, userData)
		glog.Infof("DataWatcher: Hash calculation goroutine completed for user %s", userID)
		done <- true
	}()

	select {
	case <-done:
		// Hash calculation completed successfully
		glog.Infof("DataWatcher: Hash calculation finished successfully for user %s", userID)
	case <-time.After(10 * time.Second):
		glog.Errorf("DataWatcher: Hash calculation timeout for user %s after 10 seconds", userID)
	}
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
		glog.Infof("[LOCK] dw.cacheManager.mutex.RLock() @660 Start")
		dw.cacheManager.mutex.RLock()
		defer func() {
			dw.cacheManager.mutex.RUnlock()
			glog.Infof("[LOCK] dw.cacheManager.mutex.RUnlock() @660 End")
		}()

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

	glog.Infof("DataWatcher: Processing %d pending apps for user=%s, source=%s", len(pendingApps), userID, sourceID)

	// Step 2: Lock-free processing - Check hydration completion status
	var completedApps []*types.AppInfoLatestPendingData
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i, pendingApp := range pendingApps {
		if pendingApp == nil {
			continue
		}

		if isDevEnvironment() {
			glog.V(2).Infof("DataWatcher: Checking app %d/%d: %s", i+1, len(pendingApps), dw.getAppID(pendingApp))
		}

		if dw.isAppHydrationCompletedWithTimeout(ctx, pendingApp) {
			completedApps = append(completedApps, pendingApp)
			glog.Infof("DataWatcher: App hydration completed: %s", dw.getAppID(pendingApp))
		}
	}

	if len(completedApps) == 0 {
		glog.V(2).Infof("DataWatcher: No completed apps found for user=%s, source=%s", userID, sourceID)
		return int64(len(pendingApps)), 0
	}

	glog.Infof("DataWatcher: Found %d completed apps out of %d pending for user=%s, source=%s",
		len(completedApps), len(pendingApps), userID, sourceID)

	// Step 3: Acquire write lock and move completed apps
	lockStartTime := time.Now()
	writeLockChan := make(chan bool, 1)
	go func() {
		glog.Infof("[LOCK] dw.cacheManager.mutex.Lock() @716 Start")
		dw.cacheManager.mutex.Lock()
		glog.Infof("[LOCK] dw.cacheManager.mutex.Lock() @716 Success")
		writeLockChan <- true
	}()

	select {
	case <-writeLockChan:
		glog.Infof("DataWatcher: Write lock acquired for user=%s, source=%s", userID, sourceID)

		defer func() {
			glog.Infof("[LOCK] dw.cacheManager.mutex.Unlock() @725 Start")
			dw.cacheManager.mutex.Unlock()
			totalLockTime := time.Since(lockStartTime)
			glog.Infof("DataWatcher: Write lock released after %v for user=%s, source=%s", totalLockTime, userID, sourceID)
		}()

		// Move completed apps from pending to latest
		movedCount := int64(0)
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
					glog.Infof("DataWatcher: Replaced existing app with same name: %s (index: %d)", appName, existingIndex)
				} else {
					// Add new app if no existing app with same name
					sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, latestData)
					glog.Infof("DataWatcher: Added new app to latest: %s", appName)
				}

				movedCount++

				// Send system notification for new app ready
				dw.sendNewAppReadyNotification(userID, completedApp, sourceID)
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
			glog.Infof("DataWatcher: Updated pending list: %d -> %d apps for user=%s, source=%s",
				len(sourceData.AppInfoLatestPending)+int(movedCount), len(sourceData.AppInfoLatestPending), userID, sourceID)
		}

		return int64(len(pendingApps)), movedCount

	case <-time.After(10 * time.Second):
		glog.Errorf("DataWatcher: Timeout acquiring write lock for user=%s, source=%s", userID, sourceID)
		return int64(len(pendingApps)), 0
	}
}

// isAppHydrationCompletedWithTimeout checks if app hydration is completed with timeout protection
func (dw *DataWatcher) isAppHydrationCompletedWithTimeout(ctx context.Context, pendingApp *types.AppInfoLatestPendingData) bool {
	if pendingApp == nil {
		glog.V(2).Infof("DataWatcher: isAppHydrationCompletedWithTimeout called with nil pendingApp")
		return false
	}
	if dw.hydrator == nil {
		glog.Errorf("DataWatcher: Hydrator is nil, cannot check hydration completion")
		return false
	}

	// Create a channel to receive the result
	resultChan := make(chan bool, 1)

	// Run hydration check in a goroutine with timeout
	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("DataWatcher: Panic in hydration check: %v", r)
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
		glog.Warningf("DataWatcher: Timeout checking hydration completion for app=%s", appID)
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
		glog.Warningf("DataWatcher: convertPendingToLatest called with nil pendingApp")
		return nil
	}

	// Validate that the pending app has essential data
	hasRawData := pendingApp.RawData != nil
	hasAppInfo := pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil
	hasPackageInfo := pendingApp.RawPackage != "" || pendingApp.RenderedPackage != ""

	// Return nil if no essential data is present
	if !hasRawData && !hasAppInfo && !hasPackageInfo {
		appID := dw.getAppID(pendingApp)
		glog.Warningf("DataWatcher: Skipping conversion of pending app %s - no essential data found", appID)
		return nil
	}

	// Additional validation for data integrity
	if hasRawData && (pendingApp.RawData.AppID == "" && pendingApp.RawData.ID == "" && pendingApp.RawData.Name == "") {
		glog.Warningf("DataWatcher: Skipping conversion - RawData exists but lacks identifying information")
		return nil
	}

	if hasAppInfo && (pendingApp.AppInfo.AppEntry.AppID == "" && pendingApp.AppInfo.AppEntry.ID == "" && pendingApp.AppInfo.AppEntry.Name == "") {
		glog.Warningf("DataWatcher: Skipping conversion - AppInfo exists but lacks identifying information")
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
	if pendingApp.AppSimpleInfo != nil {
		latestApp.AppSimpleInfo = pendingApp.AppSimpleInfo
	} else {
		latestApp.AppSimpleInfo = dw.createAppSimpleInfo(pendingApp)
	}

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
	glog.Infof("DataWatcher interval set to: %v", interval)
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

		// Use Categories for App Categories
		if len(pendingApp.RawData.Categories) > 0 {
			appSimpleInfo.Categories = pendingApp.RawData.Categories
		}
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

		// Fill missing Categories
		if len(appSimpleInfo.Categories) == 0 && len(entry.Categories) > 0 {
			appSimpleInfo.Categories = entry.Categories
		}
	}

	// Use pendingApp version if still empty
	if appSimpleInfo.AppVersion == "" && pendingApp.Version != "" {
		appSimpleInfo.AppVersion = pendingApp.Version
	}

	// Return nil if no essential information is available
	if appSimpleInfo.AppID == "" && appSimpleInfo.AppName == "" {
		glog.Warningf("DataWatcher: createAppSimpleInfo - no essential app information available")
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
	glog.Infof("DataWatcher: Force calculating hash for user %s", userID)

	// Get user data from cache manager
	userData := dw.cacheManager.GetUserData(userID)
	if userData == nil {
		return fmt.Errorf("user data not found for user %s", userID)
	}

	// Call hash calculation directly
	dw.calculateAndSetUserHashWithRetry(userID, userData)
	return nil
}

// ForceCalculateAllUsersHash forces hash calculation for all users
func (dw *DataWatcher) ForceCalculateAllUsersHash() error {
	glog.Infof("DataWatcher: Force calculating hash for all users")

	// Get all users data
	allUsersData := dw.cacheManager.GetAllUsersData()
	if len(allUsersData) == 0 {
		return fmt.Errorf("no users found in cache")
	}

	for userID, userData := range allUsersData {
		if userData != nil {
			glog.Infof("DataWatcher: Force calculating hash for user: %s", userID)
			dw.calculateAndSetUserHash(userID, userData)
		}
	}

	return nil
}

// getAppVersion extracts app version from pending app data
func (dw *DataWatcher) getAppVersion(pendingApp *types.AppInfoLatestPendingData) string {
	if pendingApp == nil {
		return ""
	}

	// Try to get version from RawData first
	if pendingApp.RawData != nil {
		if pendingApp.RawData.Version != "" {
			return pendingApp.RawData.Version
		}
	}

	// Try to get version from AppInfo
	if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil {
		if pendingApp.AppInfo.AppEntry.Version != "" {
			return pendingApp.AppInfo.AppEntry.Version
		}
	}

	// Try to get version from pending app's version field
	if pendingApp.Version != "" {
		return pendingApp.Version
	}

	return ""
}

// sendNewAppReadyNotification sends a system notification for a new app ready
func (dw *DataWatcher) sendNewAppReadyNotification(userID string, completedApp *types.AppInfoLatestPendingData, sourceID string) {
	if completedApp == nil {
		glog.Warningf("DataWatcher: sendNewAppReadyNotification called with nil completedApp")
		return
	}

	if dw.dataSender == nil {
		glog.Warningf("DataWatcher: dataSender is nil, unable to send notification")
		return
	}

	appName := dw.getAppName(completedApp)
	appVersion := dw.getAppVersion(completedApp)

	// Create extensions map with app information
	extensions := make(map[string]string)
	extensions["app_name"] = appName
	extensions["app_version"] = appVersion
	extensions["source"] = sourceID

	extensionsObj := make(map[string]interface{})
	extensionsObj["app_info"] = completedApp

	// Create market system update
	update := &types.MarketSystemUpdate{
		Timestamp:     time.Now().Unix(),
		User:          userID,
		NotifyType:    "market_system_point",
		Point:         "new_app_ready",
		Extensions:    extensions,
		ExtensionsObj: extensionsObj,
	}

	// Send the notification
	if err := dw.dataSender.SendMarketSystemUpdate(*update); err != nil {
		glog.Errorf("DataWatcher: Failed to send new app ready notification for app %s: %v", appName, err)
	} else {
		glog.Infof("DataWatcher: Successfully sent new app ready notification for app %s (version: %s, source: %s)", appName, appVersion, sourceID)
	}
}
