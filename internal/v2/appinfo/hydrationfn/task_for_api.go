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

// syncAppPayload is the per-app render artefact. RawDataEx is the input
// we split into the user_applications JSONB columns. RawPackage and
// RenderedPackage are file paths to the source / rendered chart packages
// on chart-repo's filesystem; they are persisted to the homonymous
// user_applications.{raw_package, rendered_package} columns via
// UpsertRenderSuccess so the API layer can derive the chart_path argument
// for app-service without a cache round-trip. AppInfo includes the same
// app_entry; the previous "image_analysis lives only inside AppInfo"
// arrangement is gone now that chart-repo emits a typed ImageAnalysis
// block at this level (see ImageAnalysis below) and Market persists it.
//
// Migration plan: RawData is the legacy flat-map field kept on the wire
// only during chart-repo's gradual rollout of the typed payload. Market
// consumes RawDataEx exclusively — RawData is not read anywhere. When
// chart-repo drops RawData on the wire, this field can be removed and
// RawDataEx renamed to RawData (with `json:"raw_data"`); no other logic
// changes needed.
type syncAppPayload struct {
	Type            string                `json:"type"`
	Timestamp       int64                 `json:"timestamp"`
	Version         string                `json:"version"`
	RawData         map[string]any        `json:"raw_data"`
	RawDataEx       *oac.AppConfiguration `json:"raw_data_ex"`
	// ImageAnalysis is chart-repo's per-app docker image analysis,
	// emitted at the same level as RawDataEx. We persist it verbatim
	// to user_applications.image_analysis via UpsertRenderSuccess so
	// /api/v2/apps callers can render image-level details (size,
	// architecture, download progress) without round-tripping back
	// to chart-repo. nil here is normal for older chart-repo builds
	// that pre-date this field — the column stays NULL until the
	// next render with the new chart-repo refreshes it.
	ImageAnalysis *types.ImageAnalysisResult `json:"image_analysis"`
	// I18n / VersionHistory live at the same level as RawDataEx on
	// the chart-repo wire and are persisted to dedicated JSONB columns
	// on user_applications (see store.UpsertRenderSuccess); they are
	// NOT split into the manifest payload anymore.
	I18n           map[string]map[string]string `json:"i18n"`
	VersionHistory []types.VersionInfo          `json:"version_history"`
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

	// Build the manifest payload (= JSONB columns) from raw_data_ex. We
	// rely on chart-repo always returning raw_data_ex alongside success=
	// true, including on the "existing" fast path. If it is missing we
	// would otherwise persist a scalar-only success row and lose the
	// per-user JSONB manifest, so we treat that case as a render failure
	// and let the upstream loop call MarkRenderFailed instead. The
	// manifest is also stashed on the task so the pipeline can compose
	// the post-render app_info for NATS notifications without doing a
	// follow-up PG read.
	if apiResponse.Data == nil || apiResponse.Data.AppData == nil || apiResponse.Data.AppData.RawDataEx == nil {
		return fmt.Errorf("chart repo sync-app returned success without raw_data_ex (user=%s source=%s app=%s)",
			task.UserID, task.SourceID, task.AppID)
	}
	rawData := apiResponse.Data.AppData.RawDataEx
	manifest := types.BuildUserAppManifest(rawData)

	// Inject entrance / shared-entrance URLs while we still have the
	// per-render context. zone was carried in from Pipeline.phaseHydrateApps
	// (sourced from the User CR's bytetrade.io/zone annotation); appID is
	// the clone-aware app_id (clones carry md5(newAppName) here, matching
	// what app-service uses to compose URLs at install time). Empty zone
	// is fine — EnrichEntranceURLs no-ops in that case.
	types.EnrichEntranceURLs(manifest, task.AppID, task.UserZone)

	task.RenderedManifest = manifest

	// Persist render success to user_applications. Skipped when the task
	// has no AppEntry (legacy cache-driven path); on that path the cache
	// write is the source of truth for render outcome.
	if task.AppEntry != nil {
		// VersionHistory is sourced from the catalog-side AppEntry
		// (loaded by store.ListRenderCandidates from
		// applications.app_entry) rather than from chart-repo's
		// sync-app response. Storage uses []types.VersionInfo while
		// the catalog projection uses []*types.VersionInfo, so the
		// pointer slice is dereffed into a value slice here. Mirror
		// of the value→pointer conversion in pkg/v2/api/app.go's
		// detail-row composer; both sides keep their own layer's
		// convention and the conversion stays local.
		var versionHistory []types.VersionInfo
		if len(task.AppEntry.VersionHistory) > 0 {
			versionHistory = make([]types.VersionInfo, 0, len(task.AppEntry.VersionHistory))
			for _, vi := range task.AppEntry.VersionHistory {
				if vi != nil {
					versionHistory = append(versionHistory, *vi)
				}
			}
		}

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
			I18n:            apiResponse.Data.AppData.I18n,
			VersionHistory:  versionHistory,
			ImageAnalysis:   apiResponse.Data.AppData.ImageAnalysis,
			RawPackage:      apiResponse.Data.AppData.RawPackage,
			RenderedPackage: apiResponse.Data.AppData.RenderedPackage,
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
