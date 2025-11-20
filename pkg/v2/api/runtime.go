package api

import (
	"log"
	"net/http"

	"market/internal/v2/runtime"
)

// getRuntimeState handles GET /api/v2/runtime/state
func (s *Server) getRuntimeState(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/runtime/state - Getting runtime state")

	if s.runtimeStateService == nil {
		log.Println("Runtime state service not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Runtime state service not available", nil)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	filters := runtime.ParseFiltersFromQuery(
		query.Get("user_id"),
		query.Get("source_id"),
		query.Get("app_name"),
		query.Get("task_status"),
		query.Get("task_type"),
	)

	// Get snapshot with filters
	var snapshot *runtime.RuntimeSnapshot
	if filters.UserID != "" || filters.SourceID != "" || filters.AppName != "" ||
		filters.TaskStatus != "" || filters.TaskType != "" {
		snapshot = s.runtimeStateService.GetSnapshotWithFilters(filters)
	} else {
		snapshot = s.runtimeStateService.GetSnapshot()
	}

	log.Printf("Runtime state retrieved: %d apps, %d tasks, %d components",
		len(snapshot.AppStates), len(snapshot.Tasks), len(snapshot.Components))

	s.sendResponse(w, http.StatusOK, true, "Runtime state retrieved successfully", snapshot)
}
