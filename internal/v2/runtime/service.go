package runtime

import (
	"strings"

	"github.com/golang/glog"
)

// RuntimeStateService provides access to runtime state
type RuntimeStateService struct {
	store     *StateStore
	collector *StateCollector
}

// NewRuntimeStateService creates a new runtime state service
func NewRuntimeStateService(store *StateStore, collector *StateCollector) *RuntimeStateService {
	return &RuntimeStateService{
		store:     store,
		collector: collector,
	}
}

// GetSnapshot returns the current runtime snapshot
func (s *RuntimeStateService) GetSnapshot() *RuntimeSnapshot {
	return s.store.GetSnapshot()
}

// GetSnapshotWithFilters returns a filtered snapshot
func (s *RuntimeStateService) GetSnapshotWithFilters(filters *SnapshotFilters) *RuntimeSnapshot {
	snapshot := s.store.GetSnapshot()

	if filters == nil {
		return snapshot
	}

	filtered := &RuntimeSnapshot{
		Timestamp:  snapshot.Timestamp,
		AppStates:  make(map[string]*AppFlowState),
		Tasks:      make(map[string]*TaskState),
		Components: snapshot.Components, // Always include all components
		Summary:    &RuntimeSummary{},
	}

	// Filter app states
	if filters.UserID != "" || filters.SourceID != "" || filters.AppName != "" {
		for key, app := range snapshot.AppStates {
			if s.matchesAppFilters(app, filters) {
				filtered.AppStates[key] = app
			}
		}
	} else {
		filtered.AppStates = snapshot.AppStates
	}

	// Filter tasks
	if filters.TaskStatus != "" || filters.TaskType != "" || filters.UserID != "" || filters.AppName != "" {
		for key, task := range snapshot.Tasks {
			if s.matchesTaskFilters(task, filters) {
				filtered.Tasks[key] = task
			}
		}
	} else {
		filtered.Tasks = snapshot.Tasks
	}

	// Recalculate summary for filtered data
	filtered.Summary = s.calculateFilteredSummary(filtered)

	return filtered
}

// SnapshotFilters defines filters for snapshot queries
type SnapshotFilters struct {
	UserID     string
	SourceID   string
	AppName    string
	TaskStatus string
	TaskType   string
}

// matchesAppFilters checks if an app state matches the filters
func (s *RuntimeStateService) matchesAppFilters(app *AppFlowState, filters *SnapshotFilters) bool {
	if filters.UserID != "" && app.UserID != filters.UserID {
		return false
	}
	if filters.SourceID != "" && app.SourceID != filters.SourceID {
		return false
	}
	if filters.AppName != "" && app.AppName != filters.AppName {
		return false
	}
	return true
}

// matchesTaskFilters checks if a task matches the filters
func (s *RuntimeStateService) matchesTaskFilters(task *TaskState, filters *SnapshotFilters) bool {
	if filters.TaskStatus != "" && task.Status != filters.TaskStatus {
		return false
	}
	if filters.TaskType != "" && task.Type != filters.TaskType {
		return false
	}
	if filters.UserID != "" && task.UserID != filters.UserID {
		return false
	}
	if filters.AppName != "" && task.AppName != filters.AppName {
		return false
	}
	return true
}

// calculateFilteredSummary calculates summary for filtered snapshot
func (s *RuntimeStateService) calculateFilteredSummary(snapshot *RuntimeSnapshot) *RuntimeSummary {
	summary := &RuntimeSummary{
		TotalApps:        len(snapshot.AppStates),
		TotalTasks:       len(snapshot.Tasks),
		ActiveComponents: 0,
	}

	for _, task := range snapshot.Tasks {
		switch task.Status {
		case "pending":
			summary.PendingTasks++
		case "running":
			summary.RunningTasks++
		case "completed":
			summary.CompletedTasks++
		case "failed":
			summary.FailedTasks++
		}
	}

	for _, app := range snapshot.AppStates {
		if app.Health == "healthy" {
			summary.HealthyApps++
		} else if app.Health == "unhealthy" {
			summary.UnhealthyApps++
		}
	}

	for _, component := range snapshot.Components {
		if component.Healthy && component.Status == "running" {
			summary.ActiveComponents++
		}
	}

	return summary
}

// GetAppState retrieves a specific app state
func (s *RuntimeStateService) GetAppState(userID, sourceID, appName string) (*AppFlowState, bool) {
	return s.store.GetAppState(userID, sourceID, appName)
}

// GetTask retrieves a specific task state
func (s *RuntimeStateService) GetTask(taskID string) (*TaskState, bool) {
	return s.store.GetTask(taskID)
}

// GetTasksByApp retrieves all tasks for a specific app
func (s *RuntimeStateService) GetTasksByApp(appName, userID string) []*TaskState {
	snapshot := s.store.GetSnapshot()
	var tasks []*TaskState

	for _, task := range snapshot.Tasks {
		if task.AppName == appName && (userID == "" || task.UserID == userID) {
			tasks = append(tasks, task)
		}
	}

	return tasks
}

// GetTasksByUser retrieves all tasks for a specific user
func (s *RuntimeStateService) GetTasksByUser(userID string) []*TaskState {
	snapshot := s.store.GetSnapshot()
	var tasks []*TaskState

	for _, task := range snapshot.Tasks {
		if task.UserID == userID {
			tasks = append(tasks, task)
		}
	}

	return tasks
}

// GetAppsByUser retrieves all apps for a specific user
func (s *RuntimeStateService) GetAppsByUser(userID string) []*AppFlowState {
	snapshot := s.store.GetSnapshot()
	var apps []*AppFlowState

	for _, app := range snapshot.AppStates {
		if app.UserID == userID {
			apps = append(apps, app)
		}
	}

	return apps
}

// Start starts the service
func (s *RuntimeStateService) Start() error {
	glog.V(3).Info("Starting runtime state service...")
	return s.collector.Start()
}

// Stop stops the service
func (s *RuntimeStateService) Stop() {
	glog.V(3).Info("Stopping runtime state service...")
	s.collector.Stop()
}

// ParseFiltersFromQuery parses filters from query parameters
func ParseFiltersFromQuery(userID, sourceID, appName, taskStatus, taskType string) *SnapshotFilters {
	filters := &SnapshotFilters{}

	if strings.TrimSpace(userID) != "" {
		filters.UserID = strings.TrimSpace(userID)
	}
	if strings.TrimSpace(sourceID) != "" {
		filters.SourceID = strings.TrimSpace(sourceID)
	}
	if strings.TrimSpace(appName) != "" {
		filters.AppName = strings.TrimSpace(appName)
	}
	if strings.TrimSpace(taskStatus) != "" {
		filters.TaskStatus = strings.TrimSpace(taskStatus)
	}
	if strings.TrimSpace(taskType) != "" {
		filters.TaskType = strings.TrimSpace(taskType)
	}

	return filters
}
