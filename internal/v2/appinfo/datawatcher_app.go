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

	// Dirty users tracking for deferred hash calculation
	dirtyUsers      map[string]bool
	dirtyUsersMutex sync.Mutex

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
		dirtyUsers:             make(map[string]bool),
	}
}

// Start begins the data watching process
func (dw *DataWatcher) Start(ctx context.Context) error {
	return dw.StartWithOptions(ctx, true)
}

// StartWithOptions begins the data watching process with options
// If enableWatchLoop is false, the periodic watchLoop is not started (used when serial pipeline handles processing)
func (dw *DataWatcher) StartWithOptions(ctx context.Context, enableWatchLoop bool) error {
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

	if enableWatchLoop {
		glog.Infof("Starting DataWatcher with interval: %v", time.Duration(atomic.LoadInt64((*int64)(&dw.interval))))
		go dw.watchLoop(ctx)
	} else {
		glog.Infof("Starting DataWatcher in passive mode (serial pipeline handles processing)")
	}

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
// ~ not used
func (dw *DataWatcher) watchLoop(ctx context.Context) {
	glog.Infof("DataWatcher monitoring loop started")
	defer glog.Infof("DataWatcher monitoring loop stopped")

	ticker := time.NewTicker(time.Duration(atomic.LoadInt64((*int64)(&dw.interval))))
	defer ticker.Stop()

	// Run once immediately
	dw.processCompletedApps() // not used

	for {
		select {
		case <-ctx.Done():
			glog.Infof("DataWatcher stopped due to context cancellation")
			return
		case <-dw.stopChan:
			glog.Infof("DataWatcher stopped due to explicit stop")
			return
		case <-ticker.C:
			dw.processCompletedApps() // not used
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

	allUsersData = dw.cacheManager.GetAllUsersData() // not used

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
			batchProcessed, batchMoved := dw.processUserBatch(ctx, userBatch, userDataBatch) // not used
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
		glog.V(2).Infof("DataWatcher: Processing cycle completed in %v - %d apps processed, %d moved to AppInfoLatest",
			processingDuration, totalProcessed, totalMoved)
	} else {
		glog.V(3).Infof("DataWatcher: Processing cycle completed in %v - %d apps processed, no moves needed",
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

		glog.V(3).Infof("DataWatcher: Processing user %d/%d in batch: %s", i+1, len(userIDs), userID)
		processed, moved := dw.processUserData(userID, userData) // not used
		totalProcessed += processed
		totalMoved += moved
		glog.V(2).Infof("DataWatcher: User %s completed: %d processed, %d moved", userID, processed, moved)
	}

	return totalProcessed, totalMoved
}

// processUserData processes a single user's data
// ~ not used
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
		processed, moved := dw.processSourceData(userID, sourceID, sourceData) // not used
		totalProcessed += processed
		totalMoved += moved
	}

	// Hash calculation is deferred to Pipeline Phase 5.
	// The caller (Pipeline.phaseHydrateApps) tracks affected users and
	// Phase 5 will calculate hashes for all affected users in one pass.

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
		glog.V(4).Infof("DataWatcher: Hash calculation already in progress for user %s (isCalculating), skipping", userID)
		return
	}

	dw.activeHashCalculations[isCalculatingKey] = true
	// Also keep the original tracking for compatibility
	if dw.activeHashCalculations[userID] {
		// delete(dw.activeHashCalculations, isCalculatingKey)
		dw.hashMutex.Unlock()
		glog.V(4).Infof("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
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
		glog.V(3).Infof("DataWatcher: Hash calculation tracking cleaned up for user %s", userID)
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

// calculateAndSetUserHashDirect calculates and updates hash for a single user.
// Does NOT call ForceSync — the caller (Pipeline Phase 5) is responsible for syncing.
func (dw *DataWatcher) calculateAndSetUserHashDirect(userID string, userData *types.UserData) bool {
	glog.V(3).Infof("DataWatcher: Starting direct hash calculation for user %s", userID)

	originalUserData := dw.cacheManager.GetUserData(userID)
	if originalUserData == nil {
		glog.Errorf("DataWatcher: Failed to get user data from cache manager for user %s", userID)
		return false
	}

	snapshot, err := utils.CreateUserDataSnapshot(userID, originalUserData)
	if err != nil {
		glog.Errorf("DataWatcher: Failed to create user data snapshot for user %s: %v", userID, err)
		return false
	}

	newHash, err := utils.CalculateUserDataHash(snapshot)
	if err != nil {
		glog.Errorf("DataWatcher: Failed to calculate hash for user %s: %v", userID, err)
		return false
	}

	currentHash := originalUserData.Hash
	if currentHash == newHash {
		glog.V(2).Infof("DataWatcher: Hash unchanged for user %s: %s", userID, newHash)
		return true
	}

	glog.V(2).Infof("DataWatcher: Hash changed for user %s: %s -> %s", userID, currentHash, newHash)
	dw.cacheManager.SetUserHash(userID, newHash)

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
// ~ not used
func (dw *DataWatcher) processSourceData(userID, sourceID string, sourceData *types.SourceData) (int64, int64) {
	if sourceData == nil {
		return 0, 0
	}

	// Step 1: Quick check and data copy with minimal lock time
	pendingApps, _ := dw.cacheManager.SnapshotSourcePending(userID, sourceID)

	// Early exit if no pending apps
	if len(pendingApps) == 0 {
		return 0, 0
	}

	glog.V(3).Infof("DataWatcher: Processing %d pending apps for user=%s, source=%s", len(pendingApps), userID, sourceID)

	// Step 2: Lock-free processing - Check hydration completion status
	var completedApps []*types.AppInfoLatestPendingData
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i, pendingApp := range pendingApps {
		if pendingApp == nil {
			continue
		}

		if isDevEnvironment() {
			glog.V(3).Infof("DataWatcher: Checking app %d/%d: %s", i+1, len(pendingApps), dw.getAppID(pendingApp))
		}

		if dw.isAppHydrationCompletedWithTimeout(ctx, pendingApp) {
			completedApps = append(completedApps, pendingApp)
		}
	}

	if len(completedApps) == 0 {
		glog.V(3).Infof("DataWatcher: No completed apps found for user=%s, source=%s", userID, sourceID)
		return int64(len(pendingApps)), 0
	}

	// Build single-line summary with all completed app IDs
	completedIDs := make([]string, 0, len(completedApps))
	for _, ca := range completedApps {
		completedIDs = append(completedIDs, dw.getAppID(ca))
	}
	glog.Infof("DataWatcher: user=%s source=%s completed=%d/%d apps=[%s]", userID, sourceID, len(completedApps), len(pendingApps), strings.Join(completedIDs, ","))

	// Step 3: Move completed apps from pending to latest via CacheManager
	movedCount := int64(0)
	for _, completedApp := range completedApps {
		latestData := dw.convertPendingToLatest(completedApp)
		if latestData == nil {
			continue
		}
		appID := dw.getAppID(completedApp)
		appName := dw.getAppName(completedApp)

		oldVersion, replaced, ok := dw.cacheManager.UpsertLatestAndRemovePending(userID, sourceID, latestData, appID, appName)
		if !ok {
			continue
		}

		if replaced {
			newVersion := ""
			if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
				newVersion = latestData.AppInfo.AppEntry.Version
			}
			if oldVersion != newVersion {
				dw.sendNewAppReadyNotification(userID, completedApp, sourceID) // ~ not used
			}
			glog.V(3).Infof("DataWatcher: Replaced existing app: %s", appName)
		} else {
			glog.V(2).Infof("DataWatcher: Added new app to latest: %s", appName)
			dw.sendNewAppReadyNotification(userID, completedApp, sourceID) // ~ not used
		}
		movedCount++
	}

	return int64(len(pendingApps)), movedCount
}

// isAppHydrationCompletedWithTimeout checks if app hydration is completed with timeout protection
func (dw *DataWatcher) isAppHydrationCompletedWithTimeout(ctx context.Context, pendingApp *types.AppInfoLatestPendingData) bool {
	if pendingApp == nil {
		glog.V(3).Info("DataWatcher: isAppHydrationCompletedWithTimeout called with nil pendingApp")
		return false
	}
	if dw.hydrator == nil {
		glog.V(3).Info("DataWatcher: Hydrator is nil, cannot check hydration completion")
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
		glog.V(3).Infof("DataWatcher: Timeout checking hydration completion for app=%s", appID)
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
		glog.V(3).Info("DataWatcher: convertPendingToLatest called with nil pendingApp")
		return nil
	}

	// Validate that the pending app has essential data
	hasRawData := pendingApp.RawData != nil
	hasAppInfo := pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil
	hasPackageInfo := pendingApp.RawPackage != "" || pendingApp.RenderedPackage != ""

	// Return nil if no essential data is present
	if !hasRawData && !hasAppInfo && !hasPackageInfo {
		appID := dw.getAppID(pendingApp)
		glog.V(3).Infof("DataWatcher: Skipping conversion of pending app %s - no essential data found", appID)
		return nil
	}

	// Additional validation for data integrity
	if hasRawData && (pendingApp.RawData.AppID == "" && pendingApp.RawData.ID == "" && pendingApp.RawData.Name == "") {
		glog.V(3).Info("DataWatcher: Skipping conversion - RawData exists but lacks identifying information")
		return nil
	}

	if hasAppInfo && (pendingApp.AppInfo.AppEntry.AppID == "" && pendingApp.AppInfo.AppEntry.ID == "" && pendingApp.AppInfo.AppEntry.Name == "") {
		glog.V(3).Info("DataWatcher: Skipping conversion - AppInfo exists but lacks identifying information")
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
	glog.V(3).Info("DataWatcher interval set to: %v", interval)
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
		glog.V(3).Info("DataWatcher: createAppSimpleInfo - no essential app information available")
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
			glog.V(3).Infof("DataWatcher: Restored SupportArch from RawData for app %s", appSimpleInfo.AppID)
		} else if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil && len(pendingApp.AppInfo.AppEntry.SupportArch) > 0 {
			appSimpleInfo.SupportArch = append([]string{}, pendingApp.AppInfo.AppEntry.SupportArch...)
			glog.Infof("DataWatcher: Restored SupportArch from AppInfo.AppEntry for app %s", appSimpleInfo.AppID)
		}
	}

	// Ensure Categories is preserved if empty
	if len(appSimpleInfo.Categories) == 0 {
		if pendingApp.RawData != nil && len(pendingApp.RawData.Categories) > 0 {
			appSimpleInfo.Categories = append([]string{}, pendingApp.RawData.Categories...)
			glog.V(3).Infof("DataWatcher: Restored Categories from RawData for app %s", appSimpleInfo.AppID)
		} else if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil && len(pendingApp.AppInfo.AppEntry.Categories) > 0 {
			appSimpleInfo.Categories = append([]string{}, pendingApp.AppInfo.AppEntry.Categories...)
			glog.Infof("DataWatcher: Restored Categories from AppInfo.AppEntry for app %s", appSimpleInfo.AppID)
		}
	}
}

// ForceCalculateUserHash forces hash calculation for a user regardless of app movement
// not used
func (dw *DataWatcher) ForceCalculateUserHash(userID string) error {
	return nil
	// glog.Infof("DataWatcher: Force calculating hash for user %s", userID)

	// // Get user data from cache manager
	// userData := dw.cacheManager.GetUserData(userID)
	// if userData == nil {
	// 	return fmt.Errorf("user data not found for user %s", userID)
	// }

	// // Call hash calculation directly
	// dw.calculateAndSetUserHashWithRetry(userID, userData)
	// return nil
}

// ForceCalculateAllUsersHash forces hash calculation for all users
func (dw *DataWatcher) ForceCalculateAllUsersHash() error {
	glog.V(3).Infof("DataWatcher: Force calculating hash for all users")

	// Get all users data
	allUsersData := dw.cacheManager.GetAllUsersData() // not used
	if len(allUsersData) == 0 {
		return fmt.Errorf("no users found in cache")
	}

	for userID, userData := range allUsersData {
		if userData != nil {
			glog.V(3).Infof("DataWatcher: Force calculating hash for user: %s", userID)
			dw.calculateAndSetUserHash(userID, userData)
		}
	}

	return nil
}

// MarkUserDirty marks a user as needing hash recalculation.
// Called by event-driven paths (e.g. DataWatcherState) that modify user data
// outside the Pipeline cycle. The dirty users will be picked up by Pipeline Phase 5.
func (dw *DataWatcher) MarkUserDirty(userID string) {
	dw.dirtyUsersMutex.Lock()
	defer dw.dirtyUsersMutex.Unlock()
	dw.dirtyUsers[userID] = true
	glog.V(3).Infof("DataWatcher: Marked user %s as dirty for deferred hash calculation", userID)
}

// CollectAndClearDirtyUsers returns all dirty user IDs and clears the set.
// Called by Pipeline Phase 5 to collect users that need hash recalculation
// from event-driven paths.
func (dw *DataWatcher) CollectAndClearDirtyUsers() map[string]bool {
	dw.dirtyUsersMutex.Lock()
	defer dw.dirtyUsersMutex.Unlock()
	if len(dw.dirtyUsers) == 0 {
		return nil
	}
	result := dw.dirtyUsers
	dw.dirtyUsers = make(map[string]bool)
	glog.V(3).Infof("DataWatcher: Collected %d dirty users for hash calculation", len(result))
	return result
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
		glog.V(3).Info("DataWatcher: sendNewAppReadyNotification called with nil completedApp")
		return
	}

	if dw.dataSender == nil {
		glog.V(3).Info("DataWatcher: dataSender is nil, unable to send notification")
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
		glog.V(2).Infof("DataWatcher: Successfully sent new app ready notification for app %s (version: %s, source: %s)", appName, appVersion, sourceID)
	}
}

// ProcessSingleAppToLatest moves a single completed pending app to AppInfoLatest
// Returns true if the app was successfully moved
func (dw *DataWatcher) ProcessSingleAppToLatest(userID, sourceID string, pendingApp *types.AppInfoLatestPendingData) bool {
	if pendingApp == nil {
		return false
	}

	// Check hydration completion
	if dw.hydrator != nil && !dw.hydrator.isAppHydrationComplete(pendingApp) {
		return false
	}

	// Convert to latest data
	latestData := dw.convertPendingToLatest(pendingApp)
	if latestData == nil {
		return false
	}

	appID := dw.getAppID(pendingApp)
	appName := dw.getAppName(pendingApp)
	glog.V(2).Infof("Pipeline Phase 2: ProcessSingleAppToLatest user=%s, source=%s, id=%s, name=%s", userID, sourceID, appID, appName)

	oldVersion, replaced, ok := dw.cacheManager.UpsertLatestAndRemovePending(userID, sourceID, latestData, appID, appName)
	if !ok {
		return false
	}

	if replaced {
		newVersion := ""
		if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
			newVersion = latestData.AppInfo.AppEntry.Version
		}
		if oldVersion != newVersion {
			dw.sendNewAppReadyNotification(userID, pendingApp, sourceID) // ~ ProcesSingleAppToLatest
		}
		glog.V(2).Infof("ProcessSingleAppToLatest: replaced existing app %s (user=%s, source=%s)", appName, userID, sourceID)
	} else {
		glog.V(2).Infof("ProcessSingleAppToLatest: added new app %s (user=%s, source=%s)", appName, userID, sourceID)
		dw.sendNewAppReadyNotification(userID, pendingApp, sourceID) // ~ ProcesSingleAppToLatest
	}

	atomic.AddInt64(&dw.totalAppsMoved, 1)
	glog.Infof("ProcessSingleAppToLatest: successfully moved app %s to Latest (user=%s, source=%s)", appName, userID, sourceID)
	return true
}

// CalculateAndSetUserHashDirect is a public wrapper for calculateAndSetUserHashDirect
func (dw *DataWatcher) CalculateAndSetUserHashDirect(userID string, userData *types.UserData) bool {
	return dw.calculateAndSetUserHashDirect(userID, userData)
}
