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

// RuntimeSnapshot represents a complete snapshot of the system runtime state
type RuntimeSnapshot struct {
	Timestamp  time.Time                   `json:"timestamp"`
	AppStates  map[string]*AppFlowState    `json:"app_states"` // key: userID:sourceID:appName
	Tasks      map[string]*TaskState       `json:"tasks"`      // key: taskID
	Components map[string]*ComponentStatus `json:"components"` // key: component name
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
