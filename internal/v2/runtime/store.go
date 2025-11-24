package runtime

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// StateStore manages the current runtime state in memory
type StateStore struct {
	mu         sync.RWMutex
	appStates  map[string]*AppFlowState    // key: userID:sourceID:appName
	tasks      map[string]*TaskState       // key: taskID
	components map[string]*ComponentStatus // key: component name
	chartRepo  *ChartRepoStatus            // Chart repo status
	lastUpdate time.Time
}

// NewStateStore creates a new state store
func NewStateStore() *StateStore {
	return &StateStore{
		appStates:  make(map[string]*AppFlowState),
		tasks:      make(map[string]*TaskState),
		components: make(map[string]*ComponentStatus),
		lastUpdate: time.Now(),
	}
}

// UpdateAppState updates or creates an app flow state
func (s *StateStore) UpdateAppState(state *AppFlowState) {
	if !s.mu.TryLock() {
		log.Printf("Failed to acquire lock for UpdateAppState, skipping update for app: %s", state.AppName)
		return
	}
	defer s.mu.Unlock()

	key := s.getAppStateKey(state.UserID, state.SourceID, state.AppName)
	state.LastUpdate = time.Now()
	s.appStates[key] = state
	s.lastUpdate = time.Now()
}

// GetAppState retrieves an app flow state
func (s *StateStore) GetAppState(userID, sourceID, appName string) (*AppFlowState, bool) {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetAppState, returning empty for app: %s", appName)
		return nil, false
	}
	defer s.mu.RUnlock()

	key := s.getAppStateKey(userID, sourceID, appName)
	state, ok := s.appStates[key]
	return state, ok
}

// GetAllAppStates returns all app states
func (s *StateStore) GetAllAppStates() map[string]*AppFlowState {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetAllAppStates, returning empty map")
		return make(map[string]*AppFlowState)
	}
	defer s.mu.RUnlock()

	result := make(map[string]*AppFlowState)
	for k, v := range s.appStates {
		result[k] = v
	}
	return result
}

// UpdateTask updates or creates a task state
func (s *StateStore) UpdateTask(task *TaskState) {
	if !s.mu.TryLock() {
		log.Printf("Failed to acquire lock for UpdateTask, skipping update for task: %s", task.TaskID)
		return
	}
	defer s.mu.Unlock()

	s.tasks[task.TaskID] = task
	s.lastUpdate = time.Now()
}

// GetTask retrieves a task state
func (s *StateStore) GetTask(taskID string) (*TaskState, bool) {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetTask, returning empty for task: %s", taskID)
		return nil, false
	}
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	return task, ok
}

// GetAllTasks returns all tasks
func (s *StateStore) GetAllTasks() map[string]*TaskState {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetAllTasks, returning empty map")
		return make(map[string]*TaskState)
	}
	defer s.mu.RUnlock()

	result := make(map[string]*TaskState)
	for k, v := range s.tasks {
		result[k] = v
	}
	return result
}

// RemoveTask removes a completed/failed/canceled task after some time
func (s *StateStore) RemoveTask(taskID string) {
	if !s.mu.TryLock() {
		log.Printf("Failed to acquire lock for RemoveTask, skipping removal for task: %s", taskID)
		return
	}
	defer s.mu.Unlock()

	delete(s.tasks, taskID)
	s.lastUpdate = time.Now()
}

// UpdateComponent updates or creates a component status
func (s *StateStore) UpdateComponent(component *ComponentStatus) {
	if !s.mu.TryLock() {
		log.Printf("Failed to acquire lock for UpdateComponent, skipping update for component: %s", component.Name)
		return
	}
	defer s.mu.Unlock()

	component.LastCheck = time.Now()
	s.components[component.Name] = component
	s.lastUpdate = time.Now()
}

// GetComponent retrieves a component status
func (s *StateStore) GetComponent(name string) (*ComponentStatus, bool) {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetComponent, returning empty for component: %s", name)
		return nil, false
	}
	defer s.mu.RUnlock()

	component, ok := s.components[name]
	return component, ok
}

// GetAllComponents returns all component statuses
func (s *StateStore) GetAllComponents() map[string]*ComponentStatus {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetAllComponents, returning empty map")
		return make(map[string]*ComponentStatus)
	}
	defer s.mu.RUnlock()

	result := make(map[string]*ComponentStatus)
	for k, v := range s.components {
		result[k] = v
	}
	return result
}

// GetSnapshot creates a complete snapshot of current state
func (s *StateStore) GetSnapshot() *RuntimeSnapshot {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetSnapshot, returning empty snapshot")
		return &RuntimeSnapshot{
			Timestamp:  time.Now(),
			AppStates:  make(map[string]*AppFlowState),
			Tasks:      make(map[string]*TaskState),
			Components: make(map[string]*ComponentStatus),
			Summary:    &RuntimeSummary{},
		}
	}
	defer s.mu.RUnlock()

	snapshot := &RuntimeSnapshot{
		Timestamp:  time.Now(),
		AppStates:  make(map[string]*AppFlowState),
		Tasks:      make(map[string]*TaskState),
		Components: make(map[string]*ComponentStatus),
		Summary:    &RuntimeSummary{},
	}

	// Copy app states
	for k, v := range s.appStates {
		snapshot.AppStates[k] = v
	}

	// Copy tasks
	for k, v := range s.tasks {
		snapshot.Tasks[k] = v
	}

	// Copy components
	for k, v := range s.components {
		snapshot.Components[k] = v
	}

	// Copy chart repo status
	if s.chartRepo != nil {
		snapshot.ChartRepo = s.chartRepo
	}

	// Calculate summary
	snapshot.Summary = s.calculateSummary(snapshot)

	return snapshot
}

// calculateSummary calculates aggregated statistics
func (s *StateStore) calculateSummary(snapshot *RuntimeSnapshot) *RuntimeSummary {
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

// getAppStateKey generates a key for app state map
func (s *StateStore) getAppStateKey(userID, sourceID, appName string) string {
	return fmt.Sprintf("%s:%s:%s", userID, sourceID, appName)
}

// UpdateChartRepoStatus updates chart repo status
func (s *StateStore) UpdateChartRepoStatus(status *ChartRepoStatus) {
	if !s.mu.TryLock() {
		log.Printf("Failed to acquire lock for UpdateChartRepoStatus, skipping update")
		return
	}
	defer s.mu.Unlock()

	if status != nil {
		status.LastUpdate = time.Now()
	}
	s.chartRepo = status
	s.lastUpdate = time.Now()
}

// GetChartRepoStatus retrieves chart repo status
func (s *StateStore) GetChartRepoStatus() *ChartRepoStatus {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetChartRepoStatus, returning nil")
		return nil
	}
	defer s.mu.RUnlock()
	return s.chartRepo
}

// GetLastUpdate returns the last update time
func (s *StateStore) GetLastUpdate() time.Time {
	if !s.mu.TryRLock() {
		log.Printf("Failed to acquire read lock for GetLastUpdate, returning zero time")
		return time.Time{}
	}
	defer s.mu.RUnlock()
	return s.lastUpdate
}
