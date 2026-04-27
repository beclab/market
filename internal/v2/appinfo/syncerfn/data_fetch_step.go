package syncerfn

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"market/internal/v2/settings"
	marketSourceStore "market/internal/v2/store/marketsource"

	"github.com/golang/glog"
)

// DataFetchStep implements the second step: fetch latest data from remote
type DataFetchStep struct {
	DataEndpointPath  string                    // Relative path like "/api/v1/appstore/info"
	SettingsManager   *settings.SettingsManager // Settings manager to build complete URLs
	MarketSourceStore marketSourceStore.Store
}

// NewDataFetchStep creates a new data fetch step
func NewDataFetchStep(
	dataEndpointPath string,
	settingsManager *settings.SettingsManager,
	marketSourceStore marketSourceStore.Store,
) *DataFetchStep {
	return &DataFetchStep{
		DataEndpointPath:  dataEndpointPath,
		SettingsManager:   settingsManager,
		MarketSourceStore: marketSourceStore,
	}
}

// GetStepName returns the name of this step
func (d *DataFetchStep) GetStepName() string {
	return "Data Fetch Step"
}

// Execute performs the data fetching logic
func (d *DataFetchStep) Execute(ctx context.Context, data *SyncContext) error {
	glog.V(2).Infof("Executing %s", d.GetStepName())

	// Get version from SyncContext for API request
	version := data.GetVersion()
	if version == "" {
		version = "1.12.3" // fallback version
		glog.V(2).Infof("No version provided in context, using default: %s", version)
	}

	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	dataURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DataEndpointPath)
	glog.V(2).Infof("Using data, Source: %s, Version: %s, URL: %s", marketSource.ID, version, dataURL)

	if strings.HasPrefix(dataURL, "file://") {
		return nil
	}

	// Initialize response struct
	response := &AppStoreInfoResponse{}

	// Make request with version parameter and use structured response
	resp, err := data.Client.R().
		SetContext(ctx).
		SetQueryParam("version", version).
		SetResult(response).
		Get(dataURL)

	if err != nil {
		return fmt.Errorf("failed to fetch latest data from %s: %w", dataURL, err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("remote data API returned status %d from %s", resp.StatusCode(), dataURL)
	}

	// Update SyncContext with structured data
	data.LatestData = response

	// Extract version from response and update context
	d.extractAndSetVersion(data)

	// Extract app IDs from the latest data
	d.extractAppIDs(data)

	marketData := BuildMarketSourceData(response)
	if marketData == nil {
		return fmt.Errorf("failed to build market source data for source %s", marketSource.ID)
	}

	if d.MarketSourceStore == nil {
		return fmt.Errorf("market source store is not configured")
	}
	changed, err := d.MarketSourceStore.SaveDataIfHashChanged(ctx, marketSource.ID, marketData)
	if err != nil {
		return fmt.Errorf("failed to save market source data for source %s: %w", marketSource.ID, err)
	}

	if changed {
		glog.V(2).Infof("Updated market_sources.data for source: %s (hash=%s)", marketSource.ID, marketData.Hash)
	} else {
		glog.V(3).Infof("Skipped market_sources.data update for source: %s (hash unchanged)", marketSource.ID)
	}

	glog.V(2).Infof("Fetched latest data with %d app IDs, version: %s",
		len(data.AppIDs), data.GetVersion())

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DataFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Get current market source - only check data for this specific source
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		glog.V(3).Infof("Executing %s - no market source available in sync context", d.GetStepName())
		return false
	}

	sourceID := marketSource.ID

	// Check whether market_sources.data already has persisted snapshot for this source.
	hasExistingData := false
	if d.MarketSourceStore != nil {
		exists, err := d.MarketSourceStore.HasData(ctx, sourceID)
		if err != nil {
			glog.Warningf("Failed checking persisted market source data for %s: %v", sourceID, err)
		} else {
			hasExistingData = exists
		}
	}

	// Skip only if hashes match AND persisted data exists for this source.
	if data.HashMatches && hasExistingData {
		glog.V(3).Infof("Skipping %s for source %s - hashes match and persisted data found, no sync required", d.GetStepName(), sourceID)
		return true
	}

	// Force execution if no persisted data for this source, even if hashes match.
	if !hasExistingData {
		glog.V(3).Infof("Executing %s for source %s - no persisted data found for this source, forcing data fetch", d.GetStepName(), sourceID)
	} else if !data.HashMatches {
		glog.V(3).Infof("Executing %s for source %s - hashes don't match, sync required", d.GetStepName(), sourceID)
	}

	return false
}

// extractAndSetVersion extracts version from the response and updates SyncContext
func (d *DataFetchStep) extractAndSetVersion(data *SyncContext) {
	if data.LatestData != nil && data.LatestData.Version != "" {
		data.SetVersion(data.LatestData.Version)
		glog.V(2).Infof("Updated version from response: %s", data.LatestData.Version)
	} else {
		glog.V(3).Infof("No version found in response or version is empty")
	}
}

// extractAppIDs extracts app IDs from the fetched data
func (d *DataFetchStep) extractAppIDs(data *SyncContext) {
	// Clear existing app IDs
	data.AppIDs = data.AppIDs[:0]

	// Check if we have valid response data
	if data.LatestData == nil || data.LatestData.Data.Apps == nil {
		glog.V(3).Infof("Warning: no apps data found in response")
		return
	}

	// Convert apps to typed projection for safer field access.
	appsMap := make(map[string]RemoteAppBrief, len(data.LatestData.Data.Apps))
	for appID, appRaw := range data.LatestData.Data.Apps {
		if appMap, ok := appRaw.(map[string]interface{}); ok {
			appsMap[appID] = mapToRemoteAppBrief(appMap)
		}
	}

	// In development environment, limit app list to 2 entries deterministically.
	if isDevelopmentEnvironment() && len(appsMap) > 200 {
		glog.V(2).Infof("Development environment detected, limiting original apps data to 2 (original count: %d)", len(appsMap))
		sortedAppIDs := make([]string, 0, len(appsMap))
		for appID := range appsMap {
			sortedAppIDs = append(sortedAppIDs, appID)
		}
		sort.Strings(sortedAppIDs)
		allowed := make(map[string]struct{}, 2)
		for idx, appID := range sortedAppIDs {
			if idx >= 2 {
				break
			}
			allowed[appID] = struct{}{}
		}
		for appID := range appsMap {
			if _, ok := allowed[appID]; !ok {
				delete(appsMap, appID)
			}
		}
	}

	// Iterate through typed apps map where keys are app IDs.
	for appID, app := range appsMap {
		// Keep historical validation semantics: only accept entries with matching id.
		if app.ID != "" && app.ID == appID {
			data.AppIDs = append(data.AppIDs, appID)
		}
	}

	glog.V(2).Infof("Extracted %d app IDs from response", len(data.AppIDs))

	// Log first few app IDs for debugging
	if len(data.AppIDs) > 0 {
		maxLog := 5
		if len(data.AppIDs) < maxLog {
			maxLog = len(data.AppIDs)
		}
		glog.V(2).Infof("First %d app IDs: %v", maxLog, data.AppIDs[:maxLog])
	}
}

// isDevelopmentEnvironment checks if the application is running in development mode
func isDevelopmentEnvironment() bool {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	return env == "dev" || env == "development" || env == ""
}

func mapToRemoteAppBrief(m map[string]interface{}) RemoteAppBrief {
	app := RemoteAppBrief{}
	if m == nil {
		return app
	}

	if value, ok := m["id"].(string); ok {
		app.ID = value
	}
	if value, ok := m["name"].(string); ok {
		app.Name = value
	}
	if value, ok := m["version"].(string); ok {
		app.Version = value
	}
	if value, ok := m["category"].(string); ok {
		app.Category = value
	}
	if value, ok := m["description"].(string); ok {
		app.Description = value
	}
	if value, ok := m["icon"].(string); ok {
		app.Icon = value
	}
	if value, exists := m["screenshots"]; exists {
		app.Screenshots = value
	}
	if value, exists := m["tags"]; exists {
		app.Tags = value
	}
	if value, exists := m["metadata"]; exists {
		app.Metadata = value
	}
	if value, exists := m["source"]; exists {
		app.Source = value
	}
	if value, ok := m["updated_at"].(string); ok {
		app.UpdatedAt = value
	}

	return app
}

