package utils

import (
	"market/internal/v2/types"
)

// FindAppInUserData is a utility function to find an app in user data by app ID
// This function can be reused across different modules
func FindAppInUserData(userData *types.UserData, appID string) (*types.AppInfoLatestData, string) {
	if userData == nil {
		return nil, ""
	}

	for srcID, sourceData := range userData.Sources {
		if sourceData == nil || sourceData.AppInfoLatest == nil {
			continue
		}

		for _, app := range sourceData.AppInfoLatest {
			if app == nil {
				continue
			}

			// Use the common app matching logic
			if MatchAppID(app, appID) {
				return app, srcID
			}
		}
	}

	return nil, ""
}

// MatchAppID checks if an app matches the given app ID using multiple possible ID fields
// This is a common utility function that can be reused across the project
func MatchAppID(app *types.AppInfoLatestData, targetAppID string) bool {
	if app == nil {
		return false
	}

	// Check multiple possible ID fields for matching
	var currentAppID string
	if app.RawData != nil {
		// Priority: ID > AppID > Name
		if app.RawData.ID != "" {
			currentAppID = app.RawData.ID
		} else if app.RawData.AppID != "" {
			currentAppID = app.RawData.AppID
		} else if app.RawData.Name != "" {
			currentAppID = app.RawData.Name
		}
	}

	// Also check AppSimpleInfo if available
	if currentAppID == "" && app.AppSimpleInfo != nil {
		currentAppID = app.AppSimpleInfo.AppID
	}

	// Match the requested app ID
	return currentAppID == targetAppID || (app.RawData != nil && app.RawData.Name == targetAppID)
}

// FindAppInSourceData finds an app in a specific source data by app ID
func FindAppInSourceData(sourceData *types.SourceData, appID string) *types.AppInfoLatestData {
	if sourceData == nil || sourceData.AppInfoLatest == nil {
		return nil
	}

	for _, app := range sourceData.AppInfoLatest {
		if app == nil {
			continue
		}

		if MatchAppID(app, appID) {
			return app
		}
	}

	return nil
}

// CheckAppExistsInUserData checks if an app exists in user data (similar to checkAppInCache)
func CheckAppExistsInUserData(userData *types.UserData, appID string) bool {
	app, _ := FindAppInUserData(userData, appID)
	return app != nil
}

// FindAppInUserDataWithSource finds an app in user data by app ID and specific source
// If source is empty, it searches all sources (same behavior as FindAppInUserData)
func FindAppInUserDataWithSource(userData *types.UserData, appID string, source string) (*types.AppInfoLatestData, string) {
	if userData == nil {
		return nil, ""
	}

	// If source is specified, only search in that source
	if source != "" {
		sourceData, exists := userData.Sources[source]
		if !exists {
			return nil, ""
		}

		app := FindAppInSourceData(sourceData, appID)
		if app != nil {
			return app, source
		}
		return nil, ""
	}

	// If source is not specified, search all sources (fallback to original behavior)
	return FindAppInUserData(userData, appID)
}
