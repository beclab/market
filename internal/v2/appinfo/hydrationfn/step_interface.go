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
type HydrationStep interface {
	// GetStepName returns the name of the step for logging and identification
	GetStepName() string

	// Execute performs the actual step logic
	Execute(ctx context.Context, task *HydrationTask) error

	// CanSkip determines if this step can be skipped based on task state
	CanSkip(ctx context.Context, task *HydrationTask) bool
}

// HydrationTask represents a single app hydration task
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
	SourceChartURL     string                 `json:"source_chart_url"`     // Source chart package URL
	RenderedChartURL   string                 `json:"rendered_chart_url"`   // Rendered chart package URL
	ChartData          map[string]interface{} `json:"chart_data"`           // Chart data
	DatabaseUpdateData map[string]interface{} `json:"database_update_data"` // Database update data

	// Context data
	Cache           *types.CacheData          `json:"-"` // Cache data reference
	SettingsManager *settings.SettingsManager `json:"-"` // Settings manager

	mutex sync.RWMutex `json:"-"` // Task mutex for thread safety

	// Task state
	Error           error
	LastFailureTime *time.Time // Time of last failure for cooldown period
}

// TaskStatus represents the status of a hydration task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"   // Task is waiting to be processed
	TaskStatusRunning   TaskStatus = "running"   // Task is currently being processed
	TaskStatusCompleted TaskStatus = "completed" // Task completed successfully
	TaskStatusFailed    TaskStatus = "failed"    // Task failed after all retries
	TaskStatusCancelled TaskStatus = "cancelled" // Task was cancelled
)

// NewHydrationTask creates a new hydration task
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
func generateTaskID(userID, sourceID, appID string) string {
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s_%s_%s_%d", userID, sourceID, appID, timestamp)
}

// SetStatus sets the task status in a thread-safe manner
func (ht *HydrationTask) SetStatus(status TaskStatus) {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.Status = status
	ht.UpdatedAt = time.Now()
}

// GetStatus returns the current task status
func (ht *HydrationTask) GetStatus() TaskStatus {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()
	return ht.Status
}

// IncrementStep increments the current step counter
func (ht *HydrationTask) IncrementStep() {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.CurrentStep++
	ht.UpdatedAt = time.Now()
}

// SetError sets the last error message and increments retry count
func (ht *HydrationTask) SetError(err error) {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.LastError = err.Error()
	ht.RetryCount++
	ht.UpdatedAt = time.Now()
}

// CanRetry returns true if the task can be retried
func (ht *HydrationTask) CanRetry() bool {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()
	return ht.RetryCount < ht.MaxRetries
}

// ResetForRetry resets the task for retry
func (ht *HydrationTask) ResetForRetry() {
	ht.mutex.Lock()
	defer ht.mutex.Unlock()
	ht.Status = TaskStatusPending
	ht.CurrentStep = 0
	ht.UpdatedAt = time.Now()
}

// IsCompleted returns true if the task is completed
func (ht *HydrationTask) IsCompleted() bool {
	ht.mutex.RLock()
	defer ht.mutex.RUnlock()
	return ht.Status == TaskStatusCompleted || ht.Status == TaskStatusFailed || ht.Status == TaskStatusCancelled
}
