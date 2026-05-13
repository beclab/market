package chartrepo

import (
	"time"

	"market/internal/v2/types"

	"github.com/beclab/Olares/framework/oac"
)

// envelope mirrors the JSON wrapper chart-repo applies to almost every
// reply: {success, message, data}. Generic-free internally; per-endpoint
// methods unmarshal data into the right concrete type using a second
// json.Unmarshal step. We keep this unexported because callers should
// never see it — the public surface is the typed payload.
type envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	// Data uses json.RawMessage so we can defer decoding until we know
	// the expected shape. This avoids the interface{} → map → marshal →
	// unmarshal round-trip the legacy callers used.
	Data jsonRawMessage `json:"data,omitempty"`
}

// jsonRawMessage is a tiny alias around []byte so we can implement the
// json marshalling without importing encoding/json into this file's
// public surface. It is functionally identical to json.RawMessage.
type jsonRawMessage []byte

func (m jsonRawMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return []byte(m), nil
}

func (m *jsonRawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errInvalidRaw
	}
	*m = append((*m)[0:0], data...)
	return nil
}

// errInvalidRaw is a tiny sentinel used only by jsonRawMessage. Not
// exported because no caller would ever encounter it (Go ensures *m
// is non-nil for the standard library decoder).
var errInvalidRaw = &BusinessError{Endpoint: "json", Message: "UnmarshalJSON on nil pointer"}

// ----------------------------------------------------------------------
// Version (GET /version)
// ----------------------------------------------------------------------

// VersionInfo is the data envelope returned by GET /version. Mirrors
// the utils.DependencyServiceResponse.Data shape verbatim so the
// existing version-check caller can migrate by renaming a field path
// rather than restructuring the consumer.
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	GitCommit string `json:"git_commit"`
	Timestamp int64  `json:"timestamp"`
}

// ----------------------------------------------------------------------
// Market sources (GET / POST / DELETE /settings/market-source)
// ----------------------------------------------------------------------

// MarketSource mirrors the chart-repo wire shape for a single source.
// The Go-side settings package today defines a near-identical struct
// (settings.ChartRepoMarketSource) and converts to/from it manually;
// once Stage 2 migrates settings to this client, that duplicate can
// be removed.
type MarketSource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	BaseURL     string    `json:"base_url"`
	Priority    int       `json:"priority"`
	IsActive    bool      `json:"is_active"`
	UpdatedAt   time.Time `json:"updated_at"`
	Description string    `json:"description"`
}

// MarketSourcesConfig is the GET /settings/market-source response
// envelope's data field. DefaultSource is chart-repo's view of the
// "primary remote" — Market's PG-backed view may legitimately diverge
// (see settings/manager.go GetMarketSource docstring).
type MarketSourcesConfig struct {
	Sources       []*MarketSource `json:"sources"`
	DefaultSource string          `json:"default_source"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ----------------------------------------------------------------------
// State changes (GET /state-changes)
// ----------------------------------------------------------------------

// StateChange is one row from chart-repo's monotonically-increasing
// state-changes feed. Type discriminates the payload: "app_upload_
// completed" populates AppData, "image_info_updated" populates ImageData.
type StateChange struct {
	ID        int64                 `json:"id"`
	Type      string                `json:"type"`
	AppData   *StateChangeAppData   `json:"app_data,omitempty"`
	ImageData *StateChangeImageData `json:"image_data,omitempty"`
	Timestamp time.Time             `json:"timestamp"`
}

// StateChangeAppData carries the source / app / user tuple for an
// app_upload_completed event.
type StateChangeAppData struct {
	Source  string `json:"source"`
	AppName string `json:"app_name"`
	UserID  string `json:"user_id"`
}

// StateChangeImageData carries the image identifier for an
// image_info_updated event.
type StateChangeImageData struct {
	ImageName string `json:"image_name"`
}

// StateChangesData is the unwrapped data field of GET /state-changes.
// AfterID / Limit / TypeFilter echo back the query parameters; Count
// is len(StateChanges); TotalAvailable lets the caller decide whether
// to issue a follow-up page.
type StateChangesData struct {
	AfterID        int64          `json:"after_id"`
	Limit          int            `json:"limit"`
	TypeFilter     string         `json:"type_filter"`
	Count          int            `json:"count"`
	TotalAvailable int            `json:"total_available"`
	StateChanges   []*StateChange `json:"state_changes"`
}

// ----------------------------------------------------------------------
// Apps batch fetch (POST /apps)
// ----------------------------------------------------------------------

// AppsRequestItem is one (appID, sourceDataName) pair in the batch
// request body. The field tags are lowercase-snake to match chart-repo's
// existing wire format — these are NOT the Go-canonical capitalisations.
type AppsRequestItem struct {
	AppID          string `json:"appid"`
	SourceDataName string `json:"sourceDataName"`
}

// AppsRequest is the full POST /apps body. The legacy callsite uses
// userid (single user, lowercase) and submits a single-item Apps slice
// per request; we keep that shape verbatim so the migration is a
// mechanical rename.
type AppsRequest struct {
	Apps   []AppsRequestItem `json:"apps"`
	UserID string            `json:"userid"`
}

// AppsResponseData is the unwrapped data field of POST /apps. The
// Apps field is opaque (map[string]any) because chart-repo's per-app
// payload schema is intentionally loose — each app may carry any
// subset of {id, appID, app_id, name, metadata, ...} depending on
// origin. Pick out fields you need via map keys rather than declaring
// a struct that would silently drop unknown fields.
type AppsResponseData struct {
	Apps []map[string]interface{} `json:"apps"`
}

// ----------------------------------------------------------------------
// Images (GET /images?imageName=...)
// ----------------------------------------------------------------------

// ImagesResponseData is the unwrapped data field of GET /images.
// ImageInfo is opaque (map[string]any) for the same reason AppsResponseData.Apps is.
type ImagesResponseData struct {
	ImageInfo map[string]interface{} `json:"image_info"`
}

// ----------------------------------------------------------------------
// Repo data (GET /repo/data)
// ----------------------------------------------------------------------

// RepoData is the raw GET /repo/data response. Distinct from the rest
// because this endpoint does NOT use the {success, message, data}
// envelope — it streams the data structure directly at the top level.
// The shape here mirrors the minimum subset the cache-correction caller
// reads (apps per source).
type RepoData struct {
	UserData *RepoUserData `json:"user_data"`
}

type RepoUserData struct {
	Sources map[string]*RepoSourceData `json:"sources"`
}

type RepoSourceData struct {
	AppInfoLatest []*RepoAppInfoLatest `json:"app_info_latest"`
}

type RepoAppInfoLatest struct {
	AppSimpleInfo *RepoAppSimpleInfo `json:"app_simple_info"`
}

type RepoAppSimpleInfo struct {
	AppID       string   `json:"app_id"`
	SupportArch []string `json:"support_arch,omitempty"`
}

// ----------------------------------------------------------------------
// Sync app (POST /dcr/sync-app) — hydration hot path
// ----------------------------------------------------------------------

// SyncAppRequest is the body posted to /dcr/sync-app. Fields mirror
// the legacy hydrationfn.TaskForApiStep request struct.
type SyncAppRequest struct {
	AppInfoLatestPendingData *types.AppInfoLatestPendingData `json:"app_info_latest_pending_data"`
	SourceID                 string                          `json:"source_id"`
	UserName                 string                          `json:"user_name"`
}

// SyncAppData is the unwrapped data field of POST /dcr/sync-app.
type SyncAppData struct {
	AppID    string          `json:"app_id"`
	UserID   string          `json:"user_id"`
	SourceID string          `json:"source_id"`
	Version  string          `json:"version"`
	Status   string          `json:"status"`
	Message  string          `json:"message"`
	AppData  *SyncAppPayload `json:"app_data"`
}

// SyncAppPayload is the per-app render artefact carried inside
// SyncAppData. Field-for-field copy of hydrationfn.syncAppPayload; the
// caller migration is therefore a rename, not a logic change.
//
// Migration plan (legacy comment preserved): RawData is the flat-map
// field kept on the wire only during chart-repo's gradual rollout of
// the typed payload. Market consumes RawDataEx exclusively. When
// chart-repo drops RawData on the wire, this field can be removed and
// RawDataEx renamed to RawData (with `json:"raw_data"`); no other
// logic changes needed.
type SyncAppPayload struct {
	Type            string                       `json:"type"`
	Timestamp       int64                        `json:"timestamp"`
	Version         string                       `json:"version"`
	RawData         map[string]any               `json:"raw_data"`
	RawDataEx       *oac.AppConfiguration        `json:"raw_data_ex"`
	ImageAnalysis   *types.ImageAnalysisResult   `json:"image_analysis"`
	I18n            map[string]map[string]string `json:"i18n"`
	VersionHistory  []types.VersionInfo          `json:"version_history"`
	RawPackage      string                       `json:"raw_package"`
	RenderedPackage string                       `json:"rendered_package"`
	Values          []any                        `json:"values"`
	AppInfo         map[string]any               `json:"app_info"`
	AppSimpleInfo   map[string]any               `json:"app_simple_info"`
}

// ----------------------------------------------------------------------
// Upload chart (POST /apps/upload, multipart) and delete (/local-apps/delete)
// ----------------------------------------------------------------------

// UploadChartInput captures the inputs to a chart upload. SourceID
// becomes the multipart "source" form field; FileBytes is sent under
// the "chart" form field with the supplied Filename.
//
// Token / UserID are passed verbatim into X-Authorization /
// X-User-ID / X-Bfl-User headers (the legacy callsite sets all three
// from the same string; we keep that behaviour for migration parity).
type UploadChartInput struct {
	UserID    string
	SourceID  string
	Filename  string
	FileBytes []byte
	Token     string
}

// UploadChartResponseData is the unwrapped data field of POST /apps/upload.
// AppData is shaped like types.AppInfoLatestData — modelled as a
// pointer so callers can distinguish "absent" from "zero-valued".
type UploadChartResponseData struct {
	AppData *types.AppInfoLatestData `json:"app_data"`
}

// DeleteLocalAppRequest is the JSON body posted to
// /local-apps/delete. The HTTP verb is DELETE; the body lives there
// because chart-repo's contract requires it (a DELETE with a JSON
// body is unusual but matches the upstream API).
type DeleteLocalAppRequest struct {
	AppName    string `json:"app_name"`
	AppVersion string `json:"app_version"`
	SourceID   string `json:"source_id"`
}

// ----------------------------------------------------------------------
// Download history (GET /app/version-for-download-history)
// ----------------------------------------------------------------------

// AppDownloadHistoryData is the unwrapped data field of GET
// /app/version-for-download-history. Version + Source are both
// required by the only caller; the legacy callsite tolerates either
// map[string]string or map[string]any encoding by chart-repo's
// reflection, so we declare the concrete struct shape and let the
// caller surface ErrInvalidResponse if the upstream drifts.
type AppDownloadHistoryData struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

// ----------------------------------------------------------------------
// Status (GET /status) — runtime collector
// ----------------------------------------------------------------------

// StatusInclude enumerates the optional sub-resources GET /status may
// return. Construct via the string constants below; callers pass a
// []StatusInclude that the client serialises into the include=
// comma-separated query parameter.
type StatusInclude string

const (
	StatusIncludeApps   StatusInclude = "apps"
	StatusIncludeImages StatusInclude = "images"
	StatusIncludeTasks  StatusInclude = "tasks"
)

// Status mirrors the chart-repo /status reply data. The shape is
// faithfully copied from runtime.ChartRepoStatus to make the Stage 2
// migration mechanical — once runtime/collector.go switches to use
// this client, the parallel definitions in runtime/types.go can be
// thinned to type aliases or removed.
type Status struct {
	System     *SystemStatus `json:"system,omitempty"`
	Apps       []*AppState   `json:"apps,omitempty"`
	Images     []*ImageState `json:"images,omitempty"`
	Tasks      *TasksStatus  `json:"tasks,omitempty"`
	LastUpdate time.Time     `json:"last_update"`
}

// SystemStatus is the system block of /status. Components and
// ResourceUsage are intentionally opaque (map[string]any) because
// chart-repo emits a heterogeneous bag of fields here that changes
// across versions; the caller picks what it needs by key.
type SystemStatus struct {
	Uptime        int64          `json:"uptime,omitempty"`
	Version       string         `json:"version,omitempty"`
	Components    map[string]any `json:"components,omitempty"`
	ResourceUsage map[string]any `json:"resource_usage,omitempty"`
	Hostname      string         `json:"hostname,omitempty"`
}

// AppState mirrors runtime.ChartRepoAppState.
type AppState struct {
	AppID       string      `json:"app_id"`
	AppName     string      `json:"app_name"`
	UserID      string      `json:"user_id"`
	SourceID    string      `json:"source_id"`
	State       string      `json:"state"`
	CurrentStep *Step       `json:"current_step,omitempty"`
	Processing  *Processing `json:"processing,omitempty"`
	Timestamps  *Timestamps `json:"timestamps,omitempty"`
	Error       *StepError  `json:"error,omitempty"`
	LastUpdate  time.Time   `json:"last_update"`
}

type Step struct {
	Name       string    `json:"name"`
	Index      int       `json:"index"`
	Total      int       `json:"total"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	RetryCount int       `json:"retry_count"`
}

type Processing struct {
	TaskID   string `json:"task_id"`
	Duration int64  `json:"duration"`
}

type Timestamps struct {
	CreatedAt     time.Time `json:"created_at"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

type StepError struct {
	Message string `json:"message"`
	Step    string `json:"step,omitempty"`
}

// ImageState mirrors runtime.ChartRepoImageState.
type ImageState struct {
	ImageName        string       `json:"image_name"`
	AppID            string       `json:"app_id,omitempty"`
	AppName          string       `json:"app_name,omitempty"`
	Status           string       `json:"status"`
	Architecture     string       `json:"architecture,omitempty"`
	TotalSize        int64        `json:"total_size,omitempty"`
	DownloadedSize   int64        `json:"downloaded_size,omitempty"`
	DownloadProgress float64      `json:"download_progress,omitempty"`
	LayerCount       int          `json:"layer_count,omitempty"`
	DownloadedLayers int          `json:"downloaded_layers,omitempty"`
	AnalysisStatus   string       `json:"analysis_status,omitempty"`
	AnalyzedAt       *time.Time   `json:"analyzed_at,omitempty"`
	ErrorMessage     string       `json:"error_message,omitempty"`
	Nodes            []*ImageNode `json:"nodes,omitempty"`
	LastUpdate       time.Time    `json:"last_update"`
}

type ImageNode struct {
	NodeName         string  `json:"node_name"`
	Architecture     string  `json:"architecture,omitempty"`
	OS               string  `json:"os,omitempty"`
	TotalSize        int64   `json:"total_size,omitempty"`
	DownloadedSize   int64   `json:"downloaded_size,omitempty"`
	LayerCount       int     `json:"layer_count,omitempty"`
	DownloadedLayers int     `json:"downloaded_layers,omitempty"`
	Progress         float64 `json:"progress,omitempty"`
}

// TasksStatus mirrors runtime.ChartRepoTasksStatus. Both sub-blocks
// are optional; chart-repo omits them when the corresponding
// subsystem is idle / disabled.
type TasksStatus struct {
	Hydrator      *HydratorStatus      `json:"hydrator,omitempty"`
	ImageAnalyzer *ImageAnalyzerStatus `json:"image_analyzer,omitempty"`
}

type HydratorStatus struct {
	QueueLength    int          `json:"queue_length"`
	ActiveTasks    int          `json:"active_tasks"`
	CompletedTasks int          `json:"completed_tasks"`
	FailedTasks    int          `json:"failed_tasks"`
	WorkerCount    int          `json:"worker_count"`
	Tasks          []*TaskState `json:"tasks,omitempty"`
	RecentFailed   []*TaskState `json:"recent_failed,omitempty"`
}

type TaskState struct {
	TaskID     string    `json:"task_id"`
	UserID     string    `json:"user_id"`
	SourceID   string    `json:"source_id"`
	AppID      string    `json:"app_id"`
	AppName    string    `json:"app_name"`
	Status     string    `json:"status"`
	StepName   string    `json:"step_name"`
	TotalSteps int       `json:"total_steps"`
	RetryCount int       `json:"retry_count"`
	LastError  string    `json:"last_error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ImageAnalyzerStatus struct {
	IsRunning           bool                       `json:"is_running"`
	HealthStatus        string                     `json:"health_status"`
	LastCheck           time.Time                  `json:"last_check"`
	QueueLength         int                        `json:"queue_length"`
	ActiveWorkers       int                        `json:"active_workers"`
	CachedImages        int                        `json:"cached_images"`
	AnalyzingCount      int                        `json:"analyzing_count"`
	QueuedTasks         []*ImageAnalysisTaskDetail `json:"queued_tasks,omitempty"`
	ProcessingTasks     []*ImageAnalysisTaskDetail `json:"processing_tasks,omitempty"`
	RecentCompleted     []*ImageAnalysisTaskDetail `json:"recent_completed,omitempty"`
	RecentFailed        []*ImageAnalysisTaskDetail `json:"recent_failed,omitempty"`
	TotalAnalyzed       int64                      `json:"total_analyzed"`
	SuccessfulAnalyzed  int64                      `json:"successful_analyzed"`
	FailedAnalyzed      int64                      `json:"failed_analyzed"`
	AverageAnalysisTime time.Duration              `json:"average_analysis_time"`
	ErrorMessage        string                     `json:"error_message,omitempty"`
}

type ImageAnalysisTaskDetail struct {
	TaskID        string         `json:"task_id"`
	AppName       string         `json:"app_name"`
	AppDir        string         `json:"app_dir"`
	Status        string         `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	WorkerID      *int           `json:"worker_id,omitempty"`
	Duration      *time.Duration `json:"duration,omitempty"`
	ImagesCount   int            `json:"images_count"`
	AnalyzedCount int            `json:"analyzed_count,omitempty"`
	Error         string         `json:"error,omitempty"`
	ErrorStep     string         `json:"error_step,omitempty"`
	Images        []string       `json:"images,omitempty"`
}
