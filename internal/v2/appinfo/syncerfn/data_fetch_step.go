package syncerfn

import (
	"context"
	"fmt"
	"log"
	"os"

	"market/internal/v2/settings"
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
	log.Printf("Executing %s", d.GetStepName())

	// Get version from SyncContext for API request
	// 从SyncContext获取版本用于API请求
	version := data.GetVersion()
	if version == "" {
		version = "1.12.0" // fallback version
		log.Printf("No version provided in context, using default: %s", version)
	}

	// Get current market source from context
	// 从上下文获取当前市场源
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	// 从市场源基础URL和端点路径构建完整URL
	dataURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DataEndpointPath)
	log.Printf("Using data URL: %s", dataURL)

	// Initialize response struct
	// 初始化响应结构体
	response := &AppStoreInfoResponse{}

	// Make request with version parameter and use structured response
	// 携带version参数发起请求并使用结构化响应
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
	// 使用结构化数据更新SyncContext
	data.LatestData = response

	// Extract version from response and update context
	// 从响应中提取version并更新上下文
	d.extractAndSetVersion(data)

	// Extract app IDs from the latest data
	// 从最新数据中提取app ID列表
	d.extractAppIDs(data)

	log.Printf("Fetched latest data with %d app IDs, version: %s",
		len(data.AppIDs), data.GetVersion())

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DataFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Check if we have any existing data in cache
	// 检查缓存中是否有现有数据
	hasExistingData := false
	if data.Cache != nil {
		data.Cache.Mutex.RLock()
		for _, userData := range data.Cache.Users {
			userData.Mutex.RLock()
			for _, sourceData := range userData.Sources {
				sourceData.Mutex.RLock()
				if len(sourceData.AppInfoLatestPending) > 0 || len(sourceData.AppInfoLatest) > 0 {
					hasExistingData = true
				}
				sourceData.Mutex.RUnlock()
				if hasExistingData {
					break
				}
			}
			userData.Mutex.RUnlock()
			if hasExistingData {
				break
			}
		}
		data.Cache.Mutex.RUnlock()
	}

	// Skip only if hashes match AND we have existing data
	// 只有在hash匹配且有现有数据时才跳过
	if data.HashMatches && hasExistingData {
		log.Printf("Skipping %s - hashes match and existing data found, no sync required", d.GetStepName())
		return true
	}

	// Force execution if no existing data, even if hashes match
	// 如果没有现有数据，即使hash匹配也强制执行
	if !hasExistingData {
		log.Printf("Executing %s - no existing data found, forcing data fetch", d.GetStepName())
	} else if !data.HashMatches {
		log.Printf("Executing %s - hashes don't match, sync required", d.GetStepName())
	}

	return false
}

// extractAndSetVersion extracts version from the response and updates SyncContext
// extractAndSetVersion 从响应中提取version并更新SyncContext
func (d *DataFetchStep) extractAndSetVersion(data *SyncContext) {
	if data.LatestData != nil && data.LatestData.Version != "" {
		data.SetVersion(data.LatestData.Version)
		log.Printf("Updated version from response: %s", data.LatestData.Version)
	} else {
		log.Printf("No version found in response or version is empty")
	}
}

// extractAppIDs extracts app IDs from the fetched data
// extractAppIDs 从获取的数据中提取app ID列表
func (d *DataFetchStep) extractAppIDs(data *SyncContext) {
	// Clear existing app IDs
	// 清空现有的app ID列表
	data.AppIDs = data.AppIDs[:0]

	// Check if we have valid response data
	// 检查是否有有效的响应数据
	if data.LatestData == nil || data.LatestData.Data.Apps == nil {
		log.Printf("Warning: no apps data found in response")
		return
	}

	// Access apps data directly from structured response
	// 直接从结构化响应访问apps数据
	appsMap := data.LatestData.Data.Apps

	// In development environment, limit the original data to only 2 apps
	// 在开发环境下，将原始数据限制为只有2个应用
	if isDevelopmentEnvironment() {
		originalCount := len(appsMap)
		if originalCount > 2 {
			log.Printf("Development environment detected, limiting original apps data to 2 (original count: %d)", originalCount)

			// Create a new map with only the first 2 apps
			// 创建一个只包含前2个应用的新map
			limitedAppsMap := make(map[string]interface{})
			count := 0
			for appID, appData := range appsMap {
				if count >= 2 {
					break
				}
				limitedAppsMap[appID] = appData
				count++
			}

			// Replace the original apps data with limited data
			// 用限制后的数据替换原始应用数据
			data.LatestData.Data.Apps = limitedAppsMap
			appsMap = limitedAppsMap
		}
	}

	// Iterate through the apps map where keys are app IDs
	// 遍历apps映射，其中键是app ID
	for appID, appData := range appsMap {
		// Verify this is a valid app entry by checking if it has required fields
		// 通过检查是否有必需字段来验证这是一个有效的app条目
		if appMap, ok := appData.(map[string]interface{}); ok {
			if id, hasID := appMap["id"].(string); hasID && id == appID {
				data.AppIDs = append(data.AppIDs, appID)
			}
		}
	}

	log.Printf("Extracted %d app IDs from response", len(data.AppIDs))

	// Log first few app IDs for debugging
	// 记录前几个app ID用于调试
	if len(data.AppIDs) > 0 {
		maxLog := 5
		if len(data.AppIDs) < maxLog {
			maxLog = len(data.AppIDs)
		}
		log.Printf("First %d app IDs: %v", maxLog, data.AppIDs[:maxLog])
	}
}

// isDevelopmentEnvironment checks if the application is running in development mode
// 检查应用是否在开发模式下运行
func isDevelopmentEnvironment() bool {
	// Check DEV_MODE environment variable
	// 检查 DEV_MODE 环境变量
	devMode := os.Getenv("DEV_MODE")
	if devMode == "true" {
		return true
	}

	// Check ENVIRONMENT environment variable
	// 检查 ENVIRONMENT 环境变量
	environment := os.Getenv("ENVIRONMENT")
	if environment == "development" {
		return true
	}

	// Check DEBUG_MODE environment variable
	// 检查 DEBUG_MODE 环境变量
	debugMode := os.Getenv("DEBUG_MODE")
	if debugMode == "true" {
		return true
	}

	return false
}
