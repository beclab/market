package appinfo

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/settings"
	"market/internal/v2/types"
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
	isRunning       bool
	mutex           sync.RWMutex

	// Task tracking
	activeTasks    map[string]*hydrationfn.HydrationTask
	completedTasks map[string]*hydrationfn.HydrationTask
	failedTasks    map[string]*hydrationfn.HydrationTask
	taskMutex      sync.RWMutex

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
}

// NewHydrator creates a new hydrator with the given configuration
func NewHydrator(cache *types.CacheData, settingsManager *settings.SettingsManager, config HydratorConfig) *Hydrator {
	hydrator := &Hydrator{
		steps:                make([]hydrationfn.HydrationStep, 0),
		cache:                cache,
		settingsManager:      settingsManager,
		cacheManager:         nil, // Will be set later via SetCacheManager
		taskQueue:            make(chan *hydrationfn.HydrationTask, config.QueueSize),
		workerCount:          config.WorkerCount,
		stopChan:             make(chan struct{}),
		isRunning:            false,
		activeTasks:          make(map[string]*hydrationfn.HydrationTask),
		completedTasks:       make(map[string]*hydrationfn.HydrationTask),
		failedTasks:          make(map[string]*hydrationfn.HydrationTask),
		batchCompletionQueue: make(chan string, 100), // Buffer for completed task IDs
		syncInterval:         30 * time.Second,       // Default sync interval
		memoryCheckInterval:  5 * time.Minute,
		lastMemoryCheck:      time.Now(),
	}

	// Add default steps
	hydrator.AddStep(hydrationfn.NewSourceChartStep())
	hydrator.AddStep(hydrationfn.NewRenderedChartStep())
	hydrator.AddStep(hydrationfn.NewCustomParamsUpdateStep())
	hydrator.AddStep(hydrationfn.NewImageAnalysisStep())
	hydrator.AddStep(hydrationfn.NewDatabaseUpdateStep())

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
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.steps = append(h.steps, step)
}

// Start begins the hydration process with workers
func (h *Hydrator) Start(ctx context.Context) error {
	h.mutex.Lock()
	if h.isRunning {
		h.mutex.Unlock()
		return fmt.Errorf("hydrator is already running")
	}
	h.isRunning = true
	h.mutex.Unlock()

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
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.isRunning {
		return
	}

	log.Println("Stopping hydrator...")
	close(h.stopChan)
	h.isRunning = false
}

// IsRunning returns whether the hydrator is currently running
func (h *Hydrator) IsRunning() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.isRunning
}

// EnqueueTask adds a task to the hydration queue
func (h *Hydrator) EnqueueTask(task *hydrationfn.HydrationTask) error {
	if !h.IsRunning() {
		return fmt.Errorf("hydrator is not running")
	}

	select {
	case h.taskQueue <- task:
		h.trackTask(task)
		log.Printf("Enqueued hydration task: %s for app: %s (user: %s, source: %s)",
			task.ID, task.AppID, task.UserID, task.SourceID)
		return nil
	default:
		return fmt.Errorf("task queue is full")
	}
}

// worker processes tasks from the queue
func (h *Hydrator) worker(ctx context.Context, workerID int) {
	log.Printf("Hydration worker %d started", workerID)
	defer log.Printf("Hydration worker %d stopped", workerID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopChan:
			return
		case task := <-h.taskQueue:
			if task != nil {
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
							h.markTaskFailed(task)
						}
					}()
					log.Printf("==================== HYDRATION TASK QUEUED FOR RETRY AFTER COOLDOWN ====================")
					return
				} else {
					// Max retries exceeded
					log.Printf("Task failed after max retries: %s", task.ID)
					h.markTaskFailed(task)
					log.Printf("==================== HYDRATION TASK FAILED ====================")
					return
				}
			*/

			// Move failed task to render failed list instead of retrying
			failureReason := err.Error()
			failureStep := step.GetStepName()

			log.Printf("Task %s failed at step %s, moving to render failed list with reason: %s",
				task.ID, failureStep, failureReason)

			h.moveTaskToRenderFailed(task, failureReason, failureStep)
			h.markTaskFailed(task)

			log.Printf("==================== HYDRATION TASK MOVED TO RENDER FAILED LIST ====================")
			return
		}

		task.IncrementStep()
		log.Printf("Step %d (%s) completed for task: %s", i+1, step.GetStepName(), task.ID)
		log.Printf("-------- HYDRATION STEP %d/%d COMPLETED: %s --------", i+1, len(h.steps), step.GetStepName())
	}

	// All steps completed successfully
	task.SetStatus(hydrationfn.TaskStatusCompleted)
	h.markTaskCompleted(task)

	log.Printf("Task completed successfully: %s for app: %s", task.ID, task.AppID)
	log.Printf("==================== HYDRATION TASK COMPLETED ====================")
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
	h.cache.Mutex.RLock()
	defer h.cache.Mutex.RUnlock()

	for userID, userData := range h.cache.Users {
		// No nested locks needed since we already hold the global lock
		for sourceID, sourceData := range userData.Sources {
			// No nested locks needed since we already hold the global lock

			// Log source type for debugging - both local and remote should be processed
			log.Printf("Checking pending data for user: %s, source: %s, type: %s", userID, sourceID, sourceData.Type)

			// Check if there's pending data - process both local and remote sources
			if len(sourceData.AppInfoLatestPending) > 0 {
				log.Printf("Found %d pending apps for user: %s, source: %s, type: %s",
					len(sourceData.AppInfoLatestPending), userID, sourceID, sourceData.Type)
				for _, pendingData := range sourceData.AppInfoLatestPending {
					h.createTasksFromPendingData(userID, sourceID, pendingData)
				}
			}
		}
	}
}

// createTasksFromPendingData creates hydration tasks from pending app data
func (h *Hydrator) createTasksFromPendingData(userID, sourceID string, pendingData *types.AppInfoLatestPendingData) {
	if pendingData == nil {
		return
	}

	// For the new structure, we can work with RawData if it exists
	if pendingData.RawData != nil {
		// Handle regular structured RawData
		appID := pendingData.RawData.AppID
		if appID == "" {
			appID = pendingData.RawData.ID
		}

		if appID != "" {
			// Check if app is already in render failed list
			if h.isAppInRenderFailedList(userID, sourceID, appID) {
				log.Printf("App %s (user: %s, source: %s) is already in render failed list, skipping task creation",
					appID, userID, sourceID)
				return
			}

			// Check if app hydration is already complete before creating new task
			if h.isAppHydrationComplete(pendingData) {
				// log.Printf("App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
				// 	appID, userID, sourceID)
				return
			}

			// Check if app already exists in latest queue before creating new task
			if h.isAppInLatestQueue(userID, sourceID, appID) {
				// log.Printf("App already exists in latest queue for app: %s (user: %s, source: %s), skipping task creation",
				// 	appID, userID, sourceID)
				return
			}

			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				// Convert ApplicationInfoEntry to map for task creation
				appDataMap := h.convertApplicationInfoEntryToMap(pendingData.RawData)

				if len(appDataMap) == 0 {
					log.Printf("Warning: Empty app data for app: %s (user: %s, source: %s), skipping task creation",
						appID, userID, sourceID)
					return
				}

				task := hydrationfn.NewHydrationTask(
					userID, sourceID, appID,
					appDataMap, h.cache, h.settingsManager,
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
	if pendingData == nil {
		return false
	}

	// Quick fail-fast checks for most common missing data
	if pendingData.RawPackage == "" {
		// Only log detailed messages in debug builds to reduce noise
		return false
	}

	if pendingData.RenderedPackage == "" {
		return false
	}

	// Check if AppInfo exists - this is created during hydration
	if pendingData.AppInfo == nil {
		return false
	}

	imageAnalysis := pendingData.AppInfo.ImageAnalysis
	if imageAnalysis == nil {
		return false
	}

	// More flexible image analysis validation - consider private images as valid
	// If image analysis exists and has been performed (regardless of public/private), consider it complete
	if imageAnalysis.TotalImages > 0 {
		// Images found and analyzed (including private images)
		return true
	}

	// For apps with no images at all, check if analysis was attempted
	// If TotalImages is 0 but Images map exists, it means analysis was done but no images found
	if imageAnalysis.TotalImages == 0 && imageAnalysis.Images != nil {
		// Analysis completed - no images found in this app
		return true
	}

	// Analysis not performed or incomplete
	return false
}

// isAppDataHydrationComplete checks if an app's hydration is complete by looking up pending data in cache
func (h *Hydrator) isAppDataHydrationComplete(userID, sourceID, appID string) bool {
	// Get the source data from cache using global lock
	h.cache.Mutex.RLock()
	defer h.cache.Mutex.RUnlock()

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

	// If no pending data found for this app, consider it not hydrated
	return false
}

// convertApplicationInfoEntryToMap converts ApplicationInfoEntry to map for task creation
func (h *Hydrator) convertApplicationInfoEntryToMap(entry *types.ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		return make(map[string]interface{})
	}

	return map[string]interface{}{
		"id":          entry.ID,
		"name":        entry.Name,
		"cfgType":     entry.CfgType,
		"chartName":   entry.ChartName,
		"icon":        entry.Icon,
		"description": entry.Description,
		"appID":       entry.AppID,
		"title":       entry.Title,
		"version":     entry.Version,
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
		"variants":       entry.Variants,

		"screenshots": entry.Screenshots,
		"tags":        entry.Tags,
		"metadata":    entry.Metadata,
		"updated_at":  entry.UpdatedAt,
	}
}

// hasActiveTaskForApp checks if there's already an active task for the given app
func (h *Hydrator) hasActiveTaskForApp(userID, sourceID, appID string) bool {
	h.taskMutex.RLock()
	defer h.taskMutex.RUnlock()

	for _, task := range h.activeTasks {
		if task.UserID == userID && task.SourceID == sourceID && task.AppID == appID {
			return true
		}
	}
	return false
}

// trackTask adds task to active tasks tracking
func (h *Hydrator) trackTask(task *hydrationfn.HydrationTask) {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()
	h.activeTasks[task.ID] = task
}

// markTaskCompleted moves task from active to completed
func (h *Hydrator) markTaskCompleted(task *hydrationfn.HydrationTask) {
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
func (h *Hydrator) markTaskFailed(task *hydrationfn.HydrationTask) {
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

	// Clean up file resources after releasing the lock
	if sourceChartPath != "" {
		if err := os.Remove(sourceChartPath); err != nil {
			log.Printf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}
}

// moveTaskToRenderFailed moves a failed task to the render failed list in cache
func (h *Hydrator) moveTaskToRenderFailed(task *hydrationfn.HydrationTask, failureReason string, failureStep string) {
	if h.cacheManager == nil {
		log.Printf("Warning: Cache manager not set, cannot move task to render failed list")
		return
	}

	// Find the pending data for this task
	h.cache.Mutex.RLock()
	userData, userExists := h.cache.Users[task.UserID]
	if !userExists {
		h.cache.Mutex.RUnlock()
		log.Printf("Warning: User data not found for task: %s, user: %s", task.ID, task.UserID)
		return
	}

	sourceData, sourceExists := userData.Sources[task.SourceID]
	if !sourceExists {
		h.cache.Mutex.RUnlock()
		log.Printf("Warning: Source data not found for task: %s, user: %s, source: %s", task.ID, task.UserID, task.SourceID)
		return
	}

	// Find the pending data for this app
	var pendingData *types.AppInfoLatestPendingData
	for _, pending := range sourceData.AppInfoLatestPending {
		if pending.RawData != nil &&
			(pending.RawData.ID == task.AppID || pending.RawData.AppID == task.AppID || pending.RawData.Name == task.AppID) {
			pendingData = pending
			break
		}
	}
	h.cache.Mutex.RUnlock()

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
	h.cache.Mutex.Lock()
	defer h.cache.Mutex.Unlock()

	userData, userExists := h.cache.Users[userID]
	if !userExists {
		return
	}

	sourceData, sourceExists := userData.Sources[sourceID]
	if !sourceExists {
		return
	}

	// Find and remove the app from pending list
	for i, pending := range sourceData.AppInfoLatestPending {
		if pending.RawData != nil &&
			(pending.RawData.ID == appID || pending.RawData.AppID == appID || pending.RawData.Name == appID) {
			// Remove from slice
			sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending[:i], sourceData.AppInfoLatestPending[i+1:]...)
			log.Printf("Removed app %s from pending list for user: %s, source: %s", appID, userID, sourceID)
			break
		}
	}
}

// GetMetrics returns hydrator metrics
func (h *Hydrator) GetMetrics() HydratorMetrics {
	h.taskMutex.RLock()
	defer h.taskMutex.RUnlock()

	return HydratorMetrics{
		TotalTasksProcessed: h.totalTasksProcessed,
		TotalTasksSucceeded: h.totalTasksSucceeded,
		TotalTasksFailed:    h.totalTasksFailed,
		ActiveTasksCount:    int64(len(h.activeTasks)),
		CompletedTasksCount: int64(len(h.completedTasks)),
		FailedTasksCount:    int64(len(h.failedTasks)),
		QueueLength:         int64(len(h.taskQueue)),
	}
}

// HydratorMetrics contains metrics for the hydrator
type HydratorMetrics struct {
	TotalTasksProcessed int64 `json:"total_tasks_processed"`
	TotalTasksSucceeded int64 `json:"total_tasks_succeeded"`
	TotalTasksFailed    int64 `json:"total_tasks_failed"`
	ActiveTasksCount    int64 `json:"active_tasks_count"`
	CompletedTasksCount int64 `json:"completed_tasks_count"`
	FailedTasksCount    int64 `json:"failed_tasks_count"`
	QueueLength         int64 `json:"queue_length"`
}

// CreateDefaultHydrator creates a hydrator with default configuration
func CreateDefaultHydrator(cache *types.CacheData, settingsManager *settings.SettingsManager) *Hydrator {
	config := DefaultHydratorConfig()
	return NewHydrator(cache, settingsManager, config)
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
			// Check if task already exists for this app to avoid duplicates
			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				// Check if app is already in render failed list
				if h.isAppInRenderFailedList(userID, sourceID, appID) {
					log.Printf("App %s (user: %s, source: %s) is already in render failed list, skipping task creation",
						appID, userID, sourceID)
					continue
				}

				// Check if app hydration is already complete before creating new task
				if h.isAppInLatestQueue(userID, sourceID, appID) {
					// log.Printf("App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
					// 	appID, userID, sourceID)
					continue
				}

				if len(appMap) == 0 {
					log.Printf("Warning: Empty app data for app: %s (user: %s, source: %s), skipping task creation",
						appID, userID, sourceID)
					continue
				}

				// Create and submit task using correct NewHydrationTask signature
				task := hydrationfn.NewHydrationTask(
					userID, sourceID, appID,
					appMap, h.cache, h.settingsManager,
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
			// Check for common app fields
			hasAppFields := false
			appFields := []string{"id", "name", "title", "version", "description", "icon"}

			for _, field := range appFields {
				if _, hasField := appMap[field]; hasField {
					hasAppFields = true
					break
				}
			}

			if hasAppFields {
				sampleCount++
			} else {
				// If this entry doesn't look like an app, it's probably not an apps map
				return false
			}
		} else {
			// Non-map entries suggest this is not an apps map
			return false
		}
	}

	return sampleCount > 0
}

// SetCacheManager sets the cache manager for database synchronization
func (h *Hydrator) SetCacheManager(cacheManager *CacheManager) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.cacheManager = cacheManager
	h.lastSyncTime = time.Now()
	log.Printf("Cache manager set for hydrator with sync interval: %v", h.syncInterval)
}

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
	h.taskMutex.RLock()
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
	h.taskMutex.RLock()
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
	h.taskMutex.RLock()
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

		log.Printf("Cleaned up %d old completed tasks from memory", removed)
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
	h.taskMutex.Lock()
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

// isAppInLatestQueue checks if an app already exists in the AppInfoLatest queue
func (h *Hydrator) isAppInLatestQueue(userID, sourceID, appID string) bool {
	// Get the source data from cache using global lock
	h.cache.Mutex.RLock()
	defer h.cache.Mutex.RUnlock()

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
				return true
			}
		}

		// Check AppInfo.AppEntry
		if latestData.AppInfo != nil && latestData.AppInfo.AppEntry != nil {
			if latestData.AppInfo.AppEntry.ID == appID ||
				latestData.AppInfo.AppEntry.AppID == appID ||
				latestData.AppInfo.AppEntry.Name == appID {
				return true
			}
		}

		// Check AppSimpleInfo
		if latestData.AppSimpleInfo != nil {
			if latestData.AppSimpleInfo.AppID == appID ||
				latestData.AppSimpleInfo.AppName == appID {
				return true
			}
		}
	}

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

	// Create and submit task
	task := hydrationfn.NewHydrationTask(
		userID, sourceID, appID,
		appDataMap, h.cache, h.settingsManager,
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
	// Get the source data from cache using global lock
	h.cache.Mutex.RLock()
	defer h.cache.Mutex.RUnlock()

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

	return false
}
