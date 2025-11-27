package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"market/internal/v2/types"

	"github.com/go-resty/resty/v2"
)

// Response represents the response from chart repo sync-app API
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type TaskForApiStep struct {
	client *resty.Client
}

func NewTaskForApiStep() *TaskForApiStep {
	return &TaskForApiStep{
		client: resty.New(),
	}
}

func (s *TaskForApiStep) GetStepName() string {
	return "TaskForApiStep"
}

func (s *TaskForApiStep) CanSkip(ctx context.Context, task *HydrationTask) bool {

	// if utils.IsPublicEnvironment() {
	// 	return true
	// }

	return false
}

func (s *TaskForApiStep) Execute(ctx context.Context, task *HydrationTask) error {
	// 1. Get chart repo service host from environment
	host := ""
	if v := getenv("CHART_REPO_SERVICE_HOST"); v != "" {
		host = v
	}
	if host == "" {
		return fmt.Errorf("CHART_REPO_SERVICE_HOST environment variable is not set")
	}

	// 2. Get AppInfoLatestPendingData from cache
	pendingData := s.findPendingDataFromCache(task)
	if pendingData == nil {
		return fmt.Errorf("AppInfoLatestPendingData not found in cache for user=%s, source=%s, app=%s",
			task.UserID, task.SourceID, task.AppID)
	}

	// 3. Build SyncAppRequest
	request := struct {
		AppInfoLatestPendingData *types.AppInfoLatestPendingData `json:"app_info_latest_pending_data"`
		SourceID                 string                          `json:"source_id"`
		UserName                 string                          `json:"user_name"`
	}{
		AppInfoLatestPendingData: pendingData,
		SourceID:                 task.SourceID,
		UserName:                 task.UserID,
	}

	// 4. Send POST request to chart repo with 3 second timeout
	url := "http://" + host + "/chart-repo/api/v2/dcr/sync-app"
	log.Printf("DEBUG: TaskForApiStep - Sending request to %s for user=%s, source=%s, app=%s (timeout: 3s)", url, task.UserID, task.SourceID, task.AppID)
	startTime := time.Now()

	// Set timeout on the client
	s.client.SetTimeout(3 * time.Second)

	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(request).
		Post(url)
	duration := time.Since(startTime)
	log.Printf("DEBUG: TaskForApiStep - Request completed in %v for user=%s, source=%s, app=%s", duration, task.UserID, task.SourceID, task.AppID)
	if err != nil {
		return fmt.Errorf("failed to call chart repo sync-app: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("chart repo sync-app returned non-2xx: %d, body: %s", resp.StatusCode(), resp.String())
	}

	// 5. Parse response
	var apiResponse Response
	if err := json.Unmarshal(resp.Body(), &apiResponse); err != nil {
		return fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// 6. Print complete response including app_data
	s.logCompleteResponse(apiResponse)

	// 7. Handle app_data if present
	if apiResponse.Success && apiResponse.Data != nil {
		if dataMap, ok := apiResponse.Data.(map[string]interface{}); ok {
			if appData, hasAppData := dataMap["app_data"]; hasAppData && appData != nil {
				// Convert app_data to AppInfoLatestData and write to cache
				if err := s.writeAppDataToCache(task, appData); err != nil {
					log.Printf("Warning: failed to write app_data to cache: %v", err)
				} else {
					log.Printf("Successfully wrote app_data to cache for user=%s, source=%s, app=%s",
						task.UserID, task.SourceID, task.AppID)
				}
			}
		}
	}

	log.Printf("SyncApp to chart repo completed successfully")
	return nil
}

// logCompleteResponse logs the complete response including app_data field
func (s *TaskForApiStep) logCompleteResponse(response Response) {
	// Marshal the complete response to JSON for full logging
	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		log.Printf("Chart repo sync-app response - Failed to marshal response: %v", err)
		log.Printf("Chart repo sync-app response - Success: %v, Message: %s, Data: %+v",
			response.Success, response.Message, response.Data)
	} else {
		log.Printf("Chart repo sync-app complete response:\n%s", string(responseJSON))
	}
}

// writeAppDataToCache writes AppInfoLatestData to cache by updating the corresponding AppInfoLatestPendingData
func (s *TaskForApiStep) writeAppDataToCache(task *HydrationTask, appData interface{}) error {
	if task == nil || task.Cache == nil {
		return fmt.Errorf("task or cache is nil")
	}

	// Convert appData to AppInfoLatestData OUTSIDE the lock to avoid blocking
	log.Printf("[DEBUG] writeAppDataToCache: Starting data conversion for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	var appInfoLatest *types.AppInfoLatestData
	if appDataMap, ok := appData.(map[string]interface{}); ok {
		log.Printf("[DEBUG] writeAppDataToCache: Calling NewAppInfoLatestData for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		appInfoLatest = types.NewAppInfoLatestData(appDataMap)
		log.Printf("[DEBUG] writeAppDataToCache: NewAppInfoLatestData completed for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		if appInfoLatest == nil {
			return fmt.Errorf("failed to create AppInfoLatestData from response data")
		}
		if rawPkg, ok := appDataMap["raw_package"].(string); ok {
			appInfoLatest.RawPackage = rawPkg
		}
		if renderedPkg, ok := appDataMap["rendered_package"].(string); ok {
			appInfoLatest.RenderedPackage = renderedPkg
		}
		if values, ok := appDataMap["values"].([]interface{}); ok {
			log.Printf("[DEBUG] writeAppDataToCache: Processing %d values for user=%s, source=%s, app=%s", len(values), task.UserID, task.SourceID, task.AppID)
			parsedValues := make([]*types.Values, 0, len(values))
			for i, v := range values {
				if i%100 == 0 { // Log every 100 values to avoid spam
					log.Printf("[DEBUG] writeAppDataToCache: Processing value %d/%d for user=%s, source=%s, app=%s", i, len(values), task.UserID, task.SourceID, task.AppID)
				}
				if vMap, ok := v.(map[string]interface{}); ok {
					val := &types.Values{}
					if fileName, ok := vMap["file_name"].(string); ok {
						val.FileName = fileName
					}
					if modifyType, ok := vMap["modify_type"].(string); ok {
						val.ModifyType = types.ModifyType(modifyType)
					}
					if modifyKey, ok := vMap["modify_key"].(string); ok {
						val.ModifyKey = modifyKey
					}
					if modifyValue, ok := vMap["modify_value"].(string); ok {
						val.ModifyValue = modifyValue
					}
					parsedValues = append(parsedValues, val)
				}
			}
			appInfoLatest.Values = parsedValues
			log.Printf("[DEBUG] writeAppDataToCache: Values processing completed for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		}
	} else {
		return fmt.Errorf("app_data is not in expected format")
	}

	// Now acquire the lock for cache operations
	if task.CacheManager != nil {
		log.Printf("[DEBUG] writeAppDataToCache: Acquiring lock for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		task.CacheManager.Lock()
		log.Printf("[DEBUG] writeAppDataToCache: Lock acquired for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		defer func() {
			log.Printf("[DEBUG] writeAppDataToCache: Releasing lock for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
			task.CacheManager.Unlock()
		}()
	}

	// Find the pendingData in cache
	log.Printf("[DEBUG] writeAppDataToCache: Calling findPendingDataFromCache for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	pendingData := s.findPendingDataFromCache(task)
	log.Printf("[DEBUG] writeAppDataToCache: findPendingDataFromCache completed for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	if pendingData == nil {
		return fmt.Errorf("pendingData not found in cache for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	}

	// Fix version history data
	appInfoLatest.RawData.VersionHistory = pendingData.RawData.VersionHistory
	appInfoLatest.AppInfo.AppEntry.VersionHistory = pendingData.RawData.VersionHistory

	// Overwrite all fields of pendingData (keep the pointer address, update all contents)
	pendingData.Type = appInfoLatest.Type
	pendingData.Timestamp = appInfoLatest.Timestamp
	pendingData.Version = appInfoLatest.Version
	pendingData.RawData = appInfoLatest.RawData
	pendingData.RawPackage = appInfoLatest.RawPackage
	pendingData.Values = appInfoLatest.Values
	pendingData.AppInfo = appInfoLatest.AppInfo
	pendingData.RenderedPackage = appInfoLatest.RenderedPackage
	pendingData.AppSimpleInfo = appInfoLatest.AppSimpleInfo

	log.Printf("[DEBUG] writeAppDataToCache: Starting final logging for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	log.Printf("Updated AppInfoLatestPendingData in cache for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	log.Printf("Type=%s, Version=%s, RawPackage=%s, RenderedPackage=%s", pendingData.Type, pendingData.Version, pendingData.RawPackage, pendingData.RenderedPackage)
	// Removed heavy logging that could cause goroutine blocking

	log.Printf("[DEBUG] writeAppDataToCache: Method completed successfully for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	return nil
}

// findPendingDataFromCache finds AppInfoLatestPendingData from cache based on task information
func (s *TaskForApiStep) findPendingDataFromCache(task *HydrationTask) *types.AppInfoLatestPendingData {
	log.Printf("[DEBUG] findPendingDataFromCache: Starting for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)

	if task == nil || task.Cache == nil {
		log.Printf("[DEBUG] findPendingDataFromCache: task or cache is nil for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		return nil
	}

	// Get user data from cache
	log.Printf("[DEBUG] findPendingDataFromCache: Getting user data for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	userData := task.Cache.Users[task.UserID]
	if userData == nil {
		log.Printf("[DEBUG] findPendingDataFromCache: userData is nil for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		return nil
	}

	// Get source data from user data
	log.Printf("[DEBUG] findPendingDataFromCache: Getting source data for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	sourceData := userData.Sources[task.SourceID]
	if sourceData == nil {
		log.Printf("[DEBUG] findPendingDataFromCache: sourceData is nil for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
		return nil
	}

	// Find matching AppInfoLatestPendingData by app ID
	log.Printf("[DEBUG] findPendingDataFromCache: Searching through %d pending data items for user=%s, source=%s, app=%s", len(sourceData.AppInfoLatestPending), task.UserID, task.SourceID, task.AppID)
	for i, pendingData := range sourceData.AppInfoLatestPending {
		if i%50 == 0 { // Log every 50 items to avoid spam
			log.Printf("[DEBUG] findPendingDataFromCache: Checking item %d/%d for user=%s, source=%s, app=%s", i, len(sourceData.AppInfoLatestPending), task.UserID, task.SourceID, task.AppID)
		}
		if pendingData == nil {
			continue
		}

		// Check RawData first
		if pendingData.RawData != nil {
			if pendingData.RawData.ID == task.AppID ||
				pendingData.RawData.AppID == task.AppID ||
				pendingData.RawData.Name == task.AppID {
				log.Printf("[DEBUG] findPendingDataFromCache: Found match in RawData for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
				return pendingData
			}
		}

		// Check AppInfo.AppEntry
		if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
			if pendingData.AppInfo.AppEntry.ID == task.AppID ||
				pendingData.AppInfo.AppEntry.AppID == task.AppID ||
				pendingData.AppInfo.AppEntry.Name == task.AppID {
				log.Printf("[DEBUG] findPendingDataFromCache: Found match in AppInfo.AppEntry for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
				return pendingData
			}
		}
	}

	log.Printf("[DEBUG] findPendingDataFromCache: No match found for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	return nil
}

// getenv is a helper to get env variable (for testability)
func getenv(key string) string {
	return os.Getenv(key)
}
