package appinfo

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/settings"
	"market/internal/v2/types"

	"github.com/golang/glog"
)

// Hydrator manages the hydration process with task queue and workers
type Hydrator struct {
	steps           []hydrationfn.HydrationStep
	cache           *types.CacheData
	settingsManager *settings.SettingsManager
	cacheManager    *CacheManager // Added cache manager for database sync
	taskQueue       chan *hydrationfn.HydrationTask
	workerCount     int
	stopChan        chan struct{}
	isRunning       atomic.Bool // Changed to atomic.Bool for better performance

	// Task tracking
	activeTasks    map[string]*hydrationfn.HydrationTask
	completedTasks map[string]*hydrationfn.HydrationTask
	failedTasks    map[string]*hydrationfn.HydrationTask
	taskMutex      sync.RWMutex

	// Cache access mutex for unified lock strategy - removed, use CacheManager.mutex instead

	// Batch completion tracking
	batchCompletionQueue chan string   // Queue for completed tasks
	completedTaskCount   int64         // Total completed tasks counter
	lastSyncTime         time.Time     // Last database sync time
	syncInterval         time.Duration // Interval for database sync

	// Metrics
	totalTasksProcessed int64
	totalTasksSucceeded int64
	totalTasksFailed    int64

	// Memory monitoring
	lastMemoryCheck     time.Time
	memoryCheckInterval time.Duration

	// Worker status tracking
	workerStatus      map[int]*WorkerStatus // Worker ID -> WorkerStatus
	workerStatusMutex sync.RWMutex

	// Task history (keep recent completed/failed tasks)
	recentCompletedTasks []*TaskHistoryEntry // Most recent completed tasks
	recentFailedTasks    []*TaskHistoryEntry // Most recent failed tasks
	maxHistorySize       int                 // Maximum number of tasks to keep in history
}

// WorkerStatus represents the status of a worker
type WorkerStatus struct {
	WorkerID     int       `json:"worker_id"`
	IsIdle       bool      `json:"is_idle"`
	CurrentTask  *TaskInfo `json:"current_task,omitempty"`
	LastActivity time.Time `json:"last_activity"`
}

// TaskInfo represents simplified task information for status display
type TaskInfo struct {
	TaskID      string    `json:"task_id"`
	AppID       string    `json:"app_id"`
	AppName     string    `json:"app_name"`
	UserID      string    `json:"user_id"`
	SourceID    string    `json:"source_id"`
	CurrentStep string    `json:"current_step"`
	StepIndex   int       `json:"step_index"`
	TotalSteps  int       `json:"total_steps"`
	Progress    float64   `json:"progress"` // 0-100
	StartedAt   time.Time `json:"started_at"`
	Status      string    `json:"status"`
}

// TaskHistoryEntry represents a task in history
type TaskHistoryEntry struct {
	TaskID      string        `json:"task_id"`
	AppID       string        `json:"app_id"`
	AppName     string        `json:"app_name"`
	UserID      string        `json:"user_id"`
	SourceID    string        `json:"source_id"`
	Status      string        `json:"status"` // completed, failed
	FailedStep  string        `json:"failed_step,omitempty"`
	ErrorMsg    string        `json:"error_msg,omitempty"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at"`
	Duration    time.Duration `json:"duration"`
}

// NewHydrator creates a new hydrator with the given configuration
func NewHydrator(cache *types.CacheData, settingsManager *settings.SettingsManager, cacheManager *CacheManager, config HydratorConfig) *Hydrator {
	if cacheManager == nil {
		glog.V(3).Infof("cacheManager must not be nil when creating Hydrator")
	}

	hydrator := &Hydrator{
		steps:                make([]hydrationfn.HydrationStep, 0),
		cache:                cache,
		settingsManager:      settingsManager,
		cacheManager:         cacheManager,
		taskQueue:            make(chan *hydrationfn.HydrationTask, config.QueueSize),
		workerCount:          config.WorkerCount,
		stopChan:             make(chan struct{}),
		isRunning:            atomic.Bool{}, // Initialize atomic.Bool
		activeTasks:          make(map[string]*hydrationfn.HydrationTask),
		completedTasks:       make(map[string]*hydrationfn.HydrationTask),
		failedTasks:          make(map[string]*hydrationfn.HydrationTask),
		batchCompletionQueue: make(chan string, 100), // Buffer for completed task IDs
		syncInterval:         30 * time.Second,       // Default sync interval
		memoryCheckInterval:  5 * time.Minute,
		lastMemoryCheck:      time.Now(),
		workerStatus:         make(map[int]*WorkerStatus),
		recentCompletedTasks: make([]*TaskHistoryEntry, 0),
		recentFailedTasks:    make([]*TaskHistoryEntry, 0),
		maxHistorySize:       50, // Keep last 50 completed and 50 failed tasks
	}

	// Add default steps

	hydrator.AddStep(hydrationfn.NewTaskForApiStep())
	// Payment information processing step: call payment logic for validation and status query
	hydrator.AddStep(hydrationfn.NewTaskForPaymentStep())

	// hydrator.AddStep(hydrationfn.NewSourceChartStep())
	// hydrator.AddStep(hydrationfn.NewRenderedChartStep())
	// hydrator.AddStep(hydrationfn.NewCustomParamsUpdateStep())
	// hydrator.AddStep(hydrationfn.NewImageAnalysisStep())
	// hydrator.AddStep(hydrationfn.NewDatabaseUpdateStep())

	return hydrator
}

// HydratorConfig contains configuration for the hydrator
type HydratorConfig struct {
	QueueSize   int `json:"queue_size"`   // Task queue size
	WorkerCount int `json:"worker_count"` // Number of worker goroutines
}

// DefaultHydratorConfig returns default configuration
func DefaultHydratorConfig() HydratorConfig {
	return HydratorConfig{
		QueueSize:   1000,
		WorkerCount: 1,
	}
}

// AddStep adds a hydration step to the hydrator
func (h *Hydrator) AddStep(step hydrationfn.HydrationStep) {
	h.steps = append(h.steps, step)
}

// Start begins the hydration process in passive mode (Pipeline handles scheduling)
func (h *Hydrator) Start(ctx context.Context) error {
	if h.isRunning.Load() {
		return fmt.Errorf("hydrator is already running")
	}
	h.isRunning.Store(true)

	glog.V(3).Infof("Starting hydrator with %d steps (passive mode, Pipeline handles scheduling)", len(h.steps))

	go h.batchCompletionProcessor(ctx)

	if h.cacheManager != nil {
		go h.databaseSyncMonitor(ctx)
	}

	return nil
}

// Stop stops the hydration process
func (h *Hydrator) Stop() {
	if !h.isRunning.Load() {
		return
	}

	glog.V(4).Info("Stopping hydrator...")
	close(h.stopChan)
	h.isRunning.Store(false)
}

// IsRunning returns whether the hydrator is currently running
func (h *Hydrator) IsRunning() bool {
	return h.isRunning.Load()
}

func (h *Hydrator) cleanupTaskResources(task *hydrationfn.HydrationTask) {
	if sourceChartPath, exists := task.ChartData["source_chart_path"].(string); exists {
		if err := os.Remove(sourceChartPath); err != nil {
			glog.Errorf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}
	task.ChartData = make(map[string]interface{})
	task.DatabaseUpdateData = make(map[string]interface{})
	task.AppData = make(map[string]interface{})
}

// isAppHydrationComplete checks if an app has completed all hydration steps
func (h *Hydrator) isAppHydrationComplete(pendingData *types.AppInfoLatestPendingData) bool {

	// if utils.IsPublicEnvironment() {
	// 	return true
	// }

	if pendingData == nil {
		glog.V(3).Infof("isAppHydrationComplete: pendingData is nil")
		return false
	}

	appID := ""
	appName := ""
	if pendingData.RawData != nil {
		appID = pendingData.RawData.AppID
		if appID == "" {
			appID = pendingData.RawData.ID
		}
		appName = pendingData.RawData.Name
	}

	glog.V(3).Infof("DEBUG: isAppHydrationComplete checking appID=%s(%s), RawPackage=%s, RenderedPackage=%s",
		appID, appName, pendingData.RawPackage, pendingData.RenderedPackage)

	if pendingData.RawPackage == "" {
		glog.Infof("DEBUG: isAppHydrationComplete RETURNING FALSE - RawPackage is empty for appID=%s(%s)", appID, appName)
		return false
	}

	if pendingData.RenderedPackage == "" {
		glog.Infof("DEBUG: isAppHydrationComplete RETURNING FALSE - RenderedPackage is empty for appID=%s(%s)", appID, appName)
		return false
	}

	if pendingData.AppInfo == nil {
		glog.Infof("DEBUG: isAppHydrationComplete RETURNING FALSE - AppInfo is nil for appID=%s(%s)", appID, appName)
		return false
	}

	imageAnalysis := pendingData.AppInfo.ImageAnalysis
	if imageAnalysis == nil {
		glog.Infof("DEBUG: isAppHydrationComplete RETURNING FALSE - ImageAnalysis is nil for appID=%s(%s)", appID, appName)
		return false
	}

	if imageAnalysis.TotalImages > 0 {
		glog.Infof("DEBUG: isAppHydrationComplete RETURNING TRUE - TotalImages > 0 for appID=%s(%s), TotalImages: %d", appID, appName, imageAnalysis.TotalImages)
		return true
	}

	if imageAnalysis.TotalImages == 0 && imageAnalysis.Images != nil {
		glog.Infof("DEBUG: isAppHydrationComplete RETURNING TRUE - TotalImages=0 but Images not nil for appID=%s(%s), Images: %v", appID, appName, imageAnalysis.Images)
		return true
	}

	glog.V(2).Infof("DEBUG: isAppHydrationComplete RETURNING FALSE - ImageAnalysis incomplete for appID=%s(%s), TotalImages: %d, Images: %v", appID, appName, imageAnalysis.TotalImages, imageAnalysis.Images)
	return false
}

// convertApplicationInfoEntryToMap converts ApplicationInfoEntry to map for task creation
func (h *Hydrator) convertApplicationInfoEntryToMap(entry *types.ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		return make(map[string]interface{})
	}

	// Create a safe map without potential circular references
	result := map[string]interface{}{
		"id":          entry.ID,
		"name":        entry.Name,
		"cfgType":     entry.CfgType,
		"chartName":   entry.ChartName,
		"icon":        entry.Icon,
		"description": entry.Description,
		"appID":       entry.AppID,
		"title":       entry.Title,
		"version":     entry.Version,
		"apiVersion":  entry.ApiVersion,
		"categories":  entry.Categories,
		"versionName": entry.VersionName,

		"fullDescription":    entry.FullDescription,
		"upgradeDescription": entry.UpgradeDescription,
		"promoteImage":       entry.PromoteImage,
		"promoteVideo":       entry.PromoteVideo,
		"subCategory":        entry.SubCategory,
		"locale":             entry.Locale,
		"developer":          entry.Developer,
		"requiredMemory":     entry.RequiredMemory,
		"requiredDisk":       entry.RequiredDisk,
		"supportClient":      entry.SupportClient,
		"supportArch":        entry.SupportArch,
		"requiredGPU":        entry.RequiredGPU,
		"requiredCPU":        entry.RequiredCPU,
		"rating":             entry.Rating,
		"target":             entry.Target,
		"permission":         entry.Permission,
		"entrances":          entry.Entrances,
		"ports":              entry.Ports,
		"tailscale":          entry.Tailscale,
		"middleware":         entry.Middleware,
		"options":            entry.Options,
		"subCharts":          entry.SubCharts,

		"submitter":     entry.Submitter,
		"doc":           entry.Doc,
		"website":       entry.Website,
		"featuredImage": entry.FeaturedImage,
		"sourceCode":    entry.SourceCode,
		"license":       entry.License,
		"legal":         entry.Legal,
		"i18n":          entry.I18n,

		"modelSize": entry.ModelSize,
		"namespace": entry.Namespace,
		"onlyAdmin": entry.OnlyAdmin,

		"lastCommitHash": entry.LastCommitHash,
		"createTime":     entry.CreateTime,
		"updateTime":     entry.UpdateTime,
		"appLabels":      entry.AppLabels,
		"count":          entry.Count,

		"screenshots": entry.Screenshots,
		"tags":        entry.Tags,
		"updated_at":  entry.UpdatedAt,

		"versionHistory": entry.VersionHistory,
	}

	// Safely copy metadata without potential circular references
	if entry.Metadata != nil {
		safeMetadata := h.createSafeMetadataCopy(entry.Metadata)
		result["metadata"] = safeMetadata
	}

	return result
}

// createSafeMetadataCopy creates a safe copy of metadata to avoid circular references
func (h *Hydrator) createSafeMetadataCopy(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}

	safeCopy := make(map[string]interface{})
	visited := make(map[uintptr]bool)

	for key, value := range metadata {
		// Skip potential circular reference keys
		if key == "source_data" || key == "raw_data" || key == "app_info" ||
			key == "parent" || key == "self" || key == "circular_ref" ||
			key == "back_ref" || key == "loop" {
			continue
		}

		safeCopy[key] = h.deepCopyValue(value, visited)
	}

	return safeCopy
}

// deepCopyValue performs a deep copy of a value while avoiding circular references
func (h *Hydrator) deepCopyValue(value interface{}, visited map[uintptr]bool) interface{} {
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
			safeMap[k] = h.deepCopyValue(val, visited)
		}
		return safeMap
	case []map[string]interface{}:
		safeSlice := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if itemCopy := h.deepCopyValue(item, visited); itemCopy != nil {
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

// markTaskCompleted moves task from active to completed
func (h *Hydrator) markTaskCompleted(task *hydrationfn.HydrationTask, startedAt time.Time, duration time.Duration) {
	// Extract file path for cleanup before the lock
	var sourceChartPath string
	if path, ok := task.ChartData["source_chart_path"].(string); ok {
		sourceChartPath = path
	}

	h.taskMutex.Lock()
	delete(h.activeTasks, task.ID)

	// Clean up in-memory data under lock
	task.ChartData = make(map[string]interface{})
	task.DatabaseUpdateData = make(map[string]interface{})
	task.AppData = make(map[string]interface{})

	h.completedTasks[task.ID] = task

	h.totalTasksProcessed++
	h.totalTasksSucceeded++
	h.taskMutex.Unlock() // Unlock before channel send and I/O

	// Add to task history
	h.addToCompletedHistory(task, startedAt, duration)

	// Add to batch completion queue for processing
	select {
	case h.batchCompletionQueue <- task.ID:
		// Successfully queued for batch processing
	default:
		// Queue is full, log warning but don't block
		glog.V(3).Infof("Warning: batch completion queue is full, task %s not queued for processing", task.ID)
	}

	// Clean up file resources after releasing the lock
	if sourceChartPath != "" {
		if err := os.Remove(sourceChartPath); err != nil {
			glog.Errorf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}
}

// markTaskFailed moves task from active to failed
func (h *Hydrator) markTaskFailed(task *hydrationfn.HydrationTask, startedAt time.Time, duration time.Duration, failedStep string, errorMsg string) {
	// Extract file path for cleanup before the lock
	var sourceChartPath string
	if path, ok := task.ChartData["source_chart_path"].(string); ok {
		sourceChartPath = path
	}

	h.taskMutex.Lock()
	task.SetStatus(hydrationfn.TaskStatusFailed)
	delete(h.activeTasks, task.ID)

	// Limit the size of failed tasks map
	maxFailedTasks := 10
	if len(h.failedTasks) >= maxFailedTasks {
		// Remove oldest failed tasks
		oldestTaskID := ""
		oldestTime := time.Now()
		for id, failedTask := range h.failedTasks {
			if failedTask.UpdatedAt.Before(oldestTime) {
				oldestTime = failedTask.UpdatedAt
				oldestTaskID = id
			}
		}
		if oldestTaskID != "" {
			delete(h.failedTasks, oldestTaskID)
		}
	}

	// Clean up in-memory data under lock
	task.ChartData = make(map[string]interface{})
	task.DatabaseUpdateData = make(map[string]interface{})
	task.AppData = make(map[string]interface{})

	h.failedTasks[task.ID] = task

	h.totalTasksProcessed++
	h.totalTasksFailed++
	h.taskMutex.Unlock() // Unlock before I/O

	// Add to task history
	h.addToFailedHistory(task, startedAt, duration, failedStep, errorMsg)

	// Clean up file resources after releasing the lock
	if sourceChartPath != "" {
		if err := os.Remove(sourceChartPath); err != nil {
			glog.Errorf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}
}

// addToCompletedHistory adds a task to the completed tasks history
func (h *Hydrator) addToCompletedHistory(task *hydrationfn.HydrationTask, startedAt time.Time, duration time.Duration) {
	h.workerStatusMutex.Lock()
	defer h.workerStatusMutex.Unlock()

	entry := &TaskHistoryEntry{
		TaskID:      task.ID,
		AppID:       task.AppID,
		AppName:     task.AppName,
		UserID:      task.UserID,
		SourceID:    task.SourceID,
		Status:      "completed",
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Duration:    duration,
	}

	// Append and limit size
	h.recentCompletedTasks = append([]*TaskHistoryEntry{entry}, h.recentCompletedTasks...)
	if len(h.recentCompletedTasks) > h.maxHistorySize {
		h.recentCompletedTasks = h.recentCompletedTasks[:h.maxHistorySize]
	}
}

// addToFailedHistory adds a task to the failed tasks history
func (h *Hydrator) addToFailedHistory(task *hydrationfn.HydrationTask, startedAt time.Time, duration time.Duration, failedStep string, errorMsg string) {
	h.workerStatusMutex.Lock()
	defer h.workerStatusMutex.Unlock()

	entry := &TaskHistoryEntry{
		TaskID:      task.ID,
		AppID:       task.AppID,
		AppName:     task.AppName,
		UserID:      task.UserID,
		SourceID:    task.SourceID,
		Status:      "failed",
		FailedStep:  failedStep,
		ErrorMsg:    errorMsg,
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Duration:    duration,
	}

	// Append and limit size
	h.recentFailedTasks = append([]*TaskHistoryEntry{entry}, h.recentFailedTasks...)
	if len(h.recentFailedTasks) > h.maxHistorySize {
		h.recentFailedTasks = h.recentFailedTasks[:h.maxHistorySize]
	}
}

// moveTaskToRenderFailed moves a failed task to the render failed list in cache
func (h *Hydrator) moveTaskToRenderFailed(task *hydrationfn.HydrationTask, failureReason string, failureStep string) {
	if h.cacheManager == nil {
		glog.V(3).Infof("Warning: Cache manager not set, cannot move task to render failed list")
		return
	}

	pendingData := h.cacheManager.FindPendingDataForApp(task.UserID, task.SourceID, task.AppID)

	if pendingData == nil {
		glog.V(3).Infof("Warning: Pending data not found for task: %s, app: %s", task.ID, task.AppID)
		return
	}

	// Create render failed data from pending data
	failedData := types.NewAppRenderFailedDataFromPending(pendingData, failureReason, failureStep, task.RetryCount)

	// Add to render failed list in cache
	if err := h.cacheManager.SetAppData(task.UserID, task.SourceID, types.AppRenderFailed, map[string]interface{}{
		"failed_app": failedData,
	}); err != nil {
		glog.Errorf("Failed to add task to render failed list: %s, error: %v", task.ID, err)
		return
	}

	glog.V(2).Infof("Successfully moved task %s (app: %s/%s/%s) to render failed list with reason: %s, step: %s",
		task.ID, task.AppID, task.AppName, task.AppVersion, failureReason, failureStep)

	// Remove from pending list
	h.removeFromPendingList(task.UserID, task.SourceID, task.AppID, task.AppName, task.AppVersion)
}

// removeFromPendingList removes an app from the pending list
func (h *Hydrator) removeFromPendingList(userID, sourceID, appID, appName, appVersion string) {
	if h.cacheManager == nil {
		glog.V(3).Infof("Warning: CacheManager not available for removeFromPendingList")
		return
	}
	h.cacheManager.RemoveFromPendingList(userID, sourceID, appID)
	glog.V(2).Infof("Removed app %s(%s) from pending list for user: %s, source: %s", appID, appName, userID, sourceID)
}

// GetMetrics returns hydrator metrics
func (h *Hydrator) GetMetrics() HydratorMetrics {
	h.taskMutex.RLock()
	activeTasksList := make([]*TaskInfo, 0, len(h.activeTasks))
	for _, task := range h.activeTasks {
		if task != nil {
			activeTasksList = append(activeTasksList, h.taskToTaskInfo(task))
		}
	}
	activeCount := int64(len(h.activeTasks))
	completedCount := int64(len(h.completedTasks))
	failedCount := int64(len(h.failedTasks))
	h.taskMutex.RUnlock()

	h.workerStatusMutex.RLock()
	workers := h.getWorkerStatusList()
	h.workerStatusMutex.RUnlock()

	return HydratorMetrics{
		TotalTasksProcessed:  h.totalTasksProcessed,
		TotalTasksSucceeded:  h.totalTasksSucceeded,
		TotalTasksFailed:     h.totalTasksFailed,
		ActiveTasksCount:     activeCount,
		CompletedTasksCount:  completedCount,
		FailedTasksCount:     failedCount,
		QueueLength:          int64(len(h.taskQueue)),
		ActiveTasks:          activeTasksList,
		RecentCompletedTasks: h.getRecentCompletedTasks(),
		RecentFailedTasks:    h.getRecentFailedTasks(),
		Workers:              workers,
	}
}

// taskToTaskInfo converts a HydrationTask to TaskInfo
func (h *Hydrator) taskToTaskInfo(task *hydrationfn.HydrationTask) *TaskInfo {
	totalSteps := len(h.steps)
	if totalSteps == 0 {
		totalSteps = task.TotalSteps
	}

	var progress float64
	if totalSteps > 0 && task.CurrentStep >= 0 {
		progress = float64(task.CurrentStep) / float64(totalSteps) * 100.0
		if progress > 100.0 {
			progress = 100.0
		}
	}

	currentStepName := ""
	if task.CurrentStep >= 0 && task.CurrentStep < len(h.steps) {
		currentStepName = h.steps[task.CurrentStep].GetStepName()
	}

	return &TaskInfo{
		TaskID:      task.ID,
		AppID:       task.AppID,
		AppName:     task.AppName,
		UserID:      task.UserID,
		SourceID:    task.SourceID,
		CurrentStep: currentStepName,
		StepIndex:   task.CurrentStep,
		TotalSteps:  totalSteps,
		Progress:    progress,
		StartedAt:   task.CreatedAt,
		Status:      string(task.GetStatus()),
	}
}

// getWorkerStatusList returns a list of all worker statuses
func (h *Hydrator) getWorkerStatusList() []*WorkerStatus {
	workers := make([]*WorkerStatus, 0, len(h.workerStatus))
	for i := 0; i < h.workerCount; i++ {
		if status, ok := h.workerStatus[i]; ok {
			workers = append(workers, status)
		} else {
			// Worker not in map means it's idle
			workers = append(workers, &WorkerStatus{
				WorkerID:     i,
				IsIdle:       true,
				LastActivity: time.Now(),
			})
		}
	}
	return workers
}

// getRecentCompletedTasks returns recent completed tasks (thread-safe)
func (h *Hydrator) getRecentCompletedTasks() []*TaskHistoryEntry {
	h.workerStatusMutex.RLock()
	defer h.workerStatusMutex.RUnlock()

	result := make([]*TaskHistoryEntry, len(h.recentCompletedTasks))
	copy(result, h.recentCompletedTasks)
	return result
}

// getRecentFailedTasks returns recent failed tasks (thread-safe)
func (h *Hydrator) getRecentFailedTasks() []*TaskHistoryEntry {
	h.workerStatusMutex.RLock()
	defer h.workerStatusMutex.RUnlock()

	result := make([]*TaskHistoryEntry, len(h.recentFailedTasks))
	copy(result, h.recentFailedTasks)
	return result
}

// HydratorMetrics contains metrics for the hydrator
type HydratorMetrics struct {
	TotalTasksProcessed  int64               `json:"total_tasks_processed"`
	TotalTasksSucceeded  int64               `json:"total_tasks_succeeded"`
	TotalTasksFailed     int64               `json:"total_tasks_failed"`
	ActiveTasksCount     int64               `json:"active_tasks_count"`
	CompletedTasksCount  int64               `json:"completed_tasks_count"`
	FailedTasksCount     int64               `json:"failed_tasks_count"`
	QueueLength          int64               `json:"queue_length"`
	ActiveTasks          []*TaskInfo         `json:"active_tasks,omitempty"`           // Current processing tasks
	RecentCompletedTasks []*TaskHistoryEntry `json:"recent_completed_tasks,omitempty"` // Recent completed tasks
	RecentFailedTasks    []*TaskHistoryEntry `json:"recent_failed_tasks,omitempty"`    // Recent failed tasks
	Workers              []*WorkerStatus     `json:"workers,omitempty"`                // Worker status list
}

// CreateDefaultHydrator creates a hydrator with default configuration
func CreateDefaultHydrator(cache *types.CacheData, settingsManager *settings.SettingsManager, cacheManager *CacheManager) *Hydrator {
	config := DefaultHydratorConfig()
	return NewHydrator(cache, settingsManager, cacheManager, config)
}

// SetCacheManager removed: cacheManager must be provided at NewHydrator

// batchCompletionProcessor processes completed tasks in batches
func (h *Hydrator) batchCompletionProcessor(ctx context.Context) {
	glog.V(3).Info("Batch completion processor started")
	defer glog.V(3).Info("Batch completion processor stopped")

	ticker := time.NewTicker(time.Second * 10) // Process every 10 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case taskID := <-h.batchCompletionQueue:
			h.processCompletedTask(taskID)
			// Add memory monitoring after processing completed tasks
			h.monitorMemoryUsage()
		case <-ticker.C:
			h.processBatchCompletions()
			// Add memory monitoring after batch processing
			h.monitorMemoryUsage()
		}
	}
}

// databaseSyncMonitor monitors for database synchronization needs
func (h *Hydrator) databaseSyncMonitor(ctx context.Context) {
	glog.V(3).Info("Database sync monitor started")
	defer glog.V(3).Info("Database sync monitor stopped")

	ticker := time.NewTicker(h.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case <-ticker.C:
			h.checkAndSyncToDatabase()
		}
	}
}

// processCompletedTask processes a single completed task
func (h *Hydrator) processCompletedTask(taskID string) {
	h.taskMutex.RLock()
	task, exists := h.completedTasks[taskID]
	h.taskMutex.RUnlock()

	if !exists {
		glog.V(3).Infof("Warning: completed task %s not found in completed tasks map", taskID)
		return
	}

	glog.V(3).Infof("Processing completed task: %s for app: %s", taskID, task.AppID)
	// Additional processing can be added here if needed
}

// processBatchCompletions processes completed tasks in batches
func (h *Hydrator) processBatchCompletions() {
	h.taskMutex.RLock()
	currentCompleted := h.totalTasksSucceeded
	h.taskMutex.RUnlock()

	// Check if significant number of tasks completed since last sync
	if currentCompleted > h.completedTaskCount+10 { // Trigger sync after 10 more completions
		glog.V(3).Infof("Batch completion detected: %d tasks completed since last check",
			currentCompleted-h.completedTaskCount)
		h.completedTaskCount = currentCompleted
		h.triggerDatabaseSync()
	}
}

// checkAndSyncToDatabase checks if database sync is needed and performs it
func (h *Hydrator) checkAndSyncToDatabase() {
	if h.cacheManager == nil {
		glog.V(3).Infof("Warning: Cache manager not set, skipping database sync")
		return
	}

	h.taskMutex.RLock()
	completedCount := len(h.completedTasks)
	h.taskMutex.RUnlock()

	if completedCount == 0 {
		return // No completed tasks to sync
	}

	// Check if enough time has passed since last sync
	if time.Since(h.lastSyncTime) >= h.syncInterval {
		glog.V(3).Infof("Periodic database sync triggered - %d completed tasks to sync", completedCount)
		h.triggerDatabaseSync()
	}
}

// triggerDatabaseSync triggers synchronization of cache data to database
func (h *Hydrator) triggerDatabaseSync() {
	if h.cacheManager == nil {
		glog.V(3).Infof("Warning: Cache manager not set, cannot sync to database")
		return
	}

	glog.V(2).Infof("Triggering database synchronization")

	// Force sync all cache data to Redis/database
	if err := h.cacheManager.ForceSync(); err != nil {
		glog.Errorf("Error during database sync: %v", err)
	} else {
		glog.V(3).Infof("Database sync completed successfully")
		h.lastSyncTime = time.Now()

		// Optionally clean up old completed tasks to prevent memory growth
		h.cleanupOldCompletedTasks()
	}
}

// cleanupOldCompletedTasks removes old completed tasks from memory
func (h *Hydrator) cleanupOldCompletedTasks() {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()

	// Keep only the most recent 100 completed tasks
	maxCompletedTasks := 10
	if len(h.completedTasks) > maxCompletedTasks {
		// Convert to slice to sort by completion time
		tasks := make([]*hydrationfn.HydrationTask, 0, len(h.completedTasks))
		for _, task := range h.completedTasks {
			tasks = append(tasks, task)
		}

		// Sort by UpdatedAt time (most recent first)
		// Simple implementation: remove half of the tasks
		toRemove := len(h.completedTasks) - maxCompletedTasks/2
		removed := 0

		for taskID := range h.completedTasks {
			if removed >= toRemove {
				break
			}
			delete(h.completedTasks, taskID)
			removed++
		}

		glog.V(3).Infof("Cleaned up %d old completed tasks from memory", removed)
	}
}

// monitorMemoryUsage monitors memory usage and logs warnings if it's too high
func (h *Hydrator) monitorMemoryUsage() {
	if time.Since(h.lastMemoryCheck) < h.memoryCheckInterval {
		return
	}

	h.lastMemoryCheck = time.Now()

	h.taskMutex.RLock()
	activeCount := len(h.activeTasks)
	completedCount := len(h.completedTasks)
	failedCount := len(h.failedTasks)
	h.taskMutex.RUnlock()

	// Log memory usage metrics
	glog.V(3).Infof("Memory usage metrics - Active tasks: %d, Completed tasks: %d, Failed tasks: %d",
		activeCount, completedCount, failedCount)

	// If we have too many tasks, trigger cleanup
	if activeCount > 100 || completedCount > 100 {
		glog.V(3).Infof("Warning: High number of tasks detected, triggering cleanup")
		h.cleanupOldTasks()
	}
}

// cleanupOldTasks cleans up old tasks from all task maps
func (h *Hydrator) cleanupOldTasks() {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()

	now := time.Now()

	// Clean up active tasks older than 1 hour
	for id, task := range h.activeTasks {
		if now.Sub(task.UpdatedAt) > time.Hour {
			glog.V(3).Infof("Cleaning up stale active task: %s", id)
			h.cleanupTaskResources(task)
			delete(h.activeTasks, id)
		}
	}

	// Clean up completed tasks (keep only last 50)
	if len(h.completedTasks) > 50 {
		// Convert to slice to sort by completion time
		tasks := make([]*hydrationfn.HydrationTask, 0, len(h.completedTasks))
		for _, task := range h.completedTasks {
			tasks = append(tasks, task)
		}

		// Sort by UpdatedAt time (most recent first)
		sort.Slice(tasks, func(i, j int) bool {
			return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
		})

		// Keep only the most recent 50 tasks
		removed := 0

		for _, task := range tasks[50:] {
			h.cleanupTaskResources(task)
			delete(h.completedTasks, task.ID)
			removed++
		}

		glog.V(3).Infof("Cleaned up %d old completed tasks", removed)
	}

	// Failed tasks are already limited to 10
	if len(h.failedTasks) > 10 {
		// Convert to slice to sort by failure time
		tasks := make([]*hydrationfn.HydrationTask, 0, len(h.failedTasks))
		for _, task := range h.failedTasks {
			tasks = append(tasks, task)
		}

		// Sort by LastFailureTime
		sort.Slice(tasks, func(i, j int) bool {
			if tasks[i].LastFailureTime == nil {
				return false
			}
			if tasks[j].LastFailureTime == nil {
				return true
			}
			return tasks[i].LastFailureTime.After(*tasks[j].LastFailureTime)
		})

		// Keep only the most recent 10 failed tasks
		removed := 0

		for _, task := range tasks[10:] {
			h.cleanupTaskResources(task)
			delete(h.failedTasks, task.ID)
			removed++
		}

		glog.V(3).Infof("Cleaned up %d old failed tasks", removed)
	}
}

// isAppInLatestQueue checks if an app already exists in the AppInfoLatest queue with version comparison
func (h *Hydrator) isAppInLatestQueue(userID, sourceID, appID, appName, version string) bool {
	glog.V(3).Infof("DEBUG: isAppInLatestQueue checking appID=%s %s, version=%s for user=%s, source=%s", appID, appName, version, userID, sourceID)

	if h.cacheManager == nil {
		glog.V(3).Infof("Warning: CacheManager not available for isAppInLatestQueue")
		return false
	}

	result := h.cacheManager.IsAppInLatestQueue(userID, sourceID, appID, version)
	glog.V(3).Infof("DEBUG: isAppInLatestQueue returning %v for appID=%s, version=%s, user=%s, source=%s", result, appID, version, userID, sourceID)
	return result
}

// isAppInRenderFailedList checks if an app already exists in the render failed list
func (h *Hydrator) isAppInRenderFailedList(userID, sourceID, appID, appName string) bool {
	if h.cacheManager == nil {
		glog.V(2).Infof("Warning: CacheManager not available for isAppInRenderFailedList")
		return false
	}
	return h.cacheManager.IsAppInRenderFailedList(userID, sourceID, appID)
}
