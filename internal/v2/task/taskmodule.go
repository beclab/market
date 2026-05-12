package task

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"market/internal/v2/history"
	"market/internal/v2/settings"
	"market/internal/v2/store"
	"market/internal/v2/types"

	"github.com/golang/glog"
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
	// CloneApp represents application clone task
	CloneApp
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

const completedTaskLimit = 100

// TaskCallback defines the callback function type for task completion
type TaskCallback func(result string, err error)

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
	Result      string                 `json:"result,omitempty"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Callback    TaskCallback           `json:"-"` // Callback function for synchronous requests
}

// DataSenderInterface defines the interface for sending system updates
type DataSenderInterface interface {
	SendMarketSystemUpdate(update types.MarketSystemUpdate) error
}

// TaskModule manages task queues and execution
type TaskModule struct {
	instanceID      string // Unique instance identifier for debugging
	mu              sync.RWMutex
	pendingTasks    []*Task          // queue for pending tasks
	runningTasks    map[string]*Task // map for running tasks
	taskStore       *TaskStore       // persistent task store
	executorTicker  *time.Ticker     // ticker for task execution (every 2 seconds)
	statusTicker    *time.Ticker     // ticker for status update (every 10 seconds)
	ctx             context.Context
	cancel          context.CancelFunc
	historyModule   *history.HistoryModule    // Add history module reference
	dataSender      DataSenderInterface       // Add data sender interface reference
	settingsManager *settings.SettingsManager // Add settings manager reference
}

// NewTaskModule creates a new task module instance
func NewTaskModule() (*TaskModule, error) {
	ctx, cancel := context.WithCancel(context.Background())

	taskStore, err := NewTaskStore()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize task store: %w", err)
	}

	tm := &TaskModule{
		instanceID:   fmt.Sprintf("tm-%d", time.Now().UnixNano()),
		pendingTasks: make([]*Task, 0),
		runningTasks: make(map[string]*Task),
		taskStore:    taskStore,
		ctx:          ctx,
		cancel:       cancel,
	}

	if err := tm.restoreTasksFromStore(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to restore tasks from store: %w", err)
	}

	// Start background goroutines
	tm.start()

	return tm, nil
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

// SetSettingsManager sets the settings manager for accessing Redis
func (tm *TaskModule) SetSettingsManager(settingsManager *settings.SettingsManager) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.settingsManager = settingsManager
}

// AddTask adds a new task to the pending queue.
//
// Persistence ordering: the PG row is committed BEFORE the task is
// appended to the in-memory pendingTasks slice. This direction is
// deliberate — it eliminates the legacy "in memory but not in PG"
// window that lost tasks across crashes (the legacy code appended to
// memory first and persisted best-effort afterwards). A PG failure
// here surfaces as a concrete AddTask error so the API handler can
// return 5xx and the user can retry; previously the task would silently
// disappear on the next restart.
//
// State-table side-effect: when the metadata carries the (source,
// realAppID) pair needed to locate the user_application_states row,
// CreateTask refreshes the row's op_type column atomically with the
// task_records insert. This replaces the API handlers' separate
// writePendingState calls so the two writes can no longer land in a
// half-committed state. Callers that don't have a state row to refresh
// (e.g. cloneApp's synthesized newAppName, which has no user_applications
// parent yet) trigger a tolerable miss inside CreateTask: the task is
// committed and the state-row absence is logged at V(3) by the helper.
func (tm *TaskModule) AddTask(taskType TaskType, appName string, user string, metadata map[string]interface{}, callback TaskCallback) (*Task, error) {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	task := &Task{
		ID:        generateTaskID(),
		Type:      taskType,
		Status:    Pending,
		AppName:   appName,
		User:      user,
		CreatedAt: time.Now(),
		Metadata:  metadata,
		Callback:  callback,
	}

	if err := store.CreateTask(context.Background(), store.CreateTaskInput{
		TaskID:       task.ID,
		Type:         int(taskType),
		AppName:      appName,
		UserAccount:  user,
		Metadata:     metadata,
		CreatedAt:    task.CreatedAt,
		PendingState: pendingStateFromMetadata(taskType, user, appName, metadata),
	}); err != nil {
		glog.Errorf("[%s] Failed to persist task %s: %v", tm.instanceID, task.ID, err)
		return nil, fmt.Errorf("persist task: %w", err)
	}

	tm.mu.Lock()
	tm.pendingTasks = append(tm.pendingTasks, task)
	tm.mu.Unlock()

	glog.V(2).Infof("[%s] Task added: ID=%s, Type=%d, AppName=%s, User=%s, HasCallback=%v", tm.instanceID, task.ID, task.Type, task.AppName, user, callback != nil)

	tm.recordTaskHistory(task, user)

	return task, nil
}

// pendingStateFromMetadata builds the optional PendingTaskState input
// for store.CreateTask out of a task's metadata. The conventions
// callers honour today:
//
//   - source           the originating market source (set by every
//     handler that has it)
//   - realAppID        the user_applications.app_id of the target app;
//     install / upgrade handlers set this directly,
//     cancel / uninstall handlers set it from
//     LookupAppLocator's projection. cloneApp uses the
//     synthesized newAppName as appName argument
//     directly (no realAppID metadata, no
//     user_applications row) — the fallback to
//     appName lets us still attempt the state UPSERT;
//     CreateTask soft-skips the not-found case.
//   - app_name         set by cancel / uninstall handlers; also used
//     as a last-resort fallback.
//
// Returns nil when neither realAppID nor an appName is available (the
// task is not associated with a user_application — admin paths, future
// reconciliation tasks, ...). CreateTask then writes only the
// task_records row.
func pendingStateFromMetadata(taskType TaskType, user, appName string, metadata map[string]interface{}) *store.PendingTaskState {
	sourceID := metadataString(metadata, "source")
	if sourceID == "" {
		return nil
	}

	appID := metadataString(metadata, "realAppID")
	if appID == "" {
		appID = metadataString(metadata, "app_name")
	}
	if appID == "" {
		appID = strings.TrimSpace(appName)
	}
	if appID == "" {
		return nil
	}

	return &store.PendingTaskState{
		UserID:   user,
		SourceID: sourceID,
		AppID:    appID,
		OpType:   getTaskTypeString(taskType),
	}
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
	case CloneApp:
		return history.TypeActionInstall // Clone is similar to install for history
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
	case CloneApp:
		return "clone"
	default:
		return fmt.Sprintf("unknown(%d)", taskType)
	}
}

// recordTaskHistory records task information in history module
func (tm *TaskModule) recordTaskHistory(task *Task, user string) {
	if tm.historyModule == nil {
		glog.V(3).Info("History module not available, skipping task history record")
		return
	}

	// Convert task to JSON for extended field
	taskJSON, err := json.Marshal(task)
	if err != nil {
		glog.Errorf("Failed to marshal task to JSON: %v", err)
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
		glog.Errorf("Failed to record task history: %v", err)
	} else {
		glog.V(2).Infof("Recorded task history with ID: %d for task: %s, user: %s", historyRecord.ID, task.ID, user)
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

// restoreTasksFromStore loads existing tasks from persistent storage
func (tm *TaskModule) restoreTasksFromStore() error {
	if tm.taskStore == nil {
		glog.V(3).Infof("[%s] Task store not configured, skipping task restoration", tm.instanceID)
		return nil
	}

	activeTasks, err := tm.taskStore.LoadActiveTasks()
	if err != nil {
		return err
	}

	if len(activeTasks) == 0 {
		glog.V(3).Infof("[%s] No active tasks to restore from store", tm.instanceID)
		return nil
	}

	for _, task := range activeTasks {
		if task.Metadata == nil {
			task.Metadata = make(map[string]interface{})
		}

		// Tasks that were running when the service stopped are moved back to pending queue
		if task.Status == Running {
			glog.V(2).Infof("[%s] Restoring running task as pending for re-execution: ID=%s, AppName=%s, User=%s",
				tm.instanceID, task.ID, task.AppName, task.User)

			task.Status = Pending
			task.StartedAt = nil
			task.CompletedAt = nil
			task.ErrorMsg = ""
			task.Result = ""

			if err := tm.taskStore.UpsertTask(task); err != nil {
				return fmt.Errorf("failed to reset running task state for %s: %w", task.ID, err)
			}
		} else {
			glog.V(2).Infof("[%s] Restoring pending task: ID=%s, AppName=%s, User=%s",
				tm.instanceID, task.ID, task.AppName, task.User)
		}

		tm.pendingTasks = append(tm.pendingTasks, task)
	}

	sort.SliceStable(tm.pendingTasks, func(i, j int) bool {
		return tm.pendingTasks[i].CreatedAt.Before(tm.pendingTasks[j].CreatedAt)
	})

	glog.V(2).Infof("[%s] Restored %d tasks from store", tm.instanceID, len(tm.pendingTasks))
	return nil
}

func (tm *TaskModule) persistTask(task *Task) error {
	if tm.taskStore == nil {
		return nil
	}

	return tm.taskStore.UpsertTask(task)
}

func (tm *TaskModule) finalizeTaskPersistence(task *Task) {
	if err := tm.persistTask(task); err != nil {
		glog.Errorf("[%s] Failed to persist finalized task %s: %v", tm.instanceID, task.ID, err)
	}

	if tm.taskStore == nil {
		return
	}

	if task.Status == Completed || task.Status == Failed || task.Status == Canceled {
		if err := tm.taskStore.TrimCompletedTasks(completedTaskLimit); err != nil {
			glog.Errorf("[%s] Failed to trim completed tasks: %v", tm.instanceID, err)
		}
	}
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

// GetRecentTasks returns recent tasks from the store (including completed and failed)
// This method queries the database to get tasks that TaskModule has persisted
func (tm *TaskModule) GetRecentTasks(limit int) []*Task {
	if tm.taskStore == nil {
		return []*Task{}
	}

	tasks, err := tm.taskStore.LoadRecentTasks(limit)
	if err != nil {
		glog.Errorf("[%s] Failed to load recent tasks: %v", tm.instanceID, err)
		return []*Task{}
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

// dequeueNextPendingTask atomically moves the next pending task to running state.
// Returns nil if no pending task is available.
func (tm *TaskModule) dequeueNextPendingTask() *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.pendingTasks) == 0 {
		return nil
	}

	task := tm.pendingTasks[0]
	tm.pendingTasks = tm.pendingTasks[1:]

	task.Status = Running
	now := time.Now()
	task.StartedAt = &now
	tm.runningTasks[task.ID] = task

	return task
}

// removeRunningTask atomically removes a task from the running tasks map.
func (tm *TaskModule) removeRunningTask(taskID string) {
	tm.mu.Lock()
	delete(tm.runningTasks, taskID)
	tm.mu.Unlock()
}

// completeRunningTask atomically finds a running task by opID or appName+user,
// updates its status, and removes it from the running tasks map.
// Returns the task for I/O operations outside the lock, or an error if not found.
func (tm *TaskModule) completeRunningTask(
	opID, appName, user string,
	taskTypes []TaskType,
	status TaskStatus,
	result, errorMsg string,
) (*Task, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var target *Task

	if opID != "" {
		for _, t := range tm.runningTasks {
			if t.OpID == opID && matchTaskTypes(t.Type, taskTypes) {
				target = t
				break
			}
		}
	}

	if target == nil && appName != "" && user != "" {
		for _, t := range tm.runningTasks {
			if matchTaskTypes(t.Type, taskTypes) && t.AppName == appName && t.User == user {
				target = t
				break
			}
		}
	}

	if target == nil {
		return nil, fmt.Errorf("no matching running task found (opID=%s, app=%s, user=%s)", opID, appName, user)
	}

	target.Status = status
	now := time.Now()
	target.CompletedAt = &now
	target.Result = result
	if errorMsg != "" {
		target.ErrorMsg = errorMsg
	}

	delete(tm.runningTasks, target.ID)
	return target, nil
}

func matchTaskTypes(t TaskType, types []TaskType) bool {
	for _, tt := range types {
		if t == tt {
			return true
		}
	}
	return false
}

// executeNextTask gets the earliest pending task and executes it
func (tm *TaskModule) executeNextTask() {
	task := tm.dequeueNextPendingTask()
	if task == nil {
		return
	}

	if err := tm.persistTask(task); err != nil {
		glog.Errorf("[%s] Failed to persist running task state for %s: %v", tm.instanceID, task.ID, err)
	}

	glog.V(2).Infof("[%s] Executing task: ID=%s, Type=%d, AppName=%s", tm.instanceID, task.ID, task.Type, task.AppName)

	tm.executeTask(task)
}

// handleTaskFailure handles the common failure path for task execution.
//
// Order of operations (deliberate — see PERSIST notes below):
//
//  1. Mutate the in-memory task fields so the PG input is built from
//     a fully populated *Task (Result, Status=Failed, ErrorMsg,
//     CompletedAt).
//  2. PG single-transaction commit: task_records → user_application_
//     states → history_records, via persistTaskFailure. PG failure
//     here is logged but does NOT short-circuit the rest of the path;
//     restoreTasksFromStore at next restart reconciles the Running →
//     Pending → re-execute fallback.
//  3. removeRunningTask: clear the in-memory map AFTER PG is durable.
//     Reversing this with step 2 would leave a phantom (memory says
//     done, PG says running) on a PG write failure.
//  4. NATS push (best-effort). After PG, so subscribers' "failed"
//     event always has a backing truth they can poll. Sending it
//     before PG would create the harder-to-self-heal "phantom NATS
//     event with no PG row" scenario on a crash between the two.
//  5. callback. AFTER PG so the sync caller's HTTP response reflects
//     durable state. Firing it before PG (the legacy order) could
//     leave a sync caller convinced "this failed" while internal
//     state still shows the task as running, blocking retries.
//
// recordTaskResult is intentionally NOT called separately — its
// history-write work is now executed inside the persistTaskFailure
// transaction, atomically with the task_records / state updates.
func (tm *TaskModule) handleTaskFailure(task *Task, result string, err error, failureDesc string) {
	glog.Errorf("[TASK] %s for task: %s, name: %s, error: %v", failureDesc, task.ID, task.AppName, err)
	task.Result = result
	task.Status = Failed
	task.ErrorMsg = fmt.Sprintf("%s: %v, task: %s", failureDesc, err, task.ID)
	now := time.Now()
	task.CompletedAt = &now

	tm.commitTaskFailure(context.Background(), task, task.ErrorMsg)

	tm.removeRunningTask(task.ID)
	tm.sendTaskFinishedUpdate(task, "failed")

	if task.Callback != nil {
		task.Callback(result, err)
	}
}

// executeTask executes the actual task logic. Each TaskType maps to a
// single method on TaskModule; the switch picks the dispatcher and the
// surrounding scaffold (logging / sendTaskExecutionUpdate / failure
// handling / success handling) is uniform across types.
//
// Per-type side-effects (e.g. cancel marking the cancelled install
// task as Canceled) live inside the executor itself, not here, so the
// switch stays a pure dispatch table.
func (tm *TaskModule) executeTask(task *Task) {
	typeStr := getTaskTypeString(task.Type)
	glog.V(2).Infof("[TASK] Starting %s: ID=%s, App=%s, User=%s",
		typeStr, task.ID, task.AppName, task.User)

	tm.sendTaskExecutionUpdate(task)

	var result string
	var err error
	switch task.Type {
	case InstallApp:
		result, err = tm.AppInstall(task)
	case UninstallApp:
		result, err = tm.AppUninstall(task)
	case CancelAppInstall:
		result, err = tm.AppCancel(task)
	case UpgradeApp:
		result, err = tm.AppUpgrade(task)
	case CloneApp:
		result, err = tm.AppClone(task)
	default:
		err = fmt.Errorf("unknown task type: %d", task.Type)
	}

	if err != nil {
		tm.handleTaskFailure(task, result, err, typeStr+" failed")
		return
	}

	glog.V(2).Infof("[TASK] %s completed: ID=%s, App=%s", typeStr, task.ID, task.AppName)
	tm.handleTaskSuccess(task, result)
}

// handleTaskSuccess is the success-side counterpart of handleTaskFailure.
// Both run the same shape — update the in-memory task fields, drop the
// task from runningTasks, persist the terminal record, push a finished
// notification, fire the (sync) callback, and record the result in the
// history module — so keeping them as symmetric helpers makes the
// success / failure paths read identically and lets future store-layer
// migrations (CompleteTask / FailTask) land in a single function each
// instead of being scattered through executeTask.
func (tm *TaskModule) handleTaskSuccess(task *Task, result string) {
	task.Result = result
	task.Status = Completed
	now := time.Now()
	task.CompletedAt = &now

	if task.StartedAt != nil {
		glog.V(2).Infof("[TASK] Task completed: ID=%s, Type=%s, App=%s, User=%s, Duration=%v",
			task.ID, getTaskTypeString(task.Type), task.AppName, task.User, now.Sub(*task.StartedAt))
	}

	tm.removeRunningTask(task.ID)
	tm.finalizeTaskPersistence(task)
	tm.sendTaskFinishedUpdate(task, "succeed")

	if task.Callback != nil {
		glog.V(3).Infof("[TASK] Calling callback for successful task: %s", task.ID)
		task.Callback(result, nil)
	}

	tm.recordTaskResult(task, result, nil)
}

// sendTaskExecutionUpdate sends system update when task execution starts
func (tm *TaskModule) sendTaskExecutionUpdate(task *Task) {
	if tm.dataSender == nil {
		glog.V(3).Infof("Data sender not available, skipping task execution update for task: %s", task.ID)
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
		glog.Errorf("Failed to send task execution update for task %s: %v", task.ID, err)
	} else {
		glog.V(3).Infof("Successfully sent task execution update for task %s (type: %s, app: %s, user: %s)",
			task.ID, getTaskTypeString(task.Type), task.AppName, task.User)
	}
}

// sendTaskFinishedUpdate sends system update when task status changes to finished state
func (tm *TaskModule) sendTaskFinishedUpdate(task *Task, status string) {
	if tm.dataSender == nil {
		glog.V(3).Infof("Data sender not available, skipping task finished update for task: %s", task.ID)
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
		glog.Errorf("Failed to send task finished update for task %s: %v", task.ID, err)
	} else {
		glog.V(3).Infof("Successfully sent task finished update for task %s (type: %s, app: %s, user: %s, status: %s)",
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
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for taskID, task := range tm.runningTasks {
		glog.V(2).Infof("[TASK] Checking status for task: ID=%s", taskID)
		tm.showTaskStatus(task)
	}
}

// showTaskStatus shows the status of a single task
func (tm *TaskModule) showTaskStatus(task *Task) {
	glog.V(2).Infof("[TASK] Task details - ID: %s, Type: %s, Status: %d, AppName: %s, User: %s, OpID: %s, CreatedAt: %v, StartedAt: %v, CompletedAt: %v, ErrorMsg: %s, Metadata: %+v",
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
	glog.V(3).Info("Stopping task module...")

	if tm.executorTicker != nil {
		tm.executorTicker.Stop()
	}
	if tm.statusTicker != nil {
		tm.statusTicker.Stop()
	}

	tm.cancel()

	if tm.taskStore != nil {
		if err := tm.taskStore.Close(); err != nil {
			glog.Errorf("[%s] Failed to close task store: %v", tm.instanceID, err)
		}
	}

	glog.V(3).Info("Task module stopped")
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

// buildTaskTerminalHistory composes the history.HistoryRecord that
// describes a task ending (successfully or otherwise). Returns nil
// when historyModule is unconfigured (public environment, tests),
// when JSON marshalling of the result payload fails, or when the
// task does not carry an Account — callers treat nil as "skip the
// history write", matching the legacy recordTaskResult behaviour.
//
// Factored out of recordTaskResult so the new transactional path
// (persistTaskFailure / persistTaskSuccess) can build the record
// once and pass it into the same gorm.DB tx that updates
// task_records + user_application_states — keeping the three writes
// atomic. The legacy recordTaskResult below remains a thin wrapper
// for callers that have not yet migrated to the tx-orchestrated
// path; once every terminal helper is on persistTask*, recordTaskResult
// can be deleted.
func (tm *TaskModule) buildTaskTerminalHistory(task *Task, result string, err error) *history.HistoryRecord {
	if tm.historyModule == nil {
		return nil
	}
	if task == nil || task.User == "" {
		return nil
	}

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
	}

	resultJSON, marshalErr := json.Marshal(resultData)
	if marshalErr != nil {
		glog.Errorf("buildTaskTerminalHistory: marshal result data for task %s: %v", task.ID, marshalErr)
		return nil
	}

	return &history.HistoryRecord{
		Type:     getHistoryType(task.Type),
		Message:  fmt.Sprintf("%s %s completed", getTaskTypeString(task.Type), task.AppName),
		Time:     time.Now().Unix(),
		App:      task.AppName,
		Account:  task.User,
		Extended: string(resultJSON),
	}
}

// recordTaskResult records task result in history module.
//
// Legacy non-transactional path: used by terminal helpers that have
// not yet migrated to persistTaskFailure / persistTaskSuccess. Newer
// failure-side helpers (handleTaskFailure + the three *TaskFailed
// external-signal helpers) compose their history via
// buildTaskTerminalHistory and pipe it into persistTaskFailure so
// task_records + user_application_states + history_records commit in
// one transaction — see persist_terminal.go.
func (tm *TaskModule) recordTaskResult(task *Task, result string, err error) {
	if tm.historyModule == nil {
		glog.V(3).Infof("History module not available, skipping task result record for task: %s", task.ID)
		return
	}

	glog.V(2).Infof("Recording task result for task: %s, type: %s, app: %s, user: %s",
		task.ID, getTaskTypeString(task.Type), task.AppName, task.User)

	hr := tm.buildTaskTerminalHistory(task, result, err)
	if hr == nil {
		return
	}

	if storeErr := tm.historyModule.StoreRecord(hr); storeErr != nil {
		glog.Errorf("Failed to record task result in history for task %s: %v", task.ID, storeErr)
	} else {
		glog.V(2).Infof("Successfully recorded task result in history: task=%s, history_id=%d, user=%s",
			task.ID, hr.ID, task.User)
	}
}

func (tm *TaskModule) GetLatestTaskByAppNameAndUser(appName, user string) (taskType string, source string, found bool, completed bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var latestTask *Task

	// Search for InstallApp and CloneApp tasks
	for _, t := range tm.runningTasks {
		if t.AppName == appName && t.User == user && (t.Type == InstallApp || t.Type == CloneApp) {
			if latestTask == nil || t.CreatedAt.After(latestTask.CreatedAt) {
				latestTask = t
			}
		}
	}

	for _, t := range tm.pendingTasks {
		if t.AppName == appName && t.User == user && (t.Type == InstallApp || t.Type == CloneApp) {
			if latestTask == nil || t.CreatedAt.After(latestTask.CreatedAt) {
				latestTask = t
			}
		}
	}

	if latestTask == nil {
		// Log all running tasks
		glog.V(3).Infof("[%s] GetLatestTaskByAppNameAndUser - All running tasks for app: %s, user: %s", tm.instanceID, appName, user)
		for _, t := range tm.runningTasks {
			glog.V(3).Infof("  Running task: ID=%s, Type=%s, AppName=%s, User=%s, Status=%d, CreatedAt=%v",
				t.ID, getTaskTypeString(t.Type), t.AppName, t.User, t.Status, t.CreatedAt)
		}

		// Log all pending tasks
		glog.V(2).Infof("[%s] GetLatestTaskByAppNameAndUser - All pending tasks for app: %s, user: %s", tm.instanceID, appName, user)
		for _, t := range tm.pendingTasks {
			glog.V(2).Infof("  Pending task: ID=%s, Type=%s, AppName=%s, User=%s, Status=%d, CreatedAt=%v",
				t.ID, getTaskTypeString(t.Type), t.AppName, t.User, t.Status, t.CreatedAt)
		}
		return "", "", false, false
	}

	typeStr := getTaskTypeString(latestTask.Type)
	source = ""
	if s, ok := latestTask.Metadata["source"].(string); ok {
		source = s
	}
	// Since we only search pending/running tasks here, the task is not completed
	return typeStr, source, true, false
}

// GetTaskByOpID returns the task with matching opID from running or pending tasks
// Returns task type, source, found flag, and completed flag
func (tm *TaskModule) GetTaskByOpID(opID string) (taskType string, source string, found bool, completed bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var targetTask *Task

	// Search in running tasks first
	for _, t := range tm.runningTasks {
		if t.OpID == opID && (t.Type == InstallApp || t.Type == CloneApp) {
			if targetTask == nil || t.CreatedAt.After(targetTask.CreatedAt) {
				targetTask = t
			}
		}
	}

	// Search in pending tasks
	for _, t := range tm.pendingTasks {
		if t.OpID == opID && (t.Type == InstallApp || t.Type == CloneApp) {
			if targetTask == nil || t.CreatedAt.After(targetTask.CreatedAt) {
				targetTask = t
			}
		}
	}

	if targetTask == nil {
		return "", "", false, false
	}

	typeStr := getTaskTypeString(targetTask.Type)
	source = ""
	if s, ok := targetTask.Metadata["source"].(string); ok {
		source = s
	}
	// Since we only search pending/running tasks here, the task is not completed
	return typeStr, source, true, false
}

// GetInstanceID returns the unique instance identifier
func (tm *TaskModule) GetInstanceID() string {
	return tm.instanceID
}

// HasPendingOrRunningInstallTask checks if there are any pending or running install/clone tasks for the given app and user
// Returns (hasTask, lockAcquired) - lockAcquired is always true with blocking lock, kept for API compatibility
func (tm *TaskModule) HasPendingOrRunningInstallTask(appName, user string) (hasTask bool, lockAcquired bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, t := range tm.runningTasks {
		if t.AppName == appName && t.User == user && (t.Type == InstallApp || t.Type == CloneApp) {
			return true, true
		}
	}

	for _, t := range tm.pendingTasks {
		if t.AppName == appName && t.User == user && (t.Type == InstallApp || t.Type == CloneApp) {
			return true, true
		}
	}

	return false, true
}

// InstallTaskSucceed marks an install or clone task as completed successfully by opID or appName+user
func (tm *TaskModule) InstallTaskSucceed(opID, appName, user string) error {
	resultMsg := "Installation completed successfully via external signal"
	task, err := tm.completeRunningTask(opID, appName, user,
		[]TaskType{InstallApp, CloneApp}, Completed, resultMsg, "")
	if err != nil {
		glog.Warningf("[%s] InstallTaskSucceed - %v", tm.instanceID, err)
		return err
	}
	if task.Type == CloneApp {
		task.Result = "Clone completed successfully via external signal"
	}

	glog.V(2).Infof("[%s] InstallTaskSucceed - Task completed: ID=%s, Type=%s, OpID=%s, App=%s, User=%s",
		tm.instanceID, task.ID, getTaskTypeString(task.Type), task.OpID, task.AppName, task.User)

	tm.finalizeTaskPersistence(task)
	tm.recordTaskResult(task, task.Result, nil)
	tm.sendTaskFinishedUpdate(task, "succeed")
	return nil
}

// InstallTaskFailed marks an install task as failed by opID or appName+user.
//
// Triggered by an external NATS signal that app-service has finalised
// the install as failed. completeRunningTask already removed the task
// from runningTasks under tm.mu, so the order here is:
//
//  1. PG single-tx commit (task_records + user_application_states +
//     history_records) via commitTaskFailure.
//  2. NATS push, best-effort.
//
// No callback is invoked: the sync caller (if any) was already
// unblocked when the executor's AppInstall returned (the "request
// accepted, OpID assigned" success), long before this NATS-driven
// terminal arrives.
func (tm *TaskModule) InstallTaskFailed(opID, appName, user, errorMsg string) error {
	task, err := tm.completeRunningTask(opID, appName, user,
		[]TaskType{InstallApp}, Failed, "Installation failed via external signal", errorMsg)
	if err != nil {
		glog.Warningf("[%s] InstallTaskFailed - %v", tm.instanceID, err)
		return err
	}

	glog.V(2).Infof("[%s] InstallTaskFailed - Task failed: ID=%s, OpID=%s, App=%s, User=%s, Error: %s",
		tm.instanceID, task.ID, task.OpID, task.AppName, task.User, errorMsg)

	tm.commitTaskFailure(context.Background(), task, errorMsg)
	tm.sendTaskFinishedUpdate(task, "failed")
	return nil
}

// InstallTaskCanceled marks an install task as canceled by app name and user
func (tm *TaskModule) InstallTaskCanceled(appName, appVersion, source, user string) error {
	task, err := tm.completeRunningTask("", appName, user,
		[]TaskType{InstallApp}, Canceled, "Installation canceled via external signal", "Installation canceled via external signal")
	if err != nil {
		glog.V(2).Infof("[%s] InstallTaskCanceled - %v", tm.instanceID, err)
		return err
	}

	glog.V(2).Infof("[%s] InstallTaskCanceled - Task canceled: ID=%s, App=%s, User=%s",
		tm.instanceID, task.ID, task.AppName, task.User)

	tm.finalizeTaskPersistence(task)
	tm.recordTaskResult(task, task.Result, fmt.Errorf("installation canceled"))
	tm.sendTaskFinishedUpdate(task, "canceled")
	return nil
}

// CancelInstallTaskSucceed marks a cancel install task as completed successfully by opID or appName+user
func (tm *TaskModule) CancelInstallTaskSucceed(opID, appName, user string) error {
	task, err := tm.completeRunningTask(opID, appName, user,
		[]TaskType{CancelAppInstall}, Completed, "Cancel installation completed successfully via external signal", "")
	if err != nil {
		glog.Warningf("[%s] CancelInstallTaskSucceed - %v", tm.instanceID, err)
		return err
	}

	glog.V(2).Infof("[%s] CancelInstallTaskSucceed - Task completed: ID=%s, OpID=%s, App=%s, User=%s",
		tm.instanceID, task.ID, task.OpID, task.AppName, task.User)

	tm.finalizeTaskPersistence(task)
	tm.recordTaskResult(task, task.Result, nil)
	tm.sendTaskFinishedUpdate(task, "succeed")
	return nil
}

// CancelInstallTaskFailed marks a cancel install task as failed by
// opID or appName+user.
//
// Same shape as InstallTaskFailed. Notably the underlying app's
// user_application_states row is NOT touched here — a failed cancel
// does not change the install's state (see buildFailedStateUpdate's
// per-type policy). Only this cancel task's own task_records row
// moves to Failed.
func (tm *TaskModule) CancelInstallTaskFailed(opID, appName, user, errorMsg string) error {
	task, err := tm.completeRunningTask(opID, appName, user,
		[]TaskType{CancelAppInstall}, Failed, "Cancel installation failed via external signal", errorMsg)
	if err != nil {
		glog.Warningf("[%s] CancelInstallTaskFailed - %v", tm.instanceID, err)
		return err
	}

	glog.V(2).Infof("[%s] CancelInstallTaskFailed - Task failed: ID=%s, OpID=%s, App=%s, User=%s, Error: %s",
		tm.instanceID, task.ID, task.OpID, task.AppName, task.User, errorMsg)

	tm.commitTaskFailure(context.Background(), task, errorMsg)
	tm.sendTaskFinishedUpdate(task, "failed")
	return nil
}

// UninstallTaskSucceed marks an uninstall task as completed successfully by opID or appName+user
func (tm *TaskModule) UninstallTaskSucceed(opID, appName, user string) error {
	task, err := tm.completeRunningTask(opID, appName, user,
		[]TaskType{UninstallApp}, Completed, "Uninstallation completed successfully via external signal", "")
	if err != nil {
		glog.Warningf("[%s] UninstallTaskSucceed - %v", tm.instanceID, err)
		return err
	}

	glog.V(2).Infof("[%s] UninstallTaskSucceed - Task completed: ID=%s, OpID=%s, App=%s, User=%s",
		tm.instanceID, task.ID, task.OpID, task.AppName, task.User)

	tm.finalizeTaskPersistence(task)
	tm.recordTaskResult(task, task.Result, nil)
	tm.sendTaskFinishedUpdate(task, "succeed")
	return nil
}

// UninstallTaskFailed marks an uninstall task as failed by opID or
// appName+user. Same shape as InstallTaskFailed.
func (tm *TaskModule) UninstallTaskFailed(opID, appName, user, errorMsg string) error {
	task, err := tm.completeRunningTask(opID, appName, user,
		[]TaskType{UninstallApp}, Failed, "Uninstallation failed via external signal", errorMsg)
	if err != nil {
		glog.Warningf("[%s] UninstallTaskFailed - %v", tm.instanceID, err)
		return err
	}

	glog.V(2).Infof("[%s] UninstallTaskFailed - Task failed: ID=%s, OpID=%s, App=%s, User=%s, Error: %s",
		tm.instanceID, task.ID, task.OpID, task.AppName, task.User, errorMsg)

	tm.commitTaskFailure(context.Background(), task, errorMsg)
	tm.sendTaskFinishedUpdate(task, "failed")
	return nil
}
