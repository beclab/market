package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"market/internal/v2/appinfo"
	"market/internal/v2/task"
)

// StateCollector collects runtime state from various modules
type StateCollector struct {
	store         *StateStore
	taskModule    *task.TaskModule
	appInfoModule *appinfo.AppInfoModule
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	collectTicker *time.Ticker
	mu            sync.RWMutex
}

// NewStateCollector creates a new state collector
func NewStateCollector(store *StateStore) *StateCollector {
	ctx, cancel := context.WithCancel(context.Background())
	return &StateCollector{
		store:  store,
		ctx:    ctx,
		cancel: cancel,
	}
}

// SetTaskModule sets the task module reference
func (c *StateCollector) SetTaskModule(tm *task.TaskModule) {
	if !c.mu.TryLock() {
		log.Printf("Failed to acquire lock for SetTaskModule, skipping update")
		return
	}
	defer c.mu.Unlock()
	c.taskModule = tm
}

// SetAppInfoModule sets the app info module reference
func (c *StateCollector) SetAppInfoModule(aim *appinfo.AppInfoModule) {
	if !c.mu.TryLock() {
		log.Printf("Failed to acquire lock for SetAppInfoModule, skipping update")
		return
	}
	defer c.mu.Unlock()
	c.appInfoModule = aim
}

// Start starts the state collector
func (c *StateCollector) Start() error {
	log.Println("Starting runtime state collector...")

	// Start periodic collection (every 5 seconds)
	c.collectTicker = time.NewTicker(5 * time.Second)
	c.wg.Add(1)
	go c.collectLoop()

	// Initial collection
	c.collect()

	log.Println("Runtime state collector started")
	return nil
}

// Stop stops the state collector
func (c *StateCollector) Stop() {
	log.Println("Stopping runtime state collector...")

	if c.collectTicker != nil {
		c.collectTicker.Stop()
	}

	c.cancel()
	c.wg.Wait()

	log.Println("Runtime state collector stopped")
}

// collectLoop runs the periodic collection loop
func (c *StateCollector) collectLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.collectTicker.C:
			c.collect()
		}
	}
}

// collect performs a full state collection
func (c *StateCollector) collect() {
	var taskModule *task.TaskModule
	var appInfoModule *appinfo.AppInfoModule

	if c.mu.TryRLock() {
		taskModule = c.taskModule
		appInfoModule = c.appInfoModule
		c.mu.RUnlock()
	} else {
		log.Printf("Failed to acquire read lock for collect, skipping collection")
		return
	}

	// Collect task states
	if taskModule != nil {
		c.collectTaskStates(taskModule)
	}

	// Collect app flow states
	if appInfoModule != nil {
		c.collectAppFlowStates(appInfoModule)
	}

	// Collect component statuses
	c.collectComponentStatuses(taskModule, appInfoModule)

	// Collect chart repo status
	c.collectChartRepoStatus(appInfoModule)
}

// collectTaskStates collects all task states from TaskModule
func (c *StateCollector) collectTaskStates(tm *task.TaskModule) {
	// Get pending and running tasks from memory
	pendingTasks := tm.GetPendingTasks()
	runningTasks := tm.GetRunningTasks()

	// Get recent tasks from database (includes completed and failed tasks)
	// This aligns with TaskModule's logic: if a task exists in the store, it should be shown
	recentTasks := tm.GetRecentTasks(100) // Get up to 100 recent tasks, matching completedTaskLimit

	// Build a map of all tasks from TaskModule (memory + database)
	taskModuleTaskMap := make(map[string]*task.Task)
	for _, t := range pendingTasks {
		taskModuleTaskMap[t.ID] = t
	}
	for _, t := range runningTasks {
		taskModuleTaskMap[t.ID] = t
	}
	for _, t := range recentTasks {
		// Only add if not already in map (prioritize memory tasks)
		if _, exists := taskModuleTaskMap[t.ID]; !exists {
			taskModuleTaskMap[t.ID] = t
		}
	}

	// Update all tasks that exist in TaskModule
	for _, t := range taskModuleTaskMap {
		var statusOverride string
		if t.Status == task.Pending {
			statusOverride = "pending"
		} else if t.Status == task.Running {
			statusOverride = "running"
		} else {
			// For completed/failed/canceled, use actual status
			statusOverride = ""
		}
		taskState := c.convertTaskToTaskState(t, statusOverride)
		c.store.UpdateTask(taskState)
	}

	// Remove tasks that no longer exist in TaskModule
	allStoredTasks := c.store.GetAllTasks()
	for taskID := range allStoredTasks {
		if _, exists := taskModuleTaskMap[taskID]; !exists {
			c.store.RemoveTask(taskID)
		}
	}
}

// convertTaskToTaskState converts a Task to TaskState
func (c *StateCollector) convertTaskToTaskState(t *task.Task, statusOverride string) *TaskState {
	status := statusOverride
	if statusOverride == "" {
		status = c.taskStatusToString(t.Status)
	}

	taskType := c.taskTypeToString(t.Type)

	taskState := &TaskState{
		TaskID:      t.ID,
		Type:        taskType,
		Status:      status,
		AppName:     t.AppName,
		UserID:      t.User,
		OpID:        t.OpID,
		CreatedAt:   t.CreatedAt,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
		Result:      t.Result,
		ErrorMsg:    t.ErrorMsg,
		Metadata:    make(map[string]interface{}),
	}

	// Copy metadata
	if t.Metadata != nil {
		for k, v := range t.Metadata {
			taskState.Metadata[k] = v
		}
	}

	// Calculate progress based on status
	if taskState.Status == "running" && taskState.StartedAt != nil {
		elapsed := time.Since(*taskState.StartedAt)
		// Simple progress estimation: assume tasks take ~5 minutes max
		progress := int((elapsed.Minutes() / 5.0) * 100)
		if progress > 90 {
			progress = 90 // Cap at 90% until completion
		}
		taskState.Progress = progress
	} else if taskState.Status == "completed" {
		taskState.Progress = 100
	} else if taskState.Status == "failed" || taskState.Status == "canceled" {
		taskState.Progress = 0
	}

	return taskState
}

// collectAppFlowStates collects app flow states from AppInfoModule
func (c *StateCollector) collectAppFlowStates(aim *appinfo.AppInfoModule) {
	cacheManager := aim.GetCacheManager()
	if cacheManager == nil {
		return
	}

	// Get all user data from cache
	allUsersData := cacheManager.GetAllUsersData()
	if allUsersData == nil {
		return
	}

	// Iterate through users and sources to collect app states
	for userID, userData := range allUsersData {
		if userData == nil || userData.Sources == nil {
			continue
		}

		for sourceID, sourceData := range userData.Sources {
			if sourceData == nil {
				continue
			}

			// Process AppStateLatest to get installed app states
			if sourceData.AppStateLatest != nil {
				for _, appState := range sourceData.AppStateLatest {
					if appState == nil {
						continue
					}

					appName := appState.Status.Name
					if appName == "" {
						continue
					}

					// Determine stage and health based on app state
					stage := StageUnknown
					health := "unknown"
					appStateStr := strings.ToLower(appState.Status.State)

					// Determine stage from app state
					if appStateStr == "" {
						stage = StageUnknown
					} else if strings.Contains(appStateStr, "running") {
						stage = StageRunning
						health = "healthy"
					} else if strings.Contains(appStateStr, "downloading") ||
						strings.Contains(appStateStr, "fetching") ||
						strings.Contains(appStateStr, "syncing") {
						stage = StageFetching
						health = "unknown"
					} else if strings.Contains(appStateStr, "installing") {
						stage = StageInstalling
						health = "unknown"
					} else if strings.Contains(appStateStr, "failed") ||
						strings.Contains(appStateStr, "error") {
						stage = StageFailed
						health = "unhealthy"
					} else if strings.Contains(appStateStr, "stopped") {
						stage = StageStopped
						health = "unhealthy"
					} else {
						// Default to running if state is not recognized
						stage = StageRunning
						health = "unknown"
					}

					// First, check if there's pending data (downloading/syncing)
					if sourceData.AppInfoLatestPending != nil {
						for _, pendingData := range sourceData.AppInfoLatestPending {
							if pendingData != nil && pendingData.RawData != nil {
								pendingAppName := ""
								if pendingData.RawData.Name != "" {
									pendingAppName = pendingData.RawData.Name
								} else if pendingData.RawData.AppID != "" {
									pendingAppName = pendingData.RawData.AppID
								} else if pendingData.RawData.ID != "" {
									pendingAppName = pendingData.RawData.ID
								}
								if pendingAppName == appName {
									// App has pending data, likely downloading or syncing
									if stage == StageUnknown || stage == StageRunning {
										stage = StageFetching
										health = "unknown"
									}
									break
								}
							}
						}
					}

					// Check progress field for downloading indicators
					progressStr := appState.Status.Progress
					if progressStr != "" && (stage == StageUnknown || stage == StageRunning) {
						progressLower := strings.ToLower(progressStr)
						if strings.Contains(progressLower, "downloading") ||
							strings.Contains(progressLower, "fetching") ||
							strings.Contains(progressLower, "syncing") {
							stage = StageFetching
							health = "unknown"
						}
					}

					// Check if there's a running task for this app
					var taskModule *task.TaskModule
					if c.mu.TryRLock() {
						taskModule = c.taskModule
						c.mu.RUnlock()
					}

					if taskModule != nil {
						// Check for running install/upgrade tasks (override fetching if installing)
						runningTasks := taskModule.GetRunningTasks()
						for _, t := range runningTasks {
							if t.AppName == appName && t.User == userID {
								if t.Type == task.InstallApp || t.Type == task.CloneApp {
									stage = StageInstalling
									health = "unknown"
								} else if t.Type == task.UpgradeApp {
									stage = StageUpgrading
									health = "unknown"
								} else if t.Type == task.UninstallApp {
									stage = StageUninstalling
									health = "unknown"
								}
								break
							}
						}

						// Check for pending tasks (override fetching if installing)
						if stage == StageFetching || stage == StageRunning {
							pendingTasks := taskModule.GetPendingTasks()
							for _, t := range pendingTasks {
								if t.AppName == appName && t.User == userID {
									if t.Type == task.InstallApp || t.Type == task.CloneApp {
										stage = StageInstalling
									} else if t.Type == task.UpgradeApp {
										stage = StageUpgrading
									} else if t.Type == task.UninstallApp {
										stage = StageUninstalling
									}
									break
								}
							}
						}
					}

					// Get version from AppStateLatestData
					version := appState.Version

					// Try to get CfgType from AppInfoLatest
					cfgType := ""
					if sourceData.AppInfoLatest != nil {
						for _, appInfo := range sourceData.AppInfoLatest {
							if appInfo != nil && appInfo.RawData != nil && appInfo.RawData.Name == appName {
								cfgType = appInfo.RawData.CfgType
								break
							}
						}
					}

					appFlowState := &AppFlowState{
						AppID:      appState.Status.Name,
						AppName:    appName,
						UserID:     userID,
						SourceID:   sourceID,
						Stage:      stage,
						Version:    version,
						CfgType:    cfgType,
						Health:     health,
						LastUpdate: time.Now(),
						Metadata: map[string]interface{}{
							"status": appState.Status,
						},
					}

					c.store.UpdateAppState(appFlowState)
				}
			}
		}
	}
}

// collectComponentStatuses collects component statuses
func (c *StateCollector) collectComponentStatuses(tm *task.TaskModule, aim *appinfo.AppInfoModule) {
	// Collect AppInfoModule status
	if aim != nil {
		moduleStatus := aim.GetModuleStatus()
		if moduleStatus != nil {
			healthy := true
			status := "running"
			message := ""

			if isStarted, ok := moduleStatus["is_started"].(bool); ok && !isStarted {
				healthy = false
				status = "stopped"
				message = "Module not started"
			}

			component := &ComponentStatus{
				Name:      "appinfo_module",
				Healthy:   healthy,
				Status:    status,
				LastCheck: time.Now(),
				Message:   message,
				Metrics:   moduleStatus,
			}
			c.store.UpdateComponent(component)

			// Collect Syncer status separately with detailed information
			if syncerRunning, ok := moduleStatus["syncer_running"].(bool); ok {
				// Get health status from module status if available
				syncerHealthy := syncerRunning
				if healthy, ok := moduleStatus["syncer_healthy"].(bool); ok {
					syncerHealthy = healthy
				}

				syncerStatus := "running"
				if !syncerRunning {
					syncerStatus = "stopped"
				} else if !syncerHealthy {
					syncerStatus = "degraded"
				}

				syncerMetrics := make(map[string]interface{})

				// Get detailed syncer information directly from syncer
				syncer := aim.GetSyncer()
				if syncer != nil {
					metrics := syncer.GetMetrics()
					syncerMetrics = map[string]interface{}{
						"is_running":            metrics.IsRunning,
						"sync_interval":         metrics.SyncInterval.String(),
						"last_sync_time":        metrics.LastSyncTime,
						"last_sync_success":     metrics.LastSyncSuccess,
						"last_sync_error":       metrics.LastSyncError,
						"current_step":          metrics.CurrentStep,
						"current_step_index":    metrics.CurrentStepIndex,
						"total_steps":           metrics.TotalSteps,
						"current_source":        metrics.CurrentSource,
						"total_syncs":           metrics.TotalSyncs,
						"success_count":         metrics.SuccessCount,
						"failure_count":         metrics.FailureCount,
						"consecutive_failures":  metrics.ConsecutiveFailures,
						"last_sync_duration":    metrics.LastSyncDuration.String(),
						"last_synced_app_count": metrics.LastSyncedAppCount,
						"success_rate":          metrics.SuccessRate,
						"next_sync_time":        metrics.NextSyncTime,
					}

					// Add step information
					steps := syncer.GetSteps()
					syncerMetrics["step_count"] = len(steps)

					// Get step names
					stepNames := make([]string, 0, len(steps))
					for _, step := range steps {
						if step != nil {
							stepNames = append(stepNames, step.GetStepName())
						}
					}
					syncerMetrics["steps"] = stepNames

					// Try to get sync interval from module status or config
					if enableSync, ok := moduleStatus["enable_sync"].(bool); ok {
						syncerMetrics["enabled"] = enableSync
					}
				} else {
					// Fallback to basic metrics if syncer is nil
					syncerMetrics["is_running"] = syncerRunning
				}

				// Build status message if there are errors
				statusMessage := ""
				if errMsg, ok := syncerMetrics["last_sync_error"].(string); ok && errMsg != "" {
					statusMessage = fmt.Sprintf("Last error: %s", errMsg)
				}
				if failures, ok := syncerMetrics["consecutive_failures"].(int64); ok && failures > 0 {
					if statusMessage != "" {
						statusMessage += "; "
					}
					statusMessage += fmt.Sprintf("%d consecutive failures", failures)
				}

				syncerComponent := &ComponentStatus{
					Name:      "syncer",
					Healthy:   syncerHealthy,
					Status:    syncerStatus,
					LastCheck: time.Now(),
					Message:   statusMessage,
					Metrics:   syncerMetrics,
				}
				c.store.UpdateComponent(syncerComponent)
			}

			// Collect Hydrator status separately with detailed information
			if hydratorRunning, ok := moduleStatus["hydrator_running"].(bool); ok {
				hydratorHealthy := hydratorRunning
				hydratorStatus := "running"
				if !hydratorRunning {
					hydratorStatus = "stopped"
				}
				hydratorMetrics := make(map[string]interface{})

				// Get metrics directly from hydrator
				hydrator := aim.GetHydrator()
				if hydrator != nil {
					metricsStruct := hydrator.GetMetrics()
					hydratorMetrics = map[string]interface{}{
						"total_tasks_processed":  metricsStruct.TotalTasksProcessed,
						"total_tasks_succeeded":  metricsStruct.TotalTasksSucceeded,
						"total_tasks_failed":     metricsStruct.TotalTasksFailed,
						"active_tasks_count":     metricsStruct.ActiveTasksCount,
						"completed_tasks_count":  metricsStruct.CompletedTasksCount,
						"failed_tasks_count":     metricsStruct.FailedTasksCount,
						"queue_length":           metricsStruct.QueueLength,
						"active_tasks":           metricsStruct.ActiveTasks,
						"recent_completed_tasks": metricsStruct.RecentCompletedTasks,
						"recent_failed_tasks":    metricsStruct.RecentFailedTasks,
						"workers":                metricsStruct.Workers,
					}
				} else {
					// Fallback to module status if available
					if metrics, ok := moduleStatus["hydrator_metrics"].(map[string]interface{}); ok {
						hydratorMetrics = metrics
					}
				}
				hydratorMetrics["is_running"] = hydratorRunning

				// Add additional hydrator information
				if enableHydrator, ok := moduleStatus["enable_hydrator"].(bool); ok {
					hydratorMetrics["enabled"] = enableHydrator
				}

				hydratorComponent := &ComponentStatus{
					Name:      "hydrator",
					Healthy:   hydratorHealthy,
					Status:    hydratorStatus,
					LastCheck: time.Now(),
					Message:   "",
					Metrics:   hydratorMetrics,
				}
				c.store.UpdateComponent(hydratorComponent)
			}

			// Collect Cache statistics
			if cacheStats, ok := moduleStatus["cache_stats"].(map[string]interface{}); ok {
				// Calculate detailed cache statistics
				cacheManager := aim.GetCacheManager()
				if cacheManager != nil {
					allUsersData := cacheManager.GetAllUsersData()
					totalApps := 0
					totalAppInfoLatest := 0
					totalAppStateLatest := 0
					totalAppInfoPending := 0

					for _, userData := range allUsersData {
						if userData == nil || userData.Sources == nil {
							continue
						}
						for _, sourceData := range userData.Sources {
							if sourceData == nil {
								continue
							}
							if sourceData.AppInfoLatest != nil {
								totalAppInfoLatest += len(sourceData.AppInfoLatest)
							}
							if sourceData.AppStateLatest != nil {
								totalAppStateLatest += len(sourceData.AppStateLatest)
							}
							if sourceData.AppInfoLatestPending != nil {
								totalAppInfoPending += len(sourceData.AppInfoLatestPending)
							}
						}
					}
					// Total apps is the union of AppInfoLatest and AppStateLatest
					// For simplicity, we use the larger of the two
					if totalAppInfoLatest > totalAppStateLatest {
						totalApps = totalAppInfoLatest
					} else {
						totalApps = totalAppStateLatest
					}

					// Merge with existing cache stats
					enhancedCacheStats := make(map[string]interface{})
					for k, v := range cacheStats {
						enhancedCacheStats[k] = v
					}
					enhancedCacheStats["total_apps"] = totalApps
					enhancedCacheStats["total_app_info_latest"] = totalAppInfoLatest
					enhancedCacheStats["total_app_state_latest"] = totalAppStateLatest
					enhancedCacheStats["total_app_info_pending"] = totalAppInfoPending

					cacheComponent := &ComponentStatus{
						Name:      "cache",
						Healthy:   true,
						Status:    "running",
						LastCheck: time.Now(),
						Message:   "",
						Metrics:   enhancedCacheStats,
					}
					c.store.UpdateComponent(cacheComponent)
				}
			}
		}
	}

	// Collect TaskModule status
	if tm != nil {
		pendingCount := len(tm.GetPendingTasks())
		runningCount := len(tm.GetRunningTasks())

		component := &ComponentStatus{
			Name:      "task_module",
			Healthy:   true,
			Status:    "running",
			LastCheck: time.Now(),
			Metrics: map[string]interface{}{
				"pending_tasks": pendingCount,
				"running_tasks": runningCount,
				"instance_id":   tm.GetInstanceID(),
			},
		}
		c.store.UpdateComponent(component)
	}
}

// taskTypeToString converts TaskType to string
func (c *StateCollector) taskTypeToString(tt task.TaskType) string {
	switch tt {
	case task.InstallApp:
		return "install"
	case task.UninstallApp:
		return "uninstall"
	case task.CancelAppInstall:
		return "cancel"
	case task.UpgradeApp:
		return "upgrade"
	case task.CloneApp:
		return "clone"
	default:
		return "unknown"
	}
}

// taskStatusToString converts TaskStatus to string
func (c *StateCollector) taskStatusToString(ts task.TaskStatus) string {
	switch ts {
	case task.Pending:
		return "pending"
	case task.Running:
		return "running"
	case task.Completed:
		return "completed"
	case task.Failed:
		return "failed"
	case task.Canceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// OnTaskCreated is called when a task is created
func (c *StateCollector) OnTaskCreated(t *task.Task) {
	taskState := c.convertTaskToTaskState(t, "")
	c.store.UpdateTask(taskState)
}

// OnTaskUpdated is called when a task is updated
func (c *StateCollector) OnTaskUpdated(t *task.Task) {
	taskState := c.convertTaskToTaskState(t, "")
	c.store.UpdateTask(taskState)
}

// collectChartRepoStatus collects status from chart repo
func (c *StateCollector) collectChartRepoStatus(aim *appinfo.AppInfoModule) {
	if aim == nil {
		return
	}

	cacheManager := aim.GetCacheManager()
	if cacheManager == nil {
		return
	}

	// Get chart repo service host from environment
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		chartRepoHost = "localhost:82" // Default port 82
	}

	// Get all users and sources from cache
	allUsersData := cacheManager.GetAllUsersData()
	if allUsersData == nil {
		return
	}

	// Aggregate chart repo status from all users and sources
	var allApps []*ChartRepoAppState
	var allImages []*ChartRepoImageState
	var allTasks *ChartRepoTasksStatus
	var systemStatus *ChartRepoSystemStatus

	// Collect status for each user and source
	for userID, userData := range allUsersData {
		if userData == nil || userData.Sources == nil {
			continue
		}

		for sourceID := range userData.Sources {
			status := c.fetchChartRepoStatus(chartRepoHost, userID, sourceID)
			if status != nil {
				if status.Apps != nil {
					allApps = append(allApps, status.Apps...)
				}
				if status.Images != nil {
					allImages = append(allImages, status.Images...)
				}
				if status.Tasks != nil {
					if allTasks == nil {
						allTasks = &ChartRepoTasksStatus{}
					}
					// Merge tasks status
					if status.Tasks.Hydrator != nil {
						if allTasks.Hydrator == nil {
							allTasks.Hydrator = &ChartRepoHydratorStatus{}
						}
						allTasks.Hydrator.QueueLength += status.Tasks.Hydrator.QueueLength
						allTasks.Hydrator.ActiveTasks += status.Tasks.Hydrator.ActiveTasks
						allTasks.Hydrator.CompletedTasks += status.Tasks.Hydrator.CompletedTasks
						allTasks.Hydrator.FailedTasks += status.Tasks.Hydrator.FailedTasks
						if status.Tasks.Hydrator.Tasks != nil {
							if allTasks.Hydrator.Tasks == nil {
								allTasks.Hydrator.Tasks = []*ChartRepoTaskState{}
							}
							allTasks.Hydrator.Tasks = append(allTasks.Hydrator.Tasks, status.Tasks.Hydrator.Tasks...)
						}
						if status.Tasks.Hydrator.RecentFailed != nil {
							if allTasks.Hydrator.RecentFailed == nil {
								allTasks.Hydrator.RecentFailed = []*ChartRepoTaskState{}
							}
							allTasks.Hydrator.RecentFailed = append(allTasks.Hydrator.RecentFailed, status.Tasks.Hydrator.RecentFailed...)
						}
					}
					if status.Tasks.ImageAnalyzer != nil {
						allTasks.ImageAnalyzer = status.Tasks.ImageAnalyzer // Use the latest one
					}
				}
				if status.System != nil && systemStatus == nil {
					systemStatus = status.System
				}
			}
		}
	}

	// Create aggregated chart repo status
	chartRepoStatus := &ChartRepoStatus{
		System:     systemStatus,
		Apps:       allApps,
		Images:     allImages,
		Tasks:      allTasks,
		LastUpdate: time.Now(),
	}

	c.store.UpdateChartRepoStatus(chartRepoStatus)
}

// fetchChartRepoStatus fetches status from chart repo for a specific user and source
func (c *StateCollector) fetchChartRepoStatus(host, userID, sourceID string) *ChartRepoStatus {
	// Build URL
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/status?user=%s&source=%s&include=apps,images,tasks", host, userID, sourceID)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Make request
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Failed to fetch chart repo status from %s: %v", url, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Chart repo status API returned non-OK status: %d", resp.StatusCode)
		return nil
	}

	// Parse response
	var apiResponse struct {
		Success bool                   `json:"success"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		log.Printf("Failed to decode chart repo status response: %v", err)
		return nil
	}

	if !apiResponse.Success {
		log.Printf("Chart repo status API returned error: %s", apiResponse.Message)
		return nil
	}

	// Convert response to ChartRepoStatus
	return c.convertChartRepoResponse(apiResponse.Data)
}

// convertChartRepoResponse converts API response to ChartRepoStatus
func (c *StateCollector) convertChartRepoResponse(data map[string]interface{}) *ChartRepoStatus {
	status := &ChartRepoStatus{
		LastUpdate: time.Now(),
	}

	// Parse system status
	if systemData, ok := data["system"].(map[string]interface{}); ok {
		status.System = &ChartRepoSystemStatus{
			Components:    make(map[string]interface{}),
			ResourceUsage: make(map[string]interface{}),
		}
		if uptime, ok := systemData["uptime"].(float64); ok {
			status.System.Uptime = int64(uptime)
		}
		if version, ok := systemData["version"].(string); ok {
			status.System.Version = version
		}
		if hostname, ok := systemData["hostname"].(string); ok {
			status.System.Hostname = hostname
		}
		if components, ok := systemData["components"].(map[string]interface{}); ok {
			status.System.Components = components
		}
		if resourceUsage, ok := systemData["resource_usage"].(map[string]interface{}); ok {
			status.System.ResourceUsage = resourceUsage
		}
	}

	// Parse apps
	if appsData, ok := data["apps"].([]interface{}); ok {
		status.Apps = make([]*ChartRepoAppState, 0, len(appsData))
		for _, appData := range appsData {
			if appMap, ok := appData.(map[string]interface{}); ok {
				appState := c.convertChartRepoAppState(appMap)
				if appState != nil {
					status.Apps = append(status.Apps, appState)
				}
			}
		}
	}

	// Parse images
	if imagesData, ok := data["images"].([]interface{}); ok {
		status.Images = make([]*ChartRepoImageState, 0, len(imagesData))
		for _, imageData := range imagesData {
			if imageMap, ok := imageData.(map[string]interface{}); ok {
				imageState := c.convertChartRepoImageState(imageMap)
				if imageState != nil {
					status.Images = append(status.Images, imageState)
				}
			}
		}
	}

	// Parse tasks
	if tasksData, ok := data["tasks"].(map[string]interface{}); ok {
		status.Tasks = &ChartRepoTasksStatus{}

		// Parse hydrator
		if hydratorData, ok := tasksData["hydrator"].(map[string]interface{}); ok {
			status.Tasks.Hydrator = c.convertChartRepoHydratorStatus(hydratorData)
		}

		// Parse image_analyzer
		if analyzerData, ok := tasksData["image_analyzer"].(map[string]interface{}); ok {
			status.Tasks.ImageAnalyzer = c.convertChartRepoImageAnalyzerStatus(analyzerData)
		}
	}

	return status
}

// convertChartRepoAppState converts app state from API response
func (c *StateCollector) convertChartRepoAppState(data map[string]interface{}) *ChartRepoAppState {
	appState := &ChartRepoAppState{
		LastUpdate: time.Now(),
	}

	if appID, ok := data["app_id"].(string); ok {
		appState.AppID = appID
	}
	if appName, ok := data["app_name"].(string); ok {
		appState.AppName = appName
	}
	if userID, ok := data["user_id"].(string); ok {
		appState.UserID = userID
	}
	if sourceID, ok := data["source_id"].(string); ok {
		appState.SourceID = sourceID
	}
	if state, ok := data["state"].(string); ok {
		appState.State = state
	}

	// Parse current_step
	if stepData, ok := data["current_step"].(map[string]interface{}); ok {
		appState.CurrentStep = &ChartRepoStep{}
		if name, ok := stepData["name"].(string); ok {
			appState.CurrentStep.Name = name
		}
		if index, ok := stepData["index"].(float64); ok {
			appState.CurrentStep.Index = int(index)
		}
		if total, ok := stepData["total"].(float64); ok {
			appState.CurrentStep.Total = int(total)
		}
		if status, ok := stepData["status"].(string); ok {
			appState.CurrentStep.Status = status
		}
		if startedAt, ok := stepData["started_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
				appState.CurrentStep.StartedAt = t
			}
		}
		if updatedAt, ok := stepData["updated_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
				appState.CurrentStep.UpdatedAt = t
			}
		}
		if retryCount, ok := stepData["retry_count"].(float64); ok {
			appState.CurrentStep.RetryCount = int(retryCount)
		}
	}

	// Parse processing
	if procData, ok := data["processing"].(map[string]interface{}); ok {
		appState.Processing = &ChartRepoProcessing{}
		if taskID, ok := procData["task_id"].(string); ok {
			appState.Processing.TaskID = taskID
		}
		if duration, ok := procData["duration"].(float64); ok {
			appState.Processing.Duration = int64(duration)
		}
	}

	// Parse timestamps
	if tsData, ok := data["timestamps"].(map[string]interface{}); ok {
		appState.Timestamps = &ChartRepoTimestamps{}
		if createdAt, ok := tsData["created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
				appState.Timestamps.CreatedAt = t
			}
		}
		if updatedAt, ok := tsData["last_updated_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
				appState.Timestamps.LastUpdatedAt = t
			}
		}
	}

	// Parse error
	if errData, ok := data["error"].(map[string]interface{}); ok {
		appState.Error = &ChartRepoError{}
		if message, ok := errData["message"].(string); ok {
			appState.Error.Message = message
		}
		if step, ok := errData["step"].(string); ok {
			appState.Error.Step = step
		}
	}

	return appState
}

// convertChartRepoImageState converts image state from API response
func (c *StateCollector) convertChartRepoImageState(data map[string]interface{}) *ChartRepoImageState {
	imageState := &ChartRepoImageState{
		LastUpdate: time.Now(),
	}

	if imageName, ok := data["image_name"].(string); ok {
		imageState.ImageName = imageName
	}
	if appID, ok := data["app_id"].(string); ok {
		imageState.AppID = appID
	}
	if appName, ok := data["app_name"].(string); ok {
		imageState.AppName = appName
	}
	if status, ok := data["status"].(string); ok {
		imageState.Status = status
	}
	if arch, ok := data["architecture"].(string); ok {
		imageState.Architecture = arch
	}
	if totalSize, ok := data["total_size"].(float64); ok {
		imageState.TotalSize = int64(totalSize)
	}
	if downloadedSize, ok := data["downloaded_size"].(float64); ok {
		imageState.DownloadedSize = int64(downloadedSize)
	}
	if progress, ok := data["download_progress"].(float64); ok {
		imageState.DownloadProgress = progress
	}
	if layerCount, ok := data["layer_count"].(float64); ok {
		imageState.LayerCount = int(layerCount)
	}
	if downloadedLayers, ok := data["downloaded_layers"].(float64); ok {
		imageState.DownloadedLayers = int(downloadedLayers)
	}
	if analysisStatus, ok := data["analysis_status"].(string); ok {
		imageState.AnalysisStatus = analysisStatus
	}
	if analyzedAt, ok := data["analyzed_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, analyzedAt); err == nil {
			imageState.AnalyzedAt = &t
		}
	}
	if errorMsg, ok := data["error_message"].(string); ok {
		imageState.ErrorMessage = errorMsg
	}

	// Parse nodes
	if nodesData, ok := data["nodes"].([]interface{}); ok {
		imageState.Nodes = make([]*ChartRepoImageNode, 0, len(nodesData))
		for _, nodeData := range nodesData {
			if nodeMap, ok := nodeData.(map[string]interface{}); ok {
				node := &ChartRepoImageNode{}
				if nodeName, ok := nodeMap["node_name"].(string); ok {
					node.NodeName = nodeName
				}
				if arch, ok := nodeMap["architecture"].(string); ok {
					node.Architecture = arch
				}
				if os, ok := nodeMap["os"].(string); ok {
					node.OS = os
				}
				if totalSize, ok := nodeMap["total_size"].(float64); ok {
					node.TotalSize = int64(totalSize)
				}
				if downloadedSize, ok := nodeMap["downloaded_size"].(float64); ok {
					node.DownloadedSize = int64(downloadedSize)
				}
				if layerCount, ok := nodeMap["layer_count"].(float64); ok {
					node.LayerCount = int(layerCount)
				}
				if downloadedLayers, ok := nodeMap["downloaded_layers"].(float64); ok {
					node.DownloadedLayers = int(downloadedLayers)
				}
				if progress, ok := nodeMap["progress"].(float64); ok {
					node.Progress = progress
				}
				imageState.Nodes = append(imageState.Nodes, node)
			}
		}
	}

	return imageState
}

// convertChartRepoHydratorStatus converts hydrator status from API response
func (c *StateCollector) convertChartRepoHydratorStatus(data map[string]interface{}) *ChartRepoHydratorStatus {
	status := &ChartRepoHydratorStatus{
		Tasks:        []*ChartRepoTaskState{},
		RecentFailed: []*ChartRepoTaskState{},
	}

	if queueLength, ok := data["queue_length"].(float64); ok {
		status.QueueLength = int(queueLength)
	}
	if activeTasks, ok := data["active_tasks"].(float64); ok {
		status.ActiveTasks = int(activeTasks)
	}
	if completedTasks, ok := data["completed_tasks"].(float64); ok {
		status.CompletedTasks = int(completedTasks)
	}
	if failedTasks, ok := data["failed_tasks"].(float64); ok {
		status.FailedTasks = int(failedTasks)
	}
	if workerCount, ok := data["worker_count"].(float64); ok {
		status.WorkerCount = int(workerCount)
	}

	// Parse tasks
	if tasksData, ok := data["tasks"].([]interface{}); ok {
		for _, taskData := range tasksData {
			if taskMap, ok := taskData.(map[string]interface{}); ok {
				taskState := c.convertChartRepoTaskState(taskMap)
				if taskState != nil {
					status.Tasks = append(status.Tasks, taskState)
				}
			}
		}
	}

	// Parse recent_failed
	if failedData, ok := data["recent_failed"].([]interface{}); ok {
		for _, taskData := range failedData {
			if taskMap, ok := taskData.(map[string]interface{}); ok {
				taskState := c.convertChartRepoTaskState(taskMap)
				if taskState != nil {
					status.RecentFailed = append(status.RecentFailed, taskState)
				}
			}
		}
	}

	return status
}

// convertChartRepoTaskState converts task state from API response
func (c *StateCollector) convertChartRepoTaskState(data map[string]interface{}) *ChartRepoTaskState {
	taskState := &ChartRepoTaskState{}

	if taskID, ok := data["task_id"].(string); ok {
		taskState.TaskID = taskID
	}
	if userID, ok := data["user_id"].(string); ok {
		taskState.UserID = userID
	}
	if sourceID, ok := data["source_id"].(string); ok {
		taskState.SourceID = sourceID
	}
	if appID, ok := data["app_id"].(string); ok {
		taskState.AppID = appID
	}
	if appName, ok := data["app_name"].(string); ok {
		taskState.AppName = appName
	}
	if status, ok := data["status"].(string); ok {
		taskState.Status = status
	}
	if stepName, ok := data["step_name"].(string); ok {
		taskState.StepName = stepName
	}
	if totalSteps, ok := data["total_steps"].(float64); ok {
		taskState.TotalSteps = int(totalSteps)
	}
	if retryCount, ok := data["retry_count"].(float64); ok {
		taskState.RetryCount = int(retryCount)
	}
	if lastError, ok := data["last_error"].(string); ok {
		taskState.LastError = lastError
	}
	if createdAt, ok := data["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			taskState.CreatedAt = t
		}
	}
	if updatedAt, ok := data["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			taskState.UpdatedAt = t
		}
	}

	return taskState
}

// convertChartRepoImageAnalyzerStatus converts image analyzer status from API response
func (c *StateCollector) convertChartRepoImageAnalyzerStatus(data map[string]interface{}) *ChartRepoImageAnalyzerStatus {
	status := &ChartRepoImageAnalyzerStatus{
		LastCheck: time.Now(),
	}

	if isRunning, ok := data["is_running"].(bool); ok {
		status.IsRunning = isRunning
	}
	if healthStatus, ok := data["health_status"].(string); ok {
		status.HealthStatus = healthStatus
	}
	if lastCheck, ok := data["last_check"].(string); ok {
		if t, err := time.Parse(time.RFC3339, lastCheck); err == nil {
			status.LastCheck = t
		}
	}
	if queueLength, ok := data["queue_length"].(float64); ok {
		status.QueueLength = int(queueLength)
	}
	if activeWorkers, ok := data["active_workers"].(float64); ok {
		status.ActiveWorkers = int(activeWorkers)
	}
	if cachedImages, ok := data["cached_images"].(float64); ok {
		status.CachedImages = int(cachedImages)
	}
	if analyzingCount, ok := data["analyzing_count"].(float64); ok {
		status.AnalyzingCount = int(analyzingCount)
	}
	if totalAnalyzed, ok := data["total_analyzed"].(float64); ok {
		status.TotalAnalyzed = int64(totalAnalyzed)
	}
	if successfulAnalyzed, ok := data["successful_analyzed"].(float64); ok {
		status.SuccessfulAnalyzed = int64(successfulAnalyzed)
	}
	if failedAnalyzed, ok := data["failed_analyzed"].(float64); ok {
		status.FailedAnalyzed = int64(failedAnalyzed)
	}
	if avgTime, ok := data["average_analysis_time"].(string); ok {
		if d, err := time.ParseDuration(avgTime); err == nil {
			status.AverageAnalysisTime = d
		}
	} else if avgTimeMs, ok := data["average_analysis_time"].(float64); ok {
		// Handle numeric duration in milliseconds
		status.AverageAnalysisTime = time.Duration(avgTimeMs) * time.Millisecond
	}
	if errorMsg, ok := data["error_message"].(string); ok {
		status.ErrorMessage = errorMsg
	}

	// Parse task lists
	if queuedTasks, ok := data["queued_tasks"].([]interface{}); ok {
		status.QueuedTasks = c.convertImageAnalysisTaskDetails(queuedTasks)
	}
	if processingTasks, ok := data["processing_tasks"].([]interface{}); ok {
		status.ProcessingTasks = c.convertImageAnalysisTaskDetails(processingTasks)
	}
	if recentCompleted, ok := data["recent_completed"].([]interface{}); ok {
		status.RecentCompleted = c.convertImageAnalysisTaskDetails(recentCompleted)
	}
	if recentFailed, ok := data["recent_failed"].([]interface{}); ok {
		status.RecentFailed = c.convertImageAnalysisTaskDetails(recentFailed)
	}

	return status
}

// convertImageAnalysisTaskDetails converts a list of image analysis task details
func (c *StateCollector) convertImageAnalysisTaskDetails(tasks []interface{}) []*ChartRepoImageAnalysisTaskDetail {
	result := make([]*ChartRepoImageAnalysisTaskDetail, 0, len(tasks))
	for _, taskData := range tasks {
		if taskMap, ok := taskData.(map[string]interface{}); ok {
			task := c.convertImageAnalysisTaskDetail(taskMap)
			if task != nil {
				result = append(result, task)
			}
		}
	}
	return result
}

// convertImageAnalysisTaskDetail converts a single image analysis task detail
func (c *StateCollector) convertImageAnalysisTaskDetail(data map[string]interface{}) *ChartRepoImageAnalysisTaskDetail {
	task := &ChartRepoImageAnalysisTaskDetail{}

	if taskID, ok := data["task_id"].(string); ok {
		task.TaskID = taskID
	}
	if appName, ok := data["app_name"].(string); ok {
		task.AppName = appName
	}
	if appDir, ok := data["app_dir"].(string); ok {
		task.AppDir = appDir
	}
	if status, ok := data["status"].(string); ok {
		task.Status = status
	}
	if createdAt, ok := data["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			task.CreatedAt = t
		}
	}
	if startedAt, ok := data["started_at"].(string); ok && startedAt != "" {
		if t, err := time.Parse(time.RFC3339, startedAt); err == nil {
			task.StartedAt = &t
		}
	}
	if completedAt, ok := data["completed_at"].(string); ok && completedAt != "" {
		if t, err := time.Parse(time.RFC3339, completedAt); err == nil {
			task.CompletedAt = &t
		}
	}
	if workerID, ok := data["worker_id"].(float64); ok {
		id := int(workerID)
		task.WorkerID = &id
	}
	if duration, ok := data["duration"].(string); ok && duration != "" {
		if d, err := time.ParseDuration(duration); err == nil {
			task.Duration = &d
		}
	} else if durationMs, ok := data["duration"].(float64); ok {
		d := time.Duration(durationMs) * time.Millisecond
		task.Duration = &d
	}
	if imagesCount, ok := data["images_count"].(float64); ok {
		task.ImagesCount = int(imagesCount)
	}
	if analyzedCount, ok := data["analyzed_count"].(float64); ok {
		task.AnalyzedCount = int(analyzedCount)
	}
	if error, ok := data["error"].(string); ok {
		task.Error = error
	}
	if errorStep, ok := data["error_step"].(string); ok {
		task.ErrorStep = errorStep
	}
	if images, ok := data["images"].([]interface{}); ok {
		task.Images = make([]string, 0, len(images))
		for _, img := range images {
			if imgStr, ok := img.(string); ok {
				task.Images = append(task.Images, imgStr)
			}
		}
	}

	return task
}
