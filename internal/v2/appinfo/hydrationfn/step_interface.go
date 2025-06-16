package hydrationfn

import (
	"context"
	"fmt"
	"sync"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// HydrationStep represents an abstract step in the hydration process
// HydrationStep 表示水合过程中的抽象步骤
type HydrationStep interface {
	// GetStepName returns the name of the step for logging and identification
	// GetStepName 返回步骤名称用于日志和识别
	GetStepName() string

	// Execute performs the actual step logic
	// Execute 执行实际的步骤逻辑
	Execute(ctx context.Context, task *HydrationTask) error

	// CanSkip determines if this step can be skipped based on task state
	// CanSkip 根据任务状态确定是否可以跳过此步骤
	CanSkip(ctx context.Context, task *HydrationTask) bool
}

// HydrationTask represents a single app hydration task
// HydrationTask 表示单个应用程序水合任务
type HydrationTask struct {
	ID          string                 `json:"id"`           // Unique task ID
	UserID      string                 `json:"user_id"`      // User ID
	SourceID    string                 `json:"source_id"`    // Source ID
	AppID       string                 `json:"app_id"`       // Application ID
	AppName     string                 `json:"app_name"`     // Application name
	AppVersion  string                 `json:"app_version"`  // Application version
	AppData     map[string]interface{} `json:"app_data"`     // Original app data
	CreatedAt   time.Time              `json:"created_at"`   // Task creation time
	UpdatedAt   time.Time              `json:"updated_at"`   // Task last update time
	Status      TaskStatus             `json:"status"`       // Task status
	CurrentStep int                    `json:"current_step"` // Current step index
	TotalSteps  int                    `json:"total_steps"`  // Total steps count
	RetryCount  int                    `json:"retry_count"`  // Retry attempts
	MaxRetries  int                    `json:"max_retries"`  // Maximum retry attempts
	LastError   string                 `json:"last_error"`   // Last error message

	// Step-specific data
	// 步骤特定数据
	SourceChartURL     string                 `json:"source_chart_url"`     // Source chart package URL
	RenderedChartURL   string                 `json:"rendered_chart_url"`   // Rendered chart package URL
	ChartData          map[string]interface{} `json:"chart_data"`           // Chart data
	DatabaseUpdateData map[string]interface{} `json:"database_update_data"` // Database update data

	// Context data
	// 上下文数据
	Cache           *types.CacheData          `json:"-"` // Cache data reference
	SettingsManager *settings.SettingsManager `json:"-"` // Settings manager

	mutex sync.RWMutex `json:"-"` // Task mutex for thread safety

	// Task state
	// 任务状态
	Error           error
	LastFailureTime *time.Time // Time of last failure for cooldown period
}

// TaskStatus represents the status of a hydration task
// TaskStatus 表示水合任务的状态
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"   // Task is waiting to be processed
	TaskStatusRunning   TaskStatus = "running"   // Task is currently being processed
	TaskStatusCompleted TaskStatus = "completed" // Task completed successfully
	TaskStatusFailed    TaskStatus = "failed"    // Task failed after all retries
	TaskStatusCancelled TaskStatus = "cancelled" // Task was cancelled
)

// NewHydrationTask creates a new hydration task
// NewHydrationTask 创建新的水合任务
func NewHydrationTask(userID, sourceID, appID string, appData map[string]interface{}, cache *types.CacheData, settingsManager *settings.SettingsManager) *HydrationTask {
	taskID := generateTaskID(userID, sourceID, appID)

	// Extract app info
	appName := ""
	appVersion := ""
	if name, ok := appData["name"].(string); ok {
		appName = name
	}
	if version, ok := appData["version"].(string); ok {
		appVersion = version
	}

	return &HydrationTask{
		ID:                 taskID,
		UserID:             userID,
		SourceID:           sourceID,
		AppID:              appID,
		AppName:            appName,
		AppVersion:         appVersion,
		AppData:            appData,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		Status:             TaskStatusPending,
		CurrentStep:        0,
		TotalSteps:         3, // Default 3 steps: source chart, rendered chart, database update
		RetryCount:         0,
		MaxRetries:         3,
		Cache:              cache,
		SettingsManager:    settingsManager,
		ChartData:          make(map[string]interface{}),
		DatabaseUpdateData: make(map[string]interface{}),
	}
}

// generateTaskID generates a unique task ID based on user, source, and app
// generateTaskID 基于用户、源和应用生成唯一任务ID
func generateTaskID(userID, sourceID, appID string) string {
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s_%s_%s_%d", userID, sourceID, appID, timestamp)
}

// SetStatus sets the task status in a thread-safe manner
// SetStatus 以线程安全的方式设置任务状态
func (ht *HydrationTask) SetStatus(status TaskStatus) {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.Status = status
	ht.UpdatedAt = time.Now()
}

// GetStatus returns the current task status
// GetStatus 返回当前任务状态
func (ht *HydrationTask) GetStatus() TaskStatus {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()
	return ht.Status
}

// IncrementStep increments the current step counter
// IncrementStep 增加当前步骤计数器
func (ht *HydrationTask) IncrementStep() {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.CurrentStep++
	ht.UpdatedAt = time.Now()
}

// SetError sets the last error message and increments retry count
// SetError 设置最后错误消息并增加重试计数
func (ht *HydrationTask) SetError(err error) {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.LastError = err.Error()
	ht.RetryCount++
	ht.UpdatedAt = time.Now()
}

// CanRetry returns true if the task can be retried
// CanRetry 如果任务可以重试则返回true
func (ht *HydrationTask) CanRetry() bool {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()
	return ht.RetryCount < ht.MaxRetries
}

// ResetForRetry resets the task for retry
// ResetForRetry 重置任务以进行重试
func (ht *HydrationTask) ResetForRetry() {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.Status = TaskStatusPending
	ht.CurrentStep = 0
	ht.UpdatedAt = time.Now()
}

// IsCompleted returns true if the task is completed
// IsCompleted 如果任务已完成则返回true
func (ht *HydrationTask) IsCompleted() bool {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()
	return ht.Status == TaskStatusCompleted || ht.Status == TaskStatusFailed || ht.Status == TaskStatusCancelled
}
