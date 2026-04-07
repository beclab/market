package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"market/internal/v2/client"
	"market/internal/v2/types"
	"sync"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/utils"

	"runtime"
	"runtime/debug"

	"github.com/golang/glog"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type CompareAppStateMsgFunc func(appState *AppStateLatestData)

// CacheManager manages the in-memory cache and Redis synchronization
type CacheManager struct {
	cache           *CacheData
	redisClient     *RedisClient
	userConfig      *UserConfig
	stateMonitor    *utils.StateMonitor // State monitor for change detection
	dataSender      *DataSender         // Direct data sender for bypassing state monitor
	mutex           sync.RWMutex
	syncChannel     chan SyncRequest
	stopChannel     chan bool
	isRunning       bool
	settingsManager *settings.SettingsManager
	cleanupTicker   *time.Ticker // Timer for periodic cleanup of AppRenderFailed

	// Lock monitoring
	lockStats struct {
		sync.Mutex
		lastLockTime   time.Time
		lastUnlockTime time.Time
		lockDuration   time.Duration
		lockCount      int64
		unlockCount    int64
	}
}

// startLockWatchdog starts a 1s watchdog for write lock sections and returns a stopper.
func (cm *CacheManager) startLockWatchdog(tag string) func() {
	fired := make(chan struct{}, 1)
	timer := time.AfterFunc(1*time.Second, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
		glog.Errorf("[WATCHDOG] Write lock held >1s at %s\nStack:\n%s", tag, string(debug.Stack()))
	})
	return func() {
		if timer.Stop() {
			select {
			case <-fired:
			default:
			}
		}
	}
}

// GetUserIDs returns a list of all user IDs in the cache
func (cm *CacheManager) GetUserIDs() []string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.cache == nil {
		return nil
	}

	ids := make([]string, 0, len(cm.cache.Users))
	for id := range cm.cache.Users {
		ids = append(ids, id)
	}
	return ids
}

// GetOrCreateUserIDs returns all user IDs; if none exist, creates a default user first.
func (cm *CacheManager) GetOrCreateUserIDs(defaultUserID string) []string {
	cm.mutex.RLock()
	ids := make([]string, 0, len(cm.cache.Users))
	for id := range cm.cache.Users {
		ids = append(ids, id)
	}
	cm.mutex.RUnlock()

	if len(ids) > 0 {
		return ids
	}

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Double-check after acquiring write lock
	if len(cm.cache.Users) > 0 {
		for id := range cm.cache.Users {
			ids = append(ids, id)
		}
		return ids
	}

	cm.cache.Users[defaultUserID] = NewUserDataEx(defaultUserID)
	glog.V(3).Infof("No existing users found, created user %s as fallback", defaultUserID)
	return []string{defaultUserID}
}

// IsLocalSource returns true if the given source is of local type.
func (cm *CacheManager) IsLocalSource(userID, sourceID string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData, exists := cm.cache.Users[userID]
	if !exists {
		return false
	}
	sourceData, exists := userData.Sources[sourceID]
	if !exists {
		return false
	}
	return sourceData.Type == types.SourceDataTypeLocal
}

// SetUserHash atomically sets the hash for a user.
func (cm *CacheManager) SetUserHash(userID, hash string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if userData, exists := cm.cache.Users[userID]; exists {
		userData.Hash = hash
	}
}

// RemoveFromPendingList removes an app from the pending list for the given user/source.
func (cm *CacheManager) RemoveFromPendingList(userID, sourceID, appID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	userData, ok := cm.cache.Users[userID]
	if !ok {
		return
	}
	sourceData, ok := userData.Sources[sourceID]
	if !ok {
		return
	}

	newSlice := make([]*types.AppInfoLatestPendingData, 0, len(sourceData.AppInfoLatestPending))
	for _, p := range sourceData.AppInfoLatestPending {
		if p != nil && p.RawData != nil &&
			(p.RawData.ID == appID || p.RawData.AppID == appID || p.RawData.Name == appID) {
			continue
		}
		newSlice = append(newSlice, p)
	}
	sourceData.AppInfoLatestPending = newSlice
}

// UpsertLatestAndRemovePending inserts or replaces an app in AppInfoLatest and removes
// it from AppInfoLatestPending. Returns the old version (if replaced), whether it was
// a replacement, and whether the user/source existed.
func (cm *CacheManager) UpsertLatestAndRemovePending(
	userID, sourceID string,
	latestData *types.AppInfoLatestData,
	appID, appName string,
) (oldVersion string, replaced bool, ok bool) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	userData, exists := cm.cache.Users[userID]
	if !exists {
		return "", false, false
	}
	sourceData, exists := userData.Sources[sourceID]
	if !exists {
		return "", false, false
	}

	// Find existing app by name
	existingIndex := -1
	for i, app := range sourceData.AppInfoLatest {
		if app == nil {
			continue
		}
		name := ""
		if app.RawData != nil {
			name = app.RawData.Name
		} else if app.AppInfo != nil && app.AppInfo.AppEntry != nil {
			name = app.AppInfo.AppEntry.Name
		} else if app.AppSimpleInfo != nil {
			name = app.AppSimpleInfo.AppName
		}
		if name == appName {
			existingIndex = i
			break
		}
	}

	if existingIndex >= 0 {
		old := sourceData.AppInfoLatest[existingIndex]
		if old.AppInfo != nil && old.AppInfo.AppEntry != nil {
			oldVersion = old.AppInfo.AppEntry.Version
		}
		sourceData.AppInfoLatest[existingIndex] = latestData
		replaced = true
	} else {
		sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, latestData)
	}

	// Remove from pending
	newPending := make([]*types.AppInfoLatestPendingData, 0, len(sourceData.AppInfoLatestPending))
	for _, p := range sourceData.AppInfoLatestPending {
		pID := ""
		if p != nil && p.RawData != nil {
			pID = p.RawData.AppID
			if pID == "" {
				pID = p.RawData.ID
			}
			if pID == "" {
				pID = p.RawData.Name
			}
		}
		if pID != appID {
			newPending = append(newPending, p)
		}
	}
	sourceData.AppInfoLatestPending = newPending

	// Remove the same app from render-failed list after successful move to latest.
	// Keep Pending/Failed disjoint and avoid stale failed entries.
	newFailed := make([]*types.AppRenderFailedData, 0, len(sourceData.AppRenderFailed))
	for _, f := range sourceData.AppRenderFailed {
		if f == nil || f.RawData == nil {
			newFailed = append(newFailed, f)
			continue
		}
		matchedByID := appID != "" && (f.RawData.ID == appID || f.RawData.AppID == appID)
		matchedByName := appName != "" && f.RawData.Name == appName
		if matchedByID || matchedByName {
			continue
		}
		newFailed = append(newFailed, f)
	}
	sourceData.AppRenderFailed = newFailed

	return oldVersion, replaced, true
}

// UpdateSourceOthers updates the Others data for a given sourceID across all users.
// If a user or source doesn't exist, it is created.
func (cm *CacheManager) UpdateSourceOthers(sourceID string, others *types.Others) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if len(cm.cache.Users) == 0 {
		systemUserID := "system"
		cm.cache.Users[systemUserID] = NewUserDataEx(systemUserID)
		glog.V(3).Infof("No existing users found, created system user as fallback")
	}

	for userID, userData := range cm.cache.Users {
		if userData.Sources == nil {
			userData.Sources = make(map[string]*SourceData)
		}
		if userData.Sources[sourceID] == nil {
			userData.Sources[sourceID] = NewSourceData()
		}
		userData.Sources[sourceID].Others = others
		glog.V(3).Infof("Updated Others data in cache for user %s, source %s", userID, sourceID)
	}
}

// RemoveAppFromAllSources removes an app (by name) from AppInfoLatest and
// AppInfoLatestPending across all users for the given sourceID. Returns the
// total number of users affected.
func (cm *CacheManager) RemoveAppFromAllSources(appName, sourceID string) int {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	affected := 0
	for _, userData := range cm.cache.Users {
		sourceData, exists := userData.Sources[sourceID]
		if !exists {
			continue
		}

		origLatest := len(sourceData.AppInfoLatest)
		origPending := len(sourceData.AppInfoLatestPending)

		newLatest := make([]*types.AppInfoLatestData, 0, origLatest)
		for _, app := range sourceData.AppInfoLatest {
			if app == nil || app.RawData == nil || app.RawData.Name != appName {
				newLatest = append(newLatest, app)
			}
		}

		newPending := make([]*types.AppInfoLatestPendingData, 0, origPending)
		for _, app := range sourceData.AppInfoLatestPending {
			if app == nil || app.RawData == nil || app.RawData.Name != appName {
				newPending = append(newPending, app)
			}
		}

		if len(newLatest) != origLatest || len(newPending) != origPending {
			sourceData.AppInfoLatest = newLatest
			sourceData.AppInfoLatestPending = newPending
			affected++
		}
	}
	return affected
}

// RemoveDelistedApps removes apps whose ID is in the provided set from
// AppInfoLatest across all users and sources. Returns the total removal count.
func (cm *CacheManager) RemoveDelistedApps(delistedAppIDs map[string]bool) int {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	removedCount := 0
	for userID, userData := range cm.cache.Users {
		for sourceID, sourceData := range userData.Sources {
			newLatest := sourceData.AppInfoLatest[:0]
			for _, app := range sourceData.AppInfoLatest {
				var appID string
				if app != nil && app.RawData != nil {
					if app.RawData.ID != "" {
						appID = app.RawData.ID
					} else if app.RawData.AppID != "" {
						appID = app.RawData.AppID
					} else if app.RawData.Name != "" {
						appID = app.RawData.Name
					}
				}
				if delistedAppIDs[appID] {
					removedCount++
					glog.V(3).Infof("Removing delisted app %s from user %s source %s", appID, userID, sourceID)
				} else {
					newLatest = append(newLatest, app)
				}
			}
			sourceData.AppInfoLatest = newLatest
		}
	}
	return removedCount
}

// CopyPendingVersionHistory finds the pending data for the given app and copies
// its VersionHistory and AppLabels into the target ApplicationInfoEntry under write lock.
// It also overwrites the pending entry with the supplied latestData fields.
func (cm *CacheManager) CopyPendingVersionHistory(
	userID, sourceID, appID, appName string,
	latestData *types.AppInfoLatestData,
) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	userData, ok := cm.cache.Users[userID]
	if !ok {
		return fmt.Errorf("user %s not found", userID)
	}
	sourceData, ok := userData.Sources[sourceID]
	if !ok {
		return fmt.Errorf("source %s not found for user %s", sourceID, userID)
	}

	// Find the pending data
	var pendingData *types.AppInfoLatestPendingData
	for _, p := range sourceData.AppInfoLatestPending {
		if p == nil || p.RawData == nil {
			continue
		}
		if p.RawData.Name == appName || p.RawData.AppID == appID || p.RawData.ID == appID {
			pendingData = p
			break
		}
	}
	if pendingData == nil {
		return fmt.Errorf("pendingData not found for user=%s, source=%s, app=%s, appName=%s", userID, sourceID, appID, appName)
	}

	// Copy version history from pending to latest
	if latestData.RawData != nil && pendingData.RawData != nil {
		latestData.RawData.VersionHistory = pendingData.RawData.VersionHistory
		if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
			latestData.AppInfo.AppEntry.VersionHistory = pendingData.RawData.VersionHistory
		}
		// Preserve appLabels from pendingData if latest doesn't have them
		if len(pendingData.RawData.AppLabels) > 0 && len(latestData.RawData.AppLabels) == 0 {
			latestData.RawData.AppLabels = pendingData.RawData.AppLabels
			if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
				latestData.AppInfo.AppEntry.AppLabels = pendingData.RawData.AppLabels
			}
		}
	}

	// Overwrite pending entry with latest data fields
	pendingData.Type = latestData.Type
	pendingData.Timestamp = latestData.Timestamp
	pendingData.Version = latestData.Version
	pendingData.RawData = latestData.RawData
	pendingData.RawPackage = latestData.RawPackage
	pendingData.Values = latestData.Values
	pendingData.AppInfo = latestData.AppInfo
	pendingData.RenderedPackage = latestData.RenderedPackage
	pendingData.AppSimpleInfo = latestData.AppSimpleInfo

	return nil
}

// FindPendingDataForApp finds a pending data entry by appID in the given user/source.
func (cm *CacheManager) FindPendingDataForApp(userID, sourceID, appID string) *types.AppInfoLatestPendingData {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData, ok := cm.cache.Users[userID]
	if !ok {
		return nil
	}
	sourceData, ok := userData.Sources[sourceID]
	if !ok {
		return nil
	}
	for _, p := range sourceData.AppInfoLatestPending {
		if p != nil && p.RawData != nil &&
			(p.RawData.ID == appID || p.RawData.AppID == appID || p.RawData.Name == appID) {
			return p
		}
	}
	return nil
}

// IsAppInLatestQueue checks if an app (by ID) with a matching version exists in AppInfoLatest.
func (cm *CacheManager) IsAppInLatestQueue(userID, sourceID, appID, version string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData, ok := cm.cache.Users[userID]
	if !ok {
		return false
	}
	sourceData, ok := userData.Sources[sourceID]
	if !ok {
		return false
	}

	for _, ld := range sourceData.AppInfoLatest {
		if ld == nil {
			continue
		}
		if ld.RawData != nil {
			if ld.RawData.ID == appID || ld.RawData.AppID == appID || ld.RawData.Name == appID {
				if version != "" && ld.RawData.Version != version {
					continue
				}
				return true
			}
		}
		if ld.AppInfo != nil && ld.AppInfo.AppEntry != nil {
			if ld.AppInfo.AppEntry.ID == appID || ld.AppInfo.AppEntry.AppID == appID || ld.AppInfo.AppEntry.Name == appID {
				if version != "" && ld.AppInfo.AppEntry.Version != version {
					continue
				}
				return true
			}
		}
	}
	return false
}

// IsAppInRenderFailedList checks if an app exists in the render failed list.
// When version is provided, only same-version failures will be treated as a match.
func (cm *CacheManager) IsAppInRenderFailedList(userID, sourceID, appID, appName, version string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData, ok := cm.cache.Users[userID]
	if !ok {
		return false
	}
	sourceData, ok := userData.Sources[sourceID]
	if !ok {
		return false
	}
	for _, fd := range sourceData.AppRenderFailed {
		if fd == nil || fd.RawData == nil {
			continue
		}

		matchedByID := appID != "" && (fd.RawData.ID == appID || fd.RawData.AppID == appID || fd.RawData.Name == appID)
		matchedByName := appName != "" && fd.RawData.Name == appName
		if !matchedByID && !matchedByName {
			continue
		}

		// If incoming version is known, only block when failed record has the same known version.
		if version != "" {
			failedVersion := fd.Version
			if failedVersion == "" {
				failedVersion = fd.RawData.Version
			}
			if failedVersion == "" || failedVersion != version {
				continue
			}
		}
		return true
	}
	return false
}

// HasSourceData returns true if any user has non-empty AppInfoLatest or
// AppInfoLatestPending data for the given sourceID.
func (cm *CacheManager) HasSourceData(sourceID string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for _, userData := range cm.cache.Users {
		if sourceData, exists := userData.Sources[sourceID]; exists {
			if len(sourceData.AppInfoLatestPending) > 0 || len(sourceData.AppInfoLatest) > 0 {
				return true
			}
		}
	}
	return false
}

// IsAppInstalled returns true if any user has the named app in a non-uninstalled
// state in AppStateLatest for the given sourceID.
func (cm *CacheManager) IsAppInstalled(sourceID, appName string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for _, userData := range cm.cache.Users {
		if sourceData, ok := userData.Sources[sourceID]; ok {
			for _, appState := range sourceData.AppStateLatest {
				if appState != nil && appState.Status.Name == appName && appState.Status.State != "uninstalled" {
					return true
				}
			}
		}
	}
	return false
}

// GetSourceOthersHash returns the Others.Hash stored for the given sourceID
// in the first user that has a valid hash.
func (cm *CacheManager) GetSourceOthersHash(sourceID string) string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for _, userData := range cm.cache.Users {
		if sourceData, exists := userData.Sources[sourceID]; exists {
			if sourceData.Others != nil && sourceData.Others.Hash != "" {
				return sourceData.Others.Hash
			}
		}
	}
	return ""
}

// ListActiveUsers returns information about all active (existing) users.
func (cm *CacheManager) ListActiveUsers() []map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var usersInfo []map[string]string
	for _, v := range cm.cache.Users {
		if v.UserInfo != nil && v.UserInfo.Exists {
			ui := map[string]string{
				"id":     v.UserInfo.Id,
				"name":   v.UserInfo.Name,
				"role":   v.UserInfo.Role,
				"status": v.UserInfo.Status,
			}
			usersInfo = append(usersInfo, ui)
		}
	}
	return usersInfo
}

// CollectAllPendingItems returns all non-nil pending items across all users and sources.
type PendingItem struct {
	UserID   string
	SourceID string
	Pending  *types.AppInfoLatestPendingData
}

func (cm *CacheManager) CollectAllPendingItems() []PendingItem {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var items []PendingItem
	for userID, userData := range cm.cache.Users {
		for sourceID, sourceData := range userData.Sources {
			for _, pd := range sourceData.AppInfoLatestPending {
				if pd != nil {
					items = append(items, PendingItem{userID, sourceID, pd})
				}
			}
		}
	}
	return items
}

// RestoreRetryableFailedToPending moves up to `limit` items from AppRenderFailed
// back to AppInfoLatestPending (FIFO order) so they can be retried by the hydrator.
// Items are removed from AppRenderFailed to avoid duplicates.
// Returns the number of items restored.
func (cm *CacheManager) RestoreRetryableFailedToPending(limit int) int {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	restored := 0
	for _, userData := range cm.cache.Users {
		if restored >= limit {
			break
		}
		for _, sourceData := range userData.Sources {
			if restored >= limit {
				break
			}
			i := 0
			for i < len(sourceData.AppRenderFailed) && restored < limit {
				fd := sourceData.AppRenderFailed[i]
				if fd == nil || fd.RawData == nil {
					i++
					continue
				}
				sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, &types.AppInfoLatestPendingData{
					Type:            types.AppInfoLatestPending,
					Timestamp:       fd.Timestamp,
					Version:         fd.Version,
					RawData:         fd.RawData,
					RawPackage:      fd.RawPackage,
					Values:          fd.Values,
					AppInfo:         fd.AppInfo,
					RenderedPackage: fd.RenderedPackage,
				})
				sourceData.AppRenderFailed = append(sourceData.AppRenderFailed[:i], sourceData.AppRenderFailed[i+1:]...)
				restored++
			}
		}
	}
	if restored > 0 {
		glog.V(2).Infof("RestoreRetryableFailedToPending: restored %d failed apps to pending queue", restored)
	}
	return restored
}

// SnapshotSourcePending returns shallow copies of the pending and latest slices
// for the given user/source, safe for iteration outside the lock.
func (cm *CacheManager) SnapshotSourcePending(userID, sourceID string) (
	pending []*types.AppInfoLatestPendingData,
	latest []*types.AppInfoLatestData,
) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData, exists := cm.cache.Users[userID]
	if !exists {
		return nil, nil
	}
	sourceData, exists := userData.Sources[sourceID]
	if !exists {
		return nil, nil
	}

	pending = make([]*types.AppInfoLatestPendingData, len(sourceData.AppInfoLatestPending))
	copy(pending, sourceData.AppInfoLatestPending)
	latest = make([]*types.AppInfoLatestData, len(sourceData.AppInfoLatest))
	copy(latest, sourceData.AppInfoLatest)
	return pending, latest
}

// SyncRequest represents a request to sync data to Redis
type SyncRequest struct {
	UserID   string
	SourceID string
	Type     SyncType
}

// SyncType represents the type of sync operation
type SyncType int

const (
	SyncUser   SyncType = iota // Sync entire user data
	SyncSource                 // Sync specific source data
	DeleteUser                 // Delete user data
)

// NewCacheManager creates a new cache manager
func NewCacheManager(redisClient *RedisClient, userConfig *UserConfig) *CacheManager {
	// Initialize data sender
	dataSender, err := NewDataSender()
	if err != nil {
		glog.Errorf("Failed to initialize DataSender: %v", err)
		// Continue without data sender
	}

	// Initialize state monitor
	var stateMonitor *utils.StateMonitor
	if dataSender != nil {
		stateMonitor = utils.NewStateMonitor(dataSender)
	}

	return &CacheManager{
		cache:        NewCacheData(),
		redisClient:  redisClient,
		userConfig:   userConfig,
		stateMonitor: stateMonitor,
		dataSender:   dataSender,
		syncChannel:  make(chan SyncRequest, 1000), // Buffer for async sync requests
		stopChannel:  make(chan bool, 1),
		isRunning:    false,
	}
}

// Start initializes the cache by loading data from Redis and starts the sync worker
func (cm *CacheManager) Start() error {
	glog.V(2).Infof("Starting cache manager")

	// Load cache data from Redis if ClearCache is false
	if !cm.userConfig.ClearCache {
		if !utils.IsPublicEnvironment() {
			cache, err := cm.redisClient.LoadCacheFromRedis()
			if err != nil {
				glog.Errorf("Failed to load cache from Redis: %v", err)
				return err
			}
			glog.V(4).Infof("[LOCK] cm.mutex.Lock() @81 Start")
			lockStart := time.Now()
			cm.mutex.Lock()
			glog.V(4).Infof("[LOCK] cm.mutex.Lock() @81 Success (wait=%v)", time.Since(lockStart))
			_wd := cm.startLockWatchdog("@81:loadCache")
			cm.cache = cache
			cm.mutex.Unlock()
			_wd()
		}

	} else {
		glog.V(3).Infof("ClearCache is enabled, clearing Redis data and starting with empty cache")

		if !utils.IsPublicEnvironment() {
			// Clear all Redis data
			if err := cm.redisClient.ClearAllData(); err != nil {
				glog.Errorf("Failed to clear Redis data: %v", err)
				return err
			}
		}

		glog.V(4).Infof("[LOCK] cm.mutex.Lock() @81 Start")
		lockStart := time.Now()
		cm.mutex.Lock()
		glog.V(4).Infof("[LOCK] cm.mutex.Lock() @81 Success (wait=%v)", time.Since(lockStart))
		_wd := cm.startLockWatchdog("@81:newCache")
		cm.cache = NewCacheData()
		cm.mutex.Unlock()
		_wd()
	}

	// Ensure all users from userConfig.UserList have their data structures initialized
	if cm.userConfig != nil && len(cm.userConfig.UserList) > 0 {
		glog.V(3).Infof("Initializing data structures for configured users")

		glog.V(4).Infof("[LOCK] cm.mutex.Lock() @102 Start")
		lockStart := time.Now()
		cm.mutex.Lock()
		glog.V(4).Infof("[LOCK] cm.mutex.Lock() @102 Success (wait=%v)", time.Since(lockStart))
		_wd := cm.startLockWatchdog("@102:initUsers")
		newUsers := make([]string, 0)
		for _, userID := range cm.userConfig.UserList {
			if _, exists := cm.cache.Users[userID]; !exists {
				glog.V(3).Infof("Creating data structure for new user: %s", userID)
				cm.cache.Users[userID] = NewUserDataEx(userID) // NewUserData()
				newUsers = append(newUsers, userID)
			}
		}
		cm.mutex.Unlock()
		_wd()

		glog.V(2).Infof("User data structure initialization completed for %d users", len(cm.userConfig.UserList))

	}

	glog.V(4).Infof("[LOCK] cm.mutex.Lock() @114 Start")
	lockStart := time.Now()
	cm.mutex.Lock()
	glog.V(4).Infof("[LOCK] cm.mutex.Lock() @114 Success (wait=%v)", time.Since(lockStart))
	_wd := cm.startLockWatchdog("@114:setRunning")
	cm.isRunning = true
	cm.mutex.Unlock()
	_wd()

	// Start sync worker goroutine
	go cm.syncWorker()

	glog.V(3).Infof("Cache manager started successfully")
	return nil
}

// Stop stops the cache manager and sync worker
func (cm *CacheManager) Stop() {
	glog.V(4).Infof("Stopping cache manager")

	glog.V(4).Infof("[LOCK] cm.mutex.Lock() @129 Start")
	lockStart := time.Now()
	cm.mutex.Lock()
	glog.V(4).Infof("[LOCK] cm.mutex.Lock() @129 Success (wait=%v)", time.Since(lockStart))
	if cm.isRunning {
		cm.isRunning = false
		cm.stopChannel <- true
	}
	cm.mutex.Unlock()

	// Close state monitor
	if cm.stateMonitor != nil {
		cm.stateMonitor.Close()
		glog.V(4).Infof("State monitor closed")
	}

	glog.V(4).Infof("Cache manager stopped")
}

// syncWorker processes sync requests in the background
func (cm *CacheManager) syncWorker() {
	glog.V(4).Infof("Sync worker started")

	for {
		select {
		case syncReq := <-cm.syncChannel:
			cm.processSyncRequest(syncReq)
		case <-cm.stopChannel:
			glog.V(4).Infof("Sync worker stopped")
			return
		}
	}
}

// processSyncRequest handles individual sync requests
func (cm *CacheManager) processSyncRequest(req SyncRequest) {

	if utils.IsPublicEnvironment() {
		return
	}

	glog.Infof("[CACHE] SyncRequest, user: %s, source: %s, type: %d", req.UserID, req.SourceID, req.Type)
	switch req.Type {
	case SyncUser:
		if userData := cm.getUserData(req.UserID); userData != nil {
			if err := cm.redisClient.SaveUserDataToRedis(req.UserID, userData); err != nil {
				glog.Errorf("Failed to sync user data to Redis: %v", err)
			}
		}
	case SyncSource:
		if sourceData := cm.getSourceData(req.UserID, req.SourceID); sourceData != nil {
			if err := cm.redisClient.SaveSourceDataToRedis(req.UserID, req.SourceID, sourceData); err != nil {
				glog.Errorf("Failed to sync source data to Redis: %v", err)
			}
		}
	case DeleteUser:
		if err := cm.redisClient.DeleteUserDataFromRedis(req.UserID); err != nil {
			glog.Errorf("Failed to delete user data from Redis: %v", err)
		}
	}
}

// GetUserData retrieves user data from cache
func (cm *CacheManager) GetUserData(userID string) *UserData {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	return cm.cache.Users[userID]
}

// getUserData internal method to get user data without external locking
func (cm *CacheManager) getUserData(userID string) *UserData {
	return cm.cache.Users[userID]
}

// GetSourceData retrieves source data from cache
func (cm *CacheManager) GetSourceData(userID, sourceID string) *SourceData {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if userData, exists := cm.cache.Users[userID]; exists {
		return userData.Sources[sourceID]
	}
	return nil
}

// GetAppVersionFromState retrieves app version from AppStateLatest in the specified source
// Returns version and found flag
func (cm *CacheManager) GetAppVersionFromState(userID, sourceID, appName string) (version string, found bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData := cm.cache.Users[userID]
	if userData == nil {
		return "", false
	}

	sourceData := userData.Sources[sourceID]
	if sourceData == nil {
		return "", false
	}

	// Search for the app in AppStateLatest in the specified source
	for _, appState := range sourceData.AppStateLatest {
		if appState != nil && appState.Status.Name == appName {
			if appState.Version != "" {
				return appState.Version, true
			}
		}
	}
	return "", false
}

// getSourceData internal method to get source data without external locking
func (cm *CacheManager) getSourceData(userID, sourceID string) *SourceData {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	if userData, exists := cm.cache.Users[userID]; exists {
		return userData.Sources[sourceID]
	}
	return nil
}

// updateAppStateLatest updates or adds a single app state based on name matching
func (cm *CacheManager) updateAppStateLatest(userID, sourceID string, sourceData *SourceData, newAppState *types.AppStateLatestData) {
	if newAppState == nil {
		glog.Errorf("Invalid app state data: app state is nil")
		return
	}

	if newAppState.Status.Name == "" {
		glog.Errorf("Invalid app state data: missing name field - app state will be rejected")
		return
	}

	// Check if any running entrance has empty URL (only running entrances require URLs)
	hasRunningWithEmptyUrl := false
	for _, entrance := range newAppState.Status.EntranceStatuses {
		if entrance.State == "running" && entrance.Url == "" {
			hasRunningWithEmptyUrl = true
			break
		}
	}

	if hasRunningWithEmptyUrl {
		glog.Warningf("App state data has running entrance with empty URL for app %s - app state will be rejected", newAppState.Status.Name)
		return
	}

	// Try to find existing app state with the same name
	found := false
	for i, existingAppState := range sourceData.AppStateLatest {
		if existingAppState != nil && existingAppState.Status.Name == newAppState.Status.Name {
			// Preserve rawAppName from existing state if new state doesn't have it
			if newAppState.Status.RawAppName == "" && existingAppState.Status.RawAppName != "" {
				glog.V(3).Infof("New app state for %s has empty RawAppName, preserving old RawAppName: %s", newAppState.Status.Name, existingAppState.Status.RawAppName)
				newAppState.Status.RawAppName = existingAppState.Status.RawAppName
			}

			// Preserve title from existing state if new state doesn't have it
			if newAppState.Status.Title == "" && existingAppState.Status.Title != "" {
				glog.V(3).Infof("New app state for %s has empty Title, preserving old Title: %s", newAppState.Status.Name, existingAppState.Status.Title)
				newAppState.Status.Title = existingAppState.Status.Title
			}

			// Preserve SharedEntrances if new state lacks them but cache has some
			if len(newAppState.Status.SharedEntrances) == 0 && len(existingAppState.Status.SharedEntrances) > 0 {
				glog.V(3).Infof("New app state for %s has empty SharedEntrances, preserving old shared entrances", newAppState.Status.Name)
				newAppState.Status.SharedEntrances = existingAppState.Status.SharedEntrances
			}

			// If new app state has empty EntranceStatuses, preserve the old ones
			if len(newAppState.Status.EntranceStatuses) == 0 && len(existingAppState.Status.EntranceStatuses) > 0 {
				glog.V(3).Infof("New app state for %s has empty EntranceStatuses, preserving old entrance statuses", newAppState.Status.Name)
				newAppState.Status.EntranceStatuses = existingAppState.Status.EntranceStatuses

				// Check if main state has changed - if yes, let pendingNotifications handle it to avoid duplicate push
				// Only force push if main state hasn't changed AND other fields haven't changed either
				// This ensures we only push when EntranceStatuses preservation is the only change
				mainStateChanged := newAppState.Status.State != existingAppState.Status.State
				progressChanged := newAppState.Status.Progress != existingAppState.Status.Progress
				otherFieldsChanged := mainStateChanged || progressChanged

				if !otherFieldsChanged {
					// Only EntranceStatuses was "changed" (from empty to preserved), but after preservation,
					// the state is actually the same as before. However, we still need to notify to ensure
					// client gets the updated state with preserved EntranceStatuses (in case statusTime or other metadata changed)
					if cm.dataSender != nil {
						// Find corresponding AppInfoLatestData
						var appInfoLatest *types.AppInfoLatestData
						for _, appInfo := range sourceData.AppInfoLatest {
							if appInfo != nil && appInfo.RawData != nil && appInfo.RawData.Name == newAppState.Status.Name {
								appInfoLatest = appInfo
								break
							}
						}

						// Create and send update directly
						update := types.AppInfoUpdate{
							AppStateLatest: newAppState,
							AppInfoLatest:  appInfoLatest,
							Timestamp:      time.Now().Unix(),
							User:           userID,
							AppName:        newAppState.Status.Name,
							NotifyType:     "app_state_change",
							Source:         sourceID,
						}

						if err := cm.dataSender.SendAppInfoUpdate(update, "cache"); err != nil {
							glog.Errorf("Force push state update for app %s failed: %v", newAppState.Status.Name, err)
						} else {
							glog.V(3).Infof("Force pushed state update for app %s due to EntranceStatuses fallback (only metadata changed)", newAppState.Status.Name)
						}
					}
				} else {
					// Main state or progress has changed, pendingNotifications will handle the notification
					// No need to force push here to avoid duplicate
					glog.V(3).Infof("Skipping force push for app %s - state/progress changed, will be handled by pendingNotifications", newAppState.Status.Name)
				}
			}

			// Update existing app state
			sourceData.AppStateLatest[i] = newAppState
			glog.V(3).Infof("Updated existing app state for app: %s", newAppState.Status.Name)
			found = true
			break
		}
	}

	// If not found, add new app state
	if !found {
		sourceData.AppStateLatest = append(sourceData.AppStateLatest, newAppState)
		glog.V(2).Infof("Added new app state for app: %s", newAppState.Status.Name)
	}
}

func (cm *CacheManager) setAppDataInternal(userID, sourceID string, dataType AppDataType, data map[string]interface{}) error {
	cm.mutex.Lock()
	cm.updateLockStats("lock")
	// Watchdog: warn if write lock is held >1s
	watchdogFired := make(chan struct{}, 1)
	timer := time.AfterFunc(1*time.Second, func() {
		select {
		case watchdogFired <- struct{}{}:
		default:
		}
		glog.Errorf("[WATCHDOG] Write lock held >1s in setAppDataInternal (user=%s, source=%s, type=%v)\nStack:\n%s", userID, sourceID, dataType, string(debug.Stack()))
	})

	// Collect state change notifications inside lock, send them after unlock in defer
	type pendingNotify struct {
		userID       string
		sourceID     string
		appName      string
		state        *types.AppStateLatestData
		existing     []*types.AppStateLatestData
		latest       []*types.AppInfoLatestData
		hasChanged   bool
		changeReason string
	}
	pendingNotifications := make([]pendingNotify, 0, 8)

	defer func() {
		cm.mutex.Unlock()
		cm.updateLockStats("unlock")
		glog.V(4).Infof("[LOCK] cm.mutex.Unlock() @269 End")
		if timer.Stop() {
			// timer stopped before firing; try to drain channel if any
			select {
			case <-watchdogFired:
			default:
			}
		}

		// Send notifications after the global lock is released to avoid deadlocks
		glog.V(4).Infof("DEBUG: Processing %d pending notifications", len(pendingNotifications))
		if cm.stateMonitor != nil {
			glog.V(4).Infof("DEBUG: State monitor is available for processing notifications")
			for i, p := range pendingNotifications {
				glog.V(4).Infof("DEBUG: Processing notification %d: appName=%s, state=%v", i, p.appName, p.state != nil)
				if p.appName == "" || p.state == nil {
					glog.Warningf("DEBUG: Skipping notification %d: appName=%s, state=%v", i, p.appName, p.state != nil)
					continue
				}
				glog.V(4).Infof("DEBUG: Calling NotifyStateChange for app=%s", p.appName)
				if err := cm.stateMonitor.NotifyStateChange(
					p.userID, p.sourceID, p.appName,
					p.state,
					p.existing,
					p.latest,
					p.hasChanged,
					p.changeReason,
				); err != nil {
					glog.Errorf("Failed to check and notify state change for app %s: %v", p.appName, err)
				} else {
					glog.V(2).Infof("DEBUG: Successfully processed notification for app=%s", p.appName)
				}
			}
		} else {
			glog.V(3).Infof("DEBUG: State monitor is nil, cannot process %d pending notifications", len(pendingNotifications))
		}
	}()

	if !cm.isRunning {
		return fmt.Errorf("cache manager is not running")
	}

	if cm.cache == nil {
		return fmt.Errorf("cache is not initialized")
	}

	// Ensure user exists
	if _, exists := cm.cache.Users[userID]; !exists {
		cm.cache.Users[userID] = NewUserDataEx(userID) // NewUserData()
	}

	// Check source limit for user
	userData := cm.cache.Users[userID]
	// No nested locks needed since we already hold the global lock

	// Check if we're adding a new source and if it exceeds the limit
	if _, exists := userData.Sources[sourceID]; !exists {
		if cm.userConfig != nil {
			maxSources := cm.userConfig.MaxSourcesPerUser

			// Give admin users double the limit
			for _, admin := range cm.userConfig.AdminList {
				if admin == userID {
					maxSources *= 2
					break
				}
			}

			if len(userData.Sources) >= maxSources {
				glog.Warningf("User '%s' has reached maximum sources limit (%d)", userID, maxSources)
				return fmt.Errorf("user '%s' has reached maximum sources limit (%d)", userID, maxSources)
			}
		}

		userData.Sources[sourceID] = NewSourceData()
	}

	// Set the data
	sourceData := userData.Sources[sourceID]
	// No nested locks needed since we already hold the global lock

	// Log image analysis information if present
	if imageAnalysis, hasImageAnalysis := data["image_analysis"]; hasImageAnalysis {
		glog.V(3).Infof("Setting app data with image analysis for user=%s, source=%s, type=%s", userID, sourceID, dataType)
		if analysisMap, ok := imageAnalysis.(map[string]interface{}); ok {
			if totalImages, ok := analysisMap["total_images"].(int); ok {
				glog.V(3).Infof("App data includes %d Docker images", totalImages)
			}
		}
	}

	switch dataType {
	case AppInfoHistory:
		appData := NewAppInfoHistoryData(data)
		appData.Timestamp = time.Now().Unix()
		sourceData.AppInfoHistory = append(sourceData.AppInfoHistory, appData)
	case AppStateLatest:
		// Check if this is a list of app states
		if appStatesData, hasAppStates := data["app_states"].([]*types.AppStateLatestData); hasAppStates {
			glog.V(2).Infof("DEBUG: Processing batch of %d app states for user=%s, source=%s", len(appStatesData), userID, sourceID)
			// Collect state change notifications for each app state (send after unlock)
			if cm.stateMonitor != nil {
				glog.V(3).Infof("DEBUG: State monitor available for batch processing")
				for i, appState := range appStatesData {
					if appState == nil {
						glog.V(4).Infof("DEBUG: App state %d is nil, skipping", i)
						continue
					}
					appName := appState.Status.Name
					if appName == "" {
						glog.V(4).Infof("DEBUG: App state %d has empty name, skipping", i)
						continue
					}
					glog.V(3).Infof("DEBUG: Adding batch pending notification for app=%s (index=%d)", appName, i)

					// Check if state has changed before creating notification
					hasChanged, changeReason := cm.stateMonitor.HasStateChanged(appName, appState, sourceData.AppStateLatest)
					glog.V(3).Infof("DEBUG: Batch state change check for app=%s: hasChanged=%v, reason=%s", appName, hasChanged, changeReason)

					pendingNotifications = append(pendingNotifications, pendingNotify{
						userID:       userID,
						sourceID:     sourceID,
						appName:      appName,
						state:        appState,
						existing:     sourceData.AppStateLatest,
						latest:       sourceData.AppInfoLatest,
						hasChanged:   hasChanged,
						changeReason: changeReason,
					})
					glog.V(3).Infof("DEBUG: Added batch pending notification for app=%s, total pending=%d", appName, len(pendingNotifications))
				}
			} else {
				glog.V(3).Infof("DEBUG: State monitor is nil, skipping batch pending notifications for %d app states", len(appStatesData))
			}

			// Update each app state individually using name matching
			for _, appState := range appStatesData {
				if appState != nil {
					cm.updateAppStateLatest(userID, sourceID, sourceData, appState)
				}
			}
			glog.V(2).Infof("Updated %d app states for user=%s, source=%s", len(appStatesData), userID, sourceID)

			// After batch updateAppStateLatest, check if any EntranceStatuses were preserved and force push was sent
			// If so, remove the corresponding pending notifications to avoid duplicate push
			// This is similar to the single state update case, but we need to check all notifications
			if len(pendingNotifications) > 0 {
				// Create a map to track which apps had force push sent
				forcePushedApps := make(map[string]bool)

				// Check each updated state to see if force push was sent
				for _, appState := range appStatesData {
					if appState == nil || appState.Status.Name == "" {
						continue
					}

					// Check if EntranceStatuses was preserved by checking the state in cache
					var updatedState *types.AppStateLatestData
					for _, state := range sourceData.AppStateLatest {
						if state != nil && state.Status.Name == appState.Status.Name {
							updatedState = state
							break
						}
					}

					// If updated state has EntranceStatuses (was preserved), check if main state/progress didn't change
					if updatedState != nil && len(updatedState.Status.EntranceStatuses) > 0 {
						// Find the existing state before update
						var existingStateBeforeUpdate *types.AppStateLatestData
						for _, notify := range pendingNotifications {
							if notify.appName == appState.Status.Name {
								for _, state := range notify.existing {
									if state != nil && state.Status.Name == appState.Status.Name {
										existingStateBeforeUpdate = state
										break
									}
								}
								break
							}
						}

						if existingStateBeforeUpdate != nil {
							mainStateChanged := updatedState.Status.State != existingStateBeforeUpdate.Status.State
							progressChanged := updatedState.Status.Progress != existingStateBeforeUpdate.Status.Progress
							if !mainStateChanged && !progressChanged {
								// EntranceStatuses was preserved, main state/progress didn't change,
								// and force push was already sent in updateAppStateLatest
								forcePushedApps[appState.Status.Name] = true
							}
						}
					}
				}

				// Remove pending notifications for apps that had force push sent
				if len(forcePushedApps) > 0 {
					filteredNotifications := make([]pendingNotify, 0, len(pendingNotifications))
					for _, notify := range pendingNotifications {
						if notify.changeReason == "entrance statuses changed" && forcePushedApps[notify.appName] {
							glog.V(3).Infof("Removing duplicate batch pending notification for app=%s (force push already sent)", notify.appName)
							continue
						}
						filteredNotifications = append(filteredNotifications, notify)
					}
					pendingNotifications = filteredNotifications
				}
			}
		} else {
			// Fallback to old logic for backward compatibility
			// Check if entrance URLs are missing and fetch them if needed
			enhancedData := cm.enhanceAppStateDataWithUrls(data, userID)

			appData, sourceIDFromRecord := types.NewAppStateLatestData(enhancedData, userID, utils.GetAppInfoLastInstalled)

			// Validate that the created app state has a name field
			if appData == nil {
				glog.Errorf("Failed to create AppStateLatestData from data for user=%s, source=%s - data may be invalid", userID, sourceID)
				return fmt.Errorf("invalid app state data: NewAppStateLatestData returned nil")
			}

			if appData.Status.Name == "" {
				glog.Errorf("Invalid app state data: missing name field for user=%s, source=%s - app state will be rejected", userID, sourceID)
				return fmt.Errorf("invalid app state data: missing name field")
			}

			// Collect single state change notification (send after unlock)
			if cm.stateMonitor != nil {
				appName := appData.Status.Name
				glog.V(2).Infof("DEBUG: State monitor available, appName=%s, appData=%v", appName, appData != nil)
				if appName != "" {
					// Check if state has changed before creating notification
					hasChanged, changeReason := cm.stateMonitor.HasStateChanged(appName, appData, sourceData.AppStateLatest)
					glog.V(4).Infof("DEBUG: State change check for app=%s: hasChanged=%v, reason=%s", appName, hasChanged, changeReason)

					pendingNotifications = append(pendingNotifications, pendingNotify{
						userID:       userID,
						sourceID:     sourceIDFromRecord,
						appName:      appName,
						state:        appData,
						existing:     sourceData.AppStateLatest,
						latest:       sourceData.AppInfoLatest,
						hasChanged:   hasChanged,
						changeReason: changeReason,
					})
					glog.V(2).Infof("DEBUG: Added pending notification for app=%s, total pending=%d", appName, len(pendingNotifications))
				} else {
					glog.V(3).Infof("DEBUG: AppName is empty, skipping pending notification")
				}
			} else {
				glog.V(3).Infof("DEBUG: State monitor is nil, skipping pending notification for app=%s", appData.Status.Name)
			}

			// Update or add the app state using name matching
			cm.updateAppStateLatest(userID, sourceIDFromRecord, sourceData, appData)
			glog.V(2).Infof("Updated single app state for user=%s, source=%s", userID, sourceIDFromRecord)

			// After updateAppStateLatest, check if EntranceStatuses was preserved and force push was sent
			// If so, remove the corresponding pending notification to avoid duplicate push
			// This happens when: EntranceStatuses was empty -> preserved, but HasStateChanged detected "entrance statuses changed"
			// before the preservation happened, and updateAppStateLatest sent a force push
			if len(pendingNotifications) > 0 {
				lastIdx := len(pendingNotifications) - 1
				lastNotify := &pendingNotifications[lastIdx]
				if lastNotify.appName == appData.Status.Name &&
					lastNotify.changeReason == "entrance statuses changed" {
					// Check if EntranceStatuses was actually preserved by checking the state in cache
					var updatedState *types.AppStateLatestData
					for _, state := range sourceData.AppStateLatest {
						if state != nil && state.Status.Name == appData.Status.Name {
							updatedState = state
							break
						}
					}

					// If updated state has EntranceStatuses (was preserved) and main state/progress didn't change,
					// it means updateAppStateLatest sent a force push, so remove this pending notification
					if updatedState != nil && len(updatedState.Status.EntranceStatuses) > 0 {
						// Compare with original appData to check if main state/progress changed
						// Note: appData was modified by updateAppStateLatest (EntranceStatuses was preserved),
						// so we need to compare with the existing state before update
						var existingStateBeforeUpdate *types.AppStateLatestData
						for _, state := range lastNotify.existing {
							if state != nil && state.Status.Name == appData.Status.Name {
								existingStateBeforeUpdate = state
								break
							}
						}

						if existingStateBeforeUpdate != nil {
							mainStateChanged := updatedState.Status.State != existingStateBeforeUpdate.Status.State
							progressChanged := updatedState.Status.Progress != existingStateBeforeUpdate.Status.Progress
							if !mainStateChanged && !progressChanged {
								// EntranceStatuses was preserved, main state/progress didn't change,
								// and force push was already sent in updateAppStateLatest, so remove this notification
								glog.V(3).Infof("Removing duplicate pending notification for app=%s (force push already sent)", appData.Status.Name)
								pendingNotifications = pendingNotifications[:lastIdx]
							}
						}
					}
				}
			}
		}
	case AppInfoLatest:
		appData := NewAppInfoLatestData(data)
		if appData == nil {
			glog.Warningf("Failed to create AppInfoLatestData from data for user=%s, source=%s - data may be invalid", userID, sourceID)
			return fmt.Errorf("invalid app data: NewAppInfoLatestData returned nil")
		}
		appData.Timestamp = time.Now().Unix()
		sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, appData)
	case AppInfoLatestPending:
		// Build version map from AppInfoLatest to skip apps with unchanged versions
		latestVersionMap := make(map[string]string)
		for _, latestApp := range sourceData.AppInfoLatest {
			if latestApp == nil || latestApp.RawData == nil {
				continue
			}
			v := latestApp.RawData.Version
			if v == "" {
				continue
			}
			if latestApp.RawData.Name != "" {
				latestVersionMap[latestApp.RawData.Name] = v
			}
			if latestApp.RawData.AppID != "" {
				latestVersionMap[latestApp.RawData.AppID] = v
			}
			if latestApp.RawData.ID != "" {
				latestVersionMap[latestApp.RawData.ID] = v
			}
		}
		// Build version map from AppRenderFailed to avoid re-adding the same failed app
		// into Pending on every sync cycle.
		failedVersionMap := make(map[string]string)
		for _, failedApp := range sourceData.AppRenderFailed {
			if failedApp == nil || failedApp.RawData == nil {
				continue
			}
			v := failedApp.Version
			if v == "" {
				v = failedApp.RawData.Version
			}
			if failedApp.RawData.Name != "" {
				failedVersionMap[failedApp.RawData.Name] = v
			}
			if failedApp.RawData.AppID != "" {
				failedVersionMap[failedApp.RawData.AppID] = v
			}
			if failedApp.RawData.ID != "" {
				failedVersionMap[failedApp.RawData.ID] = v
			}
		}

		originalCount := len(sourceData.AppInfoLatestPending)
		sourceData.AppInfoLatestPending = sourceData.AppInfoLatestPending[:0]
		glog.V(3).Infof("Cleared %d existing AppInfoLatestPending entries for user=%s, source=%s", originalCount, userID, sourceID)

		shouldSkipApp := func(appData *AppInfoLatestPendingData) bool {
			if appData == nil || appData.RawData == nil {
				return false
			}
			incomingVersion := appData.RawData.Version
			if incomingVersion != "" {
				if name := appData.RawData.Name; name != "" {
					if existing, ok := latestVersionMap[name]; ok && existing == incomingVersion {
						return true
					}
				}
				if id := appData.RawData.AppID; id != "" {
					if existing, ok := latestVersionMap[id]; ok && existing == incomingVersion {
						return true
					}
				}
				if id := appData.RawData.ID; id != "" {
					if existing, ok := latestVersionMap[id]; ok && existing == incomingVersion {
						return true
					}
				}
			}

			// Skip app only when the same app-version is already in render-failed.
			// Unknown versions should not block upgrades/new retries.
			matchFailed := func(key string) bool {
				if key == "" || incomingVersion == "" {
					return false
				}
				failedVersion, ok := failedVersionMap[key]
				if !ok || failedVersion == "" {
					return false
				}
				return failedVersion == incomingVersion
			}
			if matchFailed(appData.RawData.Name) || matchFailed(appData.RawData.AppID) || matchFailed(appData.RawData.ID) {
				return true
			}
			return false
		}

		skippedCount := 0

		if appsData, hasApps := data["apps"].(map[string]interface{}); hasApps {
			glog.V(3).Infof("Processing complete market data with %d apps for user=%s, source=%s", len(appsData), userID, sourceID)

			others := &types.Others{}
			if version, ok := data["version"].(string); ok {
				others.Version = version
			}
			if hash, ok := data["hash"].(string); ok {
				others.Hash = hash
			}
			if topics, ok := data["topics"].(map[string]interface{}); ok {
				for _, topicData := range topics {
					if topicMap, ok := topicData.(map[string]interface{}); ok {
						topic := &types.Topic{}
						if name, ok := topicMap["name"].(string); ok {
							topic.Name = name
						}
						if data, ok := topicMap["data"].(map[string]interface{}); ok {
							topic.Data = make(map[string]*types.TopicData)
							for lang, topicDataInterface := range data {
								if topicDataMap, ok := topicDataInterface.(map[string]interface{}); ok {
									topicData := &types.TopicData{}
									if apps, ok := topicDataMap["apps"].(string); ok {
										topicData.Apps = apps
									}
									topic.Data[lang] = topicData
								}
							}
						}
						others.Topics = append(others.Topics, topic)
					}
				}
			}
			sourceData.Others = others

			for appID, appDataInterface := range appsData {
				if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
					appData := NewAppInfoLatestPendingDataFromLegacyData(appDataMap)
					if appData == nil {
						continue
					}
					if shouldSkipApp(appData) {
						skippedCount++
						continue
					}
					appData.Timestamp = time.Now().Unix()
					sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
					glog.V(3).Infof("Added app %s for user=%s, source=%s", appID, userID, sourceID)
				}
			}
		} else { // syncer
			if dataSection, hasData := data["data"].(map[string]interface{}); hasData {
				if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
					for appID, appDataInterface := range appsData {
						if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
							appData := NewAppInfoLatestPendingDataFromLegacyData(appDataMap)
							if appData == nil {
								continue
							}
							if shouldSkipApp(appData) {
								skippedCount++
								continue
							}
							appData.Timestamp = time.Now().Unix()
							sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
							glog.V(3).Infof("Added app %s for user=%s, source=%s", appID, userID, sourceID)
						}
					}
				} else {
					glog.Warningf("Market data found but no apps section for user=%s, source=%s", userID, sourceID)
				}
			} else {
				appData := NewAppInfoLatestPendingDataFromLegacyData(data)
				if appData == nil {
					glog.Warningf("Failed to create AppInfoLatestPendingData for user=%s, source=%s", userID, sourceID)
					return fmt.Errorf("invalid app data: missing required identifiers (id, name, or appID)")
				}
				if !shouldSkipApp(appData) {
					appData.Timestamp = time.Now().Unix()
					sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, appData)
				} else {
					skippedCount++
				}
			}
		}

		glog.V(2).Infof("Updated AppInfoLatestPending: %d new, %d skipped (unchanged version or in render-failed) for user=%s, source=%s",
			len(sourceData.AppInfoLatestPending), skippedCount, userID, sourceID)

	case types.AppRenderFailed:
		// Handle render failed data - this is typically set by the hydrator when tasks fail
		if failedAppData, hasFailedApp := data["failed_app"].(*types.AppRenderFailedData); hasFailedApp {
			if failedAppData == nil || failedAppData.RawData == nil {
				glog.Errorf("Invalid render failed data: nil failed app or raw data for user=%s, source=%s", userID, sourceID)
				return fmt.Errorf("invalid render failed data: nil failed app or raw data")
			}

			replaced := false
			for i, existing := range sourceData.AppRenderFailed {
				if existing == nil || existing.RawData == nil {
					continue
				}
				matchedByID := (failedAppData.RawData.ID != "" && existing.RawData.ID == failedAppData.RawData.ID) ||
					(failedAppData.RawData.AppID != "" && existing.RawData.AppID == failedAppData.RawData.AppID)
				matchedByName := failedAppData.RawData.Name != "" && existing.RawData.Name == failedAppData.RawData.Name
				if matchedByID || matchedByName {
					sourceData.AppRenderFailed[i] = failedAppData
					replaced = true
					break
				}
			}

			if !replaced {
				sourceData.AppRenderFailed = append(sourceData.AppRenderFailed, failedAppData)
			}
			glog.V(3).Infof("Upserted render failed app for user=%s, source=%s, app=%s, reason=%s",
				userID, sourceID, failedAppData.RawData.AppID, failedAppData.FailureReason)
		} else {
			glog.Errorf("Invalid render failed data format for user=%s, source=%s", userID, sourceID)
			return fmt.Errorf("invalid render failed data: missing failed_app field")
		}
	}

	// Trigger async sync to Redis
	cm.requestSync(SyncRequest{
		UserID:   userID,
		SourceID: sourceID,
		Type:     SyncSource,
	})

	glog.V(3).Infof("Set app data for user=%s, source=%s, type=%s", userID, sourceID, dataType)
	return nil
}

func (cm *CacheManager) SetAppData(userID, sourceID string, dataType AppDataType, data map[string]interface{}, tracing string) error {

	glog.Infof("[SetAppData] user: %s, source: %s, dataType: %s, trace: %s", userID, sourceID, dataType, tracing)
	// go func() {
	if err := cm.setAppDataInternal(userID, sourceID, dataType, data); err != nil {
		glog.Errorf("Failed to set app data in goroutine: %v", err)
	}
	// }()

	return nil
}

func (cm *CacheManager) setLocalAppDataInternal(userID, sourceID string, dataType AppDataType, data types.AppInfoLatestData) error {
	cm.mutex.Lock()
	cm.updateLockStats("lock")
	_wd := cm.startLockWatchdog("@SetLocalAppData")

	defer func() {
		cm.mutex.Unlock()
		cm.updateLockStats("unlock")
		glog.V(4).Infof("[LOCK] cm.mutex.Unlock() @SetLocalAppData End")
		_wd()
	}()

	if !cm.isRunning {
		return fmt.Errorf("cache manager is not running")
	}
	if cm.cache == nil {
		return fmt.Errorf("cache is not initialized")
	}

	if _, exists := cm.cache.Users[userID]; !exists {
		cm.cache.Users[userID] = NewUserDataEx(userID) // NewUserData()
	}
	userData := cm.cache.Users[userID]
	if _, exists := userData.Sources[sourceID]; !exists {
		userData.Sources[sourceID] = NewSourceData()
	}

	pending := &types.AppInfoLatestPendingData{
		Type:            types.AppInfoLatestPending,
		Timestamp:       data.Timestamp,
		Version:         data.Version,
		RawData:         data.RawData,
		RawPackage:      data.RawPackage,
		Values:          data.Values,
		AppInfo:         data.AppInfo,
		RenderedPackage: data.RenderedPackage,
	}
	sourceData := userData.Sources[sourceID]

	found := false
	for i, exist := range sourceData.AppInfoLatestPending {
		if exist != nil && exist.RawData != nil && pending.RawData != nil {
			nameEqual := exist.RawData.Name == pending.RawData.Name && exist.RawData.Name != ""
			appIDEqual := exist.RawData.AppID == pending.RawData.AppID && exist.RawData.AppID != ""
			if nameEqual || appIDEqual {
				existVer := exist.Version
				newVer := pending.Version
				if existVer == newVer || newVer > existVer {
					sourceData.AppInfoLatestPending[i] = pending
				}
				found = true
				break
			}
		}
	}
	if !found {
		sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, pending)
	}

	cm.requestSync(SyncRequest{
		UserID:   userID,
		SourceID: sourceID,
		Type:     SyncSource,
	})

	glog.V(2).Infof("SetLocalAppData: added AppInfoLatestPending for user=%s, source=%s", userID, sourceID)
	return nil
}

func (cm *CacheManager) SetLocalAppData(userID, sourceID string, dataType AppDataType, data types.AppInfoLatestData) error {
	go func() {
		if err := cm.setLocalAppDataInternal(userID, sourceID, dataType, data); err != nil {
			glog.Errorf("Failed to set local app data in goroutine: %v", err)
		}
	}()
	return nil
}

// GetAppData retrieves app data from cache using single global lock
func (cm *CacheManager) GetAppData(userID, sourceID string, dataType AppDataType) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if userData, exists := cm.cache.Users[userID]; exists {
		if sourceData, exists := userData.Sources[sourceID]; exists {
			// No nested locks needed since we already hold the global lock
			switch dataType {
			case AppInfoHistory:
				return sourceData.AppInfoHistory
			case AppStateLatest:
				return sourceData.AppStateLatest
			case AppInfoLatest:
				return sourceData.AppInfoLatest
			case AppInfoLatestPending:
				return sourceData.AppInfoLatestPending
			case types.AppRenderFailed:
				return sourceData.AppRenderFailed
			}
		}
	}
	return nil
}

// RemoveUserData removes user data from cache and Redis
func (cm *CacheManager) removeUserDataInternal(userID string) error {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@568:removeUser")
	defer func() { cm.mutex.Unlock(); _wd() }()

	// Remove from cache
	delete(cm.cache.Users, userID)

	// Trigger async deletion from Redis
	cm.requestSync(SyncRequest{
		UserID: userID,
		Type:   DeleteUser,
	})

	glog.V(3).Infof("Removed user data for user=%s", userID)
	return nil
}

// RemoveUserData removes user data from cache and Redis
func (cm *CacheManager) RemoveUserData(userID string) error {
	go func() {
		if err := cm.removeUserDataInternal(userID); err != nil {
			glog.Errorf("Failed to remove user data in goroutine: %v", err)
		}
	}()
	return nil
}

// AddUser adds a new user to the cache
func (cm *CacheManager) addUserInternal(userID string) error {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@AddUser")
	defer func() { cm.mutex.Unlock(); _wd() }()

	if _, exists := cm.cache.Users[userID]; exists {
		glog.V(4).Infof("User %s already exists in cache", userID)
		return nil
	}

	userData := NewUserDataEx(userID) // NewUserData()

	// Initialize sources from settingsManager
	if cm.settingsManager != nil {
		sourcesConfig := cm.settingsManager.GetMarketSources()
		if sourcesConfig != nil {
			for _, src := range sourcesConfig.Sources {

				userData.Sources[src.ID] = types.NewSourceDataWithType(types.SourceDataType(src.Type))
				glog.V(3).Infof("Initialized source %s for user %s", src.ID, userID)
			}
		} else {
			glog.Warningf("settingsManager.GetMarketSources() returned nil, no sources initialized for user %s", userID)
		}
	} else {
		glog.Warningf("settingsManager is nil, no sources initialized for user %s", userID)
	}

	cm.cache.Users[userID] = userData
	glog.V(2).Infof("User %s added to cache with %d sources", userID, len(userData.Sources))

	if cm.isRunning {
		cm.requestSync(SyncRequest{
			UserID: userID,
			Type:   SyncUser,
		})
	}
	return nil
}

// GetCacheStats returns cache statistics using single global lock
func (cm *CacheManager) GetCacheStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_users"] = len(cm.cache.Users)
	stats["is_running"] = cm.isRunning

	totalSources := 0
	for _, userData := range cm.cache.Users {
		// No nested locks needed since we already hold the global lock
		totalSources += len(userData.Sources)
	}
	stats["total_sources"] = totalSources

	return stats
}

// requestSync sends a sync request to the sync worker
func (cm *CacheManager) requestSync(req SyncRequest) {
	if cm.isRunning {
		select {
		case cm.syncChannel <- req:
			// Request queued successfully
		default:
			glog.Warningf("Sync channel is full, dropping sync request")
		}
	}
}

// ForceSync forces immediate synchronization of all data to Redis
func (cm *CacheManager) ForceSync() error {
	glog.V(2).Infof("Force syncing all cache data to Redis")

	// 1. Quickly obtain a data snapshot to minimize lock holding time
	var userDataSnapshot map[string]*UserData
	func() {
		cm.mutex.RLock()
		defer func() {
			cm.mutex.RUnlock()
			glog.V(4).Infof("[LOCK] cm.mutex.RUnlock() @617 End")
		}()

		// Quickly copy data to minimize lock holding time
		userDataSnapshot = make(map[string]*UserData)
		for userID, userData := range cm.cache.Users {
			userDataSnapshot[userID] = userData
		}
	}()

	if userDataSnapshot == nil {
		return fmt.Errorf("read lock not available for force sync")
	}

	// 2. Perform Redis operations outside the lock
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		var err error
		for userID, userData := range userDataSnapshot {
			if err = cm.redisClient.SaveUserDataToRedis(userID, userData); err != nil {
				glog.Errorf("Failed to force sync user data: %v", err)
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			return err
		}
		glog.V(3).Infof("Force sync completed successfully")
		return nil
	case <-ctx.Done():
		glog.Errorf("Force sync timed out after 5 seconds")
		return fmt.Errorf("force sync timed out: %v", ctx.Err())
	}
}

// GetAllUsersData returns all users data from cache using single global lock
func (cm *CacheManager) GetAllUsersData() map[string]*UserData {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.cache == nil {
		return make(map[string]*UserData)
	}

	result := make(map[string]*UserData)
	for userID, userData := range cm.cache.Users {
		userDataCopy := &UserData{
			Sources: make(map[string]*SourceData),
			Hash:    userData.Hash,
		}
		for sourceID, sourceData := range userData.Sources {
			userDataCopy.Sources[sourceID] = sourceData
		}
		result[userID] = userDataCopy
	}
	return result
}

// HasUserStateDataForSource checks if any user has non-empty state data for a specific source
func (cm *CacheManager) HasUserStateDataForSource(sourceID string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if cm.cache == nil {
		return false
	}

	// Check all users for non-empty state data in the specified source
	for userID, userData := range cm.cache.Users {
		if sourceData, exists := userData.Sources[sourceID]; exists {
			// Check if any of the state-related data is non-empty
			if len(sourceData.AppStateLatest) > 0 {
				glog.V(3).Infof("Found non-empty state data for user %s in source %s", userID, sourceID)
				return true
			}
		}
	}

	glog.V(2).Infof("No user state data found for source: %s", sourceID)
	return false
}

// UpdateUserConfig updates the user configuration and ensures all users have data structures
func (cm *CacheManager) updateUserConfigInternal(newUserConfig *UserConfig) error {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@660:updateUserConfig")
	defer func() { cm.mutex.Unlock(); _wd() }()

	if newUserConfig == nil {
		return fmt.Errorf("user config cannot be nil")
	}

	glog.V(3).Infof("Updating user configuration")

	// oldUserConfig := cm.userConfig // Commented out as it's only used in optional removal logic
	cm.userConfig = newUserConfig

	// Initialize data structures for new users in the updated configuration
	if len(newUserConfig.UserList) > 0 {
		for _, userID := range newUserConfig.UserList {
			if _, exists := cm.cache.Users[userID]; !exists {
				glog.V(3).Infof("Creating data structure for newly configured user: %s", userID)
				userData := NewUserDataEx(userID) // NewUserData()
				cm.cache.Users[userID] = userData

				// Trigger sync to Redis for the new user
				if cm.isRunning {
					cm.requestSync(SyncRequest{
						UserID: userID,
						Type:   SyncUser,
					})
				}
			}
		}
	}

	// Optionally remove users that are no longer in the configuration
	// (This is commented out by default to preserve data)
	/*
		oldUserConfig := cm.userConfig // Uncomment this line if enabling user removal logic
		if oldUserConfig != nil {
			for _, oldUserID := range oldUserConfig.UserList {
				found := false
				for _, newUserID := range newUserConfig.UserList {
					if oldUserID == newUserID {
						found = true
						break
					}
				}
				if !found {
					glog.Infof("User %s is no longer in configuration, but data is preserved", oldUserID)
					// Uncomment the line below if you want to remove data for users not in the new configuration
					// delete(cm.cache.Users, oldUserID)
				}
			}
		}
	*/

	glog.V(2).Infof("User configuration updated successfully")
	return nil
}

// UpdateUserConfig updates the user configuration and ensures all users have data structures
func (cm *CacheManager) UpdateUserConfig(newUserConfig *UserConfig) error {
	go func() {
		if err := cm.updateUserConfigInternal(newUserConfig); err != nil {
			glog.Errorf("Failed to update user config in goroutine: %v", err)
		}
	}()
	return nil
}

// SyncUserListToCache ensures all users from current userConfig have initialized data structures
func (cm *CacheManager) syncUserListToCacheInternal() error {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@718:syncUserList")
	defer func() { cm.mutex.Unlock(); _wd() }()

	if cm.userConfig == nil || len(cm.userConfig.UserList) == 0 {
		glog.Warningf("No user configuration available for syncing")
		return nil
	}

	glog.V(3).Infof("Syncing user list to cache")

	newUsersCount := 0
	newUsersList := make([]string, 0)
	for _, userID := range cm.userConfig.UserList {
		if _, exists := cm.cache.Users[userID]; !exists {
			glog.V(4).Infof("Adding missing user to cache: %s", userID)
			userData := NewUserDataEx(userID) // NewUserData()
			cm.cache.Users[userID] = userData
			newUsersCount++
			newUsersList = append(newUsersList, userID)

			// Trigger sync to Redis for the new user
			if cm.isRunning {
				cm.requestSync(SyncRequest{
					UserID: userID,
					Type:   SyncUser,
				})
			}
		}
	}

	glog.V(2).Infof("User list sync completed, added %d new users", newUsersCount)

	return nil
}

// SyncUserListToCache ensures all users from current userConfig have initialized data structures
func (cm *CacheManager) SyncUserListToCache() error {
	go func() {
		if err := cm.syncUserListToCacheInternal(); err != nil {
			glog.Errorf("Failed to sync user list to cache in goroutine: %v", err)
		}
	}()
	return nil
}

// CleanupInvalidPendingData removes invalid pending data entries that lack required identifiers
func (cm *CacheManager) cleanupInvalidPendingDataInternal() int {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@751:cleanupInvalidPending")
	defer func() { cm.mutex.Unlock(); _wd() }()

	totalCleaned := 0

	for userID, userData := range cm.cache.Users {
		// No nested locks needed since we already hold the global lock
		for sourceID, sourceData := range userData.Sources {
			// No nested locks needed since we already hold the global lock

			originalCount := len(sourceData.AppInfoLatestPending)
			cleanedPendingData := make([]*AppInfoLatestPendingData, 0, originalCount)

			for _, pendingData := range sourceData.AppInfoLatestPending {
				// Check if this pending data has valid identifiers
				isValid := false

				if pendingData.RawData != nil {
					// Check for valid ID or AppID
					if (pendingData.RawData.ID != "" && pendingData.RawData.ID != "0") ||
						(pendingData.RawData.AppID != "" && pendingData.RawData.AppID != "0") ||
						(pendingData.RawData.Name != "" && pendingData.RawData.Name != "unknown") {
						isValid = true
					}
				}

				if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
					// Also check AppInfo.AppEntry for valid identifiers
					if (pendingData.AppInfo.AppEntry.ID != "" && pendingData.AppInfo.AppEntry.ID != "0") ||
						(pendingData.AppInfo.AppEntry.AppID != "" && pendingData.AppInfo.AppEntry.AppID != "0") ||
						(pendingData.AppInfo.AppEntry.Name != "" && pendingData.AppInfo.AppEntry.Name != "unknown") {
						isValid = true
					}
				}

				if isValid {
					cleanedPendingData = append(cleanedPendingData, pendingData)
				} else {
					glog.V(3).Infof("Removing invalid pending data entry for user=%s, source=%s (missing identifiers)", userID, sourceID)
					totalCleaned++
				}
			}

			// Update the source data with cleaned list
			sourceData.AppInfoLatestPending = cleanedPendingData

			if originalCount != len(cleanedPendingData) {
				glog.V(3).Infof("Cleaned %d invalid pending data entries for user=%s, source=%s",
					originalCount-len(cleanedPendingData), userID, sourceID)

				// Trigger sync to Redis to persist the cleanup
				if cm.isRunning {
					cm.requestSync(SyncRequest{
						UserID:   userID,
						SourceID: sourceID,
						Type:     SyncSource,
					})
				}
			}
		}
	}

	if totalCleaned > 0 {
		glog.V(2).Infof("Cleanup completed: removed %d invalid pending data entries across all users", totalCleaned)
	}

	return totalCleaned
}

// CleanupInvalidPendingData removes invalid pending data entries that lack required identifiers
func (cm *CacheManager) CleanupInvalidPendingData() int {
	result := make(chan int, 1)
	cancel := make(chan bool, 1)

	go func() {
		// Use a non-blocking approach with cancellation support
		done := make(chan int, 1)
		go func() {
			done <- cm.cleanupInvalidPendingDataInternal()
		}()

		select {
		case cleaned := <-done:
			// Successfully completed, send result
			select {
			case result <- cleaned:
			case <-cancel:
				glog.V(4).Info("CleanupInvalidPendingData: Operation cancelled before sending result")
			}
		case <-cancel:
			glog.V(4).Info("CleanupInvalidPendingData: Operation cancelled")
		}
	}()

	select {
	case cleaned := <-result:
		return cleaned
	case <-time.After(5 * time.Second):
		close(cancel) // Cancel the goroutine
		glog.V(4).Info("CleanupInvalidPendingData timeout, returning 0")
		return 0
	}
}

// enhanceAppStateDataWithUrls enhances app state data with entrance URLs
func (cm *CacheManager) enhanceAppStateDataWithUrls(data map[string]interface{}, user string) map[string]interface{} {
	// Create a copy of the data to avoid modifying the original
	enhancedData := make(map[string]interface{})
	for k, v := range data {
		enhancedData[k] = v
	}

	// Add debug logging for input data
	if entranceStatusesVal, ok := data["entranceStatuses"]; ok {
		glog.V(2).Infof("DEBUG: enhanceAppStateDataWithUrls - input entranceStatuses type: %T, value: %+v", entranceStatusesVal, entranceStatusesVal)
	} else {
		glog.V(3).Info("DEBUG: enhanceAppStateDataWithUrls - no entranceStatuses found in input data")
	}

	// Extract app name for URL fetching
	var appName string
	if name, ok := data["name"].(string); ok && name != "" {
		appName = name
	} else if appNameVal, ok := data["appName"].(string); ok && appNameVal != "" {
		appName = appNameVal
	} else if appIDVal, ok := data["appID"].(string); ok && appIDVal != "" {
		appName = appIDVal
	} else if idVal, ok := data["id"].(string); ok && idVal != "" {
		appName = idVal
	}

	if appName == "" {
		glog.V(3).Infof("Cannot determine app name for URL enhancement")
		return enhancedData
	}

	// Process entrance statuses
	if entranceStatusesVal, ok := data["entranceStatuses"]; ok {
		if entranceStatuses, ok := entranceStatusesVal.([]interface{}); ok {
			// Check if any running entrance has empty URL
			hasRunningWithEmptyUrl := false
			runningEntrancesWithoutUrl := make([]string, 0)

			for _, entranceVal := range entranceStatuses {
				if entranceMap, ok := entranceVal.(map[string]interface{}); ok {
					state, _ := entranceMap["state"].(string)
					url, hasUrl := entranceMap["url"].(string)
					name, _ := entranceMap["name"].(string)

					// Only require URL for running entrances
					if state == "running" && (!hasUrl || url == "") {
						hasRunningWithEmptyUrl = true
						if name != "" {
							runningEntrancesWithoutUrl = append(runningEntrancesWithoutUrl, name)
						}
					}
				}
			}

			// If running entrances have empty URLs, try to fetch them from app-service
			if hasRunningWithEmptyUrl {
				glog.V(4).Infof("Running entrances %v have empty URLs for app %s - attempting to fetch URLs from app-service", runningEntrancesWithoutUrl, appName)

				// Fetch entrance URLs from app-service
				entranceUrls, err := utils.FetchAppEntranceUrls(appName, user)
				if err != nil {
					glog.Errorf("Failed to fetch entrance URLs for app %s: %v - returning empty entrance statuses", appName, err)
					enhancedData["entranceStatuses"] = []interface{}{}
					return enhancedData
				}

				// Update entrance statuses with fetched URLs
				updatedEntrances := make([]interface{}, 0, len(entranceStatuses))
				for _, entranceVal := range entranceStatuses {
					if entranceMap, ok := entranceVal.(map[string]interface{}); ok {
						name, _ := entranceMap["name"].(string)
						state, _ := entranceMap["state"].(string)
						url, hasUrl := entranceMap["url"].(string)

						// If this is a running entrance without URL, try to get it from fetched URLs
						if state == "running" && (!hasUrl || url == "") {
							if fetchedUrl, exists := entranceUrls[name]; exists && fetchedUrl != "" {
								entranceMap["url"] = fetchedUrl
								glog.V(2).Infof("Updated entrance %s URL for app %s: %s", name, appName, fetchedUrl)
							} else {
								glog.V(3).Infof("Running entrance %s for app %s still has no URL after fetching - skipping", name, appName)
								continue // Skip this entrance
							}
						}

						updatedEntrances = append(updatedEntrances, entranceMap)
					}
				}

				enhancedData["entranceStatuses"] = updatedEntrances
				glog.V(3).Infof("DEBUG: enhanceAppStateDataWithUrls - output entranceStatuses type: %T, value: %+v", enhancedData["entranceStatuses"], enhancedData["entranceStatuses"])
				return enhancedData
			}

			// If no running entrances with empty URLs, return as is
			enhancedData["entranceStatuses"] = entranceStatuses
			glog.V(2).Infof("DEBUG: enhanceAppStateDataWithUrls - output entranceStatuses type: %T, value: %+v", enhancedData["entranceStatuses"], enhancedData["entranceStatuses"])
			return enhancedData
		}
	}

	return enhancedData
}

// getLockStats returns current lock statistics for internal monitoring
func (cm *CacheManager) getLockStats() map[string]interface{} {
	cm.lockStats.Lock()
	defer cm.lockStats.Unlock()

	stats := make(map[string]interface{})
	stats["last_lock_time"] = cm.lockStats.lastLockTime
	stats["last_unlock_time"] = cm.lockStats.lastUnlockTime
	stats["lock_duration"] = cm.lockStats.lockDuration
	stats["lock_count"] = cm.lockStats.lockCount
	stats["unlock_count"] = cm.lockStats.unlockCount

	if cm.lockStats.lockCount > cm.lockStats.unlockCount {
		stats["lock_imbalance"] = cm.lockStats.lockCount - cm.lockStats.unlockCount
		stats["potential_deadlock"] = true
	} else {
		stats["lock_imbalance"] = 0
		stats["potential_deadlock"] = false
	}

	if !cm.lockStats.lastLockTime.IsZero() && cm.lockStats.lockDuration > 30*time.Second {
		stats["long_lock_duration"] = true
		stats["current_lock_duration"] = time.Since(cm.lockStats.lastLockTime)
	} else {
		stats["long_lock_duration"] = false
	}

	return stats
}

// dumpLockInfo prints lock stats and all goroutine stacks for diagnosing lock holders
func (cm *CacheManager) dumpLockInfo(reason string) {
	glog.V(4).Infof("LOCK DIAG: reason=%s", reason)
	stats := cm.getLockStats()
	glog.V(4).Infof("LOCK DIAG: stats=%v", stats)

	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	glog.V(4).Infof("LOCK DIAG: goroutine dump (%d bytes)\n%s", n, string(buf[:n]))
}

// updateLockStats updates lock statistics
func (cm *CacheManager) updateLockStats(lockType string) {
	glog.V(4).Infof("[LOCK] cm.lockStats.Lock() Start")
	cm.lockStats.Lock()
	defer func() {
		cm.lockStats.Unlock()
		glog.V(4).Infof("[LOCK] cm.lockStats.Unlock() End")
	}()

	now := time.Now()
	if lockType == "lock" {
		cm.lockStats.lastLockTime = now
		cm.lockStats.lockCount++
		glog.V(4).Infof("[LOCK] Lock stats updated - lock count: %d", cm.lockStats.lockCount)
	} else if lockType == "unlock" {
		cm.lockStats.lastUnlockTime = now
		cm.lockStats.unlockCount++
		if !cm.lockStats.lastLockTime.IsZero() {
			cm.lockStats.lockDuration = now.Sub(cm.lockStats.lastLockTime)
		}
		glog.V(4).Infof("[LOCK] Lock stats updated - unlock count: %d, duration: %v", cm.lockStats.unlockCount, cm.lockStats.lockDuration)
	}
}

// RemoveAppStateData removes a specific app from AppStateLatest for a user and source
func (cm *CacheManager) removeAppStateDataInternal(userID, sourceID, appName string) error {
	glog.Infof("[CACHE], remove appStateData, user: %s, source: %s, app: %s", userID, sourceID, appName)
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@RemoveAppStateData")
	defer func() { cm.mutex.Unlock(); _wd() }()

	if !cm.isRunning {
		return fmt.Errorf("cache manager is not running")
	}

	userData, userExists := cm.cache.Users[userID]
	if !userExists {
		return fmt.Errorf("user %s not found", userID)
	}

	sourceData, sourceExists := userData.Sources[sourceID]
	if !sourceExists {
		return fmt.Errorf("source %s not found for user %s", sourceID, userID)
	}

	originalCount := len(sourceData.AppStateLatest)
	newList := make([]*types.AppStateLatestData, 0, originalCount)
	for _, appState := range sourceData.AppStateLatest {
		if appState == nil || appState.Status.Name != appName {
			newList = append(newList, appState)
		}
	}
	sourceData.AppStateLatest = newList

	if len(newList) < originalCount {
		glog.V(2).Infof("Removed app %s from AppStateLatest for user=%s, source=%s", appName, userID, sourceID)
		cm.requestSync(SyncRequest{
			UserID:   userID,
			SourceID: sourceID,
			Type:     SyncSource,
		})
	} else {
		glog.V(3).Infof("App %s not found in AppStateLatest for user=%s, source=%s", appName, userID, sourceID)
	}

	return nil
}

// RemoveAppStateData removes a specific app from AppStateLatest for a user and source
func (cm *CacheManager) RemoveAppStateData(userID, sourceID, appName string) error {
	go func() {
		if err := cm.removeAppStateDataInternal(userID, sourceID, appName); err != nil {
			glog.Errorf("Failed to remove app state data in goroutine: %v", err)
		}
	}()
	return nil
}

// RemoveAppInfoLatestData removes a specific app from AppInfoLatest for a user and source
func (cm *CacheManager) removeAppInfoLatestDataInternal(userID, sourceID, appName string) error {
	glog.Infof("[CACHE], remove appInfoLatestData, user: %s, source: %s, app: %s", userID, sourceID, appName)
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@RemoveAppInfoLatestData")
	defer func() { cm.mutex.Unlock(); _wd() }()

	if !cm.isRunning {
		return fmt.Errorf("cache manager is not running")
	}

	userData, userExists := cm.cache.Users[userID]
	if !userExists {
		return fmt.Errorf("user %s not found", userID)
	}

	sourceData, sourceExists := userData.Sources[sourceID]
	if !sourceExists {
		return fmt.Errorf("source %s not found for user %s", sourceID, userID)
	}

	originalCount := len(sourceData.AppInfoLatest)
	newList := make([]*types.AppInfoLatestData, 0, originalCount)
	for _, appInfo := range sourceData.AppInfoLatest {
		if appInfo == nil {
			continue
		}

		// Check multiple possible ID fields for matching
		var appID string
		if appInfo.RawData != nil {
			// Priority: ID > AppID > Name
			if appInfo.RawData.ID != "" {
				appID = appInfo.RawData.ID
			} else if appInfo.RawData.AppID != "" {
				appID = appInfo.RawData.AppID
			} else if appInfo.RawData.Name != "" {
				appID = appInfo.RawData.Name
			}
		}

		// Also check AppSimpleInfo if available
		if appID == "" && appInfo.AppSimpleInfo != nil {
			appID = appInfo.AppSimpleInfo.AppID
		}

		// Match the requested app name
		if appID != appName && (appInfo.RawData == nil || appInfo.RawData.Name != appName) {
			newList = append(newList, appInfo)
		}
	}
	sourceData.AppInfoLatest = newList

	if len(newList) < originalCount {
		glog.V(2).Infof("Removed app %s from AppInfoLatest for user=%s, source=%s", appName, userID, sourceID)
		cm.requestSync(SyncRequest{
			UserID:   userID,
			SourceID: sourceID,
			Type:     SyncSource,
		})
	} else {
		glog.V(3).Infof("App %s not found in AppInfoLatest for user=%s, source=%s", appName, userID, sourceID)
	}

	return nil
}

// RemoveAppInfoLatestData removes a specific app from AppInfoLatest for a user and source
func (cm *CacheManager) RemoveAppInfoLatestData(userID, sourceID, appName string) error {
	go func() {
		if err := cm.removeAppInfoLatestDataInternal(userID, sourceID, appName); err != nil {
			glog.Errorf("Failed to remove app info latest data in goroutine: %v", err)
		}
	}()
	return nil
}

// SetSettingsManager sets the settings manager for the cache manager
func (cm *CacheManager) SetSettingsManager(sm *settings.SettingsManager) {
	cm.settingsManager = sm
}

// GetSettingsManager returns the settings manager instance
func (cm *CacheManager) GetSettingsManager() *settings.SettingsManager {
	return cm.settingsManager
}

// SyncMarketSourcesToCache synchronizes market sources to all users in cache
// todo remove watch dog
func (cm *CacheManager) syncMarketSourcesToCacheInternal(sources []*settings.MarketSource) error {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@SyncMarketSourcesToCache")
	defer func() {
		cm.mutex.Unlock()
		glog.Infof("[LOCK] cm.mutex.Unlock() @SyncMarketSourcesToCache End")
		_wd()
	}()

	if !cm.isRunning {
		return fmt.Errorf("cache manager is not running")
	}

	glog.V(3).Infof("Syncing %d market sources to cache for all users", len(sources))

	// Create a map of source IDs for quick lookup
	sourceIDMap := make(map[string]*settings.MarketSource)
	for _, source := range sources {
		sourceIDMap[source.ID] = source
	}

	// Update all users in cache
	for userID, userData := range cm.cache.Users {
		glog.V(3).Infof("Updating market sources for user: %s", userID)

		// Remove sources that no longer exist
		var sourcesToRemove []string
		for sourceID := range userData.Sources {
			if _, exists := sourceIDMap[sourceID]; !exists {
				if sourceID != "" {
					sourcesToRemove = append(sourcesToRemove, sourceID)
				}

			}
		}

		// Remove non-existent sources
		for _, sourceID := range sourcesToRemove {
			delete(userData.Sources, sourceID)
			glog.V(3).Infof("Removed source %s from user %s", sourceID, userID)
		}

		// Add new sources that don't exist for this user
		for _, source := range sources {
			if _, exists := userData.Sources[source.ID]; !exists {
				userData.Sources[source.ID] = types.NewSourceDataWithType(types.SourceDataType(source.Type))
				glog.V(3).Infof("Added new source %s (%s) for user %s", source.Name, source.ID, userID)
			}
		}

		// Trigger sync to Redis for this user
		if cm.isRunning {
			cm.requestSync(SyncRequest{
				UserID: userID,
				Type:   SyncUser,
			})
		}
	}

	glog.V(2).Infof("Successfully synced market sources to cache for all users")
	return nil
}

// SyncMarketSourcesToCache synchronizes market sources to all users in cache
func (cm *CacheManager) SyncMarketSourcesToCache(sources []*settings.MarketSource) error {
	go func() {
		if err := cm.syncMarketSourcesToCacheInternal(sources); err != nil {
			glog.Errorf("Failed to sync market sources to cache in goroutine: %v", err)
		}
	}()
	return nil
}

func (cm *CacheManager) resynceUserInternal() error {
	cm.mutex.Lock()
	_wd := cm.startLockWatchdog("@resynceUserInternal")
	defer func() { cm.mutex.Unlock(); _wd() }()

	if cm.cache == nil {
		return fmt.Errorf("cache is not initialized")
	}

	if err := client.NewFactory(); err != nil {
		return fmt.Errorf("k8s factory init error: %v", err)
	}

	err := utils.SetupAppServiceData()
	if err == nil {
		extractedUsers := utils.GetExtractedUsers()
		for _, userID := range extractedUsers {
			if _, exists := cm.cache.Users[userID]; !exists {
				// Add user directly without calling AddUserToCache to avoid deadlock
				userData := types.NewUserDataExt(userID) //types.NewUserData()
				activeSources := cm.settingsManager.GetActiveMarketSources()
				for _, source := range activeSources {
					userData.Sources[source.ID] = types.NewSourceDataWithType(types.SourceDataType(source.Type))
				}
				cm.cache.Users[userID] = userData
				glog.V(3).Infof("INFO: User %s has been added to cache and all sources initialized", userID)
			}
		}
	}
	return nil
}

func (cm *CacheManager) ResynceUser() error {
	go func() {
		if err := cm.resynceUserInternal(); err != nil {
			glog.Errorf("Failed to resync user in goroutine: %v", err)
		}
	}()
	return nil
}

// ClearAppRenderFailedData clears all AppRenderFailed data for all users and sources
func (cm *CacheManager) ClearAppRenderFailedData() {
	glog.Info("INFO: [Cleanup] Starting periodic cleanup of AppRenderFailed data")

	cm.mutex.RLock()
	if cm.cache == nil {
		cm.mutex.RUnlock()
		return
	}

	type target struct{ userID, sourceID string }
	targets := make([]target, 0, 128)

	for userID, userData := range cm.cache.Users {
		for sourceID, sourceData := range userData.Sources {
			if len(sourceData.AppRenderFailed) > 0 {
				targets = append(targets, target{userID: userID, sourceID: sourceID})
			}
		}
	}

	cm.mutex.RUnlock()

	if len(targets) == 0 {
		return
	}

	start := time.Now()
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	count := 0
	failedAppNames := []string{}
	for _, t := range targets {
		if userData, ok := cm.cache.Users[t.userID]; ok {
			if sourceData, ok := userData.Sources[t.sourceID]; ok {
				if len(sourceData.AppRenderFailed) > 0 {
					count += len(sourceData.AppRenderFailed)
					for _, f := range sourceData.AppRenderFailed {
						failedAppNames = append(failedAppNames, fmt.Sprintf("%s_%s_%s", t.userID, t.sourceID, f.AppInfo.AppEntry.Name))
					}
					sourceData.AppRenderFailed = make([]*types.AppRenderFailedData, 0)
				}
			}
		}
	}

	if count > 0 {
		glog.Infof("INFO: [Cleanup] Cleared %d AppRenderFailed entries in %v, apps: %v", count, time.Since(start), failedAppNames)
	}
}

func (cm *CacheManager) HandlerEvent() cache.ResourceEventHandler {
	return cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			return true
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				cm.ListUsers("Add")
			},
			DeleteFunc: func(obj interface{}) {
				cm.ListUsers("Delete")
			},
		},
	}
}

func (cm *CacheManager) ListUsers(opType string) {
	dynamicClient := client.Factory.Client()
	unstructuredUsers, err := dynamicClient.Resource(client.UserGVR).List(context.Background(), v1.ListOptions{})
	if err != nil {
		glog.Errorf("Watchers, get user list error: %v", err)
		return
	}

	glog.Infof("[Cache] User watch handler, type: %s", opType)
	var userList = make([]*client.User, 0)

	for _, unstructuredUser := range unstructuredUsers.Items {
		b, err := unstructuredUser.MarshalJSON()
		if err != nil {
			glog.Errorf("Watchers, marshal list error: %v", err)
			continue
		}
		var user *client.User
		if err := json.Unmarshal(b, &user); err != nil {
			glog.Errorf("unmarshal user error: %v", err)
			continue
		}

		userList = append(userList, user)
	}

	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if len(cm.cache.Users) == 0 {
		glog.V(2).Info("watch user list, cache user not exists")
		return
	}

	for userId, u := range cm.cache.Users {
		if u.UserInfo == nil {
			continue
		}
		var find bool
		var role, id string
		for _, ul := range userList {
			if ul.Name == userId {
				var anno = ul.ObjectMeta.Annotations
				role = anno["bytetrade.io/owner-role"]
				id = anno["bytetrade.io/terminus-name"]
				find = true
				break
			}
		}
		u.UserInfo.Id = id
		u.UserInfo.Role = role
		if find {
			u.UserInfo.Exists = true
		} else {
			u.UserInfo.Exists = false
		}
	}
}

func (cm *CacheManager) RemoveDeletedUser() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	var users []string
	for _, user := range cm.cache.Users {
		if user.UserInfo == nil {
			continue
		}
		if !user.UserInfo.Exists {
			users = append(users, user.UserInfo.Name)
		}
	}

	if len(users) == 0 {
		return
	}

	glog.Infof("[Cache] Remove deleted users: %v", users)
	for _, u := range users {
		delete(cm.cache.Users, u)
	}
}

func (cm *CacheManager) GetCachedData() string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var items []map[string]interface{}

	for un, uv := range cm.cache.Users {
		var user = make(map[string]interface{})
		var ss = make(map[string]interface{})
		for sn, sv := range uv.Sources {
			var apps = make(map[string]interface{})
			apps["latest"] = len(sv.AppInfoLatest)
			apps["pending"] = len(sv.AppInfoLatestPending)
			var pendings []string
			if len(sv.AppInfoLatestPending) < 10 {
				for _, pending := range sv.AppInfoLatestPending {
					pendings = append(pendings, fmt.Sprintf("%s_%s", pending.AppInfo.AppEntry.Name, pending.AppInfo.AppEntry.Version))
				}
			}
			apps["pending_apps"] = pendings

			apps["failed"] = len(sv.AppRenderFailed)
			var failes []string
			if len(sv.AppRenderFailed) < 5 {
				for _, fail := range sv.AppRenderFailed {
					failes = append(failes, fmt.Sprintf("%s_%s", fail.AppInfo.AppEntry.Name, fail.AppInfo.AppEntry.Version))
				}
			}
			apps["failed_apps"] = failes

			apps["history"] = len(sv.AppInfoHistory)
			apps["state"] = len(sv.AppStateLatest)
			var status []string
			if len(sv.AppStateLatest) > 0 {
				for _, state := range sv.AppStateLatest {
					status = append(status, fmt.Sprintf("%s_%s", state.Status.Name, state.Status.State))
				}
			}
			apps["state_apps"] = status

			ss[sn] = apps
		}
		user[un] = ss
		items = append(items, user)
	}

	result, _ := json.Marshal(items)
	return string(result)
}

func (cm *CacheManager) CompareAppStateMsg(userID string, sourceID string, appName string, checker CompareAppStateMsgFunc) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	userData := cm.cache.Users[userID]
	if userData == nil {
		return
	}

	sourceData := userData.Sources[sourceID]
	if sourceData == nil {
		return
	}

	for _, appState := range sourceData.AppStateLatest {
		if appState.Status.Name != appName {
			continue
		}
		checker(appState)
		return
	}
}
