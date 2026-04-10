package appinfo

import (
	"context"
	"fmt"
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
		cacheManager: cacheManager,
		hydrator:     hydrator,
		dataSender:   dataSender,
		interval:     30 * time.Second, // Run every 30 seconds
		stopChan:     make(chan struct{}),
		isRunning:    0, // Initialize as false
		dirtyUsers:   make(map[string]bool),
	}
}

// StartWithOptions begins the data watching process with options
// If enableWatchLoop is false, the periodic watchLoop is not started (used when serial pipeline handles processing)
func (dw *DataWatcher) StartWithOptions(ctx context.Context) error {
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

	glog.Infof("Starting DataWatcher in passive mode (serial pipeline handles processing)")

	return nil
}

// Stop stops the data watching process
func (dw *DataWatcher) Stop() {
	if !atomic.CompareAndSwapInt32(&dw.isRunning, 1, 0) {
		return
	}

	glog.Infof("Stopping DataWatcher...")
	close(dw.stopChan)
}

// IsRunning returns whether the DataWatcher is currently running
func (dw *DataWatcher) IsRunning() bool {
	return atomic.LoadInt32(&dw.isRunning) == 1
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
