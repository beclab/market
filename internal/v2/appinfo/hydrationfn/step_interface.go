package hydrationfn

import (
	"context"
	"fmt"
	"reflect"
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
	SourceType  string                 `json:"source_type"`  // Source type
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
	Cache           *types.CacheData            `json:"-"` // Cache data reference
	CacheManager    types.CacheManagerInterface `json:"-"` // CacheManager for unified lock strategy
	SettingsManager *settings.SettingsManager   `json:"-"` // Settings manager

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

	// Create a safe copy of app data to avoid circular references
	safeAppData := createSafeAppDataCopy(appData)

	marketSources := settingsManager.GetActiveMarketSources()

	sourceType := ""
	for _, source := range marketSources {
		if source.ID == sourceID {
			sourceType = string(source.Type)
			break
		}
	}

	return &HydrationTask{
		ID:                 taskID,
		UserID:             userID,
		SourceID:           sourceID,
		SourceType:         sourceType,
		AppID:              appID,
		AppName:            appName,
		AppVersion:         appVersion,
		AppData:            safeAppData,
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

// NewHydrationTaskWithManager creates a new hydration task with CacheManager
func NewHydrationTaskWithManager(userID, sourceID, appID string, appData map[string]interface{}, cache *types.CacheData, cacheManager types.CacheManagerInterface, settingsManager *settings.SettingsManager) *HydrationTask {
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

	// Create a safe copy of app data to avoid circular references
	safeAppData := createSafeAppDataCopy(appData)

	marketSources := settingsManager.GetActiveMarketSources()

	sourceType := ""
	for _, source := range marketSources {
		if source.ID == sourceID {
			sourceType = string(source.Type)
			break
		}
	}

	return &HydrationTask{
		ID:                 taskID,
		UserID:             userID,
		SourceID:           sourceID,
		SourceType:         sourceType,
		AppID:              appID,
		AppName:            appName,
		AppVersion:         appVersion,
		AppData:            safeAppData,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		Status:             TaskStatusPending,
		CurrentStep:        0,
		TotalSteps:         3, // Default 3 steps: source chart, rendered chart, database update
		RetryCount:         0,
		MaxRetries:         3,
		Cache:              cache,
		CacheManager:       cacheManager,
		SettingsManager:    settingsManager,
		ChartData:          make(map[string]interface{}),
		DatabaseUpdateData: make(map[string]interface{}),
	}
}

// createSafeAppDataCopy creates a safe copy of app data to avoid circular references
func createSafeAppDataCopy(appData map[string]interface{}) map[string]interface{} {
	if appData == nil {
		return make(map[string]interface{})
	}

	safeCopy := make(map[string]interface{})
	visited := make(map[uintptr]bool)

	for key, value := range appData {
		// Skip potential circular reference keys
		if key == "source_data" || key == "raw_data" || key == "app_info" ||
			key == "parent" || key == "self" || key == "circular_ref" ||
			key == "back_ref" || key == "loop" {
			continue
		}

		safeCopy[key] = deepCopyAppDataValue(value, visited)
	}

	return safeCopy
}

// deepCopyAppDataValue performs a deep copy of a value while avoiding circular references
func deepCopyAppDataValue(value interface{}, visited map[uintptr]bool) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string, int, int64, float64, bool:
		return v
	case []string:
		return append([]string{}, v...)
	case []interface{}:
		// Only copy simple types from interface slice
		safeSlice := make([]interface{}, 0, len(v))
		for _, item := range v {
			switch item.(type) {
			case string, int, int64, float64, bool:
				safeSlice = append(safeSlice, item)
			default:
				// Skip complex slice items to avoid circular references
			}
		}
		return safeSlice
	case map[string]interface{}:
		// Check for circular references using pointer
		ptr := reflect.ValueOf(v).Pointer()
		if visited[ptr] {
			return nil // Skip circular reference
		}
		visited[ptr] = true
		defer delete(visited, ptr)

		safeMap := make(map[string]interface{})
		for k, val := range v {
			// Skip potential circular reference keys
			if k == "source_data" || k == "raw_data" || k == "app_info" ||
				k == "parent" || k == "self" || k == "circular_ref" ||
				k == "back_ref" || k == "loop" {
				continue
			}
			safeMap[k] = deepCopyAppDataValue(val, visited)
		}
		return safeMap
	case []map[string]interface{}:
		safeSlice := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if itemCopy := deepCopyAppDataValue(item, visited); itemCopy != nil {
				if itemMap, ok := itemCopy.(map[string]interface{}); ok {
					safeSlice = append(safeSlice, itemMap)
				}
			}
		}
		return safeSlice
	default:
		// For other types, return nil to avoid potential circular references
		return nil
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
