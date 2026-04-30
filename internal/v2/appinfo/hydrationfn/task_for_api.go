package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"market/internal/v2/store"
	"market/internal/v2/types"

	"github.com/go-resty/resty/v2"
	"github.com/golang/glog"
)

// syncAppResponse is the minimal shape we need from chart-repo's
// /dcr/sync-app reply. Only data.app_data.raw_data is consumed; every
// other field in the response is intentionally ignored.
type syncAppResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    *struct {
		AppData *struct {
			RawData map[string]any `json:"raw_data"`
		} `json:"app_data"`
	} `json:"data"`
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

	// if helper.IsPublicEnvironment() {
	// 	return true
	// }

	return false
}

func (s *TaskForApiStep) Execute(ctx context.Context, task *HydrationTask) error {
	host := getenv("CHART_REPO_SERVICE_HOST")
	if host == "" {
		return fmt.Errorf("CHART_REPO_SERVICE_HOST environment variable is not set")
	}

	// PG-driven hydration sets task.PendingPayload directly; the legacy
	// cache-driven path leaves it nil and we fall back to the in-memory cache.
	pendingData := task.PendingPayload
	if pendingData == nil {
		pendingData = s.findPendingDataFromCache(task)
	}
	if pendingData == nil {
		return fmt.Errorf("AppInfoLatestPendingData not available for user=%s, source=%s, app=%s",
			task.UserID, task.SourceID, task.AppID)
	}

	request := struct {
		AppInfoLatestPendingData *types.AppInfoLatestPendingData `json:"app_info_latest_pending_data"`
		SourceID                 string                          `json:"source_id"`
		UserName                 string                          `json:"user_name"`
	}{
		AppInfoLatestPendingData: pendingData,
		SourceID:                 task.SourceID,
		UserName:                 task.UserID,
	}

	url := "http://" + host + "/chart-repo/api/v2/dcr/sync-app"
	startTime := time.Now()

	s.client.SetTimeout(3 * time.Second)

	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(request).
		Post(url)
	duration := time.Since(startTime)
	if err != nil {
		glog.Errorf("TaskForApiStep - Request failed in %v for user=%s, source=%s, app=%s(%s/%s): %v",
			duration, task.UserID, task.SourceID, task.AppID, task.AppName, task.AppVersion, err)
		return fmt.Errorf("failed to call chart repo sync-app: %w", err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("chart repo sync-app returned non-2xx: %d, body: %s", resp.StatusCode(), resp.String())
	}

	var apiResponse syncAppResponse
	if err := json.Unmarshal(resp.Body(), &apiResponse); err != nil {
		return fmt.Errorf("failed to parse response JSON: %w", err)
	}
	// Treat 2xx + Success=false as a render failure so the hydration loop can
	// MarkRenderFailed instead of recording a phantom success.
	if !apiResponse.Success {
		msg := apiResponse.Message
		if msg == "" {
			msg = "chart repo reported success=false"
		}
		return fmt.Errorf("chart repo sync-app rejected app: %s", msg)
	}

	// Build the manifest payload (= JSONB columns) from raw_data when
	// chart-repo provided it. A missing data.app_data.raw_data is treated
	// as "manifest unavailable, persist scalar render status only" — the
	// next render attempt will fill in the manifest.
	var manifest *types.UserAppManifest
	if apiResponse.Data != nil && apiResponse.Data.AppData != nil && apiResponse.Data.AppData.RawData != nil {
		rawData := apiResponse.Data.AppData.RawData
		// chart-repo's /dcr/sync-app returns wrong values for raw_data.id
		// and raw_data.appID; the catalog (applications.app_id) is the
		// source of truth, so we override on the map before splitting.
		rawData["id"] = task.AppID
		rawData["appID"] = task.AppID
		manifest = types.BuildUserAppManifest(rawData)
	}

	// Persist render success to user_applications. Skipped when the task
	// has no AppEntry (legacy cache-driven path); on that path the cache
	// write is the source of truth for render outcome.
	if task.AppEntry != nil {
		in := store.UpsertRenderSuccessInput{
			UserID:          task.UserID,
			SourceID:        task.SourceID,
			AppID:           task.AppID,
			AppName:         task.AppName,
			AppRawID:        task.AppID,
			AppRawName:      task.AppName,
			ManifestVersion: task.AppVersion,
			ManifestType:    task.AppType,
			APIVersion:      task.AppEntry.ApiVersion,
			Manifest:        manifest,
		}
		if err := store.UpsertRenderSuccess(ctx, in); err != nil {
			return fmt.Errorf("persist render success to user_applications: %w", err)
		}
	}

	glog.V(2).Infof("[TaskForApi] SyncApp %s(%s %s) to chart repo completed successfully in %v",
		task.AppID, task.AppName, task.AppVersion, duration)
	return nil
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
