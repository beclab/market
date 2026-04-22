package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"market/internal/v2/types"

	"github.com/go-resty/resty/v2"
	"github.com/golang/glog"
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

	// 4. Send POST request to chart repo
	url := "http://" + host + "/chart-repo/api/v2/dcr/sync-app"
	startTime := time.Now()

	// Set timeout on the client. Default is 30 seconds because chart-repo's
	// sync-app endpoint runs the full hydration pipeline synchronously
	// (chart download, render, image analysis, db update) and can take a
	// few seconds when the chart is new — even longer when chart-repo is
	// busy hydrating other sources in the background. The previous 3s
	// timeout caused first-time installs of new charts to silently fail
	// even though chart-repo itself completed the work successfully.
	// Override via MARKET_CHART_REPO_TIMEOUT_SECONDS if needed.
	s.client.SetTimeout(getChartRepoTimeout())

	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(request).
		Post(url)
	duration := time.Since(startTime)
	if err != nil || resp.StatusCode() >= 300 {
		glog.Errorf("TaskForApiStep - Request failed in %v for user=%s, source=%s, app=%s(%s/%s): %v", duration, task.UserID, task.SourceID, task.AppID, task.AppName, task.AppVersion, err)
	}
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

	// 6. Handle app_data if present
	if apiResponse.Success && apiResponse.Data != nil {
		if dataMap, ok := apiResponse.Data.(map[string]interface{}); ok {
			if appData, hasAppData := dataMap["app_data"]; hasAppData && appData != nil {
				// Convert app_data to AppInfoLatestData and write to cache
				if err := s.writeAppDataToCache(task, appData); err != nil {
					glog.Errorf("Warning: failed to write app_data to cache: %v", err)
				} else {
					glog.V(2).Infof("Successfully wrote app_data to cache for user=%s, source=%s, app=%s, appName=%s",
						task.UserID, task.SourceID, task.AppID, task.AppName)
				}
			}
		}
	}

	glog.V(2).Infof("[TaskForApi] SyncApp %s(%s %s) to chart repo completed successfully", task.AppID, task.AppName, task.AppVersion)
	return nil
}

// writeAppDataToCache writes AppInfoLatestData to cache by updating the corresponding AppInfoLatestPendingData
func (s *TaskForApiStep) writeAppDataToCache(task *HydrationTask, appData interface{}) error {
	if task == nil || task.Cache == nil {
		return fmt.Errorf("task or cache is nil")
	}

	// Convert appData to AppInfoLatestData OUTSIDE the lock to avoid blocking
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
		return fmt.Errorf("app_data is not in expected format, app=%s, appName=%s", task.AppID, task.AppName)
	}

	if task.CacheManager == nil {
		return fmt.Errorf("CacheManager not available for fixVersionHistoryFromPendingData")
	}

	// Check if chartrepo returned appLabels before taking the lock
	chartrepoHasLabels := false
	if appDataMap, ok := appData.(map[string]interface{}); ok {
		if appInfoMap, ok := appDataMap["app_info"].(map[string]interface{}); ok {
			if appEntryMap, ok := appInfoMap["app_entry"].(map[string]interface{}); ok {
				if appLabels, ok := appEntryMap["appLabels"].([]interface{}); ok && len(appLabels) > 0 {
					chartrepoHasLabels = true
				}
			}
		}
	}
	if chartrepoHasLabels {
		// Clear the AppLabels so CopyPendingVersionHistory won't overwrite them
		// (it only copies when latest has empty labels)
	} else if appInfoLatest.RawData != nil {
		appInfoLatest.RawData.AppLabels = nil
		if appInfoLatest.AppInfo != nil && appInfoLatest.AppInfo.AppEntry != nil {
			appInfoLatest.AppInfo.AppEntry.AppLabels = nil
		}
	}

	return task.CacheManager.CopyPendingVersionHistory(
		task.UserID, task.SourceID, task.AppID, task.AppName, appInfoLatest,
	)
}

// findPendingDataFromCache finds AppInfoLatestPendingData from cache based on task information.
// Uses CacheManager.FindPendingDataForApp (which holds the global cache lock) instead of
// accessing the shared CacheData directly, preventing concurrent map read/write races.
func (s *TaskForApiStep) findPendingDataFromCache(task *HydrationTask) *types.AppInfoLatestPendingData {
	if task == nil {
		return nil
	}

	if task.CacheManager != nil {
		return task.CacheManager.FindPendingDataForApp(task.UserID, task.SourceID, task.AppID)
	}

	return nil
}

// getenv is a helper to get env variable (for testability)
func getenv(key string) string {
	return os.Getenv(key)
}

// defaultChartRepoTimeout is the default timeout for the chart-repo
// sync-app HTTP call. The endpoint runs the full hydration pipeline
// synchronously and can take several seconds for new charts.
const defaultChartRepoTimeout = 30 * time.Second

// getChartRepoTimeout returns the timeout for the chart-repo sync-app
// HTTP client. Configurable via MARKET_CHART_REPO_TIMEOUT_SECONDS
// (positive integer in seconds). Falls back to defaultChartRepoTimeout
// if the env var is unset, empty, or invalid.
func getChartRepoTimeout() time.Duration {
	v := getenv("MARKET_CHART_REPO_TIMEOUT_SECONDS")
	if v == "" {
		return defaultChartRepoTimeout
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		glog.Warningf("invalid MARKET_CHART_REPO_TIMEOUT_SECONDS=%q, falling back to %s", v, defaultChartRepoTimeout)
		return defaultChartRepoTimeout
	}
	return time.Duration(n) * time.Second
}
