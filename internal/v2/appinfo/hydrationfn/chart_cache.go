package hydrationfn

import (
	"fmt"
	"log"
	"market/internal/v2/types"
)

// updatePendingDataRenderedPackage updates the RenderedPackage field in AppInfoLatestPendingData
func (s *RenderedChartStep) updatePendingDataRenderedPackage(task *HydrationTask, chartDir string) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Lock cache for thread-safe access
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Check if user exists in cache
	userData, exists := task.Cache.Users[task.UserID]
	if !exists {
		log.Printf("User %s not found in cache, skipping RenderedPackage update", task.UserID)
		return nil
	}

	// Find the corresponding pending data and update RenderedPackage
	for i, pendingData := range userData.Sources[task.SourceID].AppInfoLatestPending {
		if s.isTaskForPendingDataRendered(task, pendingData) {
			log.Printf("Updating RenderedPackage for pending data at index %d: %s", i, chartDir)
			userData.Sources[task.SourceID].AppInfoLatestPending[i].RenderedPackage = chartDir
			log.Printf("Successfully updated RenderedPackage for app: %s", task.AppID)
			return nil
		}
	}

	log.Printf("No matching pending data found for task %s, skipping RenderedPackage update", task.ID)
	return nil
}

// isTaskForPendingDataRendered checks if the current task corresponds to the pending data for rendered package
func (s *RenderedChartStep) isTaskForPendingDataRendered(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		return false
	}

	taskAppID := task.AppID

	// Check if the task AppID matches the pending data's RawData
	if pendingData.RawData != nil {
		// Standard checks for data
		// Match by ID, AppID or Name
		if pendingData.RawData.ID == taskAppID ||
			pendingData.RawData.AppID == taskAppID ||
			pendingData.RawData.Name == taskAppID {
			return true
		}
	}

	// Check AppInfo if available
	if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
		// Match by ID, AppID or Name in AppInfo
		if pendingData.AppInfo.AppEntry.ID == taskAppID ||
			pendingData.AppInfo.AppEntry.AppID == taskAppID ||
			pendingData.AppInfo.AppEntry.Name == taskAppID {
			return true
		}
	}

	return false
}

// isAppInLatestList checks if the app exists in the Latest list in cache
func (s *RenderedChartStep) isAppInLatestList(task *HydrationTask) bool {
	if task.Cache == nil {
		log.Printf("Warning: Cache is nil, cannot check Latest list")
		return false
	}

	// Lock cache for thread-safe access
	task.Cache.Mutex.RLock()
	defer task.Cache.Mutex.RUnlock()

	// Check if user exists in cache
	userData, exists := task.Cache.Users[task.UserID]
	if !exists {
		log.Printf("User %s not found in cache", task.UserID)
		return false
	}

	// Check if source exists in user data
	sourceData, exists := userData.Sources[task.SourceID]
	if !exists {
		log.Printf("Source %s not found for user %s", task.SourceID, task.UserID)
		return false
	}

	// Check if app exists in AppInfoLatest list
	for _, latestApp := range sourceData.AppInfoLatest {
		if latestApp == nil {
			continue
		}

		// Compare by app name (primary identifier)
		if s.compareAppIdentifiers(latestApp, task.AppName) {
			log.Printf("Found matching app in Latest list: %s", task.AppName)
			return true
		}
	}

	log.Printf("App %s not found in Latest list", task.AppName)
	return false
}

// compareAppIdentifiers compares app identifiers between latest app data and task
func (s *RenderedChartStep) compareAppIdentifiers(latestApp *types.AppInfoLatestData, taskAppName string) bool {
	if latestApp == nil {
		return false
	}

	// Check RawData first
	if latestApp.RawData != nil {
		if latestApp.RawData.Name == taskAppName ||
			latestApp.RawData.AppID == taskAppName ||
			latestApp.RawData.ID == taskAppName {
			return true
		}
	}

	// Check AppInfo.AppEntry
	if latestApp.AppInfo != nil && latestApp.AppInfo.AppEntry != nil {
		if latestApp.AppInfo.AppEntry.Name == taskAppName ||
			latestApp.AppInfo.AppEntry.AppID == taskAppName ||
			latestApp.AppInfo.AppEntry.ID == taskAppName {
			return true
		}
	}

	// Check AppSimpleInfo
	if latestApp.AppSimpleInfo != nil {
		if latestApp.AppSimpleInfo.AppName == taskAppName ||
			latestApp.AppSimpleInfo.AppID == taskAppName {
			return true
		}
	}

	return false
}
