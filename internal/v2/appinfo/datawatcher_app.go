package appinfo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	isRunning    int32 // 0 = false, 1 = true, using atomic operations
	stopChan     chan struct{}

	// Processing mutex to ensure only one cycle runs at a time
	processingMutex sync.Mutex

	// Active hash calculations tracking
	activeHashCalculations map[string]bool
	hashMutex              sync.Mutex

	// Metrics - using atomic operations for thread safety
	totalAppsProcessed int64
	totalAppsMoved     int64
	lastRunTime        int64 // Unix timestamp for atomic operations
}

// NewDataWatcher creates a new DataWatcher instance
func NewDataWatcher(cacheManager *CacheManager, hydrator *Hydrator, dataSender *DataSender) *DataWatcher {
	return &DataWatcher{
		cacheManager:           cacheManager,
		hydrator:               hydrator,
		dataSender:             dataSender,
		interval:               30 * time.Second, // Run every 30 seconds
		stopChan:               make(chan struct{}),
		isRunning:              0, // Initialize as false
		activeHashCalculations: make(map[string]bool),
	}
}

// Start begins the data watching process
func (dw *DataWatcher) Start(ctx context.Context) error {
	if atomic.LoadInt32(&dw.isRunning) == 1 {
		return fmt.Errorf("DataWatcher is already running")
	}

	if dw.cacheManager == nil {
		return fmt.Errorf("CacheManager is required for DataWatcher")
	}

	if dw.hydrator == nil {
		return fmt.Errorf("Hydrator is required for DataWatcher")
	}

	atomic.StoreInt32(&dw.isRunning, 1)
	glog.Infof("Starting DataWatcher with interval: %v", time.Duration(atomic.LoadInt64((*int64)(&dw.interval))))

	// Start the monitoring goroutine
	go dw.watchLoop(ctx)

	return nil
}

// Stop stops the data watching process
func (dw *DataWatcher) Stop() {
	if atomic.LoadInt32(&dw.isRunning) == 0 {
		return
	}

	glog.Infof("Stopping DataWatcher...")
	close(dw.stopChan)
	atomic.StoreInt32(&dw.isRunning, 0)
}

// IsRunning returns whether the DataWatcher is currently running
func (dw *DataWatcher) IsRunning() bool {
	return atomic.LoadInt32(&dw.isRunning) == 1
}

// watchLoop is the main monitoring loop
func (dw *DataWatcher) watchLoop(ctx context.Context) {
	glog.Infof("DataWatcher monitoring loop started")
	defer glog.Infof("DataWatcher monitoring loop stopped")

	ticker := time.NewTicker(time.Duration(atomic.LoadInt64((*int64)(&dw.interval))))
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
	atomic.StoreInt64(&dw.lastRunTime, processingStart.Unix())

	glog.Infof("DataWatcher: Starting to process completed apps")

	// Create timeout context for entire processing cycle
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Get all users data from cache manager with timeout
	var allUsersData map[string]*types.UserData

	// Use fallback method with TryRLock to avoid blocking
	allUsersData = dw.cacheManager.GetAllUsersDataWithFallback()

	if len(allUsersData) == 0 {
		glog.Infof("DataWatcher: No users data found, processing cycle completed")
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
	atomic.AddInt64(&dw.totalAppsProcessed, totalProcessed)
	atomic.AddInt64(&dw.totalAppsMoved, totalMoved)

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

		// Schedule hash calculation in a separate goroutine without setting the flag here
		go func() {
			// Check if hash calculation is already in progress for this user
			dw.hashMutex.Lock()
			if dw.activeHashCalculations[userID] {
				dw.hashMutex.Unlock()
				glog.Warningf("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
				return
			}
			dw.activeHashCalculations[userID] = true
			dw.hashMutex.Unlock()

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

			// Call the hash calculation function directly
			dw.calculateAndSetUserHashDirect(userID, userData)
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
	dw.hashMutex.Lock()
	if dw.activeHashCalculations[isCalculatingKey] {
		dw.hashMutex.Unlock()
		glog.Infof("DataWatcher: Hash calculation already in progress for user %s (isCalculating), skipping", userID)
		return
	}

	dw.activeHashCalculations[isCalculatingKey] = true
	// Also keep the original tracking for compatibility
	if dw.activeHashCalculations[userID] {
		// delete(dw.activeHashCalculations, isCalculatingKey)
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
	retryDelay := 20000 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		glog.Infof("DataWatcher: Hash calculation attempt %d/%d for user %s", attempt, maxRetries, userID)

		// Perform hash calculation directly
		success := dw.calculateAndSetUserHashDirect(userID, userData)

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
	cancel := make(chan bool, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("DataWatcher: Panic during write lock acquisition for user %s: %v", userID, r)
				writeLockError <- fmt.Errorf("panic during write lock acquisition: %v", r)
			}
		}()

		glog.Infof("DataWatcher: Attempting to acquire write lock for user %s", userID)
		glog.Infof("[LOCK] dw.cacheManager.mutex.TryLock() @439 Start")
		if !dw.cacheManager.mutex.TryLock() {
			glog.Warningf("DataWatcher: Write lock not available for user %s, skipping hash update", userID)
			writeLockError <- fmt.Errorf("write lock not available")
			return
		}
		defer func() {
			dw.cacheManager.mutex.Unlock()
			glog.Infof("[LOCK] dw.cacheManager.mutex.Unlock() @453 Start")
			glog.Infof("DataWatcher: Write lock released for user %s", userID)
		}()

		// Check if cancelled before sending signal
		select {
		case <-cancel:
			glog.Warningf("DataWatcher: Write lock acquisition cancelled for user %s", userID)
			return
		default:
		}

		glog.Infof("DataWatcher: Write lock acquired for user %s", userID)
		glog.Infof("[LOCK] dw.cacheManager.mutex.Lock() @439 Success")

		// Send signal and wait for processing
		select {
		case writeLockAcquired <- true:
			// Successfully sent signal, wait for cancellation or completion
			<-cancel
		case <-cancel:
			glog.Warningf("DataWatcher: Write lock acquisition cancelled before signal for user %s", userID)
		}
	}()

	select {
	case <-writeLockAcquired:
		// Write lock acquired successfully
		glog.Infof("DataWatcher: Write lock acquired for hash update, user %s", userID)

		// Update hash and release lock immediately
		originalUserData.Hash = newHash
		glog.Infof("DataWatcher: Hash updated in memory for user %s", userID)

		// Cancel the goroutine to release the lock
		close(cancel)

	case err := <-writeLockError:
		glog.Errorf("DataWatcher: Error acquiring write lock for user %s: %v", userID, err)
		close(cancel)
		return false

	case <-time.After(writeTimeout):
		glog.Errorf("DataWatcher: Timeout acquiring write lock for hash update, user %s", userID)
		close(cancel)
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
		glog.Infof("[LOCK] dw.cacheManager.mutex.TryRLock() @660 Start")
		if !dw.cacheManager.mutex.TryRLock() {
			glog.Warningf("processSourceData: Read lock not available for user %s, source %s, skipping", userID, sourceID)
			return
		}
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
		}
	}

	if len(completedApps) == 0 {
		glog.V(2).Infof("DataWatcher: No completed apps found for user=%s, source=%s", userID, sourceID)
		return int64(len(pendingApps)), 0
	}

	// Build single-line summary with all completed app IDs
	completedIDs := make([]string, 0, len(completedApps))
	for _, ca := range completedApps {
		completedIDs = append(completedIDs, dw.getAppID(ca))
	}
	glog.Infof("DataWatcher: user=%s source=%s completed=%d/%d apps=[%s]", userID, sourceID, len(completedApps), len(pendingApps), strings.Join(completedIDs, ","))

	// Step 3: Try to acquire write lock non-blocking and move completed apps
	lockStartTime := time.Now()

	// Try to acquire write lock non-blocking with cancellation support
	lockAcquired := make(chan bool, 1)
	lockCancel := make(chan bool, 1)

	go func() {
		glog.Infof("[LOCK] dw.cacheManager.mutex.TryLock() @716 Start")
		if !dw.cacheManager.mutex.TryLock() {
			glog.Warningf("DataWatcher: Write lock not available for user %s, source %s, skipping app move", userID, sourceID)
			return
		}
		defer func() {
			dw.cacheManager.mutex.Unlock()
			glog.Infof("[LOCK] dw.cacheManager.mutex.Unlock() @725 Start")
		}()

		// Check if cancelled before sending signal
		select {
		case <-lockCancel:
			glog.Warningf("DataWatcher: Write lock acquisition cancelled for user=%s, source=%s", userID, sourceID)
			return
		default:
		}

		glog.Infof("[LOCK] dw.cacheManager.mutex.Lock() @716 Success")

		// Send signal and wait for processing
		select {
		case lockAcquired <- true:
			// Successfully sent signal, wait for cancellation
			<-lockCancel
		case <-lockCancel:
			glog.Warningf("DataWatcher: Write lock acquisition cancelled before signal for user=%s, source=%s", userID, sourceID)
		}
	}()

	// Use a short timeout to avoid blocking too long
	select {
	case <-lockAcquired:
		glog.Infof("DataWatcher: Write lock acquired for user=%s, source=%s", userID, sourceID)

		defer func() {
			totalLockTime := time.Since(lockStartTime)
			glog.Infof("DataWatcher: Write lock released after %v for user=%s, source=%s", totalLockTime, userID, sourceID)
			// Cancel the goroutine to release the lock
			close(lockCancel)
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

					if latestData.AppInfo.AppEntry.Version != sourceData.AppInfoLatest[existingIndex].AppInfo.AppEntry.Version {
						// Send system notification for new app ready
						dw.sendNewAppReadyNotification(userID, completedApp, sourceID)
						glog.Infof("DataWatcher: Sent system notification for new app ready: %s", appName)
					}

					// Replace existing app with same name
					sourceData.AppInfoLatest[existingIndex] = latestData
					glog.Infof("DataWatcher: Replaced existing app with same name: %s (index: %d)", appName, existingIndex)

				} else {
					// Add new app if no existing app with same name
					sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, latestData)
					glog.Infof("DataWatcher: Added new app to latest: %s", appName)
					// Send system notification for new app ready
					dw.sendNewAppReadyNotification(userID, completedApp, sourceID)
				}

				movedCount++

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

	case <-time.After(2 * time.Second):
		close(lockCancel) // Cancel the goroutine to release the lock
		glog.Warningf("DataWatcher: Skipping write lock acquisition for user=%s, source=%s (timeout after 2s) - will retry in next cycle", userID, sourceID)
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
		// Ensure Categories and SupportArch are preserved from RawData or AppInfo if empty
		dw.ensureAppSimpleInfoFields(latestApp.AppSimpleInfo, pendingApp)
	} else {
		latestApp.AppSimpleInfo = dw.createAppSimpleInfo(pendingApp)
	}

	return latestApp
}

// GetMetrics returns DataWatcher metrics
func (dw *DataWatcher) GetMetrics() DataWatcherMetrics {
	isRunning := atomic.LoadInt32(&dw.isRunning) == 1

	return DataWatcherMetrics{
		IsRunning:          isRunning,
		TotalAppsProcessed: atomic.LoadInt64(&dw.totalAppsProcessed),
		TotalAppsMoved:     atomic.LoadInt64(&dw.totalAppsMoved),
		LastRunTime:        time.Unix(atomic.LoadInt64(&dw.lastRunTime), 0),
		Interval:           time.Duration(atomic.LoadInt64((*int64)(&dw.interval))),
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
	if interval < time.Second {
		interval = time.Second // Minimum 1 second
	}

	// Use atomic operation for thread safety since interval can be modified at runtime
	atomic.StoreInt64((*int64)(&dw.interval), int64(interval))
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
		SupportArch:    make([]string, 0),
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

		// Use SupportArch for App SupportArch
		if len(pendingApp.RawData.SupportArch) > 0 {
			appSimpleInfo.SupportArch = append([]string{}, pendingApp.RawData.SupportArch...)
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

		// Fill missing SupportArch
		if len(appSimpleInfo.SupportArch) == 0 && len(entry.SupportArch) > 0 {
			appSimpleInfo.SupportArch = append([]string{}, entry.SupportArch...)
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

// ensureAppSimpleInfoFields ensures Categories and SupportArch are preserved from RawData or AppInfo if empty
func (dw *DataWatcher) ensureAppSimpleInfoFields(appSimpleInfo *types.AppSimpleInfo, pendingApp *types.AppInfoLatestPendingData) {
	if appSimpleInfo == nil || pendingApp == nil {
		return
	}

	// Ensure SupportArch is preserved if empty
	if len(appSimpleInfo.SupportArch) == 0 {
		if pendingApp.RawData != nil && len(pendingApp.RawData.SupportArch) > 0 {
			appSimpleInfo.SupportArch = append([]string{}, pendingApp.RawData.SupportArch...)
			glog.Infof("DataWatcher: Restored SupportArch from RawData for app %s", appSimpleInfo.AppID)
		} else if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil && len(pendingApp.AppInfo.AppEntry.SupportArch) > 0 {
			appSimpleInfo.SupportArch = append([]string{}, pendingApp.AppInfo.AppEntry.SupportArch...)
			glog.Infof("DataWatcher: Restored SupportArch from AppInfo.AppEntry for app %s", appSimpleInfo.AppID)
		}
	}

	// Ensure Categories is preserved if empty
	if len(appSimpleInfo.Categories) == 0 {
		if pendingApp.RawData != nil && len(pendingApp.RawData.Categories) > 0 {
			appSimpleInfo.Categories = append([]string{}, pendingApp.RawData.Categories...)
			glog.Infof("DataWatcher: Restored Categories from RawData for app %s", appSimpleInfo.AppID)
		} else if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil && len(pendingApp.AppInfo.AppEntry.Categories) > 0 {
			appSimpleInfo.Categories = append([]string{}, pendingApp.AppInfo.AppEntry.Categories...)
			glog.Infof("DataWatcher: Restored Categories from AppInfo.AppEntry for app %s", appSimpleInfo.AppID)
		}
	}
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
