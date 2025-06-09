package appinfo

import (
	"context"
	"fmt"
	"log"
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
		steps:           make([]hydrationfn.HydrationStep, 0),
		cache:           cache,
		settingsManager: settingsManager,
		taskQueue:       make(chan *hydrationfn.HydrationTask, config.QueueSize),
		workerCount:     config.WorkerCount,
		stopChan:        make(chan struct{}),
		isRunning:       false,
		activeTasks:     make(map[string]*hydrationfn.HydrationTask),
		completedTasks:  make(map[string]*hydrationfn.HydrationTask),
		failedTasks:     make(map[string]*hydrationfn.HydrationTask),
	}

	// Add default steps
	// 添加默认步骤
	hydrator.AddStep(hydrationfn.NewSourceChartStep())
	hydrator.AddStep(hydrationfn.NewRenderedChartStep())
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
	log.Printf("Worker %d processing task: %s for app: %s", workerID, task.ID, task.AppID)

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
			task.IncrementStep()
			continue
		}

		log.Printf("Executing step %d (%s) for task: %s", i+1, step.GetStepName(), task.ID)

		// Execute step
		// 执行步骤
		if err := step.Execute(ctx, task); err != nil {
			log.Printf("Step %d (%s) failed for task: %s, error: %v", i+1, step.GetStepName(), task.ID, err)
			task.SetError(err)

			// Check if task can be retried
			// 检查任务是否可以重试
			if task.CanRetry() {
				log.Printf("Retrying task: %s (attempt %d/%d)", task.ID, task.RetryCount, task.MaxRetries)
				task.ResetForRetry()

				// Re-enqueue for retry
				// 重新入队重试
				go func() {
					time.Sleep(time.Second * 5) // Wait before retry
					if err := h.EnqueueTask(task); err != nil {
						log.Printf("Failed to re-enqueue task for retry: %s, error: %v", task.ID, err)
						h.markTaskFailed(task)
					}
				}()
				return
			} else {
				// Max retries exceeded
				// 超过最大重试次数
				log.Printf("Task failed after max retries: %s", task.ID)
				h.markTaskFailed(task)
				return
			}
		}

		task.IncrementStep()
		log.Printf("Step %d (%s) completed for task: %s", i+1, step.GetStepName(), task.ID)
	}

	// All steps completed successfully
	// 所有步骤成功完成
	task.SetStatus(hydrationfn.TaskStatusCompleted)
	h.markTaskCompleted(task)

	log.Printf("Task completed successfully: %s for app: %s", task.ID, task.AppID)
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

// checkForPendingData checks cache for pending data and creates hydration tasks
// checkForPendingData 检查缓存中的待处理数据并创建水合任务
func (h *Hydrator) checkForPendingData() {
	h.cache.Mutex.RLock()
	defer h.cache.Mutex.RUnlock()

	for userID, userData := range h.cache.Users {
		userData.Mutex.RLock()
		for sourceID, sourceData := range userData.Sources {
			sourceData.Mutex.RLock()

			// Check if there's pending data
			// 检查是否有待处理数据
			if sourceData.AppInfoLatestPending != nil {
				h.createTasksFromPendingData(userID, sourceID, sourceData.AppInfoLatestPending)
			}

			sourceData.Mutex.RUnlock()
		}
		userData.Mutex.RUnlock()
	}
}

// createTasksFromPendingData creates hydration tasks from pending app data
// createTasksFromPendingData 从待处理应用数据创建水合任务
func (h *Hydrator) createTasksFromPendingData(userID, sourceID string, pendingData *types.AppData) {
	if pendingData == nil || pendingData.Data == nil {
		return
	}

	// Extract apps from pending data
	// 从待处理数据中提取应用
	if dataSection, ok := pendingData.Data["data"]; ok {
		if dataMap, ok := dataSection.(map[string]interface{}); ok {
			if apps, ok := dataMap["apps"]; ok {
				if appsMap, ok := apps.(map[string]interface{}); ok {
					// Create task for each app
					// 为每个应用创建任务
					for appID, appData := range appsMap {
						if appDataMap, ok := appData.(map[string]interface{}); ok {
							// Check if task already exists for this app
							// 检查此应用是否已存在任务
							if !h.hasActiveTaskForApp(userID, sourceID, appID) {
								task := hydrationfn.NewHydrationTask(
									userID, sourceID, appID,
									appDataMap, h.cache, h.settingsManager,
								)

								if err := h.EnqueueTask(task); err != nil {
									log.Printf("Failed to enqueue task for app: %s (user: %s, source: %s), error: %v",
										appID, userID, sourceID, err)
								}
							}
						}
					}
				}
			}
		}
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
}

// markTaskFailed moves task from active to failed
// markTaskFailed 将任务从活动移动到失败
func (h *Hydrator) markTaskFailed(task *hydrationfn.HydrationTask) {
	h.taskMutex.Lock()
	defer h.taskMutex.Unlock()

	task.SetStatus(hydrationfn.TaskStatusFailed)
	delete(h.activeTasks, task.ID)
	h.failedTasks[task.ID] = task

	h.metricsMutex.Lock()
	h.totalTasksProcessed++
	h.totalTasksFailed++
	h.metricsMutex.Unlock()
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
