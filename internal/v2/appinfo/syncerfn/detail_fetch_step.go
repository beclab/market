package syncerfn

import (
	"context"
	"fmt"
	"log"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// DetailFetchStep implements the third step: fetch detailed info for each app
type DetailFetchStep struct {
	DetailEndpointPath string                    // Relative path like "/api/v1/applications/info"
	BatchSize          int                       // Number of apps to fetch in each batch
	Version            string                    // Version parameter for API requests
	SettingsManager    *settings.SettingsManager // Settings manager to build complete URLs
}

// NewDetailFetchStep creates a new detail fetch step
func NewDetailFetchStep(detailEndpointPath string, version string, settingsManager *settings.SettingsManager) *DetailFetchStep {
	return &DetailFetchStep{
		DetailEndpointPath: detailEndpointPath,
		BatchSize:          10, // Default batch size of 10
		Version:            version,
		SettingsManager:    settingsManager,
	}
}

// NewDetailFetchStepWithBatchSize creates a new detail fetch step with custom batch size
func NewDetailFetchStepWithBatchSize(detailEndpointPath string, version string, settingsManager *settings.SettingsManager, batchSize int) *DetailFetchStep {
	return &DetailFetchStep{
		DetailEndpointPath: detailEndpointPath,
		BatchSize:          batchSize,
		Version:            version,
		SettingsManager:    settingsManager,
	}
}

// GetStepName returns the name of this step
func (d *DetailFetchStep) GetStepName() string {
	return "Detail Fetch Step"
}

// Execute performs the detail fetching logic with batch processing
func (d *DetailFetchStep) Execute(ctx context.Context, data *SyncContext) error {
	log.Printf("Executing %s for %d apps in batches of %d", d.GetStepName(), len(data.AppIDs), d.BatchSize)

	if len(data.AppIDs) == 0 {
		log.Printf("No app IDs to fetch details for")
		return nil
	}

	// Process apps in batches
	totalBatches := (len(data.AppIDs) + d.BatchSize - 1) / d.BatchSize
	successCount := 0
	errorCount := 0

	for i := 0; i < len(data.AppIDs); i += d.BatchSize {
		batchNumber := (i / d.BatchSize) + 1
		end := i + d.BatchSize
		if end > len(data.AppIDs) {
			end = len(data.AppIDs)
		}

		batch := data.AppIDs[i:end]
		log.Printf("Processing batch %d/%d with %d apps: %v", batchNumber, totalBatches, len(batch), batch)

		batchSuccessCount, batchErrorCount := d.fetchAppsBatch(ctx, batch, data)
		successCount += batchSuccessCount
		errorCount += batchErrorCount
	}

	log.Printf("Completed detail fetch: %d successful, %d errors, total %d apps",
		successCount, errorCount, len(data.AppIDs))

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DetailFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Skip if hashes match or no app IDs
	if data.HashMatches {
		log.Printf("Skipping %s - hashes match, no sync required", d.GetStepName())
		return true
	}

	if len(data.AppIDs) == 0 {
		log.Printf("Skipping %s - no app IDs to fetch details for", d.GetStepName())
		return true
	}

	return false
}

// fetchAppsBatch fetches detailed information for a batch of apps
func (d *DetailFetchStep) fetchAppsBatch(ctx context.Context, appIDs []string, data *SyncContext) (int, int) {
	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		errMsg := fmt.Errorf("no market source available in sync context for detail fetch")
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Build complete URL from market source base URL and endpoint path
	detailURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DetailEndpointPath)

	request := types.AppsInfoRequest{
		AppIds:  appIDs,
		Version: d.Version,
	}

	// Use map[string]interface{} to avoid type mismatch with multi-language fields
	var rawResponse map[string]interface{}

	resp, err := data.Client.R().
		SetContext(ctx).
		SetBody(request).
		SetResult(&rawResponse).
		Post(detailURL)

	if err != nil {
		errMsg := fmt.Errorf("failed to fetch batch details for apps %v from %s: %w", appIDs, detailURL, err)
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Handle different status codes
	switch resp.StatusCode() {
	case 200:
		// Success - process the apps and replace simplified data with detailed data
		data.mutex.Lock()

		// Extract apps from raw response
		if appsData, ok := rawResponse["apps"].(map[string]interface{}); ok {
			// Update the original LatestData with detailed information
			if data.LatestData != nil && data.LatestData.Data.Apps != nil {
				for appID, appData := range appsData {
					if appInfoMap, ok := appData.(map[string]interface{}); ok {
						// Replace the simplified app data with detailed data in LatestData
						detailedAppData := map[string]interface{}{
							// Basic fields
							"id":        appInfoMap["id"],
							"name":      appInfoMap["name"],
							"cfgType":   appInfoMap["cfgType"],
							"chartName": appInfoMap["chartName"],
							"icon":      appInfoMap["icon"],
							// Skip multi-language fields to avoid type mismatch
							// "description": appInfoMap["description"],
							"appID": appInfoMap["appID"],
							// "title":       appInfoMap["title"],
							"version":     appInfoMap["version"],
							"categories":  appInfoMap["categories"],
							"versionName": appInfoMap["versionName"],

							// Extended fields - skip multi-language fields
							// "fullDescription":    appInfoMap["fullDescription"],
							// "upgradeDescription": appInfoMap["upgradeDescription"],
							"promoteImage":   appInfoMap["promoteImage"],
							"promoteVideo":   appInfoMap["promoteVideo"],
							"subCategory":    appInfoMap["subCategory"],
							"locale":         appInfoMap["locale"],
							"developer":      appInfoMap["developer"],
							"requiredMemory": appInfoMap["requiredMemory"],
							"requiredDisk":   appInfoMap["requiredDisk"],
							"supportClient":  appInfoMap["supportClient"],
							"supportArch":    appInfoMap["supportArch"],
							"requiredGPU":    appInfoMap["requiredGPU"],
							"requiredCPU":    appInfoMap["requiredCPU"],
							"rating":         appInfoMap["rating"],
							"target":         appInfoMap["target"],
							"permission":     appInfoMap["permission"],
							"entrances":      appInfoMap["entrances"],
							"middleware":     appInfoMap["middleware"],
							"options":        appInfoMap["options"],

							// Additional metadata fields
							"submitter":     appInfoMap["submitter"],
							"doc":           appInfoMap["doc"],
							"website":       appInfoMap["website"],
							"featuredImage": appInfoMap["featuredImage"],
							"sourceCode":    appInfoMap["sourceCode"],
							"license":       appInfoMap["license"],
							"legal":         appInfoMap["legal"],
							"i18n":          appInfoMap["i18n"],

							"modelSize": appInfoMap["modelSize"],
							"namespace": appInfoMap["namespace"],
							"onlyAdmin": appInfoMap["onlyAdmin"],

							"lastCommitHash": appInfoMap["lastCommitHash"],
							"createTime":     appInfoMap["createTime"],
							"updateTime":     appInfoMap["updateTime"],
							"appLabels":      appInfoMap["appLabels"],
							"count":          appInfoMap["count"],
							"variants":       appInfoMap["variants"],

							// Legacy fields for backward compatibility
							"screenshots": appInfoMap["screenshots"],
							"tags":        appInfoMap["tags"],
							"metadata":    appInfoMap["metadata"],
							"updated_at":  appInfoMap["updated_at"],
						}

						data.LatestData.Data.Apps[appID] = detailedAppData

						// Also store in DetailedApps for backward compatibility
						data.DetailedApps[appID] = detailedAppData

						log.Printf("DetailedAppData: %v", detailedAppData)

						// Log the main app information with more details
						log.Printf("Replaced app data with details - ID: %s, Name: %s, Version: %s",
							appInfoMap["id"], appInfoMap["name"], appInfoMap["version"])
					}
				}
			}
		}

		data.mutex.Unlock()

		// Count successful and failed apps
		successCount := 0
		if appsData, ok := rawResponse["apps"].(map[string]interface{}); ok {
			successCount = len(appsData)
		}

		errorCount := 0
		if notFound, ok := rawResponse["not_found"].([]interface{}); ok {
			errorCount = len(notFound)
		}

		if errorCount > 0 {
			log.Printf("WARNING: Apps not found in batch: %v", rawResponse["not_found"])
		}

		log.Printf("Successfully fetched and replaced details for %d/%d apps in batch", successCount, len(appIDs))
		return successCount, errorCount

	case 202:
		// Accepted - data is being loaded
		message := ""
		if msg, ok := rawResponse["message"].(string); ok {
			message = msg
		}
		errMsg := fmt.Errorf("data is being loaded for version %s, batch: %v - %s", d.Version, appIDs, message)
		data.AddError(errMsg)
		log.Printf("WARNING: %v", errMsg)
		return 0, len(appIDs)

	case 400:
		// Bad Request
		errMsg := fmt.Errorf("bad request for batch %v to %s: status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)

	default:
		// Other errors
		errMsg := fmt.Errorf("detail API for batch %v to %s returned status %d", appIDs, detailURL, resp.StatusCode())
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}
}
