package utils

import (
	"log"
	"time"

	"market/internal/v2/types"
)

// DataSenderInterface defines the interface for sending app info updates
type DataSenderInterface interface {
	SendAppInfoUpdate(update types.AppInfoUpdate) error
	IsConnected() bool
	Close()
}

// StateMonitor handles app state monitoring and change detection
type StateMonitor struct {
	dataSender DataSenderInterface
}

// NewStateMonitor creates a new StateMonitor instance
func NewStateMonitor(dataSender DataSenderInterface) *StateMonitor {
	return &StateMonitor{
		dataSender: dataSender,
	}
}

// CheckAndNotifyStateChange checks if app state has changed and sends notification if needed
func (sm *StateMonitor) CheckAndNotifyStateChange(
	userID, sourceID, appName string,
	newStateData *types.AppStateLatestData,
	existingStateData []*types.AppStateLatestData,
	appInfoLatestData []*types.AppInfoLatestData,
) error {
	// Check if state has changed
	hasChanged, changeReason := sm.hasStateChanged(appName, newStateData, existingStateData)

	if !hasChanged {
		// Only log at debug level for no changes to reduce log noise
		log.Printf("No state change detected for app %s (user=%s, source=%s), reason: %s",
			appName, userID, sourceID, changeReason)
		return nil
	}

	log.Printf("State change detected for app %s (user=%s, source=%s), reason: %s",
		appName, userID, sourceID, changeReason)

	// Find corresponding AppInfoLatestData
	var appInfoLatest *types.AppInfoLatestData
	for _, appInfo := range appInfoLatestData {
		if appInfo != nil && appInfo.RawData != nil && appInfo.RawData.Name == appName {
			appInfoLatest = appInfo
			break
		}
	}

	// Create and send update
	update := types.AppInfoUpdate{
		AppStateLatest: newStateData,
		AppInfoLatest:  appInfoLatest,
		Timestamp:      time.Now().Unix(),
		User:           userID,
		AppName:        appName,
		NotifyType:     "app_state_change",
		Source:         sourceID,
	}

	return sm.dataSender.SendAppInfoUpdate(update)
}

// hasStateChanged checks if the app state has changed compared to existing state
func (sm *StateMonitor) hasStateChanged(
	appName string,
	newStateData *types.AppStateLatestData,
	existingStateData []*types.AppStateLatestData,
) (bool, string) {
	if newStateData == nil {
		return false, "new state data is nil"
	}

	// Find existing state for this specific app by name
	var existingState *types.AppStateLatestData
	for _, state := range existingStateData {
		if state != nil && state.Status.Name == appName {
			existingState = state
			break
		}
	}

	// If no existing state found, this is a new app state
	if existingState == nil {
		return true, "new app state (no existing state found)"
	}

	// Compare main state
	if newStateData.Status.State != existingState.Status.State {
		return true, "main state changed: " + existingState.Status.State + " -> " + newStateData.Status.State
	}

	// Compare entrance statuses
	if !sm.compareEntranceStatuses(newStateData.Status.EntranceStatuses, existingState.Status.EntranceStatuses) {
		return true, "entrance statuses changed"
	}

	return false, "no changes detected"
}

// compareEntranceStatuses compares two arrays of entrance statuses
func (sm *StateMonitor) compareEntranceStatuses(
	newStatuses []struct {
		ID         string `json:"id"` // ID extracted from URL's first segment after splitting by "."
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
		Url        string `json:"url"`
		Invisible  bool   `json:"invisible"`
	},
	existingStatuses []struct {
		ID         string `json:"id"` // ID extracted from URL's first segment after splitting by "."
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
		Url        string `json:"url"`
		Invisible  bool   `json:"invisible"`
	},
) bool {
	// If lengths are different, statuses have changed
	if len(newStatuses) != len(existingStatuses) {
		return false
	}

	// Create maps for easier comparison
	newStatusMap := make(map[string]struct {
		State     string
		Url       string
		Invisible bool
	})
	existingStatusMap := make(map[string]struct {
		State     string
		Url       string
		Invisible bool
	})

	for _, status := range newStatuses {
		newStatusMap[status.Name] = struct {
			State     string
			Url       string
			Invisible bool
		}{
			State:     status.State,
			Url:       status.Url,
			Invisible: status.Invisible,
		}
	}

	for _, status := range existingStatuses {
		existingStatusMap[status.Name] = struct {
			State     string
			Url       string
			Invisible bool
		}{
			State:     status.State,
			Url:       status.Url,
			Invisible: status.Invisible,
		}
	}

	// Compare each entrance status
	for name, newStatus := range newStatusMap {
		if existingStatus, exists := existingStatusMap[name]; !exists ||
			existingStatus.State != newStatus.State ||
			existingStatus.Url != newStatus.Url ||
			existingStatus.Invisible != newStatus.Invisible {
			return false
		}
	}

	// Check for any entrances that exist in old but not in new
	for name := range existingStatusMap {
		if _, exists := newStatusMap[name]; !exists {
			return false
		}
	}

	return true
}

// Close closes the state monitor and its data sender
func (sm *StateMonitor) Close() {
	if sm.dataSender != nil {
		sm.dataSender.Close()
	}
}

// IsConnected checks if the data sender is connected
func (sm *StateMonitor) IsConnected() bool {
	if sm.dataSender == nil {
		return false
	}
	return sm.dataSender.IsConnected()
}
