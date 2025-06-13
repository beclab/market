package task

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
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
)

// Task represents a task in the system
type Task struct {
	ID          string                 `json:"id"`
	Type        TaskType               `json:"type"`
	Status      TaskStatus             `json:"status"`
	AppName     string                 `json:"app_name"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TaskModule manages task queues and execution
type TaskModule struct {
	mu             sync.RWMutex
	pendingTasks   []*Task          // queue for pending tasks
	runningTasks   map[string]*Task // map for running tasks
	executorTicker *time.Ticker     // ticker for task execution (every 2 seconds)
	statusTicker   *time.Ticker     // ticker for status update (every 10 seconds)
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewTaskModule creates a new task module instance
func NewTaskModule() *TaskModule {
	ctx, cancel := context.WithCancel(context.Background())

	tm := &TaskModule{
		pendingTasks: make([]*Task, 0),
		runningTasks: make(map[string]*Task),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start background goroutines
	tm.start()

	return tm
}

// AddTask adds a new task to the pending queue
func (tm *TaskModule) AddTask(taskType TaskType, appName string, metadata map[string]interface{}) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task := &Task{
		ID:        generateTaskID(),
		Type:      taskType,
		Status:    Pending,
		AppName:   appName,
		CreatedAt: time.Now(),
		Metadata:  metadata,
	}

	tm.pendingTasks = append(tm.pendingTasks, task)

	log.Printf("Task added: ID=%s, Type=%d, AppName=%s", task.ID, task.Type, task.AppName)

	return task
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

	// Start status updater (every 10 seconds)
	tm.statusTicker = time.NewTicker(10 * time.Second)
	go tm.statusUpdater()
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

	log.Printf("Executing task: ID=%s, Type=%d, AppName=%s", task.ID, task.Type, task.AppName)

	// Execute the task
	tm.executeTask(task)
}

// executeTask executes the actual task logic
func (tm *TaskModule) executeTask(task *Task) {
	switch task.Type {
	case InstallApp:
		// Get token from metadata
		token, ok := task.Metadata["token"].(string)
		if !ok {
			task.Status = Failed
			task.ErrorMsg = "Missing token in task metadata"
			return
		}

		// Get source from metadata
		source, ok := task.Metadata["source"].(string)
		if !ok {
			source = "store" // Default source
		}

		// Execute app installation
		_, err := tm.AppInstall(task.AppName, source, token)
		if err != nil {
			task.Status = Failed
			task.ErrorMsg = fmt.Sprintf("Installation failed: %v", err)
			return
		}

	case UninstallApp:
		// TODO: Implement app uninstall logic
		log.Printf("Uninstalling app: %s", task.AppName)
	case CancelAppInstall:
		// TODO: Implement cancel installation logic
		log.Printf("Canceling app installation: %s", task.AppName)
	}
}

// statusUpdater runs every 10 seconds to update running task status
func (tm *TaskModule) statusUpdater() {
	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-tm.statusTicker.C:
			tm.updateRunningTasksStatus()
		}
	}
}

// updateRunningTasksStatus updates the status of all running tasks
func (tm *TaskModule) updateRunningTasksStatus() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for taskID, task := range tm.runningTasks {
		log.Printf("Updating status for task: ID=%s", taskID)
		tm.updateTaskStatus(task)
	}
}

// updateTaskStatus updates the status of a single task
func (tm *TaskModule) updateTaskStatus(task *Task) {
	log.Printf("Checking status for task: ID=%s, Type=%d, AppName=%s", task.ID, task.Type, task.AppName)
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
