package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"market/internal/v2/store"
	"market/internal/v2/types"

	"github.com/beclab/Olares/framework/oac"
	"github.com/go-resty/resty/v2"
	"github.com/golang/glog"
)

// syncAppResponse mirrors the full shape of chart-repo's /dcr/sync-app
// reply. The hydration logic currently only reads Data.AppData.RawData
// (everything else is left in place for future cross-checks, debug logs,
// and forward compatibility — declaring the shape costs almost nothing
// and avoids silently dropping fields when chart-repo's contract evolves).
type syncAppResponse struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Data    *syncAppData `json:"data"`
}

// syncAppData is the data envelope returned by chart-repo. Status mirrors
// chart-repo's idea of the app's render state ("existing", "new",
// "updated", ...); Message is its human-readable counterpart.
type syncAppData struct {
	AppID    string          `json:"app_id"`
	UserID   string          `json:"user_id"`
	SourceID string          `json:"source_id"`
	Version  string          `json:"version"`
	Status   string          `json:"status"`
	Message  string          `json:"message"`
	AppData  *syncAppPayload `json:"app_data"`
}

// syncAppPayload is the per-app render artefact. RawData is the input we
// split into the user_applications JSONB columns. RawPackage and
// RenderedPackage are file paths to the source / rendered chart packages
// on chart-repo's filesystem — kept here for diagnostic logging even
// though the database does not have columns for them. AppInfo includes
// the same app_entry plus image_analysis (which is not persisted).
type syncAppPayload struct {
	Type            string                `json:"type"`
	Timestamp       int64                 `json:"timestamp"`
	Version         string                `json:"version"`
	RawData         map[string]any        `json:"raw_data"`
	RawDataEx       *oac.AppConfiguration `json:"raw_data_ex"`
	RawPackage      string                `json:"raw_package"`
	RenderedPackage string                `json:"rendered_package"`
	Values          []any                 `json:"values"`
	AppInfo         map[string]any        `json:"app_info"`
	AppSimpleInfo   map[string]any        `json:"app_simple_info"`
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

	// Build the manifest payload (= JSONB columns) from raw_data. We rely
	// on chart-repo always returning raw_data alongside success=true,
	// including on the "existing" fast path. If raw_data is missing we
	// would otherwise persist a scalar-only success row and lose the
	// per-user JSONB manifest, so we treat that case as a render failure
	// and let the upstream loop call MarkRenderFailed instead. The
	// manifest is also stashed on the task so the pipeline can compose
	// the post-render app_info for NATS notifications without doing a
	// follow-up PG read.
	if apiResponse.Data == nil || apiResponse.Data.AppData == nil || apiResponse.Data.AppData.RawData == nil {
		return fmt.Errorf("chart repo sync-app returned success without raw_data (user=%s source=%s app=%s)",
			task.UserID, task.SourceID, task.AppID)
	}
	rawData := apiResponse.Data.AppData.RawDataEx
	// chart-repo's /dcr/sync-app returns wrong values for raw_data.id
	// and raw_data.appID; the catalog (applications.app_id) is the
	// source of truth, so we override on the map before splitting.
	manifest := types.BuildUserAppManifest(rawData)
	task.RenderedManifest = manifest

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
			ManifestVersion: rawData.ConfigVersion,
			ManifestType:    rawData.ConfigType,
			APIVersion:      rawData.APIVersion,
			Manifest:        manifest,
		}
		if err := store.UpsertRenderSuccess(ctx, in); err != nil {
			return fmt.Errorf("persist render success to user_applications: %w", err)
		}
	}

	// Surface chart-repo's own status/message on the success path; this is
	// useful for distinguishing a fresh render from a "already existed"
	// fast-path response (chart-repo sets status="existing" / message=
	// "App already exists in latest with same version" in that case).
	var (
		repoStatus, repoMessage, renderedPackage string
	)
	if apiResponse.Data != nil {
		repoStatus = apiResponse.Data.Status
		repoMessage = apiResponse.Data.Message
		if apiResponse.Data.AppData != nil {
			renderedPackage = apiResponse.Data.AppData.RenderedPackage
		}
	}
	glog.V(2).Infof("[TaskForApi] SyncApp %s(%s %s) to chart repo completed in %v: status=%q msg=%q rendered=%s",
		task.AppID, task.AppName, task.AppVersion, duration, repoStatus, repoMessage, renderedPackage)
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
