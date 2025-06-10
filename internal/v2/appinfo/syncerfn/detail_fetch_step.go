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
	// 如果hash匹配或没有app ID则跳过
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
	// 从上下文获取当前市场源
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		errMsg := fmt.Errorf("no market source available in sync context for detail fetch")
		data.AddError(errMsg)
		log.Printf("ERROR: %v", errMsg)
		return 0, len(appIDs)
	}

	// Build complete URL from market source base URL and endpoint path
	// 从市场源基础URL和端点路径构建完整URL
	detailURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DetailEndpointPath)

	request := types.AppsInfoRequest{
		AppIds:  appIDs,
		Version: d.Version,
	}

	var response types.AppsInfoResponse

	resp, err := data.Client.R().
		SetContext(ctx).
		SetBody(request).
		SetResult(&response).
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
		// 成功 - 处理应用并用详细数据替换简化数据
		data.mutex.Lock()

		// Update the original LatestData with detailed information
		// 用详细信息更新原始的LatestData
		if data.LatestData != nil && data.LatestData.Data.Apps != nil {
			for appID, appInfo := range response.Apps {
				// Replace the simplified app data with detailed data in LatestData
				// 在LatestData中用详细数据替换简化的应用数据
				detailedAppData := map[string]interface{}{
					// Basic fields
					"id":          appInfo.ID,
					"name":        appInfo.Name,
					"cfgType":     appInfo.CfgType,
					"chartName":   appInfo.ChartName,
					"icon":        appInfo.Icon,
					"description": appInfo.Description,
					"appID":       appInfo.AppID,
					"title":       appInfo.Title,
					"version":     appInfo.Version,
					"categories":  appInfo.Categories,
					"versionName": appInfo.VersionName,

					// Extended fields
					"fullDescription":    appInfo.FullDescription,
					"upgradeDescription": appInfo.UpgradeDescription,
					"promoteImage":       appInfo.PromoteImage,
					"promoteVideo":       appInfo.PromoteVideo,
					"subCategory":        appInfo.SubCategory,
					"locale":             appInfo.Locale,
					"developer":          appInfo.Developer,
					"requiredMemory":     appInfo.RequiredMemory,
					"requiredDisk":       appInfo.RequiredDisk,
					"supportClient":      appInfo.SupportClient,
					"supportArch":        appInfo.SupportArch,
					"requiredGPU":        appInfo.RequiredGPU,
					"requiredCPU":        appInfo.RequiredCPU,
					"rating":             appInfo.Rating,
					"target":             appInfo.Target,
					"permission":         appInfo.Permission,
					"entrances":          appInfo.Entrances,
					"middleware":         appInfo.Middleware,
					"options":            appInfo.Options,

					// Additional metadata fields
					"submitter":     appInfo.Submitter,
					"doc":           appInfo.Doc,
					"website":       appInfo.Website,
					"featuredImage": appInfo.FeaturedImage,
					"sourceCode":    appInfo.SourceCode,
					"license":       appInfo.License,
					"legal":         appInfo.Legal,
					"i18n":          appInfo.I18n,

					"modelSize": appInfo.ModelSize,
					"namespace": appInfo.Namespace,
					"onlyAdmin": appInfo.OnlyAdmin,

					"lastCommitHash": appInfo.LastCommitHash,
					"createTime":     appInfo.CreateTime,
					"updateTime":     appInfo.UpdateTime,
					"appLabels":      appInfo.AppLabels,
					"count":          appInfo.Count,
					"variants":       appInfo.Variants,

					// Legacy fields for backward compatibility
					"screenshots": appInfo.Screenshots,
					"tags":        appInfo.Tags,
					"metadata":    appInfo.Metadata,
					"updated_at":  appInfo.UpdatedAt,
				}

				data.LatestData.Data.Apps[appID] = detailedAppData

				// Also store in DetailedApps for backward compatibility
				// 同时存储在DetailedApps中以保持向后兼容
				data.DetailedApps[appID] = detailedAppData

				// Log the main app information with more details
				log.Printf("Replaced app data with details - ID: %s, Name: %s, Title: %s, Version: %s, Categories: %v, Developer: %s, UpdatedAt: %s",
					appInfo.ID, appInfo.Name, appInfo.Title, appInfo.Version, appInfo.Categories, appInfo.Developer, appInfo.UpdatedAt)
			}
		}

		data.mutex.Unlock()

		successCount := len(response.Apps)
		errorCount := len(response.NotFound)

		if len(response.NotFound) > 0 {
			log.Printf("WARNING: Apps not found in batch: %v", response.NotFound)
		}

		log.Printf("Successfully fetched and replaced details for %d/%d apps in batch", successCount, len(appIDs))
		return successCount, errorCount

	case 202:
		// Accepted - data is being loaded
		errMsg := fmt.Errorf("data is being loaded for version %s, batch: %v - %s", d.Version, appIDs, response.Message)
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
