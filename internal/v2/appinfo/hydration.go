package appinfo

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// Hydrator manages the hydration process with task queue and workers
// Hydrator 管理带有任务队列和工作器的水合过程
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
	// 任务跟踪
	activeTasks    map[string]*hydrationfn.HydrationTask
	completedTasks map[string]*hydrationfn.HydrationTask
	failedTasks    map[string]*hydrationfn.HydrationTask
	taskMutex      sync.RWMutex

	// Batch completion tracking
	// 批量完成跟踪
	batchCompletionQueue chan string   // Queue for completed tasks
	completedTaskCount   int64         // Total completed tasks counter
	lastSyncTime         time.Time     // Last database sync time
	syncInterval         time.Duration // Interval for database sync

	// Metrics
	// 指标
	totalTasksProcessed int64
	totalTasksSucceeded int64
	totalTasksFailed    int64
	metricsMutex        sync.RWMutex
}

// NewHydrator creates a new hydrator with the given configuration
// NewHydrator 使用给定配置创建新的水合器
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
	}

	// Add default steps
	// 添加默认步骤
	hydrator.AddStep(hydrationfn.NewSourceChartStep())
	hydrator.AddStep(hydrationfn.NewRenderedChartStep())
	hydrator.AddStep(hydrationfn.NewCustomParamsUpdateStep())
	hydrator.AddStep(hydrationfn.NewImageAnalysisStep())
	hydrator.AddStep(hydrationfn.NewDatabaseUpdateStep())

	return hydrator
}

// HydratorConfig contains configuration for the hydrator
// HydratorConfig 包含水合器的配置
type HydratorConfig struct {
	QueueSize   int `json:"queue_size"`   // Task queue size
	WorkerCount int `json:"worker_count"` // Number of worker goroutines
}

// DefaultHydratorConfig returns default configuration
// DefaultHydratorConfig 返回默认配置
func DefaultHydratorConfig() HydratorConfig {
	return HydratorConfig{
		QueueSize:   1000,
		WorkerCount: 5,
	}
}

// AddStep adds a hydration step to the hydrator
// AddStep 向水合器添加水合步骤
func (h *Hydrator) AddStep(step hydrationfn.HydrationStep) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.steps = append(h.steps, step)
}

// Start begins the hydration process with workers
// Start 开始带有工作器的水合过程
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
	// 启动工作器协程
	for i := 0; i < h.workerCount; i++ {
		go h.worker(ctx, i)
	}

	// Start pending data monitor
	// 启动待处理数据监控器
	go h.pendingDataMonitor(ctx)

	// Start batch completion processor
	// 启动批量完成处理器
	go h.batchCompletionProcessor(ctx)

	// Start database sync monitor if cache manager is available
	// 如果缓存管理器可用，启动数据库同步监控器
	if h.cacheManager != nil {
		go h.databaseSyncMonitor(ctx)
	}

	return nil
}

// Stop stops the hydration process
// Stop 停止水合过程
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
// IsRunning 返回水合器是否正在运行
func (h *Hydrator) IsRunning() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.isRunning
}

// EnqueueTask adds a task to the hydration queue
// EnqueueTask 向水合队列添加任务
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
// worker 处理队列中的任务
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
// processTask 处理单个水合任务
func (h *Hydrator) processTask(ctx context.Context, task *hydrationfn.HydrationTask, workerID int) {
	log.Printf("==================== HYDRATION TASK STARTED ====================")
	log.Printf("Worker %d processing task: %s for app: %s", workerID, task.ID, task.AppID)

	// Check if task is in cooldown period
	// 检查任务是否在冷却期内
	if task.LastFailureTime != nil && time.Since(*task.LastFailureTime) < 5*time.Minute {
		log.Printf("Task %s is in cooldown period, skipping. Next retry available at: %v",
			task.ID, task.LastFailureTime.Add(5*time.Minute))
		return
	}

	task.SetStatus(hydrationfn.TaskStatusRunning)

	// Execute all steps
	// 执行所有步骤
	for i, step := range h.steps {
		if task.CurrentStep > i {
			continue // Skip already completed steps
		}

		// Check if step can be skipped
		// 检查步骤是否可以跳过
		if step.CanSkip(ctx, task) {
			log.Printf("Skipping step %d (%s) for task: %s", i+1, step.GetStepName(), task.ID)
			log.Printf("-------- HYDRATION STEP %d/%d SKIPPED: %s --------", i+1, len(h.steps), step.GetStepName())
			task.IncrementStep()
			continue
		}

		log.Printf("-------- HYDRATION STEP %d/%d STARTED: %s --------", i+1, len(h.steps), step.GetStepName())
		log.Printf("Executing step %d (%s) for task: %s", i+1, step.GetStepName(), task.ID)

		// Execute step
		// 执行步骤
		if err := step.Execute(ctx, task); err != nil {
			log.Printf("Step %d (%s) failed for task: %s, error: %v", i+1, step.GetStepName(), task.ID, err)
			log.Printf("-------- HYDRATION STEP %d/%d FAILED: %s --------", i+1, len(h.steps), step.GetStepName())
			task.SetError(err)

			// Clean up resources before retry or failure
			// 在重试或失败前清理资源
			h.cleanupTaskResources(task)

			// Set failure time for cooldown period
			// 设置失败时间用于冷却期
			now := time.Now()
			task.LastFailureTime = &now

			// Check if task can be retried
			// 检查任务是否可以重试
			if task.CanRetry() {
				log.Printf("Task %s failed, will retry after cooldown period (5 minutes). Next retry available at: %v",
					task.ID, task.LastFailureTime.Add(5*time.Minute))
				task.ResetForRetry()

				// Re-enqueue for retry after cooldown
				// 冷却期后重新入队重试
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
				// 超过最大重试次数
				log.Printf("Task failed after max retries: %s", task.ID)
				h.markTaskFailed(task)
				log.Printf("==================== HYDRATION TASK FAILED ====================")
				return
			}
		}

		task.IncrementStep()
		log.Printf("Step %d (%s) completed for task: %s", i+1, step.GetStepName(), task.ID)
		log.Printf("-------- HYDRATION STEP %d/%d COMPLETED: %s --------", i+1, len(h.steps), step.GetStepName())
	}

	// All steps completed successfully
	// 所有步骤成功完成
	task.SetStatus(hydrationfn.TaskStatusCompleted)
	h.markTaskCompleted(task)

	log.Printf("Task completed successfully: %s for app: %s", task.ID, task.AppID)
	log.Printf("==================== HYDRATION TASK COMPLETED ====================")
}

// cleanupTaskResources cleans up resources associated with a task
// cleanupTaskResources 清理与任务相关的资源
func (h *Hydrator) cleanupTaskResources(task *hydrationfn.HydrationTask) {
	// Clean up chart data
	// 清理chart数据
	if renderedDir, exists := task.ChartData["rendered_chart_dir"].(string); exists {
		if err := os.RemoveAll(renderedDir); err != nil {
			log.Printf("Warning: Failed to clean up rendered chart directory %s: %v", renderedDir, err)
		}
	}

	// Clean up source chart
	// 清理源chart
	if sourceChartPath, exists := task.ChartData["source_chart_path"].(string); exists {
		if err := os.Remove(sourceChartPath); err != nil {
			log.Printf("Warning: Failed to clean up source chart file %s: %v", sourceChartPath, err)
		}
	}

	// Clear task data maps
	// 清理任务数据映射
	task.ChartData = make(map[string]interface{})
	task.DatabaseUpdateData = make(map[string]interface{})
}

// pendingDataMonitor monitors for new pending data and creates tasks
// pendingDataMonitor 监控新的待处理数据并创建任务
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
// checkForPendingData 扫描缓存中的待处理数据并创建水合任务
func (h *Hydrator) checkForPendingData() {
	h.cache.Mutex.RLock()
	defer h.cache.Mutex.RUnlock()

	for userID, userData := range h.cache.Users {
		// 不再需要嵌套锁，因为我们已经持有全局锁
		// No nested locks needed since we already hold the global lock
		for sourceID, sourceData := range userData.Sources {
			// 不再需要嵌套锁，因为我们已经持有全局锁
			// No nested locks needed since we already hold the global lock

			// Log source type for debugging - both local and remote should be processed
			// 记录源类型以供调试 - 本地和远程类型都应被处理
			log.Printf("Checking pending data for user: %s, source: %s, type: %s", userID, sourceID, sourceData.Type)

			// Check if there's pending data - process both local and remote sources
			// 检查是否有待处理数据 - 处理本地和远程源
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
// createTasksFromPendingData 从待处理应用数据创建水合任务
func (h *Hydrator) createTasksFromPendingData(userID, sourceID string, pendingData *types.AppInfoLatestPendingData) {
	if pendingData == nil {
		return
	}

	// For the new structure, we can work with RawData if it exists
	// 对于新结构，如果存在RawData，我们可以使用它
	if pendingData.RawData != nil {
		// Check if this is legacy data with metadata containing the real app data
		// 检查这是否是包含真实应用数据元数据的传统数据
		if pendingData.RawData.Metadata != nil {
			// Handle legacy data stored in metadata
			// 处理存储在元数据中的传统数据
			if legacyData, hasLegacyData := pendingData.RawData.Metadata["legacy_data"]; hasLegacyData {
				if legacyDataMap, ok := legacyData.(map[string]interface{}); ok {
					log.Printf("Processing legacy data from metadata for user: %s, source: %s", userID, sourceID)
					h.createTasksFromPendingDataLegacy(userID, sourceID, legacyDataMap)
					return
				}
			}

			// Handle legacy raw data stored in metadata
			// 处理存储在元数据中的传统原始数据
			if legacyRawData, hasLegacyRawData := pendingData.RawData.Metadata["legacy_raw_data"]; hasLegacyRawData {
				if legacyRawDataMap, ok := legacyRawData.(map[string]interface{}); ok {
					log.Printf("Processing legacy raw data from metadata for user: %s, source: %s", userID, sourceID)
					h.createTasksFromPendingDataLegacy(userID, sourceID, legacyRawDataMap)
					return
				}
			}
		}

		// Handle regular structured RawData (not legacy)
		// 处理常规结构化的RawData（非传统格式）
		appID := pendingData.RawData.AppID
		if appID == "" {
			appID = pendingData.RawData.ID
		}

		if appID != "" {
			// Check if app hydration is already complete before creating new task
			// 在创建新任务前检查应用水合是否已完成
			if h.isAppHydrationComplete(pendingData) {
				// log.Printf("App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
				// 	appID, userID, sourceID)
				return
			}

			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				// Convert ApplicationInfoEntry to map for task creation
				// 将ApplicationInfoEntry转换为map以创建任务
				appDataMap := h.convertApplicationInfoEntryToMap(pendingData.RawData)

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

	// Legacy handling: This should be deprecated but kept for backward compatibility
	// 传统处理：这应该被弃用，但为了向后兼容而保留
	log.Printf("Warning: createTasksFromPendingData called with legacy data structure")
}

// isAppHydrationComplete checks if an app has completed all hydration steps
// isAppHydrationComplete 检查应用是否已完成所有水合步骤
func (h *Hydrator) isAppHydrationComplete(pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		return false
	}

	// Quick fail-fast checks for most common missing data
	// 对最常见缺失数据的快速失败检查
	if pendingData.RawPackage == "" {
		// Only log detailed messages in debug builds to reduce noise
		// 只在调试构建中记录详细消息以减少噪音
		return false
	}

	if pendingData.RenderedPackage == "" {
		return false
	}

	// Check if AppInfo exists - this is created during hydration
	// 检查AppInfo是否存在 - 这在水合过程中创建
	if pendingData.AppInfo == nil {
		return false
	}

	imageAnalysis := pendingData.AppInfo.ImageAnalysis
	if imageAnalysis == nil {
		return false
	}

	// More flexible image analysis validation - consider private images as valid
	// 更灵活的镜像分析验证 - 将私有镜像视为有效
	// If image analysis exists and has been performed (regardless of public/private), consider it complete
	// 如果镜像分析存在并已执行（无论公有/私有），认为其已完成
	if imageAnalysis.TotalImages > 0 {
		// Images found and analyzed (including private images)
		// 找到并分析了镜像（包括私有镜像）
		return true
	}

	// For apps with no images at all, check if analysis was attempted
	// 对于完全没有镜像的应用，检查是否尝试过分析
	// If TotalImages is 0 but Images map exists, it means analysis was done but no images found
	// 如果TotalImages为0但Images映射存在，意味着已完成分析但未找到镜像
	if imageAnalysis.TotalImages == 0 && imageAnalysis.Images != nil {
		// Analysis completed - no images found in this app
		// 分析完成 - 在此应用中未找到镜像
		return true
	}

	// Analysis not performed or incomplete
	// 分析未执行或不完整
	return false
}

// isAppDataHydrationComplete checks if an app's hydration is complete by looking up pending data in cache
// isAppDataHydrationComplete 通过在缓存中查找待处理数据来检查应用的水合是否完成
func (h *Hydrator) isAppDataHydrationComplete(userID, sourceID, appID string) bool {
	// Get the source data from cache using global lock
	// 使用全局锁从缓存获取源数据
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
	// 查找特定应用的待处理数据
	for _, pendingData := range sourceData.AppInfoLatestPending {
		if pendingData.RawData != nil &&
			(pendingData.RawData.ID == appID || pendingData.RawData.AppID == appID || pendingData.RawData.Name == appID) {
			// Found the pending data for this app, check if hydration is complete
			// 找到此应用的待处理数据，检查水合是否完成
			return h.isAppHydrationComplete(pendingData)
		}
	}

	// If no pending data found for this app, consider it not hydrated
	// 如果未找到此应用的待处理数据，认为它未被水合
	return false
}

// convertApplicationInfoEntryToMap converts ApplicationInfoEntry to map for task creation
// convertApplicationInfoEntryToMap 将ApplicationInfoEntry转换为map以创建任务
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
// hasActiveTaskForApp 检查给定应用是否已有活动任务
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
// trackTask 将任务添加到活动任务跟踪
func (h *Hydrator) trackTask(task *hydrationfn.HydrationTask) {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()
	h.activeTasks[task.ID] = task
}

// markTaskCompleted moves task from active to completed
// markTaskCompleted 将任务从活动移动到已完成
func (h *Hydrator) markTaskCompleted(task *hydrationfn.HydrationTask) {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()

	delete(h.activeTasks, task.ID)
	h.completedTasks[task.ID] = task

	h.metricsMutex.Lock()
	h.totalTasksProcessed++
	h.totalTasksSucceeded++
	h.metricsMutex.Unlock()

	// Add to batch completion queue for processing
	// 添加到批量完成队列进行处理
	select {
	case h.batchCompletionQueue <- task.ID:
		// Successfully queued for batch processing
		// 成功加入批量处理队列
	default:
		// Queue is full, log warning but don't block
		// 队列已满，记录警告但不阻塞
		log.Printf("Warning: batch completion queue is full, task %s not queued for processing", task.ID)
	}
}

// markTaskFailed moves task from active to failed
// markTaskFailed 将任务从活动移动到失败
func (h *Hydrator) markTaskFailed(task *hydrationfn.HydrationTask) {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()

	task.SetStatus(hydrationfn.TaskStatusFailed)
	delete(h.activeTasks, task.ID)

	// Limit the size of failed tasks map
	// 限制失败任务映射的大小
	maxFailedTasks := 1000
	if len(h.failedTasks) >= maxFailedTasks {
		// Remove oldest failed tasks
		// 删除最旧的失败任务
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

	h.failedTasks[task.ID] = task

	h.metricsMutex.Lock()
	h.totalTasksProcessed++
	h.totalTasksFailed++
	h.metricsMutex.Unlock()

	// Clean up task resources
	// 清理任务资源
	h.cleanupTaskResources(task)
}

// GetMetrics returns hydrator metrics
// GetMetrics 返回水合器指标
func (h *Hydrator) GetMetrics() HydratorMetrics {
	h.metricsMutex.RLock()
	defer h.metricsMutex.RUnlock()

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
// HydratorMetrics 包含水合器的指标
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
// CreateDefaultHydrator 使用默认配置创建水合器
func CreateDefaultHydrator(cache *types.CacheData, settingsManager *settings.SettingsManager) *Hydrator {
	config := DefaultHydratorConfig()
	return NewHydrator(cache, settingsManager, config)
}

// NotifyPendingDataUpdate implements HydrationNotifier interface
// Processes pending data update notification and creates hydration tasks immediately
// NotifyPendingDataUpdate 实现HydrationNotifier接口
// 处理待处理数据更新通知并立即创建水合任务
func (h *Hydrator) NotifyPendingDataUpdate(userID, sourceID string, pendingData map[string]interface{}) {
	if !h.IsRunning() {
		log.Printf("Hydrator is not running, ignoring pending data notification for user: %s, source: %s", userID, sourceID)
		return
	}

	log.Printf("Received pending data update notification for user: %s, source: %s", userID, sourceID)

	// Create tasks from the pending data immediately
	// 立即从待处理数据创建任务
	h.createTasksFromPendingDataMap(userID, sourceID, pendingData)
}

// createTasksFromPendingDataMap creates hydration tasks from pending data map
// createTasksFromPendingDataMap 从待处理数据映射创建水合任务
func (h *Hydrator) createTasksFromPendingDataMap(userID, sourceID string, pendingData map[string]interface{}) {
	log.Printf("Creating tasks from pending data for user: %s, source: %s", userID, sourceID)

	// Extract data section from pendingData
	// 从pendingData中提取数据部分
	dataSection, ok := pendingData["data"]
	if !ok {
		log.Printf("No data section found in pending data for user: %s, source: %s", userID, sourceID)
		return
	}

	// Handle different data section formats
	// 处理不同的数据部分格式
	var appsMap map[string]interface{}

	// First, try to handle the case where dataSection is an AppStoreDataSection struct
	// 首先，尝试处理dataSection是AppStoreDataSection结构体的情况
	log.Printf("Data section type: %T for user: %s, source: %s", dataSection, userID, sourceID)

	// Check if it's an AppStoreDataSection by checking if it has Apps field
	// 通过检查是否有Apps字段来判断是否为AppStoreDataSection
	if dataStruct := dataSection; dataStruct != nil {
		// Use reflection or type assertion to access the Apps field
		// 使用反射或类型断言来访问Apps字段

		// Try to access as map first (for backwards compatibility)
		// 首先尝试作为map访问（向后兼容）
		if dataMap, ok := dataSection.(map[string]interface{}); ok {
			// Check if it's in the expected format with "apps" key
			// 检查是否为预期格式（包含"apps"键）
			if apps, hasApps := dataMap["apps"]; hasApps {
				if appsMapValue, ok := apps.(map[string]interface{}); ok {
					appsMap = appsMapValue
					log.Printf("Found apps data in standard map format for user: %s, source: %s", userID, sourceID)
				}
			} else {
				// Check if the dataMap itself contains app entries
				// 检查dataMap本身是否包含应用条目
				if h.looksLikeAppsMap(dataMap) {
					appsMap = dataMap
					log.Printf("Data section appears to contain apps directly for user: %s, source: %s", userID, sourceID)
				}
			}
		} else {
			// Try to handle AppStoreDataSection struct using interface conversion
			// 尝试使用接口转换处理AppStoreDataSection结构体
			log.Printf("Attempting to handle AppStoreDataSection struct for user: %s, source: %s", userID, sourceID)

			// Convert struct to map using interface{} conversion
			// 使用interface{}转换将结构体转换为map
			if appsData := h.extractAppsFromStruct(dataSection); appsData != nil {
				appsMap = appsData
				log.Printf("Successfully extracted apps from AppStoreDataSection struct for user: %s, source: %s", userID, sourceID)
			} else {
				log.Printf("Failed to extract apps from data structure for user: %s, source: %s", userID, sourceID)
				return
			}
		}
	}

	if appsMap == nil || len(appsMap) == 0 {
		log.Printf("No apps found in pending data for user: %s, source: %s", userID, sourceID)
		return
	}

	log.Printf("Found %d apps in pending data for user: %s, source: %s", len(appsMap), userID, sourceID)

	// Create hydration task for each app
	// 为每个应用创建水合任务
	for appID, appData := range appsMap {
		// Validate app data
		// 验证应用数据
		if appMap, ok := appData.(map[string]interface{}); ok {
			// Check if task already exists for this app to avoid duplicates
			// 检查此应用是否已存在任务以避免重复
			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				// Check if app hydration is already complete before creating new task
				// 在创建新任务前检查应用水合是否已完成
				if h.isAppDataHydrationComplete(userID, sourceID, appID) {
					// log.Printf("App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
					// 	appID, userID, sourceID)
					continue
				}

				// Create and submit task using correct NewHydrationTask signature
				// 使用正确的NewHydrationTask签名创建并提交任务
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

// extractAppsFromStruct attempts to extract apps data from AppStoreDataSection struct
// extractAppsFromStruct 尝试从AppStoreDataSection结构体中提取应用数据
func (h *Hydrator) extractAppsFromStruct(dataSection interface{}) map[string]interface{} {
	// Try to use type assertion for known struct types
	// 尝试对已知结构体类型使用类型断言

	// Since we can't import syncerfn here due to circular dependency,
	// we'll use reflection-like approach through interface{}
	// 由于循环依赖无法在此导入syncerfn，我们将通过interface{}使用类似反射的方法

	// Try to access the struct fields dynamically
	// 尝试动态访问结构体字段
	if structMap, ok := dataSection.(map[string]interface{}); ok {
		// If it's already a map, look for apps
		// 如果已经是map，查找apps
		if apps, hasApps := structMap["apps"]; hasApps {
			if appsMap, ok := apps.(map[string]interface{}); ok {
				return appsMap
			}
		}
		// If no apps key, but the whole map looks like apps data
		// 如果没有apps键，但整个map看起来像应用数据
		if h.looksLikeAppsMap(structMap) {
			return structMap
		}
	}

	// Try to convert struct to map using JSON marshal/unmarshal
	// 尝试使用JSON marshal/unmarshal将结构体转换为map
	return h.convertStructToMap(dataSection)
}

// looksLikeAppsMap checks if a map looks like it contains app entries
// looksLikeAppsMap 检查映射是否看起来包含应用条目
func (h *Hydrator) looksLikeAppsMap(data map[string]interface{}) bool {
	// Sample a few entries to see if they look like app data
	// 采样几个条目看是否像应用数据
	sampleCount := 0
	maxSamples := 3

	for _, value := range data {
		if sampleCount >= maxSamples {
			break
		}

		if appMap, ok := value.(map[string]interface{}); ok {
			// Check for common app fields
			// 检查常见的应用字段
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
				// 如果此条目不像应用，那么这可能不是应用映射
				return false
			}
		} else {
			// Non-map entries suggest this is not an apps map
			// 非映射条目表明这不是应用映射
			return false
		}
	}

	return sampleCount > 0
}

// convertStructToMap converts a struct to map[string]interface{} using JSON
// convertStructToMap 使用JSON将结构体转换为map[string]interface{}
func (h *Hydrator) convertStructToMap(data interface{}) map[string]interface{} {
	// Import encoding/json in the file imports if not already present
	// For AppStoreDataSection struct, we need to access its Apps field
	// 对于AppStoreDataSection结构体，我们需要访问其Apps字段

	// Use type assertion to check if it's the AppStoreDataSection type
	// 使用类型断言检查是否为AppStoreDataSection类型

	// Since we can't directly import syncerfn.AppStoreDataSection due to circular dependency,
	// we'll try to access the Apps field using reflection-like approach
	// 由于循环依赖不能直接导入syncerfn.AppStoreDataSection，我们将尝试使用类似反射的方法访问Apps字段

	// For now, we'll try a different approach - check if the data has the expected structure
	// 现在，我们将尝试不同的方法 - 检查数据是否具有预期的结构

	// Try to access fields that AppStoreDataSection should have
	// 尝试访问AppStoreDataSection应该具有的字段
	if v := data; v != nil {
		// Use interface{} to try accessing common fields
		// 使用interface{}尝试访问通用字段
		log.Printf("Attempting to extract data from struct type: %T", data)

		// Since we know the structure has Apps, Recommends, etc., try to access them
		// 由于我们知道结构有Apps、Recommends等，尝试访问它们
		// We'll return nil for now and log the issue
		// 暂时返回nil并记录问题
		log.Printf("Unable to convert struct to map - need to handle AppStoreDataSection conversion")

		// TODO: Implement proper struct to map conversion
		// This would require either:
		// 1. Using reflection (reflect package)
		// 2. Having AppStoreDataSection implement a ToMap() method
		// 3. Using JSON marshal/unmarshal (but that's expensive)
		// TODO: 实现适当的结构体到map转换
		// 这需要：
		// 1. 使用反射（reflect包）
		// 2. 让AppStoreDataSection实现ToMap()方法
		// 3. 使用JSON marshal/unmarshal（但这很昂贵）
	}

	return nil
}

// SetCacheManager sets the cache manager for database synchronization
// SetCacheManager 设置缓存管理器以进行数据库同步
func (h *Hydrator) SetCacheManager(cacheManager *CacheManager) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.cacheManager = cacheManager
	h.lastSyncTime = time.Now()
	log.Printf("Cache manager set for hydrator with sync interval: %v", h.syncInterval)
}

// batchCompletionProcessor processes completed tasks in batches
// batchCompletionProcessor 批量处理已完成的任务
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
		case <-ticker.C:
			h.processBatchCompletions()
		}
	}
}

// databaseSyncMonitor monitors for database synchronization needs
// databaseSyncMonitor 监控数据库同步需求
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
// processCompletedTask 处理单个已完成的任务
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
	// 如果需要，可以在这里添加额外的处理
}

// processBatchCompletions processes completed tasks in batches
// processBatchCompletions 批量处理已完成的任务
func (h *Hydrator) processBatchCompletions() {
	h.metricsMutex.RLock()
	currentCompleted := h.totalTasksSucceeded
	h.metricsMutex.RUnlock()

	// Check if significant number of tasks completed since last sync
	// 检查自上次同步以来是否有大量任务完成
	if currentCompleted > h.completedTaskCount+10 { // Trigger sync after 10 more completions
		log.Printf("Batch completion detected: %d tasks completed since last check",
			currentCompleted-h.completedTaskCount)
		h.completedTaskCount = currentCompleted
		h.triggerDatabaseSync()
	}
}

// checkAndSyncToDatabase checks if database sync is needed and performs it
// checkAndSyncToDatabase 检查是否需要数据库同步并执行
func (h *Hydrator) checkAndSyncToDatabase() {
	if h.cacheManager == nil {
		log.Printf("Warning: Cache manager not set, skipping database sync")
		return
	}

	// Check if there are completed tasks that need syncing
	// 检查是否有需要同步的已完成任务
	h.taskMutex.RLock()
	completedCount := len(h.completedTasks)
	h.taskMutex.RUnlock()

	if completedCount == 0 {
		return // No completed tasks to sync
	}

	// Check if enough time has passed since last sync
	// 检查自上次同步以来是否已经过了足够的时间
	if time.Since(h.lastSyncTime) >= h.syncInterval {
		log.Printf("Periodic database sync triggered - %d completed tasks to sync", completedCount)
		h.triggerDatabaseSync()
	}
}

// triggerDatabaseSync triggers synchronization of cache data to database
// triggerDatabaseSync 触发缓存数据到数据库的同步
func (h *Hydrator) triggerDatabaseSync() {
	if h.cacheManager == nil {
		log.Printf("Warning: Cache manager not set, cannot sync to database")
		return
	}

	log.Printf("Triggering database synchronization")

	// Force sync all cache data to Redis/database
	// 强制同步所有缓存数据到Redis/数据库
	if err := h.cacheManager.ForceSync(); err != nil {
		log.Printf("Error during database sync: %v", err)
	} else {
		log.Printf("Database sync completed successfully")
		h.lastSyncTime = time.Now()

		// Optionally clean up old completed tasks to prevent memory growth
		// 可选择清理旧的已完成任务以防止内存增长
		h.cleanupOldCompletedTasks()
	}
}

// cleanupOldCompletedTasks removes old completed tasks from memory
// cleanupOldCompletedTasks 从内存中删除旧的已完成任务
func (h *Hydrator) cleanupOldCompletedTasks() {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()

	// Keep only the most recent 100 completed tasks
	// 只保留最近的100个已完成任务
	maxCompletedTasks := 100
	if len(h.completedTasks) > maxCompletedTasks {
		// Convert to slice to sort by completion time
		// 转换为切片以按完成时间排序
		tasks := make([]*hydrationfn.HydrationTask, 0, len(h.completedTasks))
		for _, task := range h.completedTasks {
			tasks = append(tasks, task)
		}

		// Sort by UpdatedAt time (most recent first)
		// 按UpdatedAt时间排序（最新的在前）
		// Simple implementation: remove half of the tasks
		// 简单实现：删除一半的任务
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

// createTasksFromPendingDataLegacy creates hydration tasks from legacy pending data format
// createTasksFromPendingDataLegacy 从传统的待处理数据格式创建水合任务
func (h *Hydrator) createTasksFromPendingDataLegacy(userID, sourceID string, pendingData map[string]interface{}) {
	log.Printf("Creating tasks from pending data for user: %s, source: %s", userID, sourceID)

	// Extract data section from pendingData
	// 从pendingData中提取数据部分
	dataSection, ok := pendingData["data"]
	if !ok {
		log.Printf("No data section found in pending data for user: %s, source: %s", userID, sourceID)
		return
	}

	// Handle different data section formats
	// 处理不同的数据部分格式
	var appsMap map[string]interface{}

	// First, try to handle the case where dataSection is an AppStoreDataSection struct
	// 首先，尝试处理dataSection是AppStoreDataSection结构体的情况
	log.Printf("Data section type: %T for user: %s, source: %s", dataSection, userID, sourceID)

	// Check if it's an AppStoreDataSection by checking if it has Apps field
	// 通过检查是否有Apps字段来判断是否为AppStoreDataSection
	if dataStruct := dataSection; dataStruct != nil {
		// Use reflection or type assertion to access the Apps field
		// 使用反射或类型断言来访问Apps字段

		// Try to access as map first (for backwards compatibility)
		// 首先尝试作为map访问（向后兼容）
		if dataMap, ok := dataSection.(map[string]interface{}); ok {
			// Check if it's in the expected format with "apps" key
			// 检查是否为预期格式（包含"apps"键）
			if apps, hasApps := dataMap["apps"]; hasApps {
				if appsMapValue, ok := apps.(map[string]interface{}); ok {
					appsMap = appsMapValue
					log.Printf("Found apps data in standard map format for user: %s, source: %s", userID, sourceID)
				}
			} else {
				// Check if the dataMap itself contains app entries
				// 检查dataMap本身是否包含应用条目
				if h.looksLikeAppsMap(dataMap) {
					appsMap = dataMap
					log.Printf("Data section appears to contain apps directly for user: %s, source: %s", userID, sourceID)
				}
			}
		} else {
			// Try to handle AppStoreDataSection struct using interface conversion
			// 尝试使用接口转换处理AppStoreDataSection结构体
			log.Printf("Attempting to handle AppStoreDataSection struct for user: %s, source: %s", userID, sourceID)

			// Convert struct to map using interface{} conversion
			// 使用interface{}转换将结构体转换为map
			if appsData := h.extractAppsFromStruct(dataSection); appsData != nil {
				appsMap = appsData
				log.Printf("Successfully extracted apps from AppStoreDataSection struct for user: %s, source: %s", userID, sourceID)
			} else {
				log.Printf("Failed to extract apps from data structure for user: %s, source: %s", userID, sourceID)
				return
			}
		}
	}

	if appsMap == nil || len(appsMap) == 0 {
		log.Printf("No apps found in pending data for user: %s, source: %s", userID, sourceID)
		return
	}

	log.Printf("Found %d apps in pending data for user: %s, source: %s", len(appsMap), userID, sourceID)

	// Create hydration tasks for each app
	// 为每个应用创建水合任务
	tasksCreated := 0
	for appID, appDataInterface := range appsMap {
		if appDataMap, ok := appDataInterface.(map[string]interface{}); ok {
			// Check if task already exists for this app
			// 检查该应用是否已有任务存在
			if !h.hasActiveTaskForApp(userID, sourceID, appID) {
				// Check if app hydration is already complete before creating new task
				// 在创建新任务前检查应用水合是否已完成
				if h.isAppDataHydrationComplete(userID, sourceID, appID) {
					// log.Printf("App hydration already complete for app: %s (user: %s, source: %s), skipping task creation",
					// 	appID, userID, sourceID)
					continue
				}

				task := hydrationfn.NewHydrationTask(
					userID, sourceID, appID,
					appDataMap, h.cache, h.settingsManager,
				)

				if err := h.EnqueueTask(task); err != nil {
					log.Printf("Failed to enqueue task for app: %s (user: %s, source: %s), error: %v",
						appID, userID, sourceID, err)
				} else {
					log.Printf("Created hydration task for app: %s (user: %s, source: %s)",
						appID, userID, sourceID)
					tasksCreated++
				}
			} else {
				log.Printf("Task already exists for app: %s (user: %s, source: %s)",
					appID, userID, sourceID)
			}
		} else {
			log.Printf("Warning: app data is not a map for app: %s (user: %s, source: %s)",
				appID, userID, sourceID)
		}
	}

	log.Printf("Created %d hydration tasks from pending data for user: %s, source: %s",
		tasksCreated, userID, sourceID)
}
