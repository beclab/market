package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
		log.Panicf("cacheManager must not be nil when creating Hydrator")
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

// Start begins the hydration process with workers
func (h *Hydrator) Start(ctx context.Context) error {
	if h.isRunning.Load() {
		return fmt.Errorf("hydrator is already running")
	}
	h.isRunning.Store(true)

	log.Printf("Starting hydrator with %d workers and %d steps", h.workerCount, len(h.steps))

	// Start worker goroutines
	for i := 0; i < h.workerCount; i++ {
		go h.worker(ctx, i)
	}

	// Start pending data monitor
	go h.pendingDataMonitor(ctx)

	// Start batch completion processor
	go h.batchCompletionProcessor(ctx)

	// Start database sync monitor if cache manager is available
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

	log.Println("Stopping hydrator...")
	close(h.stopChan)
	h.isRunning.Store(false)
}

// IsRunning returns whether the hydrator is currently running
func (h *Hydrator) IsRunning() bool {
	return h.isRunning.Load()
}

// EnqueueTask adds a task to the hydration queue
func (h *Hydrator) EnqueueTask(task *hydrationfn.HydrationTask) error {
	if !h.IsRunning() {
		return fmt.Errorf("hydrator is not running")
	}

	select {
	case h.taskQueue <- task:
		h.trackTask(task)
		log.Printf("Enqueued hydration task: %s for app: %s (user: %s, source: %s) - Queue length: %d",
			task.ID, task.AppID, task.UserID, task.SourceID, len(h.taskQueue))
		return nil
	default:
		log.Printf("ERROR: Task queue is full! Cannot enqueue task: %s for app: %s (user: %s, source: %s) - Queue length: %d",
			task.ID, task.AppID, task.UserID, task.SourceID, len(h.taskQueue))
		return fmt.Errorf("task queue is full")
	}
}

// worker processes tasks from the queue
func (h *Hydrator) worker(ctx context.Context, workerID int) {
	log.Printf("Hydration worker %d started", workerID)

	// Initialize worker status
	h.updateWorkerStatus(workerID, nil, true)

	defer func() {
		// Mark worker as idle when stopping
		h.updateWorkerStatus(workerID, nil, true)
		log.Printf("Hydration worker %d stopped", workerID)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case task := <-h.taskQueue:
			if task != nil {
				log.Printf("DEBUG: Worker %d received task from queue: %s for app: %s (user: %s, source: %s)", workerID, task.ID, task.AppID, task.UserID, task.SourceID)
				h.processTask(ctx, task, workerID)
			}
		}
	}
}

// processTask processes a single hydration task
func (h *Hydrator) processTask(ctx context.Context, task *hydrationfn.HydrationTask, workerID int) {
	// Add memory monitoring at the start of task processing
	h.monitorMemoryUsage()

	log.Printf("==================== HYDRATION TASK STARTED ====================")
	log.Printf("Worker %d processing task: %s for app: %s", workerID, task.ID, task.AppID)

	// Update worker status to indicate it's processing this task
	h.updateWorkerStatus(workerID, task, false)
	taskStartTime := time.Now()

	// Ensure worker status is cleared when task completes or fails
	defer h.updateWorkerStatus(workerID, nil, true)

	// Check if task is in cooldown period
	if task.LastFailureTime != nil && time.Since(*task.LastFailureTime) < 5*time.Minute {
		log.Printf("Task %s is in cooldown period, skipping. Next retry available at: %v",
			task.ID, task.LastFailureTime.Add(5*time.Minute))
		return
	}

	task.SetStatus(hydrationfn.TaskStatusRunning)

	// Execute all steps
	for i, step := range h.steps {
		if task.CurrentStep > i {
			continue // Skip already completed steps
		}

		// Check if step can be skipped
		if step.CanSkip(ctx, task) {
			log.Printf("Skipping step %d (%s) for task: %s", i+1, step.GetStepName(), task.ID)
			log.Printf("-------- HYDRATION STEP %d/%d SKIPPED: %s --------", i+1, len(h.steps), step.GetStepName())
			task.IncrementStep()
			continue
		}

		log.Printf("-------- HYDRATION STEP %d/%d STARTED: %s --------", i+1, len(h.steps), step.GetStepName())
		log.Printf("Executing step %d (%s) for task: %s", i+1, step.GetStepName(), task.ID)

		// Update worker status with current step
		h.updateWorkerStatus(workerID, task, false)

		// Log task data before step execution
		h.logTaskDataBeforeStep(task, i+1, step.GetStepName())

		// Execute step
		if err := step.Execute(ctx, task); err != nil {
			log.Printf("Step %d (%s) failed for task: %s, error: %v", i+1, step.GetStepName(), task.ID, err)
			log.Printf("-------- HYDRATION STEP %d/%d FAILED: %s --------", i+1, len(h.steps), step.GetStepName())
			task.SetError(err)

			// Clean up resources before failure
			h.cleanupTaskResources(task)

			// Set failure time
			now := time.Now()
			task.LastFailureTime = &now

			// Comment out retry logic - instead move to render failed list
			/*
				// Check if task can be retried
				if task.CanRetry() {
					log.Printf("Task %s failed, will retry after cooldown period (5 minutes). Next retry available at: %v",
						task.ID, task.LastFailureTime.Add(5*time.Minute))
					task.ResetForRetry()

					// Re-enqueue for retry after cooldown
					go func() {
						time.Sleep(5 * time.Minute) // Wait for cooldown period
						if err := h.EnqueueTask(task); err != nil {
							log.Printf("Failed to re-enqueue task for retry: %s, error: %v", task.ID, err)
							h.markTaskFailed(task, time.Now(), 0, "retry", err.Error())
						}
					}()
					log.Printf("==================== HYDRATION TASK QUEUED FOR RETRY AFTER COOLDOWN ====================")
					return
				} else {
					// Max retries exceeded
					log.Printf("Task failed after max retries: %s", task.ID)
					h.markTaskFailed(task, time.Now(), 0, "max_retries", "max retries exceeded")
					log.Printf("==================== HYDRATION TASK FAILED ====================")
					return
				}
			*/

			// Move failed task to render failed list instead of retrying
			failureReason := err.Error()
			failureStep := step.GetStepName()

			log.Printf("Task %s failed at step %s, moving to render failed list with reason: %s",
				task.ID, failureStep, failureReason)

			duration := time.Since(taskStartTime)
			h.moveTaskToRenderFailed(task, failureReason, failureStep)
			h.markTaskFailed(task, taskStartTime, duration, failureStep, failureReason)

			log.Printf("==================== HYDRATION TASK MOVED TO RENDER FAILED LIST ====================")
			return
		}

		// Log task data after step execution
		h.logTaskDataAfterStep(task, i+1, step.GetStepName())

		task.IncrementStep()
		log.Printf("Step %d (%s) completed for task: %s", i+1, step.GetStepName(), task.ID)
		log.Printf("-------- HYDRATION STEP %d/%d COMPLETED: %s --------", i+1, len(h.steps), step.GetStepName())
	}

	// All steps completed successfully
	task.SetStatus(hydrationfn.TaskStatusCompleted)
	duration := time.Since(taskStartTime)
	h.markTaskCompleted(task, taskStartTime, duration)

	log.Printf("Task completed successfully: %s for app: %s", task.ID, task.AppID)
	log.Printf("==================== HYDRATION TASK COMPLETED ====================")
}

// updateWorkerStatus updates the status of a worker
func (h *Hydrator) updateWorkerStatus(workerID int, task *hydrationfn.HydrationTask, isIdle bool) {
	if !h.workerStatusMutex.TryLock() {
		return // Skip if can't acquire lock
	}
	defer h.workerStatusMutex.Unlock()

	if isIdle {
		delete(h.workerStatus, workerID)
		return
	}

	// Worker is processing a task
	var taskInfo *TaskInfo
	if task != nil {
		taskInfo = h.taskToTaskInfo(task)
	}

	h.workerStatus[workerID] = &WorkerStatus{
		WorkerID:     workerID,
		IsIdle:       false,
		CurrentTask:  taskInfo,
		LastActivity: time.Now(),
	}
}

// logTaskDataBeforeStep logs task data before step execution to help debug JSON cycle issues
func (h *Hydrator) logTaskDataBeforeStep(task *hydrationfn.HydrationTask, stepNum int, stepName string) {
	log.Printf("DEBUG: Before step %d (%s) - Task data structure check", stepNum, stepName)

	// Try to JSON marshal task.ChartData
	if len(task.ChartData) > 0 {
		if jsonData, err := json.Marshal(task.ChartData); err != nil {
			log.Printf("ERROR: JSON marshal failed for task.ChartData before step %d: %v", stepNum, err)
			log.Printf("ERROR: ChartData keys: %v", h.getMapKeys(task.ChartData))
		} else {
			log.Printf("DEBUG: task.ChartData JSON length before step %d: %d bytes", stepNum, len(jsonData))
		}
	}

	// Try to JSON marshal task.AppData
	if len(task.AppData) > 0 {
		if jsonData, err := json.Marshal(task.AppData); err != nil {
			log.Printf("ERROR: JSON marshal failed for task.AppData before step %d: %v", stepNum, err)
			log.Printf("ERROR: AppData keys: %v", h.getMapKeys(task.AppData))
		} else {
			log.Printf("DEBUG: task.AppData JSON length before step %d: %d bytes", stepNum, len(jsonData))
		}
	}

	// Try to JSON marshal task.DatabaseUpdateData
	if len(task.DatabaseUpdateData) > 0 {
		if jsonData, err := json.Marshal(task.DatabaseUpdateData); err != nil {
			log.Printf("ERROR: JSON marshal failed for task.DatabaseUpdateData before step %d: %v", stepNum, err)
			log.Printf("ERROR: DatabaseUpdateData keys: %v", h.getMapKeys(task.DatabaseUpdateData))
		} else {
			log.Printf("DEBUG: task.DatabaseUpdateData JSON length before step %d: %d bytes", stepNum, len(jsonData))
		}
	}
}

// logTaskDataAfterStep logs task data after step execution to help debug JSON cycle issues
func (h *Hydrator) logTaskDataAfterStep(task *hydrationfn.HydrationTask, stepNum int, stepName string) {
	log.Printf("DEBUG: After step %d (%s) - Task data structure check", stepNum, stepName)

	// Try to JSON marshal task.ChartData
	if len(task.ChartData) > 0 {
		if jsonData, err := json.Marshal(task.ChartData); err != nil {
			log.Printf("ERROR: JSON marshal failed for task.ChartData after step %d: %v", stepNum, err)
			log.Printf("ERROR: ChartData keys: %v", h.getMapKeys(task.ChartData))
		} else {
			log.Printf("DEBUG: task.ChartData JSON length after step %d: %d bytes", stepNum, len(jsonData))
		}
	}

	// Try to JSON marshal task.AppData
	if len(task.AppData) > 0 {
		if jsonData, err := json.Marshal(task.AppData); err != nil {
			log.Printf("ERROR: JSON marshal failed for task.AppData after step %d: %v", stepNum, err)
			log.Printf("ERROR: AppData keys: %v", h.getMapKeys(task.AppData))
		} else {
			log.Printf("DEBUG: task.AppData JSON length after step %d: %d bytes", stepNum, len(jsonData))
		}
	}

	// Try to JSON marshal task.DatabaseUpdateData
	if len(task.DatabaseUpdateData) > 0 {
		if jsonData, err := json.Marshal(task.DatabaseUpdateData); err != nil {
			log.Printf("ERROR: JSON marshal failed for task.DatabaseUpdateData after step %d: %v", stepNum, err)
			log.Printf("ERROR: DatabaseUpdateData keys: %v", h.getMapKeys(task.DatabaseUpdateData))
		} else {
			log.Printf("DEBUG: task.DatabaseUpdateData JSON length after step %d: %d bytes", stepNum, len(jsonData))
		}
	}
}

// getMapKeys safely extracts keys from a map for debugging
func (h *Hydrator) getMapKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

// cleanupTaskResources cleans up resources associated with a task
func (h *Hydrator) cleanupTaskResources(task *hydrationfn.HydrationTask) {
	// Clean up chart data
	// if renderedDir, exists := task.ChartData["rendered_chart_dir"].(string); exists {
	// 	if err := os.RemoveAll(renderedDir); err != nil {
	// 		log.Printf("Warning: Failed to clean up rendered chart directory %s: %v", renderedDir, err)
	// 	}
	// }

	// Clean up source chart
	if sourceChartPath, exists := task.ChartData["source_chart_path"].(string); exists {
		if err := os.Remove(sourceChartPath); err != nil {
			log.Printf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}

	// Clear task data maps
	task.ChartData = make(map[string]interface{})
	task.DatabaseUpdateData = make(map[string]interface{})

	// Clear app data to reduce memory usage
	task.AppData = make(map[string]interface{})
}

// pendingDataMonitor monitors for new pending data and creates tasks
func (h *Hydrator) pendingDataMonitor(ctx context.Context) {
	log.Println("Pending data monitor started")
	defer log.Println("Pending data monitor stopped")

	ticker := time.NewTicker(time.Second * 30) // Check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case <-ticker.C:
			h.checkForPendingData()
		}
	}
}

// checkForPendingData scans cache for pending data and creates hydration tasks
func (h *Hydrator) checkForPendingData() {
	// Use CacheManager's lock if available
	if h.cacheManager != nil {
		if !h.cacheManager.mutex.TryRLock() {
			glog.Warningf("Hydrator.checkForPendingData: CacheManager read lock not available, skipping")
			return
		}
		defer h.cacheManager.mutex.RUnlock()

		for userID, userData := range h.cache.Users {
			// No nested locks needed since we already hold the global lock
			for sourceID, sourceData := range userData.Sources {
				// No nested locks needed since we already hold the global lock

				// Log source type for debugging - both local and remote should be processed
				log.Printf("Checking pending data for user: %s, source: %s, type: %s, pending: %d", userID, sourceID, sourceData.Type, len(sourceData.AppInfoLatestPending))

				// Check if there's pending data - process both local and remote sources
				if len(sourceData.AppInfoLatestPending) > 0 {
					log.Printf("Found %d pending apps for user: %s, source: %s, type: %s",
						len(sourceData.AppInfoLatestPending), userID, sourceID, sourceData.Type)
					log.Printf("DEBUG: About to process %d pending apps for user: %s, source: %s", len(sourceData.AppInfoLatestPending), userID, sourceID)
					for i, pendingData := range sourceData.AppInfoLatestPending {
						log.Printf("DEBUG: Processing pending data %d/%d for user: %s, source: %s, pendingData: %v", i+1, len(sourceData.AppInfoLatestPending), userID, sourceID, pendingData != nil)
						h.createTasksFromPendingData(userID, sourceID, pendingData)
					}
				}
			}
		}
	} else {
		log.Printf("Warning: CacheManager not available for checkForPendingData")
	}
}

// createTasksFromPendingData creates hydration tasks from pending app data
func (h *Hydrator) createTasksFromPendingData(userID, sourceID string, pendingData *types.AppInfoLatestPendingData) {
	if pendingData == nil {
		log.Printf("DEBUG: createTasksFromPendingData called with nil pendingData for user: %s, source: %s", userID, sourceID)
		return
	}

	log.Printf("DEBUG: createTasksFromPendingData called for user: %s, source: %s, pendingData.RawData: %v", userID, sourceID, pendingData.RawData != nil)

	// For the new structure, we can work with RawData if it exists
	if pendingData.RawData != nil {
		// Handle regular structured RawData
		appID := pendingData.RawData.AppID
		if appID == "" {
			appID = pendingData.RawData.ID
		}

		log.Printf("DEBUG: Processing appID: %s for user: %s, source: %s", appID, userID, sourceID)

		if appID != "" {
			// Check if app is already in render failed list
			if h.isAppInRenderFailedList(userID, sourceID, appID) {
				log.Printf("App %s (user: %s, source: %s) is already in render failed list, skipping task creation",
					appID, userID, sourceID)
				return
			}

			// Check if app hydration is already complete before creating new task
			if h.isAppHydrationComplete(pendingData) {
				log.Printf("DEBUG: App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
					appID, userID, sourceID)
				return
			}

			// Check if app already exists in latest queue before creating new task
			// Extract version from pending data for version comparison
			version := ""
			if pendingData.RawData != nil {
				version = pendingData.RawData.Version
			}
			if h.isAppInLatestQueue(userID, sourceID, appID, version) {
				log.Printf("DEBUG: App already exists in latest queue for app: %s (user: %s, source: %s), skipping task creation",
					appID, userID, sourceID)
				return
			}

			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				log.Printf("DEBUG: No active task found for app: %s (user: %s, source: %s), proceeding with task creation", appID, userID, sourceID)
				// Convert ApplicationInfoEntry to map for task creation
				appDataMap := h.convertApplicationInfoEntryToMap(pendingData.RawData)

				if len(appDataMap) == 0 {
					log.Printf("Warning: Empty app data for app: %s (user: %s, source: %s), skipping task creation",
						appID, userID, sourceID)
					return
				}

				// Create task with CacheManager for unified lock strategy
				var cacheManager types.CacheManagerInterface
				if h.cacheManager != nil {
					cacheManager = h.cacheManager
				}
				task := hydrationfn.NewHydrationTaskWithManager(
					userID, sourceID, appID,
					appDataMap, h.cache, cacheManager, h.settingsManager,
				)

				if err := h.EnqueueTask(task); err != nil {
					log.Printf("Failed to enqueue task for app: %s (user: %s, source: %s), error: %v",
						appID, userID, sourceID, err)
				} else {
					log.Printf("Created hydration task for structured app: %s (user: %s, source: %s)",
						appID, userID, sourceID)
				}
			}
		}
		return
	}
}

// isAppHydrationComplete checks if an app has completed all hydration steps
func (h *Hydrator) isAppHydrationComplete(pendingData *types.AppInfoLatestPendingData) bool {

	// if utils.IsPublicEnvironment() {
	// 	return true
	// }

	if pendingData == nil {
		log.Printf("isAppHydrationComplete: pendingData is nil")
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

	log.Printf("DEBUG: isAppHydrationComplete checking appID=%s, name=%s, RawPackage=%s, RenderedPackage=%s",
		appID, appName, pendingData.RawPackage, pendingData.RenderedPackage)

	if pendingData.RawPackage == "" {
		log.Printf("DEBUG: isAppHydrationComplete RETURNING FALSE - RawPackage is empty for appID=%s, name=%s", appID, appName)
		return false
	}

	if pendingData.RenderedPackage == "" {
		log.Printf("DEBUG: isAppHydrationComplete RETURNING FALSE - RenderedPackage is empty for appID=%s, name=%s", appID, appName)
		return false
	}

	if pendingData.AppInfo == nil {
		log.Printf("DEBUG: isAppHydrationComplete RETURNING FALSE - AppInfo is nil for appID=%s, name=%s", appID, appName)
		return false
	}

	imageAnalysis := pendingData.AppInfo.ImageAnalysis
	if imageAnalysis == nil {
		log.Printf("DEBUG: isAppHydrationComplete RETURNING FALSE - ImageAnalysis is nil for appID=%s, name=%s", appID, appName)
		return false
	}

	if imageAnalysis.TotalImages > 0 {
		log.Printf("DEBUG: isAppHydrationComplete RETURNING TRUE - TotalImages > 0 for appID=%s, name=%s, TotalImages: %d", appID, appName, imageAnalysis.TotalImages)
		return true
	}

	if imageAnalysis.TotalImages == 0 && imageAnalysis.Images != nil {
		log.Printf("DEBUG: isAppHydrationComplete RETURNING TRUE - TotalImages=0 but Images not nil for appID=%s, name=%s, Images: %v", appID, appName, imageAnalysis.Images)
		return true
	}

	log.Printf("DEBUG: isAppHydrationComplete RETURNING FALSE - ImageAnalysis incomplete for appID=%s, name=%s, TotalImages: %d, Images: %v", appID, appName, imageAnalysis.TotalImages, imageAnalysis.Images)
	return false
}

// isAppDataHydrationComplete checks if an app's hydration is complete by looking up pending data in cache
func (h *Hydrator) isAppDataHydrationComplete(userID, sourceID, appID string) bool {
	// Use CacheManager's lock if available
	if h.cacheManager != nil {
		if !h.cacheManager.mutex.TryRLock() {
			glog.Warningf("Hydrator.isAppDataHydrationComplete: CacheManager read lock not available for user %s, source %s, app %s, returning false", userID, sourceID, appID)
			return false
		}
		defer h.cacheManager.mutex.RUnlock()

		userData, userExists := h.cache.Users[userID]
		if !userExists {
			return false
		}

		sourceData, sourceExists := userData.Sources[sourceID]
		if !sourceExists {
			return false
		}

		// Find the pending data for the specific app
		for _, pendingData := range sourceData.AppInfoLatestPending {
			if pendingData.RawData != nil &&
				(pendingData.RawData.ID == appID || pendingData.RawData.AppID == appID || pendingData.RawData.Name == appID) {
				// Found the pending data for this app, check if hydration is complete
				return h.isAppHydrationComplete(pendingData)
			}
		}
	} else {
		log.Printf("Warning: CacheManager not available for isAppDataHydrationComplete")
	}

	// If no pending data found for this app, consider it not hydrated
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
		"middleware":         entry.Middleware,
		"options":            entry.Options,

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
		"subCharts":      entry.SubCharts,
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

// hasActiveTaskForApp checks if there's already an active task for the given app
func (h *Hydrator) hasActiveTaskForApp(userID, sourceID, appID string) bool {
	if !h.taskMutex.TryRLock() {
		log.Printf("Failed to acquire read lock for hasActiveTaskForApp, returning false")
		return false
	}
	defer h.taskMutex.RUnlock()

	log.Printf("DEBUG: Checking active tasks for app: %s (user: %s, source: %s), total active tasks: %d", appID, userID, sourceID, len(h.activeTasks))

	for _, task := range h.activeTasks {
		if task.UserID == userID && task.SourceID == sourceID && task.AppID == appID {
			log.Printf("DEBUG: Found active task for app: %s (user: %s, source: %s), taskID: %s", appID, userID, sourceID, task.ID)
			return true
		}
	}
	log.Printf("DEBUG: No active task found for app: %s (user: %s, source: %s)", appID, userID, sourceID)
	return false
}

// trackTask adds task to active tasks tracking
func (h *Hydrator) trackTask(task *hydrationfn.HydrationTask) {
	if !h.taskMutex.TryLock() {
		log.Printf("Failed to acquire lock for trackTask, skipping task tracking")
		return
	}
	defer h.taskMutex.Unlock()
	h.activeTasks[task.ID] = task
}

// markTaskCompleted moves task from active to completed
func (h *Hydrator) markTaskCompleted(task *hydrationfn.HydrationTask, startedAt time.Time, duration time.Duration) {
	// Extract file path for cleanup before the lock
	var sourceChartPath string
	if path, ok := task.ChartData["source_chart_path"].(string); ok {
		sourceChartPath = path
	}

	if !h.taskMutex.TryLock() {
		log.Printf("Failed to acquire lock for markTaskCompleted, skipping status update")
		return
	}
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
		log.Printf("Warning: batch completion queue is full, task %s not queued for processing", task.ID)
	}

	// Clean up file resources after releasing the lock
	if sourceChartPath != "" {
		if err := os.Remove(sourceChartPath); err != nil {
			log.Printf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
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

	if !h.taskMutex.TryLock() {
		log.Printf("Failed to acquire lock for markTaskFailed, skipping status update")
		return
	}

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
			log.Printf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}
}

// addToCompletedHistory adds a task to the completed tasks history
func (h *Hydrator) addToCompletedHistory(task *hydrationfn.HydrationTask, startedAt time.Time, duration time.Duration) {
	if !h.workerStatusMutex.TryLock() {
		log.Printf("Failed to acquire lock for addToCompletedHistory, skipping")
		return
	}
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
	if !h.workerStatusMutex.TryLock() {
		log.Printf("Failed to acquire lock for addToFailedHistory, skipping")
		return
	}
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
		log.Printf("Warning: Cache manager not set, cannot move task to render failed list")
		return
	}

	// Find the pending data for this task
	var pendingData *types.AppInfoLatestPendingData
	if h.cacheManager != nil {
		if !h.cacheManager.mutex.TryRLock() {
			glog.Warningf("Hydrator.moveTaskToRenderFailed: CacheManager read lock not available for user %s, skipping", task.UserID)
			return
		}
		userData, userExists := h.cache.Users[task.UserID]
		if !userExists {
			h.cacheManager.mutex.RUnlock()
			log.Printf("Warning: User data not found for task: %s, user: %s", task.ID, task.UserID)
			return
		}

		sourceData, sourceExists := userData.Sources[task.SourceID]
		if !sourceExists {
			h.cacheManager.mutex.RUnlock()
			log.Printf("Warning: Source data not found for task: %s, user: %s, source: %s", task.ID, task.UserID, task.SourceID)
			return
		}

		// Find the pending data for this app
		for _, pending := range sourceData.AppInfoLatestPending {
			if pending.RawData != nil &&
				(pending.RawData.ID == task.AppID || pending.RawData.AppID == task.AppID || pending.RawData.Name == task.AppID) {
				pendingData = pending
				break
			}
		}
		h.cacheManager.mutex.RUnlock()
	} else {
		log.Printf("Warning: CacheManager not available for moveTaskToRenderFailed")
		return
	}

	if pendingData == nil {
		log.Printf("Warning: Pending data not found for task: %s, app: %s", task.ID, task.AppID)
		return
	}

	// Create render failed data from pending data
	failedData := types.NewAppRenderFailedDataFromPending(pendingData, failureReason, failureStep, task.RetryCount)

	// Add to render failed list in cache
	if err := h.cacheManager.SetAppData(task.UserID, task.SourceID, types.AppRenderFailed, map[string]interface{}{
		"failed_app": failedData,
	}); err != nil {
		log.Printf("Failed to add task to render failed list: %s, error: %v", task.ID, err)
		return
	}

	log.Printf("Successfully moved task %s (app: %s) to render failed list with reason: %s, step: %s",
		task.ID, task.AppID, failureReason, failureStep)

	// Remove from pending list
	h.removeFromPendingList(task.UserID, task.SourceID, task.AppID)
}

// removeFromPendingList removes an app from the pending list
func (h *Hydrator) removeFromPendingList(userID, sourceID, appID string) {
	if h.cacheManager == nil {
		log.Printf("Warning: CacheManager not available for removeFromPendingList")
		return
	}

	// 1) Read-lock phase: locate index to remove (no writes under RLock)
	if !h.cacheManager.mutex.TryRLock() {
		glog.Warningf("Hydrator.removeFromPendingList: CacheManager read lock not available for user %s, source %s, app %s, skipping", userID, sourceID, appID)
		return
	}
	userData, userExists := h.cache.Users[userID]
	if !userExists {
		h.cacheManager.mutex.RUnlock()
		return
	}
	sourceData, sourceExists := userData.Sources[sourceID]
	if !sourceExists {
		h.cacheManager.mutex.RUnlock()
		return
	}
	removeIdx := -1
	for i, pending := range sourceData.AppInfoLatestPending {
		if pending != nil && pending.RawData != nil &&
			(pending.RawData.ID == appID || pending.RawData.AppID == appID || pending.RawData.Name == appID) {
			removeIdx = i
			break
		}
	}
	h.cacheManager.mutex.RUnlock()

	if removeIdx == -1 {
		return
	}

	// 2) Try to acquire short write-lock and apply removal with new slice; skip if contended
	// Use TryLock to avoid blocking
	if !h.cacheManager.mutex.TryLock() {
		log.Printf("DEBUG: removeFromPendingList skipped (lock not available) for user=%s source=%s app=%s", userID, sourceID, appID)
		return
	}
	defer h.cacheManager.mutex.Unlock()

	// Re-validate pointers under write-lock
	if userData2, ok := h.cache.Users[userID]; ok {
		if sourceData2, ok2 := userData2.Sources[sourceID]; ok2 {
			if removeIdx >= 0 && removeIdx < len(sourceData2.AppInfoLatestPending) {
				// Re-check match to be safe
				p := sourceData2.AppInfoLatestPending[removeIdx]
				if p != nil && p.RawData != nil && (p.RawData.ID == appID || p.RawData.AppID == appID || p.RawData.Name == appID) {
					// Create new slice dropping index removeIdx
					old := sourceData2.AppInfoLatestPending
					newSlice := make([]*types.AppInfoLatestPendingData, 0, len(old)-1)
					newSlice = append(newSlice, old[:removeIdx]...)
					if removeIdx+1 <= len(old)-1 {
						newSlice = append(newSlice, old[removeIdx+1:]...)
					}
					sourceData2.AppInfoLatestPending = newSlice
					log.Printf("Removed app %s from pending list for user: %s, source: %s", appID, userID, sourceID)
				}
			}
		}
	}
}

// GetMetrics returns hydrator metrics
func (h *Hydrator) GetMetrics() HydratorMetrics {
	if !h.taskMutex.TryRLock() {
		log.Printf("Failed to acquire read lock for GetMetrics, returning zero metrics")
		// Try to get worker status even if we can't get task lock
		var workers []*WorkerStatus
		if h.workerStatusMutex.TryRLock() {
			workers = h.getWorkerStatusList()
			h.workerStatusMutex.RUnlock()
		}
		return HydratorMetrics{
			TotalTasksProcessed:  h.totalTasksProcessed,
			TotalTasksSucceeded:  h.totalTasksSucceeded,
			TotalTasksFailed:     h.totalTasksFailed,
			ActiveTasksCount:     0,
			CompletedTasksCount:  0,
			FailedTasksCount:     0,
			QueueLength:          int64(len(h.taskQueue)),
			ActiveTasks:          []*TaskInfo{},
			RecentCompletedTasks: h.getRecentCompletedTasks(),
			RecentFailedTasks:    h.getRecentFailedTasks(),
			Workers:              workers,
		}
	}

	// Get active tasks info
	activeTasksList := make([]*TaskInfo, 0, len(h.activeTasks))
	for _, task := range h.activeTasks {
		if task != nil {
			activeTasksList = append(activeTasksList, h.taskToTaskInfo(task))
		}
	}

	h.taskMutex.RUnlock()

	// Get worker status
	var workers []*WorkerStatus
	if h.workerStatusMutex.TryRLock() {
		workers = h.getWorkerStatusList()
		h.workerStatusMutex.RUnlock()
	}

	return HydratorMetrics{
		TotalTasksProcessed:  h.totalTasksProcessed,
		TotalTasksSucceeded:  h.totalTasksSucceeded,
		TotalTasksFailed:     h.totalTasksFailed,
		ActiveTasksCount:     int64(len(h.activeTasks)),
		CompletedTasksCount:  int64(len(h.completedTasks)),
		FailedTasksCount:     int64(len(h.failedTasks)),
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
	// Return a copy to avoid race conditions
	if !h.workerStatusMutex.TryRLock() {
		return make([]*TaskHistoryEntry, 0)
	}
	defer h.workerStatusMutex.RUnlock()

	// Return a copy
	result := make([]*TaskHistoryEntry, len(h.recentCompletedTasks))
	copy(result, h.recentCompletedTasks)
	return result
}

// getRecentFailedTasks returns recent failed tasks (thread-safe)
func (h *Hydrator) getRecentFailedTasks() []*TaskHistoryEntry {
	// Return a copy to avoid race conditions
	if !h.workerStatusMutex.TryRLock() {
		return make([]*TaskHistoryEntry, 0)
	}
	defer h.workerStatusMutex.RUnlock()

	// Return a copy
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

// NotifyPendingDataUpdate implements HydrationNotifier interface
// Processes pending data update notification and creates hydration tasks immediately
func (h *Hydrator) NotifyPendingDataUpdate(userID, sourceID string, pendingData map[string]interface{}) {
	if !h.IsRunning() {
		log.Printf("Hydrator is not running, ignoring pending data notification for user: %s, source: %s", userID, sourceID)
		return
	}

	log.Printf("Received pending data update notification for user: %s, source: %s", userID, sourceID)

	// Create tasks from the pending data immediately
	h.createTasksFromPendingDataMap(userID, sourceID, pendingData)
}

// createTasksFromPendingDataMap creates hydration tasks from pending data map
func (h *Hydrator) createTasksFromPendingDataMap(userID, sourceID string, pendingData map[string]interface{}) {
	log.Printf("Creating tasks from pending data for user: %s, source: %s", userID, sourceID)

	// Extract data section from pendingData
	dataSection, ok := pendingData["data"]
	if !ok {
		log.Printf("No data section found in pending data for user: %s, source: %s", userID, sourceID)
		return
	}

	// Handle different data section formats
	var appsMap map[string]interface{}

	// First, try to handle the case where dataSection is an AppStoreDataSection struct
	log.Printf("Data section type: %T for user: %s, source: %s", dataSection, userID, sourceID)

	// Check if it's an AppStoreDataSection by checking if it has Apps field
	if dataStruct := dataSection; dataStruct != nil {
		// Use reflection or type assertion to access the Apps field

		// Try to access as map first (for backwards compatibility)
		if dataMap, ok := dataSection.(map[string]interface{}); ok {
			// Check if it's in the expected format with "apps" key
			if apps, hasApps := dataMap["apps"]; hasApps {
				if appsMapValue, ok := apps.(map[string]interface{}); ok {
					appsMap = appsMapValue
					log.Printf("Found apps data in standard map format for user: %s, source: %s", userID, sourceID)
				}
			} else {
				// Check if the dataMap itself contains app entries
				if h.looksLikeAppsMap(dataMap) {
					appsMap = dataMap
					log.Printf("Data section appears to contain apps directly for user: %s, source: %s", userID, sourceID)
				}
			}
		} else {
			// Try to handle AppStoreDataSection struct using interface conversion
			log.Printf("Unsupported data format for user: %s, source: %s. Expected map[string]interface{} but got %T", userID, sourceID, dataSection)
			log.Printf("Data section content: %+v", dataSection)
			return
		}
	}

	if appsMap == nil || len(appsMap) == 0 {
		log.Printf("No apps found in pending data for user: %s, source: %s", userID, sourceID)
		return
	}

	log.Printf("Found %d apps in pending data for user: %s, source: %s", len(appsMap), userID, sourceID)

	// Create hydration task for each app
	for appID, appData := range appsMap {
		// Validate app data
		if appMap, ok := appData.(map[string]interface{}); ok {
			// Check if app data contains necessary raw data fields before creating task
			if !h.hasRequiredRawDataFields(appMap) {
				log.Printf("App %s (user: %s, source: %s) missing required raw data fields, skipping task creation",
					appID, userID, sourceID)
				continue
			}

			// Check if task already exists for this app to avoid duplicates
			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				// Check if app is already in render failed list
				if h.isAppInRenderFailedList(userID, sourceID, appID) {
					log.Printf("App %s (user: %s, source: %s) is already in render failed list, skipping task creation",
						appID, userID, sourceID)
					continue
				}

				// Check if app hydration is already complete before creating new task
				// Extract version from app data for version comparison
				version := ""
				if versionValue, exists := appMap["version"]; exists && versionValue != nil {
					if versionStr, ok := versionValue.(string); ok {
						version = versionStr
					}
				}
				if h.isAppInLatestQueue(userID, sourceID, appID, version) {
					// log.Printf("App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
					// 	appID, userID, sourceID)
					continue
				}

				if len(appMap) == 0 {
					log.Printf("Warning: Empty app data for app: %s (user: %s, source: %s), skipping task creation",
						appID, userID, sourceID)
					continue
				}

				// Create and submit task with CacheManager for unified lock strategy
				var cacheManager types.CacheManagerInterface
				if h.cacheManager != nil {
					cacheManager = h.cacheManager
				}
				task := hydrationfn.NewHydrationTaskWithManager(
					userID, sourceID, appID,
					appMap, h.cache, cacheManager, h.settingsManager,
				)

				if err := h.EnqueueTask(task); err != nil {
					log.Printf("Failed to enqueue hydration task for app %s (user: %s, source: %s): %v",
						appID, userID, sourceID, err)
				} else {
					log.Printf("Successfully enqueued hydration task for app %s (user: %s, source: %s)",
						appID, userID, sourceID)
				}
			} else {
				log.Printf("Task already exists for app: %s (user: %s, source: %s), skipping", appID, userID, sourceID)
			}
		} else {
			log.Printf("Invalid app data format for app %s (user: %s, source: %s)", appID, userID, sourceID)
		}
	}
}

// hasRequiredRawDataFields checks if app data contains the minimum required fields for hydration
func (h *Hydrator) hasRequiredRawDataFields(appMap map[string]interface{}) bool {
	if appMap == nil {
		return false
	}

	// Required fields that must be present for hydration to succeed
	requiredFields := []string{"id", "name", "appID"}

	// Check if at least one of the required fields exists
	hasRequiredField := false
	for _, field := range requiredFields {
		if value, exists := appMap[field]; exists && value != nil && value != "" {
			hasRequiredField = true
			break
		}
	}

	if !hasRequiredField {
		return false
	}

	// Additional recommended fields that indicate this is valid app data
	recommendedFields := []string{"title", "version", "description", "chartName"}
	hasRecommendedField := false

	for _, field := range recommendedFields {
		if value, exists := appMap[field]; exists && value != nil && value != "" {
			hasRecommendedField = true
			break
		}
	}

	// Log warning if missing recommended fields but still proceed
	if !hasRecommendedField {
		log.Printf("Warning: App data missing recommended fields (title, version, description, chartName), but proceeding with required fields")
	}

	return hasRequiredField
}

// looksLikeAppsMap checks if a map looks like it contains app entries
func (h *Hydrator) looksLikeAppsMap(data map[string]interface{}) bool {
	// Sample a few entries to see if they look like app data
	sampleCount := 0
	maxSamples := 3

	for _, value := range data {
		if sampleCount >= maxSamples {
			break
		}

		if appMap, ok := value.(map[string]interface{}); ok {
			// Check if this app data has required raw data fields
			if h.hasRequiredRawDataFields(appMap) {
				sampleCount++
			} else {
				// If this entry doesn't have required fields, it's probably not valid app data
				return false
			}
		} else {
			// Non-map entries suggest this is not an apps map
			return false
		}
	}

	return sampleCount > 0
}

// SetCacheManager removed: cacheManager must be provided at NewHydrator

// batchCompletionProcessor processes completed tasks in batches
func (h *Hydrator) batchCompletionProcessor(ctx context.Context) {
	log.Println("Batch completion processor started")
	defer log.Println("Batch completion processor stopped")

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
	log.Println("Database sync monitor started")
	defer log.Println("Database sync monitor stopped")

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
	if !h.taskMutex.TryRLock() {
		log.Printf("Failed to acquire read lock for processCompletedTask, skipping")
		return
	}
	task, exists := h.completedTasks[taskID]
	h.taskMutex.RUnlock()

	if !exists {
		log.Printf("Warning: completed task %s not found in completed tasks map", taskID)
		return
	}

	log.Printf("Processing completed task: %s for app: %s", taskID, task.AppID)
	// Additional processing can be added here if needed
}

// processBatchCompletions processes completed tasks in batches
func (h *Hydrator) processBatchCompletions() {
	if !h.taskMutex.TryRLock() {
		log.Printf("Failed to acquire read lock for processBatchCompletions, skipping")
		return
	}
	currentCompleted := h.totalTasksSucceeded
	h.taskMutex.RUnlock()

	// Check if significant number of tasks completed since last sync
	if currentCompleted > h.completedTaskCount+10 { // Trigger sync after 10 more completions
		log.Printf("Batch completion detected: %d tasks completed since last check",
			currentCompleted-h.completedTaskCount)
		h.completedTaskCount = currentCompleted
		h.triggerDatabaseSync()
	}
}

// checkAndSyncToDatabase checks if database sync is needed and performs it
func (h *Hydrator) checkAndSyncToDatabase() {
	if h.cacheManager == nil {
		log.Printf("Warning: Cache manager not set, skipping database sync")
		return
	}

	// Check if there are completed tasks that need syncing
	if !h.taskMutex.TryRLock() {
		log.Printf("Failed to acquire read lock for checkAndSyncToDatabase, skipping")
		return
	}
	completedCount := len(h.completedTasks)
	h.taskMutex.RUnlock()

	if completedCount == 0 {
		return // No completed tasks to sync
	}

	// Check if enough time has passed since last sync
	if time.Since(h.lastSyncTime) >= h.syncInterval {
		log.Printf("Periodic database sync triggered - %d completed tasks to sync", completedCount)
		h.triggerDatabaseSync()
	}
}

// triggerDatabaseSync triggers synchronization of cache data to database
func (h *Hydrator) triggerDatabaseSync() {
	if h.cacheManager == nil {
		log.Printf("Warning: Cache manager not set, cannot sync to database")
		return
	}

	log.Printf("Triggering database synchronization")

	// Force sync all cache data to Redis/database
	if err := h.cacheManager.ForceSync(); err != nil {
		log.Printf("Error during database sync: %v", err)
	} else {
		log.Printf("Database sync completed successfully")
		h.lastSyncTime = time.Now()

		// Optionally clean up old completed tasks to prevent memory growth
		h.cleanupOldCompletedTasks()
	}
}

// cleanupOldCompletedTasks removes old completed tasks from memory
func (h *Hydrator) cleanupOldCompletedTasks() {
	if !h.taskMutex.TryLock() {
		log.Printf("Failed to acquire lock for cleanupOldCompletedTasks, skipping")
		return
	}
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

		log.Printf("Cleaned up %d old completed tasks from memory", removed)
	}
}

// monitorMemoryUsage monitors memory usage and logs warnings if it's too high
func (h *Hydrator) monitorMemoryUsage() {
	if time.Since(h.lastMemoryCheck) < h.memoryCheckInterval {
		return
	}

	h.lastMemoryCheck = time.Now()

	if !h.taskMutex.TryRLock() {
		log.Printf("Failed to acquire read lock for monitorMemoryUsage, skipping")
		return
	}
	activeCount := len(h.activeTasks)
	completedCount := len(h.completedTasks)
	failedCount := len(h.failedTasks)
	h.taskMutex.RUnlock()

	// Log memory usage metrics
	log.Printf("Memory usage metrics - Active tasks: %d, Completed tasks: %d, Failed tasks: %d",
		activeCount, completedCount, failedCount)

	// If we have too many tasks, trigger cleanup
	if activeCount > 100 || completedCount > 100 {
		log.Printf("Warning: High number of tasks detected, triggering cleanup")
		h.cleanupOldTasks()
	}
}

// cleanupOldTasks cleans up old tasks from all task maps
func (h *Hydrator) cleanupOldTasks() {
	if !h.taskMutex.TryLock() {
		log.Printf("Failed to acquire lock for cleanupOldTasks, skipping")
		return
	}
	defer h.taskMutex.Unlock()

	now := time.Now()

	// Clean up active tasks older than 1 hour
	for id, task := range h.activeTasks {
		if now.Sub(task.UpdatedAt) > time.Hour {
			log.Printf("Cleaning up stale active task: %s", id)
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

		log.Printf("Cleaned up %d old completed tasks", removed)
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

		log.Printf("Cleaned up %d old failed tasks", removed)
	}
}

// isAppInLatestQueue checks if an app already exists in the AppInfoLatest queue with version comparison
func (h *Hydrator) isAppInLatestQueue(userID, sourceID, appID, version string) bool {
	log.Printf("DEBUG: isAppInLatestQueue checking appID=%s, version=%s for user=%s, source=%s", appID, version, userID, sourceID)

	// Use CacheManager's lock if available
	if h.cacheManager != nil {
		if !h.cacheManager.mutex.TryRLock() {
			glog.Warningf("Hydrator.isAppInLatestQueue: CacheManager read lock not available for user %s, source %s, app %s, returning false", userID, sourceID, appID)
			return false
		}
		defer h.cacheManager.mutex.RUnlock()

		userData, userExists := h.cache.Users[userID]
		if !userExists {
			return false
		}

		sourceData, sourceExists := userData.Sources[sourceID]
		if !sourceExists {
			return false
		}

		// Check if app exists in AppInfoLatest queue
		for _, latestData := range sourceData.AppInfoLatest {
			if latestData == nil {
				continue
			}

			// Check RawData first
			if latestData.RawData != nil {
				if latestData.RawData.ID == appID ||
					latestData.RawData.AppID == appID ||
					latestData.RawData.Name == appID {
					// Add version comparison - only return true if versions match
					if version != "" && latestData.RawData.Version != version {
						log.Printf("App %s found in latest queue but version mismatch: current=%s, latest=%s, skipping",
							appID, version, latestData.RawData.Version)
						continue
					}
					log.Printf("App %s found in latest queue with matching version: %s", appID, version)
					return true
				}
			}

			// Check AppInfo.AppEntry
			if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
				if latestData.AppInfo.AppEntry.ID == appID ||
					latestData.AppInfo.AppEntry.AppID == appID ||
					latestData.AppInfo.AppEntry.Name == appID {
					// Add version comparison - only return true if versions match
					if version != "" && latestData.AppInfo.AppEntry.Version != version {
						log.Printf("App %s found in latest queue but version mismatch: current=%s, latest=%s, skipping",
							appID, version, latestData.AppInfo.AppEntry.Version)
						continue
					}
					log.Printf("App %s found in latest queue with matching version: %s", appID, version)
					return true
				}
			}

			// Check AppSimpleInfo
			if latestData.AppSimpleInfo != nil {
				if latestData.AppSimpleInfo.AppID == appID ||
					latestData.AppSimpleInfo.AppName == appID {
					// For AppSimpleInfo, we may not have version info, so only check if version is empty
					if version == "" {
						log.Printf("App %s found in latest queue (AppSimpleInfo)", appID)
						return true
					}
					// If version is provided but AppSimpleInfo doesn't have version, skip
					log.Printf("App %s found in latest queue but AppSimpleInfo has no version info, skipping", appID)
					continue
				}
			}
		}
	} else {
		log.Printf("Warning: CacheManager not available for isAppInLatestQueue")
	}

	log.Printf("DEBUG: isAppInLatestQueue returning false for appID=%s, version=%s, user=%s, source=%s", appID, version, userID, sourceID)
	return false
}

// ForceAddTaskFromLatestData forces creation of hydration task from latest app data, skipping isAppInLatestQueue check
// This method is exposed for external use when you need to force add a task regardless of existing state
func (h *Hydrator) ForceAddTaskFromLatestData(userID, sourceID string, latestData *types.AppInfoLatestData) error {
	if !h.IsRunning() {
		return fmt.Errorf("hydrator is not running")
	}

	if latestData == nil {
		return fmt.Errorf("latest data is nil")
	}

	// Extract app ID from latest data
	var appID string
	if latestData.RawData != nil {
		appID = latestData.RawData.AppID
		if appID == "" {
			appID = latestData.RawData.ID
		}
	} else if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
		appID = latestData.AppInfo.AppEntry.AppID
		if appID == "" {
			appID = latestData.AppInfo.AppEntry.ID
		}
	} else if latestData.AppSimpleInfo != nil {
		appID = latestData.AppSimpleInfo.AppID
	}

	if appID == "" {
		return fmt.Errorf("cannot extract app ID from latest data")
	}

	// Check if task already exists for this app to avoid duplicates
	if h.hasActiveTaskForApp(userID, sourceID, appID) {
		log.Printf("Task already exists for app: %s (user: %s, source: %s), skipping force add", appID, userID, sourceID)
		return nil
	}

	// Check if app is already in render failed list
	if h.isAppInRenderFailedList(userID, sourceID, appID) {
		log.Printf("App %s (user: %s, source: %s) is already in render failed list, skipping force add",
			appID, userID, sourceID)
		return nil
	}

	// Convert latest data to map for task creation
	appDataMap := h.convertLatestDataToMap(latestData)

	if len(appDataMap) == 0 {
		log.Printf("Warning: Empty app data for app: %s (user: %s, source: %s), skipping task creation",
			appID, userID, sourceID)
		return nil
	}

	// Create and submit task with CacheManager for unified lock strategy
	var cacheManager types.CacheManagerInterface
	if h.cacheManager != nil {
		cacheManager = h.cacheManager
	}
	task := hydrationfn.NewHydrationTaskWithManager(
		userID, sourceID, appID,
		appDataMap, h.cache, cacheManager, h.settingsManager,
	)

	if err := h.EnqueueTask(task); err != nil {
		log.Printf("Failed to enqueue force task for app: %s (user: %s, source: %s), error: %v",
			appID, userID, sourceID, err)
		return err
	}

	log.Printf("Successfully force added hydration task for app: %s (user: %s, source: %s)",
		appID, userID, sourceID)
	return nil
}

// convertLatestDataToMap converts AppInfoLatestData to map for task creation
func (h *Hydrator) convertLatestDataToMap(latestData *types.AppInfoLatestData) map[string]interface{} {
	if latestData == nil {
		return make(map[string]interface{})
	}

	// Start with basic data
	data := map[string]interface{}{
		"type":      string(latestData.Type),
		"timestamp": latestData.Timestamp,
		"version":   latestData.Version,
	}

	// Add RawData if available
	if latestData.RawData != nil {
		rawDataMap := h.convertApplicationInfoEntryToMap(latestData.RawData)
		// Merge raw data into main data map
		for key, value := range rawDataMap {
			data[key] = value
		}
	}

	// Add package information
	if latestData.RawPackage != "" {
		data["raw_package"] = latestData.RawPackage
	}
	if latestData.RenderedPackage != "" {
		data["rendered_package"] = latestData.RenderedPackage
	}

	// Add Values if available
	if latestData.Values != nil && len(latestData.Values) > 0 {
		valuesData := make([]map[string]interface{}, 0, len(latestData.Values))
		for _, value := range latestData.Values {
			if value != nil {
				valueMap := map[string]interface{}{
					"file_name":    value.FileName,
					"modify_type":  string(value.ModifyType),
					"modify_key":   value.ModifyKey,
					"modify_value": value.ModifyValue,
				}
				valuesData = append(valuesData, valueMap)
			}
		}
		data["values"] = valuesData
	}

	// Add AppInfo if available
	if latestData.AppInfo != nil {
		if latestData.AppInfo.AppEntry != nil {
			appEntryMap := h.convertApplicationInfoEntryToMap(latestData.AppInfo.AppEntry)
			// Merge app entry data
			for key, value := range appEntryMap {
				data[key] = value
			}
		}
		if latestData.AppInfo.ImageAnalysis != nil {
			data["image_analysis"] = latestData.AppInfo.ImageAnalysis
		}
	}

	// Add AppSimpleInfo if available
	if latestData.AppSimpleInfo != nil {
		data["app_simple_info"] = latestData.AppSimpleInfo
	}

	return data
}

// isAppInRenderFailedList checks if an app already exists in the render failed list
func (h *Hydrator) isAppInRenderFailedList(userID, sourceID, appID string) bool {
	// Use CacheManager's lock if available
	if h.cacheManager != nil {
		if !h.cacheManager.mutex.TryRLock() {
			glog.Warningf("Hydrator.isAppInRenderFailedList: CacheManager read lock not available for user %s, source %s, app %s, returning false", userID, sourceID, appID)
			return false
		}
		defer h.cacheManager.mutex.RUnlock()

		userData, userExists := h.cache.Users[userID]
		if !userExists {
			return false
		}

		sourceData, sourceExists := userData.Sources[sourceID]
		if !sourceExists {
			return false
		}

		// Check if app exists in render failed list
		for _, failedData := range sourceData.AppRenderFailed {
			if failedData.RawData != nil &&
				(failedData.RawData.ID == appID || failedData.RawData.AppID == appID || failedData.RawData.Name == appID) {
				return true
			}
		}
	} else {
		log.Printf("Warning: CacheManager not available for isAppInRenderFailedList")
	}

	return false
}

// ForceCheckPendingData immediately triggers checkForPendingData without waiting for the 30-second interval
// This method can be called externally to force immediate processing of pending data
func (h *Hydrator) ForceCheckPendingData() {
	if !h.IsRunning() {
		log.Printf("Hydrator is not running, cannot force check pending data")
		return
	}

	log.Printf("Force checking pending data triggered externally")
	h.checkForPendingData()
}
