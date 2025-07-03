package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"market/internal/v2/history"
	"market/internal/v2/types"
)

// TaskType defines the type of task
type TaskType int

const (
	// InstallApp represents application installation task
	InstallApp TaskType = iota + 1
	// UninstallApp represents application uninstall task
	UninstallApp
	// CancelAppInstall represents cancel application installation task
	CancelAppInstall
	// UpgradeApp represents application upgrade task
	UpgradeApp
)

// TaskStatus defines the status of task
type TaskStatus int

const (
	// Pending task is waiting to be executed
	Pending TaskStatus = iota + 1
	// Running task is currently being executed
	Running
	// Completed task has finished successfully
	Completed
	// Failed task has failed
	Failed
	// Canceled task has been canceled
	Canceled
)

// Task represents a task in the system
type Task struct {
	ID          string                 `json:"id"`
	Type        TaskType               `json:"type"`
	Status      TaskStatus             `json:"status"`
	AppName     string                 `json:"app_name"`
	User        string                 `json:"user,omitempty"`
	OpID        string                 `json:"op_id,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// DataSenderInterface defines the interface for sending system updates
type DataSenderInterface interface {
	SendMarketSystemUpdate(update types.MarketSystemUpdate) error
}

// TaskModule manages task queues and execution
type TaskModule struct {
	instanceID     string // Unique instance identifier for debugging
	mu             sync.RWMutex
	pendingTasks   []*Task          // queue for pending tasks
	runningTasks   map[string]*Task // map for running tasks
	executorTicker *time.Ticker     // ticker for task execution (every 2 seconds)
	statusTicker   *time.Ticker     // ticker for status update (every 10 seconds)
	ctx            context.Context
	cancel         context.CancelFunc
	historyModule  *history.HistoryModule // Add history module reference
	dataSender     DataSenderInterface    // Add data sender interface reference
}

// NewTaskModule creates a new task module instance
func NewTaskModule() *TaskModule {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TaskModule{
		instanceID:   fmt.Sprintf("tm-%d", time.Now().UnixNano()),
		pendingTasks: make([]*Task, 0),
		runningTasks: make(map[string]*Task),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start background goroutines
	tm.start()

	return tm
}

// SetHistoryModule sets the history module for recording task events
func (tm *TaskModule) SetHistoryModule(historyModule *history.HistoryModule) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.historyModule = historyModule
}

// SetDataSender sets the data sender for sending system updates
func (tm *TaskModule) SetDataSender(dataSender DataSenderInterface) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.dataSender = dataSender
}

// AddTask adds a new task to the pending queue
func (tm *TaskModule) AddTask(taskType TaskType, appName string, user string, metadata map[string]interface{}) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task := &Task{
		ID:        generateTaskID(),
		Type:      taskType,
		Status:    Pending,
		AppName:   appName,
		User:      user,
		CreatedAt: time.Now(),
		Metadata:  metadata,
	}

	tm.pendingTasks = append(tm.pendingTasks, task)

	log.Printf("[%s] Task added: ID=%s, Type=%d, AppName=%s, User=%s", tm.instanceID, task.ID, task.Type, task.AppName, user)

	// Record task addition in history
	tm.recordTaskHistory(task, user)

	return task
}

// getHistoryType converts TaskType to appropriate HistoryType
func getHistoryType(taskType TaskType) history.HistoryType {
	switch taskType {
	case InstallApp:
		return history.TypeActionInstall
	case UninstallApp:
		return history.TypeActionUninstall
	case CancelAppInstall:
		return history.TypeActionCancel
	case UpgradeApp:
		return history.TypeActionUpgrade
	default:
		return history.TypeAction
	}
}

// getTaskTypeString converts TaskType to a meaningful string representation
func getTaskTypeString(taskType TaskType) string {
	switch taskType {
	case InstallApp:
		return "install"
	case UninstallApp:
		return "uninstall"
	case CancelAppInstall:
		return "cancel"
	case UpgradeApp:
		return "upgrade"
	default:
		return fmt.Sprintf("unknown(%d)", taskType)
	}
}

// recordTaskHistory records task information in history module
func (tm *TaskModule) recordTaskHistory(task *Task, user string) {
	if tm.historyModule == nil {
		log.Printf("History module not available, skipping task history record")
		return
	}

	// Convert task to JSON for extended field
	taskJSON, err := json.Marshal(task)
	if err != nil {
		log.Printf("Failed to marshal task to JSON: %v", err)
		return
	}

	// Get meaningful task type string and history type
	taskTypeStr := getTaskTypeString(task.Type)
	historyType := getHistoryType(task.Type)

	// Create history record
	historyRecord := &history.HistoryRecord{
		Type:     historyType,
		Message:  fmt.Sprintf("%s %s", taskTypeStr, task.AppName),
		Time:     time.Now().Unix(),
		App:      task.AppName,
		Account:  user, // Use the provided user ID
		Extended: string(taskJSON),
	}

	// Store the record
	if err := tm.historyModule.StoreRecord(historyRecord); err != nil {
		log.Printf("Failed to record task history: %v", err)
	} else {
		log.Printf("Recorded task history with ID: %d for task: %s, user: %s", historyRecord.ID, task.ID, user)
	}
}

// GetPendingTasks returns all pending tasks
func (tm *TaskModule) GetPendingTasks() []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make([]*Task, len(tm.pendingTasks))
	copy(tasks, tm.pendingTasks)
	return tasks
}

// GetRunningTasks returns all running tasks
func (tm *TaskModule) GetRunningTasks() []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make([]*Task, 0, len(tm.runningTasks))
	for _, task := range tm.runningTasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// start initializes and starts the background goroutines
func (tm *TaskModule) start() {
	// Start task executor (every 2 seconds)
	tm.executorTicker = time.NewTicker(2 * time.Second)
	go tm.taskExecutor()

	// Start status checker (every 10 seconds)
	tm.statusTicker = time.NewTicker(30 * time.Second)
	go tm.statusChecker()
}

// taskExecutor runs every 2 seconds to execute pending tasks
func (tm *TaskModule) taskExecutor() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-tm.executorTicker.C:
			tm.executeNextTask()
		}
	}
}

// executeNextTask gets the earliest pending task and executes it
func (tm *TaskModule) executeNextTask() {
	tm.mu.Lock()

	if len(tm.pendingTasks) == 0 {
		tm.mu.Unlock()
		return
	}

	// Get the first task (FIFO)
	task := tm.pendingTasks[0]
	tm.pendingTasks = tm.pendingTasks[1:]

	// Move to running tasks
	task.Status = Running
	now := time.Now()
	task.StartedAt = &now
	tm.runningTasks[task.ID] = task

	tm.mu.Unlock()

	log.Printf("[%s] Executing task: ID=%s, Type=%d, AppName=%s", tm.instanceID, task.ID, task.Type, task.AppName)

	// Execute the task
	tm.executeTask(task)
}

// executeTask executes the actual task logic
func (tm *TaskModule) executeTask(task *Task) {
	var result string
	var err error

	log.Printf("Starting task execution: ID=%s, Type=%s, AppName=%s, User=%s",
		task.ID, getTaskTypeString(task.Type), task.AppName, task.User)

	// Send task execution system update
	tm.sendTaskExecutionUpdate(task)

	switch task.Type {
	case InstallApp:
		// Execute app installation
		log.Printf("Executing app installation for task: %s, app: %s", task.ID, task.AppName)
		result, err = tm.AppInstall(task)
		if err != nil {
			log.Printf("App installation failed for task: %s, app: %s, error: %v", task.ID, task.AppName, err)
			task.Status = Failed
			task.ErrorMsg = fmt.Sprintf("Installation failed: %v", err)
			tm.recordTaskResult(task, result, err)
			return
		}
		log.Printf("App installation completed successfully for task: %s, app: %s", task.ID, task.AppName)

	case UninstallApp:
		// Execute app uninstallation
		log.Printf("Executing app uninstallation for task: %s, app: %s", task.ID, task.AppName)
		result, err = tm.AppUninstall(task)
		if err != nil {
			log.Printf("App uninstallation failed for task: %s, app: %s, error: %v", task.ID, task.AppName, err)
			task.Status = Failed
			task.ErrorMsg = fmt.Sprintf("Uninstallation failed: %v", err)
			tm.recordTaskResult(task, result, err)
			return
		}
		log.Printf("App uninstallation completed successfully for task: %s, app: %s", task.ID, task.AppName)

	case CancelAppInstall:
		// Execute app cancel - cancel running install tasks
		log.Printf("Executing app cancel for task: %s, app: %s", task.ID, task.AppName)

		// Get version and source from task metadata
		version := ""
		source := ""
		if v, ok := task.Metadata["version"].(string); ok {
			version = v
		}
		if s, ok := task.Metadata["source"].(string); ok {
			source = s
		}

		// Call InstallTaskCanceled to cancel running install tasks
		err = tm.InstallTaskCanceled(task.AppName, version, source, task.User)
		if err != nil {
			log.Printf("App cancel failed for task: %s, app: %s, error: %v", task.ID, task.AppName, err)
			task.Status = Failed
			task.ErrorMsg = fmt.Sprintf("Cancel failed: %v", err)
			tm.recordTaskResult(task, "Cancel operation failed", err)
			return
		}

		result = "Install task canceled successfully"
		log.Printf("App cancel completed successfully for task: %s, app: %s", task.ID, task.AppName)

	case UpgradeApp:
		// Execute app upgrade
		log.Printf("Executing app upgrade for task: %s, app: %s", task.ID, task.AppName)
		result, err = tm.AppUpgrade(task)
		if err != nil {
			log.Printf("App upgrade failed for task: %s, app: %s, error: %v", task.ID, task.AppName, err)
			task.Status = Failed
			task.ErrorMsg = fmt.Sprintf("Upgrade failed: %v", err)
			tm.recordTaskResult(task, result, err)
			return
		}
		log.Printf("App upgrade completed successfully for task: %s, app: %s", task.ID, task.AppName)
	}

	// Task completed successfully
	task.Status = Completed
	now := time.Now()
	task.CompletedAt = &now
	log.Printf("Task completed successfully: ID=%s, Type=%s, AppName=%s, User=%s, Duration=%v",
		task.ID, getTaskTypeString(task.Type), task.AppName, task.User, now.Sub(*task.StartedAt))

	// Log the result summary
	log.Printf("Task result summary: ID=%s, Result length=%d bytes", task.ID, len(result))

	tm.recordTaskResult(task, result, nil)

}

// sendTaskExecutionUpdate sends system update when task execution starts
func (tm *TaskModule) sendTaskExecutionUpdate(task *Task) {
	if tm.dataSender == nil {
		log.Printf("Data sender not available, skipping task execution update for task: %s", task.ID)
		return
	}

	// Create extensions map with task information
	extensions := make(map[string]string)
	extensions["task_type"] = getTaskTypeString(task.Type)
	extensions["app_name"] = task.AppName

	// Add version and source if available in metadata
	if version, ok := task.Metadata["version"].(string); ok && version != "" {
		extensions["version"] = version
	}
	if source, ok := task.Metadata["source"].(string); ok && source != "" {
		extensions["source"] = source
	}

	// Create market system update
	update := &types.MarketSystemUpdate{
		Timestamp:  time.Now().Unix(),
		User:       task.User,
		NotifyType: "market_system_point",
		Point:      "task_execute",
		Extensions: extensions,
	}

	// Send the notification
	if err := tm.dataSender.SendMarketSystemUpdate(*update); err != nil {
		log.Printf("Failed to send task execution update for task %s: %v", task.ID, err)
	} else {
		log.Printf("Successfully sent task execution update for task %s (type: %s, app: %s, user: %s)",
			task.ID, getTaskTypeString(task.Type), task.AppName, task.User)
	}
}

// sendTaskFinishedUpdate sends system update when task status changes to finished state
func (tm *TaskModule) sendTaskFinishedUpdate(task *Task, status string) {
	if tm.dataSender == nil {
		log.Printf("Data sender not available, skipping task finished update for task: %s", task.ID)
		return
	}

	// Create extensions map with task information
	extensions := make(map[string]string)
	extensions["task_type"] = getTaskTypeString(task.Type)
	extensions["app_name"] = task.AppName
	extensions["task_status"] = status

	// Add version and source if available in metadata
	if version, ok := task.Metadata["version"].(string); ok && version != "" {
		extensions["version"] = version
	}
	if source, ok := task.Metadata["source"].(string); ok && source != "" {
		extensions["source"] = source
	}

	// Create market system update
	update := &types.MarketSystemUpdate{
		Timestamp:  time.Now().Unix(),
		User:       task.User,
		NotifyType: "market_system_point",
		Point:      "task_finished_" + status,
		Extensions: extensions,
	}

	// Send the notification
	if err := tm.dataSender.SendMarketSystemUpdate(*update); err != nil {
		log.Printf("Failed to send task finished update for task %s: %v", task.ID, err)
	} else {
		log.Printf("Successfully sent task finished update for task %s (type: %s, app: %s, user: %s, status: %s)",
			task.ID, getTaskTypeString(task.Type), task.AppName, task.User, status)
	}
}

// statusChecker runs every 10 seconds to check running task status
func (tm *TaskModule) statusChecker() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-tm.statusTicker.C:
			tm.checkRunningTasksStatus()
		}
	}
}

// checkRunningTasksStatus checks the status of all running tasks
func (tm *TaskModule) checkRunningTasksStatus() {

	for taskID, task := range tm.runningTasks {
		log.Printf("Checking status for task: ID=%s", taskID)
		tm.showTaskStatus(task)
	}
}

// showTaskStatus shows the status of a single task
func (tm *TaskModule) showTaskStatus(task *Task) {
	log.Printf("Task details - ID: %s, Type: %s, Status: %d, AppName: %s, User: %s, OpID: %s, CreatedAt: %v, StartedAt: %v, CompletedAt: %v, ErrorMsg: %s, Metadata: %+v",
		task.ID,
		getTaskTypeString(task.Type),
		task.Status,
		task.AppName,
		task.User,
		task.OpID,
		task.CreatedAt,
		task.StartedAt,
		task.CompletedAt,
		task.ErrorMsg,
		task.Metadata)
}

// Stop gracefully stops the task module
func (tm *TaskModule) Stop() {
	log.Println("Stopping task module...")

	if tm.executorTicker != nil {
		tm.executorTicker.Stop()
	}
	if tm.statusTicker != nil {
		tm.statusTicker.Stop()
	}

	tm.cancel()

	log.Println("Task module stopped")
}

// generateTaskID generates a unique task ID
func generateTaskID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(6)
}

// randomString generates a random string of specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

// recordTaskResult records task result in history module
func (tm *TaskModule) recordTaskResult(task *Task, result string, err error) {
	if tm.historyModule == nil {
		log.Printf("History module not available, skipping task result record for task: %s", task.ID)
		return
	}

	log.Printf("Recording task result for task: %s, type: %s, app: %s, user: %s",
		task.ID, getTaskTypeString(task.Type), task.AppName, task.User)

	// Create result data structure
	resultData := map[string]interface{}{
		"task_id":   task.ID,
		"task_type": getTaskTypeString(task.Type),
		"app_name":  task.AppName,
		"user":      task.User,
		"status":    task.Status,
		"result":    result,
		"error":     nil,
		"timestamp": time.Now().Unix(),
	}

	if err != nil {
		resultData["error"] = err.Error()
		log.Printf("Task failed with error: %v", err)
	} else {
		log.Printf("Task completed successfully, result length: %d bytes", len(result))
	}

	// Convert result data to JSON for extended field
	resultJSON, err := json.Marshal(resultData)
	if err != nil {
		log.Printf("Failed to marshal result data to JSON for task %s: %v", task.ID, err)
		return
	}

	// Get appropriate history type based on task type
	historyType := getHistoryType(task.Type)

	// Create history record
	historyRecord := &history.HistoryRecord{
		Type:     historyType,
		Message:  fmt.Sprintf("%s %s completed", getTaskTypeString(task.Type), task.AppName),
		Time:     time.Now().Unix(),
		App:      task.AppName,
		Account:  task.User,
		Extended: string(resultJSON),
	}

	// Store the record
	if err := tm.historyModule.StoreRecord(historyRecord); err != nil {
		log.Printf("Failed to record task result in history for task %s: %v", task.ID, err)
	} else {
		log.Printf("Successfully recorded task result in history: task=%s, history_id=%d, user=%s, extended_length=%d bytes",
			task.ID, historyRecord.ID, task.User, len(resultJSON))
	}
}

func (tm *TaskModule) GetLatestTaskByAppNameAndUser(appName, user string) (taskType string, source string, found bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var latestTask *Task

	// Only search for InstallApp tasks
	for _, t := range tm.runningTasks {
		if t.AppName == appName && t.User == user && t.Type == InstallApp {
			if latestTask == nil || t.CreatedAt.After(latestTask.CreatedAt) {
				latestTask = t
			}
		}
	}

	for _, t := range tm.pendingTasks {
		if t.AppName == appName && t.User == user && t.Type == InstallApp {
			if latestTask == nil || t.CreatedAt.After(latestTask.CreatedAt) {
				latestTask = t
			}
		}
	}

	if latestTask == nil {
		// Log all running tasks
		log.Printf("[%s] GetLatestTaskByAppNameAndUser - All running tasks for app: %s, user: %s", tm.instanceID, appName, user)
		for _, t := range tm.runningTasks {
			log.Printf("  Running task: ID=%s, Type=%s, AppName=%s, User=%s, Status=%d, CreatedAt=%v",
				t.ID, getTaskTypeString(t.Type), t.AppName, t.User, t.Status, t.CreatedAt)
		}

		// Log all pending tasks
		log.Printf("[%s] GetLatestTaskByAppNameAndUser - All pending tasks for app: %s, user: %s", tm.instanceID, appName, user)
		for _, t := range tm.pendingTasks {
			log.Printf("  Pending task: ID=%s, Type=%s, AppName=%s, User=%s, Status=%d, CreatedAt=%v",
				t.ID, getTaskTypeString(t.Type), t.AppName, t.User, t.Status, t.CreatedAt)
		}
		return "", "", false
	}

	typeStr := getTaskTypeString(latestTask.Type)
	source = ""
	if s, ok := latestTask.Metadata["source"].(string); ok {
		source = s
	}
	return typeStr, source, true
}

// GetInstanceID returns the unique instance identifier
func (tm *TaskModule) GetInstanceID() string {
	return tm.instanceID
}

// InstallTaskSucceed marks an install task as completed successfully by opID
func (tm *TaskModule) InstallTaskSucceed(opID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the install task with matching opID in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.OpID == opID && task.Type == InstallApp {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] InstallTaskSucceed - No running install task found with opID: %s", tm.instanceID, opID)
		return fmt.Errorf("no running install task found with opID: %s", opID)
	}

	// Mark task as completed
	targetTask.Status = Completed
	now := time.Now()
	targetTask.CompletedAt = &now

	log.Printf("[%s] InstallTaskSucceed - Task marked as completed: ID=%s, OpID=%s, AppName=%s, User=%s, Duration=%v",
		tm.instanceID, targetTask.ID, opID, targetTask.AppName, targetTask.User, now.Sub(*targetTask.StartedAt))

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] InstallTaskSucceed - Removed completed task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task completion in history
	tm.recordTaskResult(targetTask, "Installation completed successfully via external signal", nil)

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "succeed")

	return nil
}

// InstallTaskFailed marks an install task as failed by opID
func (tm *TaskModule) InstallTaskFailed(opID string, errorMsg string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the install task with matching opID in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.OpID == opID && task.Type == InstallApp {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] InstallTaskFailed - No running install task found with opID: %s", tm.instanceID, opID)
		return fmt.Errorf("no running install task found with opID: %s", opID)
	}

	// Mark task as failed
	targetTask.Status = Failed
	now := time.Now()
	targetTask.CompletedAt = &now
	targetTask.ErrorMsg = errorMsg

	log.Printf("[%s] InstallTaskFailed - Task marked as failed: ID=%s, OpID=%s, AppName=%s, User=%s, Duration=%v, Error: %s",
		tm.instanceID, targetTask.ID, opID, targetTask.AppName, targetTask.User, now.Sub(*targetTask.StartedAt), errorMsg)

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] InstallTaskFailed - Removed failed task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task failure in history
	tm.recordTaskResult(targetTask, "Installation failed via external signal", fmt.Errorf(errorMsg))

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "failed")

	return nil
}

// InstallTaskCanceled marks an install task as canceled by app name and user
func (tm *TaskModule) InstallTaskCanceled(appName, appVersion, source, user string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the install task with matching criteria in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.Type == InstallApp && task.AppName == appName && task.User == user {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] InstallTaskCanceled - No running install task found with appName: %s, user: %s",
			tm.instanceID, appName, user)
		return fmt.Errorf("no running install task found with appName: %s, user: %s", appName, user)
	}

	// Mark task as canceled
	targetTask.Status = Canceled
	now := time.Now()
	targetTask.CompletedAt = &now
	targetTask.ErrorMsg = "Installation canceled via external signal"

	log.Printf("[%s] InstallTaskCanceled - Task marked as canceled: ID=%s, AppName=%s, User=%s, Duration=%v",
		tm.instanceID, targetTask.ID, appName, user, now.Sub(*targetTask.StartedAt))

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] InstallTaskCanceled - Removed canceled task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task cancellation in history
	tm.recordTaskResult(targetTask, "Installation canceled via external signal", fmt.Errorf("installation canceled"))

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "canceled")

	return nil
}

// CancelInstallTaskSucceed marks a cancel install task as completed successfully by opID
func (tm *TaskModule) CancelInstallTaskSucceed(opID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the cancel install task with matching opID in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.OpID == opID && task.Type == CancelAppInstall {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] CancelInstallTaskSucceed - No running cancel install task found with opID: %s", tm.instanceID, opID)
		return fmt.Errorf("no running cancel install task found with opID: %s", opID)
	}

	// Mark task as completed
	targetTask.Status = Completed
	now := time.Now()
	targetTask.CompletedAt = &now

	log.Printf("[%s] CancelInstallTaskSucceed - Task marked as completed: ID=%s, OpID=%s, AppName=%s, User=%s, Duration=%v",
		tm.instanceID, targetTask.ID, opID, targetTask.AppName, targetTask.User, now.Sub(*targetTask.StartedAt))

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] CancelInstallTaskSucceed - Removed completed task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task completion in history
	tm.recordTaskResult(targetTask, "Cancel installation completed successfully via external signal", nil)

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "succeed")

	return nil
}

// CancelInstallTaskFailed marks a cancel install task as failed by opID
func (tm *TaskModule) CancelInstallTaskFailed(opID string, errorMsg string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the cancel install task with matching opID in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.OpID == opID && task.Type == CancelAppInstall {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] CancelInstallTaskFailed - No running cancel install task found with opID: %s", tm.instanceID, opID)
		return fmt.Errorf("no running cancel install task found with opID: %s", opID)
	}

	// Mark task as failed
	targetTask.Status = Failed
	now := time.Now()
	targetTask.CompletedAt = &now
	targetTask.ErrorMsg = errorMsg

	log.Printf("[%s] CancelInstallTaskFailed - Task marked as failed: ID=%s, OpID=%s, AppName=%s, User=%s, Duration=%v, Error: %s",
		tm.instanceID, targetTask.ID, opID, targetTask.AppName, targetTask.User, now.Sub(*targetTask.StartedAt), errorMsg)

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] CancelInstallTaskFailed - Removed failed task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task failure in history
	tm.recordTaskResult(targetTask, "Cancel installation failed via external signal", fmt.Errorf(errorMsg))

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "failed")

	return nil
}

// UninstallTaskSucceed marks an uninstall task as completed successfully by opID
func (tm *TaskModule) UninstallTaskSucceed(opID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the uninstall task with matching opID in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.OpID == opID && task.Type == UninstallApp {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] UninstallTaskSucceed - No running uninstall task found with opID: %s", tm.instanceID, opID)
		return fmt.Errorf("no running uninstall task found with opID: %s", opID)
	}

	// Mark task as completed
	targetTask.Status = Completed
	now := time.Now()
	targetTask.CompletedAt = &now

	log.Printf("[%s] UninstallTaskSucceed - Task marked as completed: ID=%s, OpID=%s, AppName=%s, User=%s, Duration=%v",
		tm.instanceID, targetTask.ID, opID, targetTask.AppName, targetTask.User, now.Sub(*targetTask.StartedAt))

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] UninstallTaskSucceed - Removed completed task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task completion in history
	tm.recordTaskResult(targetTask, "Uninstallation completed successfully via external signal", nil)

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "succeed")

	return nil
}

// UninstallTaskFailed marks an uninstall task as failed by opID
func (tm *TaskModule) UninstallTaskFailed(opID string, errorMsg string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Find the uninstall task with matching opID in running tasks
	var targetTask *Task
	for _, task := range tm.runningTasks {
		if task.OpID == opID && task.Type == UninstallApp {
			targetTask = task
			break
		}
	}

	if targetTask == nil {
		log.Printf("[%s] UninstallTaskFailed - No running uninstall task found with opID: %s", tm.instanceID, opID)
		return fmt.Errorf("no running uninstall task found with opID: %s", opID)
	}

	// Mark task as failed
	targetTask.Status = Failed
	now := time.Now()
	targetTask.CompletedAt = &now
	targetTask.ErrorMsg = errorMsg

	log.Printf("[%s] UninstallTaskFailed - Task marked as failed: ID=%s, OpID=%s, AppName=%s, User=%s, Duration=%v, Error: %s",
		tm.instanceID, targetTask.ID, opID, targetTask.AppName, targetTask.User, now.Sub(*targetTask.StartedAt), errorMsg)

	// Remove task from running tasks
	delete(tm.runningTasks, targetTask.ID)
	log.Printf("[%s] UninstallTaskFailed - Removed failed task from running tasks: ID=%s", tm.instanceID, targetTask.ID)

	// Record task failure in history
	tm.recordTaskResult(targetTask, "Uninstallation failed via external signal", fmt.Errorf(errorMsg))

	// Send task finished system update
	tm.sendTaskFinishedUpdate(targetTask, "failed")

	return nil
}
