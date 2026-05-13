package syncerfn

import (
	"context"
	"fmt"
	"strings"

	"market/internal/v2/settings"
	"market/internal/v2/store/marketsource"

	"github.com/golang/glog"
)

// DataFetchStep implements the second step: fetch latest data from remote
type DataFetchStep struct {
	DataEndpointPath string                    // Relative path like "/api/v1/appstore/info"
	SettingsManager  *settings.SettingsManager // Settings manager to build complete URLs
}

// NewDataFetchStep creates a new data fetch step
func NewDataFetchStep(dataEndpointPath string, settingsManager *settings.SettingsManager) *DataFetchStep {
	return &DataFetchStep{
		DataEndpointPath: dataEndpointPath,
		SettingsManager:  settingsManager,
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
	dataURL := d.SettingsManager.BuildAPIURL(marketSource.SourceURL, d.DataEndpointPath)
	glog.V(2).Infof("Using data, Source: %s, Version: %s, URL: %s", marketSource.SourceID, version, dataURL)

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

	if response.Hash == "" || response.Version == "" {
		return fmt.Errorf("remote data invalid, hash: %s, version: %s", response.Hash, response.Version)
	}

	// Update SyncContext with structured data
	data.LatestData = response

	// Extract version from response and update context
	d.extractAndSetVersion(data)

	// Extract app IDs from the latest data
	d.extractAppIDs(data)

	marketData := BuildMarketSourceData(response)
	if marketData == nil {
		return fmt.Errorf("failed to build market source data for source %s", marketSource.SourceID)
	}

	err = marketsource.SaveData(ctx, marketSource.SourceID, marketData)
	if err != nil {
		return fmt.Errorf("failed to save market source data for source %s: %w", marketSource.SourceID, err)
	}
	glog.V(2).Infof("Fetched and Updated market_sources.data for source: %s (hash=%s, version=%s), apps: %d", marketSource.SourceID, marketData.Hash, data.GetVersion(), len(data.AppIDs))

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DataFetchStep) CanSkip(_ context.Context, _ *SyncContext) bool {
	// TODO: change to query user_applications for checking.
	return false
}

// extractAndSetVersion extracts version from the response and updates SyncContext
func (d *DataFetchStep) extractAndSetVersion(data *SyncContext) {
	data.SetVersion(data.LatestData.Version)
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

	// Convert apps to a minimal typed projection for safer field access.
	type appBrief struct {
		ID string
	}
	appsMap := make(map[string]appBrief, len(data.LatestData.Data.Apps))
	for appID, appRaw := range data.LatestData.Data.Apps {
		if appMap, ok := appRaw.(map[string]interface{}); ok {
			id, _ := appMap["id"].(string)
			appsMap[appID] = appBrief{ID: id}
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
}
