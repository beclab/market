package appinfo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"market/internal/v2/appservice"
	"market/internal/v2/helper"
	"market/internal/v2/history"
	"market/internal/v2/store"
	"market/internal/v2/task"
	"market/internal/v2/types"
	"market/internal/v2/utils"
	"market/internal/v2/watchers"

	"github.com/golang/glog"
)

// StatusCorrectionChecker manages periodic status checking and correction
type StatusCorrectionChecker struct {
	cacheManager    *CacheManager
	checkInterval   time.Duration
	appServiceHost  string
	appServicePort  string
	stopChan        chan struct{}
	running         atomic.Bool
	lastCheckTime   atomic.Value // time.Time
	checkCount      atomic.Int64
	correctionCount atomic.Int64

	historyModule *history.HistoryModule
	taskModule    *task.TaskModule

	userSystemApps sync.Map
}

// StatusChange represents a detected status change
type StatusChange struct {
	UserID          string                 `json:"user_id"`
	SourceID        string                 `json:"source_id"`
	AppName         string                 `json:"app_name"`
	ChangeType      string                 `json:"change_type"` // "state_change", "app_disappeared", "app_appeared", "state_inconsistency"
	OldState        string                 `json:"old_state"`
	NewState        string                 `json:"new_state"`
	EntranceChanges []EntranceStatusChange `json:"entrance_changes,omitempty"`
	Timestamp       time.Time              `json:"timestamp"`
}

// EntranceStatusChange represents a change in entrance status
type EntranceStatusChange struct {
	EntranceName string `json:"entrance_name"`
	OldState     string `json:"old_state"`
	NewState     string `json:"new_state"`
}

// NewStatusCorrectionChecker creates a new status correction checker
func NewStatusCorrectionChecker(cacheManager *CacheManager) *StatusCorrectionChecker {
	// Get app service configuration from environment variables
	host := os.Getenv("APP_SERVICE_SERVICE_HOST")
	if host == "" {
		host = "localhost" // Default fallback
	}

	port := os.Getenv("APP_SERVICE_SERVICE_PORT")
	if port == "" {
		port = "80" // Default fallback
	}

	return &StatusCorrectionChecker{
		cacheManager:   cacheManager,
		checkInterval:  2 * time.Minute, // Check every 2 minutes
		appServiceHost: host,
		appServicePort: port,
		stopChan:       make(chan struct{}),
	}
}

// StartWithOptions starts with options
func (scc *StatusCorrectionChecker) StartWithOptions() error {
	if !scc.running.CompareAndSwap(false, true) {
		return fmt.Errorf("status correction checker is already running")
	}

	glog.Infof("Starting status correction checker in passive mode (serial pipeline handles processing)")

	return nil
}

// PerformStatusCheckOnce executes one status check cycle, called by Pipeline Phase 4.
// Returns the set of affected user IDs whose data was modified.
func (scc *StatusCorrectionChecker) PerformStatusCheckOnce() map[string]bool {
	if !scc.running.Load() {
		return nil
	}
	return scc.performStatusCheck() // pipeline start
}

// Stop stops the periodic status checking
func (scc *StatusCorrectionChecker) Stop() {
	if !scc.running.CompareAndSwap(true, false) {
		return
	}

	select {
	case <-scc.stopChan:
		// Channel already closed, do nothing
	default:
		close(scc.stopChan)
	}
	glog.Infof("Status correction checker stopped")
}

// IsRunning returns whether the checker is currently running
func (scc *StatusCorrectionChecker) IsRunning() bool {
	return scc.running.Load()
}

// GetStats returns statistics about the checker
func (scc *StatusCorrectionChecker) GetStats() map[string]interface{} {
	var lastCheck time.Time
	if v := scc.lastCheckTime.Load(); v != nil {
		lastCheck = v.(time.Time)
	}

	return map[string]interface{}{
		"is_running":             scc.running.Load(),
		"check_interval":         scc.checkInterval,
		"last_check_time":        lastCheck,
		"check_count":            scc.checkCount.Load(),
		"correction_count":       scc.correctionCount.Load(),
		"app_service_url":        fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", scc.appServiceHost, scc.appServicePort),
		"middleware_service_url": fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/status", scc.appServiceHost, scc.appServicePort),
	}
}

// performStatusCheck performs a single status check cycle
func (scc *StatusCorrectionChecker) performStatusCheck() map[string]bool {
	startTime := time.Now()
	result := make(map[string]bool)

	scc.lastCheckTime.Store(startTime)
	cycle := scc.checkCount.Add(1)

	glog.Infof("Starting status check cycle #%d", cycle)

	latestStatus, err := scc.fetchLatestStatus()
	if err != nil {
		glog.Errorf("[UserChanged] Failed to fetch latest status from app-service: %v", err)
		return result
	}

	users := watchers.GetUsers()
	for _, u := range users {
		var us []*types.AppStateLatestData

		for _, s := range latestStatus {
			if s.Spec.Owner != u.Name {
				continue
			}
			if !s.Spec.IsSysApp {
				continue
			}

			state := types.ConvertSystemAppToAppLatest(s)
			if state == nil {
				continue
			}
			us = append(us, state)
		}

		scc.userSystemApps.Swap(u.Name, us)
	}

	glog.V(3).Infof("[UserChanged] Fetched status for %d applications and middlewares from app-service: %s", len(latestStatus), helper.ParseJson(latestStatus))

	// cachedStatus := scc.getCachedStatus()
	// if len(cachedStatus) == 0 {
	// 	glog.Error("[UserChanged] No cached status found, skipping comparison")
	// 	return result
	// }

	// glog.V(3).Infof("[UserChanged] Found cached status for %d applications and middlewares: %s", len(cachedStatus), helper.ParseJson(cachedStatus))

	// changes := scc.compareStatus(latestStatus, cachedStatus)

	// glog.V(2).Infof("[UserChanged] Found cached status, changed: %+v, app: %d, middlewares: %d", changes, len(latestStatus), len(cachedStatus))

	// if len(changes) > 0 {
	// 	glog.V(2).Infof("[UserChanged] Detected %d status changes, applying corrections, changes: %s", len(changes), helper.ParseJson(changes))
	// 	scc.applyCorrections(changes, latestStatus)

	// 	// Apply UserInfo changes and collect affected users.
	// 	// Hash calculation and ForceSync are deferred to Pipeline Phase 5.
	// 	changesByUser := make(map[string]*StatusChange)
	// 	for _, change := range changes {
	// 		changesByUser[change.UserID] = &change
	// 	}
	// 	for userID, cs := range changesByUser {
	// 		userData := scc.cacheManager.GetUserData(userID)
	// 		if userData == nil {
	// 			glog.Warningf("StatusCorrectionChecker: userData not found for user %s", userID)
	// 			continue
	// 		}

	// 		if userData.UserInfo != nil {
	// 			glog.V(2).Infof("[UserChanged] userId: %s, appName: %s, changeType: %s, newState: %s", cs.UserID, cs.AppName, cs.ChangeType, cs.NewState)
	// 			if cs.AppName == "olares-app" && cs.ChangeType == "app_disappeared" && cs.NewState == "unknown" {
	// 				userData.UserInfo.Exists = false
	// 			} else if cs.AppName == "olares-app" && cs.ChangeType == "app_appeared" && cs.NewState == "running" {
	// 				userData.UserInfo.Exists = true
	// 			}
	// 		} else {
	// 			glog.V(2).Infof("[UserChanged] userId: %s, userInfo is null", cs.UserID)
	// 		}

	// 		result[userID] = true
	// 	}

	// 	scc.correctionCount.Add(int64(len(changes)))
	// } else {
	// 	glog.V(3).Info("No status changes detected")
	// }

	// scc.checkAndCorrectTaskStatuses(latestStatus)

	// glog.V(2).Infof("Status check cycle #%d completed in %v", cycle, time.Since(startTime))
	return result
}

// fetchLatestStatus fetches the latest status from app-service
func (scc *StatusCorrectionChecker) fetchLatestStatus() ([]*types.AppServiceResponse, error) {
	// Fetch apps status
	appsStatus, err := scc.fetchLatestAppsStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch apps status: %v", err)
	}

	// Fetch middlewares status
	middlewaresStatus, err := scc.fetchLatestMiddlewaresStatus()
	if err != nil {
		glog.Warningf("Failed to fetch middlewares status: %v", err)
		return appsStatus, nil
	}

	var printf []interface{}
	for _, md := range appsStatus {
		if md.Spec.Name != "olares-app" {
			printf = append(printf, md)
		}
	}

	glog.V(3).Infof("[SCC] fetch latest appStatus: %s", helper.ParseJson(printf))

	// Combine apps and middlewares status
	// Convert middlewares to AppServiceResponse format and merge with apps
	allStatus := make([]*types.AppServiceResponse, 0, len(appsStatus)+len(middlewaresStatus))

	// Add apps status
	allStatus = append(allStatus, appsStatus...)

	// Add middlewares status (already converted to AppServiceResponse format)
	allStatus = append(allStatus, middlewaresStatus...)

	glog.Infof("[SCC] Combined status: %d apps + %d middlewares = %d total",
		len(appsStatus), len(middlewaresStatus), len(allStatus))

	return allStatus, nil
}

// fetchLatestAppsStatus fetches the latest apps status from app-service
func (scc *StatusCorrectionChecker) fetchLatestAppsStatus() ([]*types.AppServiceResponse, error) {
	c, err := appservice.NewClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	apps, err := c.GetAllApps(ctx)
	if err != nil {
		return nil, err
	}

	return apps, nil
}

// fetchLatestMiddlewaresStatus fetches the latest middlewares status from app-service
func (scc *StatusCorrectionChecker) fetchLatestMiddlewaresStatus() ([]*types.AppServiceResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := appservice.NewClient()
	if err != nil {
		return nil, err
	}

	resp, err := c.GetMiddlewares(ctx)
	if err != nil {
		return nil, err
	}

	if len(resp) == 0 {
		return nil, nil
	}

	var convertedResponses []*types.AppServiceResponse

	for _, r := range resp {
		var converted = &types.AppServiceResponse{
			Metadata: &types.AppServiceResponseMetadata{},
			Spec: &types.AppServiceResponseSpec{
				Settings: &types.AppStateLatestDataSettings{},
			},
			Status: &types.AppStateLatestDataStatus{},
		}
		converted.Metadata.Name = r.Metadata.Name

		converted.Spec.Name = r.Metadata.Name // + todo
		// converted.Spec.RawAppName = r.Metadata.Name // + todo
		converted.Spec.AppID = helper.Md5(r.Metadata.Name)
		converted.Spec.Owner = r.User
		converted.Spec.Title = r.Title
		converted.Spec.Source = "middleware" // + todo
		converted.Spec.State = r.ResourceStatus
		converted.Spec.UpdateTime = r.UpdateTime
		converted.Spec.StatusTime = r.UpdateTime
		converted.Spec.LastTransitionTime = r.UpdateTime

		converted.Spec.Settings.Title = r.Title
		converted.Spec.Settings.Source = "" // + todo
		converted.Spec.Settings.MarketSource = r.MarketSource
		converted.Spec.Settings.Version = r.Version

		converted.Status.State = r.ResourceStatus
		converted.Status.UpdateTime = r.UpdateTime
		converted.Status.StatusTime = r.UpdateTime
		converted.Status.LastTransitionTime = r.UpdateTime

		convertedResponses = append(convertedResponses, converted)
	}

	return convertedResponses, nil
}

func (scc *StatusCorrectionChecker) GetSystemAppStatus(userId string) []*types.AppStateLatestData {
	val, ok := scc.userSystemApps.Load(userId)
	if !ok {
		return nil
	}

	var res []*types.AppStateLatestData
	var v = val.([]*types.AppStateLatestData)
	if len(v) == 0 {
		return nil
	}

	for _, item := range v {
		res = append(res, item)
	}

	return res
}

func (scc *StatusCorrectionChecker) ReconcileAppStatuses() {
	// todo
}

func (scc *StatusCorrectionChecker) ReconcileAppStatusesStartup() {
	glog.Info("Reconcile, startup to sync states data")
	apps, err := scc.fetchLatestStatus()
	if err != nil {
		glog.Errorf("Reconcile, app-service statuses error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbAppStates, err := store.GetExistsAppStateRecords(ctx)
	if err != nil {
		glog.Errorf("Reconcile, query pg error: %v", err)
		return
	}

	if len(dbAppStates) == 0 {
		return
	}

	glog.Infof("Reconcile, sync states data, appservice: %d, pg: %d", len(apps), len(dbAppStates))

	var in = make(map[int64]*types.AppServiceResponse)
	var delUas []int64

	// + Is it possible for a record to be missing in the DB but still exist in appservice?
	// +
	// + 1. Present in both DB and appservice
	// +    ---> Update the state; use appservice to overwrite DB
	// +
	// + 2. Present in DB but missing in appservice
	// +    2.1 ua exists, uas exists ---> Delete uas
	// +    2.2 ua exists, uas does not exist ---> No action needed
	// +    2.3 ua does not exist, uas exists ---> Delete uas
	// +
	// + 3. Missing in DB but present in appservice
	// +    ---> Should this be repaired in the pipeline?
	// +        (This may happen if an app was installed, then the system was upgraded,
	// +        and during the next sync the app had already been removed from the marketplace.
	// +        However, there should still be a record in the cloud, so it should be possible
	// +        to sync it back to the local system.)
	// +    ---> Therefore, this case can be repaired during the pipeline stage.
	for _, dbAppState := range dbAppStates {
		// Sys-app rows are reconciled by a separate code path
		// (scc.userSystemApps); the inner loop below already
		// `continue`s over IsSysApp entries from app-service, so a
		// sys-app DB row could never find a match and would
		// otherwise land in delUas and be deleted. Skip them here.
		if dbAppState.IsSysApp {
			continue
		}

		matched := false
		for _, app := range apps {
			if app.Spec.IsSysApp {
				continue
			}

			if dbAppState.AppUserId == app.Spec.Owner &&
				dbAppState.AppId == app.Spec.AppID &&
				dbAppState.AppSourceId == app.Spec.Settings.MarketSource {
				in[dbAppState.UserAppStateId] = app
				matched = true
				break
			}
		}
		if !matched {
			delUas = append(delUas, dbAppState.UserAppStateId)
		}
	}

	glog.Infof("Reconcile, sync pg, update: %d, del: %d", len(in), len(delUas))

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer writeCancel()
	if err := store.UpdateAppStates(writeCtx, in, delUas); err != nil {
		glog.Errorf("Reconcile, update pg error: %v", err)
	}
}

// getExistingEntranceInvisibleMap collects cached invisible flags for the specified app.
// This is similar to DataWatcherState.getExistingEntranceInvisibleMap but for StatusCorrectionChecker.
func (scc *StatusCorrectionChecker) getExistingEntranceInvisibleMap(userID, appName string) map[string]bool {
	result := make(map[string]bool)
	if scc == nil || scc.cacheManager == nil || userID == "" || appName == "" {
		return result
	}

	userData := scc.cacheManager.GetUserData(userID)
	if userData == nil {
		return result
	}

	for _, srcData := range userData.Sources {
		if srcData == nil {
			continue
		}
		for _, appState := range srcData.AppStateLatest {
			if appState == nil || appState.Status.Name != appName {
				continue
			}

			for _, entrance := range appState.Status.EntranceStatuses {
				result[entrance.Name] = entrance.Invisible
			}
			return result
		}
	}

	return result
}

// compareStatus compares latest status with cached status and returns changes
func (scc *StatusCorrectionChecker) compareStatus(latestStatus []*utils.AppServiceResponse, cachedStatus map[string]*types.AppStateLatestData) []StatusChange {
	var changes []StatusChange

	// Create maps for easier lookup
	type cachedEntry struct {
		sourceID   string
		appState   *types.AppStateLatestData
		statusTime time.Time
	}

	latestApps := make(map[string]*utils.AppServiceResponse)
	for i := range latestStatus {
		app := latestStatus[i]
		key := fmt.Sprintf("%s:%s", app.Spec.Owner, app.Spec.Name)
		latestApps[key] = app
	}

	// Group cache by user+appName, keep every source entry for per-source fix and pruning
	cachedAppsByUserAndName := make(map[string][]cachedEntry)
	for key, cached := range cachedStatus {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		userID, sourceID, appName := parts[0], parts[1], parts[2]

		var statusTime time.Time
		if cached != nil && cached.Status.StatusTime != "" {
			if ts, err := time.Parse("2006-01-02T15:04:05.000000000Z", cached.Status.StatusTime); err == nil {
				statusTime = ts
			}
		}

		k := fmt.Sprintf("%s:%s", userID, appName)
		cachedAppsByUserAndName[k] = append(cachedAppsByUserAndName[k], cachedEntry{
			sourceID:   sourceID,
			appState:   cached,
			statusTime: statusTime,
		})
	}

	// 1) Detect new apps (present in latest, absent in cache)
	for _, app := range latestStatus {
		userID := app.Spec.Owner
		appName := app.Spec.Name
		key := userID + ":" + appName

		if _, exists := cachedAppsByUserAndName[key]; !exists {
			change := StatusChange{
				UserID:     userID,
				SourceID:   "",
				AppName:    appName,
				ChangeType: "app_appeared",
				OldState:   "unknown",
				NewState:   app.Status.State,
				Timestamp:  time.Now(),
			}
			changes = append(changes, change)
			glog.Infof("New app appeared: %s (user: %s, state: %s)", appName, userID, app.Status.State)
		}
	}

	// 2) Detect disappeared apps (present in cache, absent in latest) per source
	for key, entries := range cachedAppsByUserAndName {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		userID, appName := parts[0], parts[1]
		_, foundInLatest := latestApps[key]

		if !foundInLatest {
			for _, entry := range entries {
				if entry.appState == nil {
					continue
				}
				change := StatusChange{
					UserID:     userID,
					SourceID:   entry.sourceID,
					AppName:    appName,
					ChangeType: "app_disappeared",
					OldState:   entry.appState.Status.State,
					NewState:   "unknown",
					Timestamp:  time.Now(),
				}
				changes = append(changes, change)

				glog.Infof("App disappeared: %s (user: %s, source: %s, last state: %s)",
					appName, userID, entry.sourceID, entry.appState.Status.State)
			}
		}
	}

	// 3) Detect state changes for every source entry that matches latest app
	for key, latest := range latestApps {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		userID, appName := parts[0], parts[1]

		entries := cachedAppsByUserAndName[key]
		if len(entries) == 0 {
			continue // already handled as new app in step 1
		}

		for _, entry := range entries {
			if entry.appState == nil {
				continue
			}

			stateChanged := entry.appState.Status.State != latest.Status.State
			entranceChanges := scc.compareEntranceStatuses(entry.appState.Status.EntranceStatuses, latest.Status.EntranceStatuses)

			if stateChanged || len(entranceChanges) > 0 {
				change := StatusChange{
					UserID:          userID,
					SourceID:        entry.sourceID,
					AppName:         appName,
					ChangeType:      "state_change",
					OldState:        entry.appState.Status.State,
					NewState:        latest.Status.State,
					EntranceChanges: entranceChanges,
					Timestamp:       time.Now(),
				}
				changes = append(changes, change)

				glog.Infof("Status change detected for app %s (user: %s, source: %s): %s -> %s",
					appName, userID, entry.sourceID, entry.appState.Status.State, latest.Status.State)

				if len(entranceChanges) > 0 {
					for _, entranceChange := range entranceChanges {
						glog.Infof("  Entrance %s: %s -> %s",
							entranceChange.EntranceName, entranceChange.OldState, entranceChange.NewState)
					}
				}
			}
		}
	}

	return changes
}

// compareEntranceStatuses compares entrance statuses and returns changes
func (scc *StatusCorrectionChecker) compareEntranceStatuses(cachedEntrances, latestEntrances []types.AppStateLatestDataEntrances) []EntranceStatusChange {

	var changes []EntranceStatusChange

	// Create maps for easier lookup
	cachedMap := make(map[string]string)
	for _, entrance := range cachedEntrances {
		cachedMap[entrance.Name] = entrance.State
	}

	latestMap := make(map[string]string)
	for _, entrance := range latestEntrances {
		latestMap[entrance.Name] = entrance.State
	}

	// Check for changes in existing entrances
	for name, latestState := range latestMap {
		if cachedState, exists := cachedMap[name]; exists && cachedState != latestState {
			changes = append(changes, EntranceStatusChange{
				EntranceName: name,
				OldState:     cachedState,
				NewState:     latestState,
			})
		}
	}

	return changes
}

// applyCorrections applies the detected status changes to the cache
func (scc *StatusCorrectionChecker) applyCorrections(changes []StatusChange, latestStatus []*utils.AppServiceResponse) {
	for _, change := range changes {
		switch change.ChangeType {
		case "app_disappeared":
			glog.V(2).Infof("App disappeared detected: %s (user: %s, source: %s, last state: %s) - fixing in cache",
				change.AppName, change.UserID, change.SourceID, change.OldState)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App disappeared: %s (user: %s, source: %s, last state: %s)", change.AppName, change.UserID, change.SourceID, change.OldState),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Errorf("Failed to store app disappeared history record: %v", err)
				}
			}
			if err := scc.cacheManager.RemoveAppStateData(change.UserID, change.SourceID, change.AppName); err != nil {
				glog.Errorf("Failed to remove disappeared app %s from cache (user: %s, source: %s): %v",
					change.AppName, change.UserID, change.SourceID, err)
			} else {
				glog.V(2).Infof("Successfully removed disappeared app %s from cache (user: %s, source: %s)",
					change.AppName, change.UserID, change.SourceID)
			}
			if scc.taskModule != nil {
				if err := scc.taskModule.UninstallTaskSucceed("", change.AppName, change.UserID); err != nil {
					glog.Errorf("Failed to mark uninstall task as succeeded for app %s (user: %s): %v", change.AppName, change.UserID, err)
				}
			}

		case "app_appeared":
			var appToUpdate *utils.AppServiceResponse
			for _, app := range latestStatus {
				if app.Spec.Owner == change.UserID && app.Spec.Name == change.AppName {
					appToUpdate = app
					break
				}
			}
			if appToUpdate == nil {
				glog.V(3).Infof("Could not find appeared app %s for user %s in latest status", change.AppName, change.UserID)
				continue
			}
			glog.V(2).Infof("New app appeared detected: %s (user: %s, state: %s) - fixing in cache",
				change.AppName, change.UserID, appToUpdate.Status.State)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App appeared: %s (user: %s, state: %s)", change.AppName, change.UserID, appToUpdate.Status.State),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Errorf("Failed to store app appeared history record: %v", err)
				}
			}
			// Dynamically determine sourceID
			sourceID := ""
			// 1. Check cache
			if scc.cacheManager != nil {
				userData := scc.cacheManager.GetUserData(change.UserID)
				if userData != nil {
					for srcID, srcData := range userData.Sources {
						if srcData == nil || srcData.AppStateLatest == nil {
							continue
						}
						for _, appState := range srcData.AppStateLatest {
							if appState != nil && appState.Status.Name == change.AppName {
								sourceID = srcID
								break
							}
						}
						if sourceID != "" {
							break
						}
					}
				}
			}

			if scc.taskModule != nil {
				if err := scc.taskModule.InstallTaskSucceed("", change.AppName, change.UserID); err != nil {
					glog.Errorf("Failed to mark install task as succeeded for app %s (user: %s): %v", change.AppName, change.UserID, err)
				}
			}

			appStateData, sourceID := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID) // app_appeared
			if appStateData == nil {
				glog.V(3).Infof("Failed to create app state data for appeared app %s (user: %s)", change.AppName, change.UserID)
				continue
			}
			stateData := scc.createStateDataFromAppStateData(appStateData) // app_appeared
			if err := scc.cacheManager.SetAppData(change.UserID, sourceID, AppStateLatest, stateData, "SCC_app_appeared"); err != nil {
				glog.Errorf("Failed to add appeared app %s to cache (user: %s, source: %s): %v",
					change.AppName, change.UserID, sourceID, err)
			} else {
				glog.V(2).Infof("Successfully added appeared app %s to cache (user: %s, source: %s)",
					change.AppName, change.UserID, sourceID)
			}

		case "state_change":
			var appToUpdate *utils.AppServiceResponse
			for _, app := range latestStatus {
				if app.Spec.Owner == change.UserID && app.Spec.Name == change.AppName {
					appToUpdate = app
					break
				}
			}
			if appToUpdate == nil {
				glog.V(3).Infof("Could not find app %s for user %s in latest status", change.AppName, change.UserID)
				continue
			}
			glog.V(2).Infof("State change detected for app %s (user: %s, source: %s): %s -> %s - fixing in cache",
				change.AppName, change.UserID, change.SourceID, change.OldState, change.NewState)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App state changed: %s (user: %s, source: %s): %s -> %s", change.AppName, change.UserID, change.SourceID, change.OldState, change.NewState),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Errorf("Failed to store state change history record: %v", err)
				}
			}

			appStateData, sourceID := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID) // state_change
			if appStateData == nil {
				glog.V(3).Infof("Failed to create app state data for app %s (user: %s)", change.AppName, change.UserID)
				continue
			}
			stateData := scc.createStateDataFromAppStateData(appStateData) // state_change
			if err := scc.cacheManager.SetAppData(change.UserID, sourceID, AppStateLatest, stateData, "SCC_state_change"); err != nil {
				glog.Errorf("Failed to update cache with corrected status for app %s (user: %s, source: %s): %v",
					change.AppName, change.UserID, sourceID, err)
			} else {
				glog.V(2).Infof("Successfully updated cache with corrected status for app %s (user: %s, source: %s)",
					change.AppName, change.UserID, sourceID)
			}

		case "duplicate_prune":
			glog.V(2).Infof("Pruning duplicate app %s (user: %s, source: %s), kept latest elsewhere; last state: %s",
				change.AppName, change.UserID, change.SourceID, change.OldState)
			if scc.cacheManager != nil {
				if err := scc.cacheManager.RemoveAppStateData(change.UserID, change.SourceID, change.AppName); err != nil {
					glog.Errorf("Failed to remove AppStateLatest for duplicate app %s (user: %s, source: %s): %v",
						change.AppName, change.UserID, change.SourceID, err)
				}
				if err := scc.cacheManager.RemoveAppInfoLatestData(change.UserID, change.SourceID, change.AppName); err != nil {
					glog.Errorf("Failed to remove AppInfoLatest for duplicate app %s (user: %s, source: %s): %v",
						change.AppName, change.UserID, change.SourceID, err)
				}
			}

		case "state_inconsistency":
			var appToUpdate *utils.AppServiceResponse
			for _, app := range latestStatus {
				if app.Spec.Owner == change.UserID && app.Spec.Name == change.AppName {
					appToUpdate = app
					break
				}
			}
			if appToUpdate == nil {
				glog.V(3).Infof("Could not find app %s for user %s in latest status", change.AppName, change.UserID)
				continue
			}
			glog.V(2).Infof("State inconsistency detected for app %s (user: %s, source: %s): app state is %s but all entrances are running - fixing in cache",
				change.AppName, change.UserID, change.SourceID, appToUpdate.Status.State)
			if scc.historyModule != nil {
				record := &history.HistoryRecord{
					Type:     history.TypeSystem,
					Message:  fmt.Sprintf("App state inconsistency: %s (user: %s, source: %s), app state: %s, all entrances are running", change.AppName, change.UserID, change.SourceID, appToUpdate.Status.State),
					Time:     time.Now().Unix(),
					App:      change.AppName,
					Account:  change.UserID,
					Extended: "",
				}
				if err := scc.historyModule.StoreRecord(record); err != nil {
					glog.Errorf("Failed to store state inconsistency history record: %v", err)
				}
			}

			appStateData, sourceID := scc.createAppStateDataFromResponse(*appToUpdate, change.UserID) // state_inconsistency
			if appStateData == nil {
				glog.V(3).Infof("Failed to create app state data for app %s (user: %s)", change.AppName, change.UserID)
				continue
			}
			appStateData.Status.State = "running"
			stateData := scc.createStateDataFromAppStateData(appStateData) // state_inconsistency
			if err := scc.cacheManager.SetAppData(change.UserID, sourceID, AppStateLatest, stateData, "SCC_state_inconsistency"); err != nil {
				glog.Errorf("Failed to update cache with corrected state for inconsistent app %s (user: %s, source: %s): %v",
					change.AppName, change.UserID, sourceID, err)
			} else {
				glog.V(2).Infof("Successfully corrected inconsistent state for app %s (user: %s, source: %s): %s -> running",
					change.AppName, change.UserID, sourceID, change.OldState)
			}

		default:
			glog.V(3).Infof("Unknown change type: %s for app %s (user: %s)", change.ChangeType, change.AppName, change.UserID)
		}
	}
}

// createAppStateDataFromResponse creates AppStateLatestData from AppServiceResponse
func (scc *StatusCorrectionChecker) createAppStateDataFromResponse(app utils.AppServiceResponse, userID string) (*types.AppStateLatestData, string) {
	// Get existing invisible flags from cache to preserve them when spec.entrances doesn't have the value
	existingInvisible := scc.getExistingEntranceInvisibleMap(userID, app.Spec.Name)

	// Create entrance statuses
	entranceStatuses := make([]types.AppStateLatestDataEntrances, len(app.Status.EntranceStatuses))

	for i, entrance := range app.Status.EntranceStatuses {
		// Default to false, but will be updated from spec.entrances or cache
		invisible := false

		// Try to get invisible flag from spec.entrances first
		foundInSpec := false
		for _, specEntrance := range app.Spec.EntranceStatuses {
			if specEntrance.Name == entrance.Name {
				invisible = specEntrance.Invisible
				foundInSpec = true
				break
			}
		}

		// If not found in spec.entrances, try to get from cache
		if !foundInSpec {
			if cachedInvisible, ok := existingInvisible[entrance.Name]; ok {
				invisible = cachedInvisible
				glog.Infof("StatusCorrectionChecker: Using cached invisible=%t for entrance %s (app=%s, user=%s) - not found in spec.entrances",
					invisible, entrance.Name, app.Spec.Name, userID)
			} else {
				glog.Infof("StatusCorrectionChecker: Using default invisible=false for entrance %s (app=%s, user=%s) - not found in spec.entrances or cache",
					entrance.Name, app.Spec.Name, userID)
			}
		}

		entranceStatuses[i] = types.AppStateLatestDataEntrances{
			ID:         entrance.ID,
			Name:       entrance.Name,
			State:      entrance.State,
			StatusTime: entrance.StatusTime,
			Reason:     entrance.Reason,
			Url:        entrance.Url,
			Invisible:  invisible,
		}
	}

	// Get version from download record
	version := ""
	source := ""
	if userID != "" && app.Spec.Name != "" {
		if versionFromRecord, sourceFromRecord, err := task.LookupAppInfoLastInstalled(userID, app.Spec.Name); err == nil && versionFromRecord != "" {
			version = versionFromRecord
			source = sourceFromRecord
		}
	}

	// Try to get rawAppName and SharedEntrances from cache if available
	rawAppName := ""
	title := app.Spec.Title
	var sharedEntrances []types.AppStateLatestDataEntrances

	if scc.cacheManager != nil && userID != "" && app.Spec.Name != "" {
		userData := scc.cacheManager.GetUserData(userID)
		if userData != nil {
			for _, sourceData := range userData.Sources {
				for _, cachedAppState := range sourceData.AppStateLatest {
					if cachedAppState != nil && cachedAppState.Status.Name == app.Spec.Name {
						if rawAppName == "" {
							rawAppName = cachedAppState.Status.RawAppName
						}
						// Preserve title from cache if new title is empty
						if title == "" && cachedAppState.Status.Title != "" {
							title = cachedAppState.Status.Title
						}
						// Preserve SharedEntrances if cache有数据
						if len(sharedEntrances) == 0 && len(cachedAppState.Status.SharedEntrances) > 0 {
							sharedEntrances = cachedAppState.Status.SharedEntrances
						}
						if rawAppName != "" && title != "" && len(sharedEntrances) > 0 {
							break
						}
					}
				}
				if rawAppName != "" && title != "" && len(sharedEntrances) > 0 {
					break
				}
			}
		}
	}

	// Prefer upstream SharedEntrances when后端状态里已经返回该字段
	if len(app.Status.SharedEntrances) > 0 {
		sharedEntrances = app.Status.SharedEntrances
	}

	var status = &types.AppStateLatestDataSpec{}
	status.Name = app.Spec.Name
	status.RawAppName = rawAppName
	status.Title = title
	status.State = app.Status.State
	status.UpdateTime = app.Status.UpdateTime
	status.StatusTime = app.Status.StatusTime
	status.LastTransitionTime = app.Status.LastTransitionTime
	status.Progress = ""
	status.OpType = "" // AppServiceResponse doesn't have opType, will be set from NATS messages
	status.EntranceStatuses = entranceStatuses
	status.SharedEntrances = sharedEntrances

	return &types.AppStateLatestData{
		Type:    types.AppStateLatest,
		Version: version,
		Status:  status,
	}, source
}

// createStateDataFromAppStateData creates a state data from AppStateLatestData
func (scc *StatusCorrectionChecker) createStateDataFromAppStateData(appStateData *types.AppStateLatestData) map[string]interface{} {
	stateData := map[string]interface{}{
		"name":               appStateData.Status.Name,
		"state":              appStateData.Status.State,
		"updateTime":         appStateData.Status.UpdateTime,
		"statusTime":         appStateData.Status.StatusTime,
		"lastTransitionTime": appStateData.Status.LastTransitionTime,
		"progress":           appStateData.Status.Progress,
	}

	// Get rawAppName from AppStateLatestData
	rawAppName := appStateData.Status.RawAppName
	// Add rawAppName to stateData
	stateData["rawAppName"] = rawAppName

	// Get title from AppStateLatestData
	title := appStateData.Status.Title
	// Add title to stateData
	stateData["title"] = title

	// Get opType from AppStateLatestData
	opType := appStateData.Status.OpType
	// Add opType to stateData if not empty
	if opType != "" {
		stateData["opType"] = opType
	}

	// Convert entrance statuses to interface{} slice
	entranceStatuses := make([]interface{}, len(appStateData.Status.EntranceStatuses))
	for i, entrance := range appStateData.Status.EntranceStatuses {
		entranceStatuses[i] = map[string]interface{}{
			"id":         entrance.ID,
			"name":       entrance.Name,
			"state":      entrance.State,
			"statusTime": entrance.StatusTime,
			"reason":     entrance.Reason,
			"url":        entrance.Url,
			"invisible":  entrance.Invisible,
		}
	}
	stateData["entranceStatuses"] = entranceStatuses

	// Convert SharedEntrances to interface{} slice
	if len(appStateData.Status.SharedEntrances) > 0 {
		sharedEntrances := make([]interface{}, len(appStateData.Status.SharedEntrances))
		for i, entrance := range appStateData.Status.SharedEntrances {
			sharedEntrances[i] = map[string]interface{}{
				"name":            entrance.Name,
				"host":            entrance.Host,
				"port":            entrance.Port,
				"icon":            entrance.Icon,
				"title":           entrance.Title,
				"authLevel":       entrance.AuthLevel,
				"invisible":       entrance.Invisible,
				"url":             entrance.Url,
				"openMethod":      entrance.OpenMethod,
				"windowPushState": entrance.WindowPushState,
				"skip":            entrance.Skip,
			}
		}
		stateData["sharedEntrances"] = sharedEntrances
	}

	return stateData
}

// SetHistoryModule sets the history module for status correction checker
func (scc *StatusCorrectionChecker) SetHistoryModule(hm *history.HistoryModule) {
	scc.historyModule = hm
}

// SetTaskModule sets the task module for status correction checker
func (scc *StatusCorrectionChecker) SetTaskModule(tm *task.TaskModule) {
	scc.taskModule = tm
}

// checkAndCorrectTaskStatuses checks running tasks and corrects their status based on actual app state
func (scc *StatusCorrectionChecker) checkAndCorrectTaskStatuses(latestStatus []*utils.AppServiceResponse) {
	if scc.taskModule == nil {
		return
	}

	// Get all running tasks
	runningTasks := scc.taskModule.GetRunningTasks()
	if len(runningTasks) == 0 {
		return
	}

	glog.Infof("[SCC] Checking %d running tasks for status correction", len(runningTasks))

	// Create a map of app statuses for quick lookup: user:appName -> app status
	appStatusMap := make(map[string]*utils.AppServiceResponse)
	for i := range latestStatus {
		app := latestStatus[i]
		key := fmt.Sprintf("%s:%s", app.Spec.Owner, app.Spec.Name)
		appStatusMap[key] = app
	}

	correctedCount := 0
	for _, runningTask := range runningTasks {
		// Only check tasks that have been running for at least 30 seconds to avoid false positives
		if runningTask.StartedAt == nil {
			continue
		}
		if time.Since(*runningTask.StartedAt) < 30*time.Second {
			continue
		}

		appKey := fmt.Sprintf("%s:%s", runningTask.User, runningTask.AppName)
		appStatus, exists := appStatusMap[appKey]

		switch runningTask.Type {
		case task.InstallApp, task.CloneApp:
			// For install/clone tasks: if app exists and is running, mark task as completed
			if exists && appStatus != nil && appStatus.Status.State == "running" {
				taskTypeStr := "Install"
				if runningTask.Type == task.CloneApp {
					taskTypeStr = "Clone"
				}
				glog.Infof("[SCC] Task status correction: %s task %s for app %s (user: %s) should be completed - app is running",
					taskTypeStr, runningTask.ID, runningTask.AppName, runningTask.User)
				if err := scc.taskModule.InstallTaskSucceed(runningTask.OpID, runningTask.AppName, runningTask.User); err != nil {
					glog.Warningf("[SCC] Failed to mark %s task as succeeded: %v", taskTypeStr, err)
				} else {
					correctedCount++
					glog.Infof("[SCC] Successfully corrected %s task status: %s", taskTypeStr, runningTask.ID)
				}
			}

		case task.UninstallApp:
			// For uninstall tasks: if app doesn't exist, mark task as completed
			if !exists {
				glog.Infof("[SCC] Task status correction: Uninstall task %s for app %s (user: %s) should be completed - app no longer exists",
					runningTask.ID, runningTask.AppName, runningTask.User)
				if err := scc.taskModule.UninstallTaskSucceed(runningTask.OpID, runningTask.AppName, runningTask.User); err != nil {
					glog.Warningf("[SCC] Failed to mark uninstall task as succeeded: %v", err)
				} else {
					correctedCount++
					glog.Infof("[SCC] Successfully corrected uninstall task status: %s", runningTask.ID)
				}
			}

		case task.CancelAppInstall:
			// For cancel install tasks: if app doesn't exist, mark task as completed
			if !exists {
				glog.Infof("[SCC] Task status correction: Cancel install task %s for app %s (user: %s) should be completed - app no longer exists",
					runningTask.ID, runningTask.AppName, runningTask.User)
				if err := scc.taskModule.CancelInstallTaskSucceed(runningTask.OpID, runningTask.AppName, runningTask.User); err != nil {
					glog.Warningf("[SCC] Failed to mark cancel install task as succeeded: %v", err)
				} else {
					correctedCount++
					glog.Infof("[SCC] Successfully corrected cancel install task status: %s", runningTask.ID)
				}
			}

		case task.UpgradeApp:
			// For upgrade tasks: if app exists and is running, mark task as completed
			// Note: TaskModule doesn't have UpgradeTaskSucceed method, so upgrade tasks
			// are typically completed through their normal execution flow.
			// We log it for monitoring but don't auto-correct to avoid conflicts.
			if exists && appStatus != nil && appStatus.Status.State == "running" {
				glog.Infof("[SCC] Task status correction: Upgrade task %s for app %s (user: %s) appears completed - app is running (not auto-correcting)",
					runningTask.ID, runningTask.AppName, runningTask.User)
			}
		}
	}

	if correctedCount > 0 {
		glog.Infof("[SCC] Task status correction completed: corrected %d task(s)", correctedCount)
		scc.correctionCount.Add(int64(correctedCount))
	}
}
