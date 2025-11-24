package runtime

import (
	"time"
)

// AppFlowStage represents the current stage of an app in its lifecycle
type AppFlowStage string

const (
	StageUnknown      AppFlowStage = "unknown"
	StageFetching     AppFlowStage = "fetching"     // Fetching app data
	StageHydrating    AppFlowStage = "hydrating"    // Hydrating app info
	StageInstalling   AppFlowStage = "installing"   // Installation in progress
	StageRunning      AppFlowStage = "running"      // App is running
	StageUpgrading    AppFlowStage = "upgrading"    // Upgrade in progress
	StageUninstalling AppFlowStage = "uninstalling" // Uninstallation in progress
	StageFailed       AppFlowStage = "failed"       // App operation failed
	StageStopped      AppFlowStage = "stopped"      // App is stopped
)

// AppFlowState represents the current flow state of an application
type AppFlowState struct {
	AppID       string                 `json:"app_id"`
	AppName     string                 `json:"app_name"`
	UserID      string                 `json:"user_id"`
	SourceID    string                 `json:"source_id"`
	Stage       AppFlowStage           `json:"stage"`
	StageDetail string                 `json:"stage_detail,omitempty"`
	Version     string                 `json:"version,omitempty"`
	CfgType     string                 `json:"cfg_type,omitempty"`
	Health      string                 `json:"health,omitempty"` // healthy, unhealthy, unknown
	LastUpdate  time.Time              `json:"last_update"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TaskState represents the state of a task
type TaskState struct {
	TaskID      string                 `json:"task_id"`
	Type        string                 `json:"type"`   // install, uninstall, upgrade, cancel, clone
	Status      string                 `json:"status"` // pending, running, completed, failed, canceled
	AppName     string                 `json:"app_name"`
	UserID      string                 `json:"user_id,omitempty"`
	OpID        string                 `json:"op_id,omitempty"`
	Progress    int                    `json:"progress,omitempty"` // 0-100
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Result      string                 `json:"result,omitempty"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ComponentStatus represents the status of a system component
type ComponentStatus struct {
	Name      string                 `json:"name"`
	Healthy   bool                   `json:"healthy"`
	Status    string                 `json:"status"` // running, stopped, error
	Metrics   map[string]interface{} `json:"metrics,omitempty"`
	LastCheck time.Time              `json:"last_check"`
	Message   string                 `json:"message,omitempty"`
}

// ChartRepoAppState represents app processing state from chart repo
type ChartRepoAppState struct {
	AppID       string               `json:"app_id"`
	AppName     string               `json:"app_name"`
	UserID      string               `json:"user_id"`
	SourceID    string               `json:"source_id"`
	State       string               `json:"state"` // processing, completed, failed
	CurrentStep *ChartRepoStep       `json:"current_step,omitempty"`
	Processing  *ChartRepoProcessing `json:"processing,omitempty"`
	Timestamps  *ChartRepoTimestamps `json:"timestamps,omitempty"`
	Error       *ChartRepoError      `json:"error,omitempty"`
	LastUpdate  time.Time            `json:"last_update"`
}

// ChartRepoStep represents a processing step
type ChartRepoStep struct {
	Name       string    `json:"name"`
	Index      int       `json:"index"`
	Total      int       `json:"total"`
	Status     string    `json:"status"` // running, completed, failed
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	RetryCount int       `json:"retry_count"`
}

// ChartRepoProcessing represents processing information
type ChartRepoProcessing struct {
	TaskID   string `json:"task_id"`
	Duration int64  `json:"duration"` // nanoseconds
}

// ChartRepoTimestamps represents timestamp information
type ChartRepoTimestamps struct {
	CreatedAt     time.Time `json:"created_at"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

// ChartRepoError represents error information
type ChartRepoError struct {
	Message string `json:"message"`
	Step    string `json:"step,omitempty"`
}

// ChartRepoImageState represents image download state from chart repo
type ChartRepoImageState struct {
	ImageName        string                `json:"image_name"`
	AppID            string                `json:"app_id,omitempty"`
	AppName          string                `json:"app_name,omitempty"`
	Status           string                `json:"status"` // not_downloaded, partially_downloaded, fully_downloaded, registry_error
	Architecture     string                `json:"architecture,omitempty"`
	TotalSize        int64                 `json:"total_size,omitempty"`
	DownloadedSize   int64                 `json:"downloaded_size,omitempty"`
	DownloadProgress float64               `json:"download_progress,omitempty"`
	LayerCount       int                   `json:"layer_count,omitempty"`
	DownloadedLayers int                   `json:"downloaded_layers,omitempty"`
	AnalysisStatus   string                `json:"analysis_status,omitempty"`
	AnalyzedAt       *time.Time            `json:"analyzed_at,omitempty"`
	ErrorMessage     string                `json:"error_message,omitempty"`
	Nodes            []*ChartRepoImageNode `json:"nodes,omitempty"`
	LastUpdate       time.Time             `json:"last_update"`
}

// ChartRepoImageNode represents image state on a specific node
type ChartRepoImageNode struct {
	NodeName         string  `json:"node_name"`
	Architecture     string  `json:"architecture,omitempty"`
	OS               string  `json:"os,omitempty"`
	TotalSize        int64   `json:"total_size,omitempty"`
	DownloadedSize   int64   `json:"downloaded_size,omitempty"`
	LayerCount       int     `json:"layer_count,omitempty"`
	DownloadedLayers int     `json:"downloaded_layers,omitempty"`
	Progress         float64 `json:"progress,omitempty"`
}

// ChartRepoTaskState represents task state from chart repo (hydrator tasks)
type ChartRepoTaskState struct {
	TaskID     string    `json:"task_id"`
	UserID     string    `json:"user_id"`
	SourceID   string    `json:"source_id"`
	AppID      string    `json:"app_id"`
	AppName    string    `json:"app_name"`
	Status     string    `json:"status"` // running, completed, failed
	StepName   string    `json:"step_name"`
	TotalSteps int       `json:"total_steps"`
	RetryCount int       `json:"retry_count"`
	LastError  string    `json:"last_error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ChartRepoStatus represents chart repo status information
type ChartRepoStatus struct {
	System     *ChartRepoSystemStatus `json:"system,omitempty"`
	Apps       []*ChartRepoAppState   `json:"apps,omitempty"`
	Images     []*ChartRepoImageState `json:"images,omitempty"`
	Tasks      *ChartRepoTasksStatus  `json:"tasks,omitempty"`
	LastUpdate time.Time              `json:"last_update"`
}

// ChartRepoSystemStatus represents system status from chart repo
type ChartRepoSystemStatus struct {
	Uptime        int64                  `json:"uptime,omitempty"`
	Version       string                 `json:"version,omitempty"`
	Components    map[string]interface{} `json:"components,omitempty"`
	ResourceUsage map[string]interface{} `json:"resource_usage,omitempty"`
	Hostname      string                 `json:"hostname,omitempty"`
}

// ChartRepoTasksStatus represents tasks status from chart repo
type ChartRepoTasksStatus struct {
	Hydrator      *ChartRepoHydratorStatus      `json:"hydrator,omitempty"`
	ImageAnalyzer *ChartRepoImageAnalyzerStatus `json:"image_analyzer,omitempty"`
}

// ChartRepoHydratorStatus represents hydrator status
type ChartRepoHydratorStatus struct {
	QueueLength    int                   `json:"queue_length"`
	ActiveTasks    int                   `json:"active_tasks"`
	CompletedTasks int                   `json:"completed_tasks"`
	FailedTasks    int                   `json:"failed_tasks"`
	WorkerCount    int                   `json:"worker_count"`
	Tasks          []*ChartRepoTaskState `json:"tasks,omitempty"`
	RecentFailed   []*ChartRepoTaskState `json:"recent_failed,omitempty"`
}

// ChartRepoImageAnalyzerStatus represents image analyzer status
type ChartRepoImageAnalyzerStatus struct {
	QueueLength    int `json:"queue_length"`
	ActiveWorkers  int `json:"active_workers"`
	CachedImages   int `json:"cached_images"`
	AnalyzingCount int `json:"analyzing_count"`
}

// RuntimeSnapshot represents a complete snapshot of the system runtime state
type RuntimeSnapshot struct {
	Timestamp  time.Time                   `json:"timestamp"`
	AppStates  map[string]*AppFlowState    `json:"app_states"`           // key: userID:sourceID:appName
	Tasks      map[string]*TaskState       `json:"tasks"`                // key: taskID
	Components map[string]*ComponentStatus `json:"components"`           // key: component name
	ChartRepo  *ChartRepoStatus            `json:"chart_repo,omitempty"` // Chart repo status
	Summary    *RuntimeSummary             `json:"summary"`
}

// RuntimeSummary provides aggregated statistics
type RuntimeSummary struct {
	TotalApps        int `json:"total_apps"`
	TotalTasks       int `json:"total_tasks"`
	PendingTasks     int `json:"pending_tasks"`
	RunningTasks     int `json:"running_tasks"`
	CompletedTasks   int `json:"completed_tasks"`
	FailedTasks      int `json:"failed_tasks"`
	HealthyApps      int `json:"healthy_apps"`
	UnhealthyApps    int `json:"unhealthy_apps"`
	ActiveComponents int `json:"active_components"`
}

// StateEvent represents a state change event
type StateEvent struct {
	Type      string                 `json:"type"`      // task_created, task_updated, app_stage_changed, component_updated
	EntityID  string                 `json:"entity_id"` // task_id or app_id
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}
