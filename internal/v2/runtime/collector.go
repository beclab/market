package runtime

import (
	"context"
	"log"
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
}

// collectTaskStates collects all task states from TaskModule
func (c *StateCollector) collectTaskStates(tm *task.TaskModule) {
	// Get pending tasks
	pendingTasks := tm.GetPendingTasks()
	for _, t := range pendingTasks {
		taskState := c.convertTaskToTaskState(t, "pending")
		c.store.UpdateTask(taskState)
	}

	// Get running tasks
	runningTasks := tm.GetRunningTasks()
	for _, t := range runningTasks {
		taskState := c.convertTaskToTaskState(t, "running")
		c.store.UpdateTask(taskState)
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

					// Determine stage based on app state
					stage := StageRunning
					health := "healthy"

					// Check if there's a running task for this app
					var taskModule *task.TaskModule
					if c.mu.TryRLock() {
						taskModule = c.taskModule
						c.mu.RUnlock()
					}

					if taskModule != nil {
						// Check for running install/upgrade tasks
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

						// Check for pending tasks
						if stage == StageRunning {
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
