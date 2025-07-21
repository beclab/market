package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

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

	// 4. Send POST request to chart repo
	url := "http://" + host + "/chart-repo/api/v2/dcr/sync-app"
	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(request).
		Post(url)
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

	// 6. Print complete response except app_data
	s.logResponseWithoutAppData(apiResponse)

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

// logResponseWithoutAppData logs the complete response except app_data field
func (s *TaskForApiStep) logResponseWithoutAppData(response Response) {
	// Create a copy of data without app_data for logging
	var logData interface{}
	if response.Data != nil {
		if dataMap, ok := response.Data.(map[string]interface{}); ok {
			// Create a copy without app_data
			logDataMap := make(map[string]interface{})
			for k, v := range dataMap {
				if k != "app_data" {
					logDataMap[k] = v
				}
			}
			logData = logDataMap
		} else {
			logData = response.Data
		}
	}

	log.Printf("Chart repo sync-app response - Success: %v, Message: %s, Data: %+v",
		response.Success, response.Message, logData)
}

// writeAppDataToCache writes AppInfoLatestData to cache by updating the corresponding AppInfoLatestPendingData
func (s *TaskForApiStep) writeAppDataToCache(task *HydrationTask, appData interface{}) error {
	if task == nil || task.Cache == nil {
		return fmt.Errorf("task or cache is nil")
	}

	// Convert appData to AppInfoLatestData
	var appInfoLatest *types.AppInfoLatestData
	if appDataMap, ok := appData.(map[string]interface{}); ok {
		appInfoLatest = types.NewAppInfoLatestData(appDataMap)
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
			parsedValues := make([]*types.Values, 0, len(values))
			for _, v := range values {
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
		}
	} else {
		return fmt.Errorf("app_data is not in expected format")
	}

	// Find the pendingData in cache
	pendingData := s.findPendingDataFromCache(task)
	if pendingData == nil {
		return fmt.Errorf("pendingData not found in cache for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	}

	// Overwrite all fields of pendingData (keep the pointer address, update all contents)
	pendingData.Type = appInfoLatest.Type
	pendingData.Timestamp = appInfoLatest.Timestamp
	pendingData.Version = appInfoLatest.Version
	pendingData.RawData = appInfoLatest.RawData
	pendingData.RawPackage = appInfoLatest.RawPackage
	pendingData.Values = appInfoLatest.Values
	pendingData.AppInfo = appInfoLatest.AppInfo
	pendingData.RenderedPackage = appInfoLatest.RenderedPackage

	log.Printf("Updated AppInfoLatestPendingData in cache for user=%s, source=%s, app=%s", task.UserID, task.SourceID, task.AppID)
	log.Printf("Type=%s, Version=%s, RawPackage=%s, RenderedPackage=%s", pendingData.Type, pendingData.Version, pendingData.RawPackage, pendingData.RenderedPackage)

	return nil
}

// findPendingDataFromCache finds AppInfoLatestPendingData from cache based on task information
func (s *TaskForApiStep) findPendingDataFromCache(task *HydrationTask) *types.AppInfoLatestPendingData {
	if task == nil || task.Cache == nil {
		return nil
	}

	// Get user data from cache
	userData := task.Cache.Users[task.UserID]
	if userData == nil {
		return nil
	}

	// Get source data from user data
	sourceData := userData.Sources[task.SourceID]
	if sourceData == nil {
		return nil
	}

	// Find matching AppInfoLatestPendingData by app ID
	for _, pendingData := range sourceData.AppInfoLatestPending {
		if pendingData == nil {
			continue
		}

		// Check RawData first
		if pendingData.RawData != nil {
			if pendingData.RawData.ID == task.AppID ||
				pendingData.RawData.AppID == task.AppID ||
				pendingData.RawData.Name == task.AppID {
				return pendingData
			}
		}

		// Check AppInfo.AppEntry
		if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
			if pendingData.AppInfo.AppEntry.ID == task.AppID ||
				pendingData.AppInfo.AppEntry.AppID == task.AppID ||
				pendingData.AppInfo.AppEntry.Name == task.AppID {
				return pendingData
			}
		}
	}

	return nil
}

// getenv is a helper to get env variable (for testability)
func getenv(key string) string {
	return os.Getenv(key)
}
