package api

import (
	"log"
	"net/http"
	"strconv"

	"market/internal/v2/history"
	"market/internal/v2/utils"
)

// 5. Query logs by specific conditions
func (s *Server) queryLogs(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/logs - Querying logs")

	// Check if history module is available
	if s.historyModule == nil {
		log.Println("History module not available")
		s.sendResponse(w, http.StatusServiceUnavailable, false, "History service not available", nil)
		return
	}

	// Convert http.Request to restful.Request to reuse utils functions
	restfulReq := s.httpToRestfulRequest(r)

	// Get user information from request
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}

	// Get query parameters
	queryParams := r.URL.Query()
	log.Printf("Log query parameters: %v", queryParams)

	// Parse start parameter (offset)
	start := 0
	if startStr := queryParams.Get("start"); startStr != "" {
		if parsedStart, err := strconv.Atoi(startStr); err == nil && parsedStart >= 0 {
			start = parsedStart
		} else {
			log.Printf("Invalid start parameter: %s", startStr)
			s.sendResponse(w, http.StatusBadRequest, false, "Invalid start parameter", nil)
			return
		}
	}

	// Parse size parameter (limit)
	size := 100 // default size
	if sizeStr := queryParams.Get("size"); sizeStr != "" {
		if parsedSize, err := strconv.Atoi(sizeStr); err == nil && parsedSize > 0 && parsedSize <= 1000 {
			size = parsedSize
		} else {
			log.Printf("Invalid size parameter: %s (must be 1-1000)", sizeStr)
			s.sendResponse(w, http.StatusBadRequest, false, "Invalid size parameter (must be 1-1000)", nil)
			return
		}
	}

	// Build query condition
	condition := &history.QueryCondition{
		Account: userID, // Only query records for the authenticated user
		Offset:  start,
		Limit:   size,
	}

	// Add optional filters
	if typeStr := queryParams.Get("type"); typeStr != "" {
		condition.Type = history.HistoryType(typeStr)
	}

	if app := queryParams.Get("app"); app != "" {
		condition.App = app
	}

	if startTimeStr := queryParams.Get("start_time"); startTimeStr != "" {
		if startTime, err := strconv.ParseInt(startTimeStr, 10, 64); err == nil {
			condition.StartTime = startTime
		} else {
			log.Printf("Invalid start_time parameter: %s", startTimeStr)
			s.sendResponse(w, http.StatusBadRequest, false, "Invalid start_time parameter", nil)
			return
		}
	}

	if endTimeStr := queryParams.Get("end_time"); endTimeStr != "" {
		if endTime, err := strconv.ParseInt(endTimeStr, 10, 64); err == nil {
			condition.EndTime = endTime
		} else {
			log.Printf("Invalid end_time parameter: %s", endTimeStr)
			s.sendResponse(w, http.StatusBadRequest, false, "Invalid end_time parameter", nil)
			return
		}
	}

	// Query records from history module
	records, err := s.historyModule.QueryRecords(condition)
	if err != nil {
		log.Printf("Failed to query history records: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to query logs", nil)
		return
	}

	// Get total count for pagination info
	totalCount, err := s.historyModule.GetRecordCount(condition)
	if err != nil {
		log.Printf("Failed to get total count: %v", err)
		// Continue without total count
		totalCount = 0
	}

	// Prepare response data
	responseData := map[string]interface{}{
		"records":        records,
		"total_count":    totalCount,
		"start":          start,
		"size":           len(records),
		"requested_size": size,
	}

	log.Printf("Successfully retrieved %d history records (start: %d, size: %d)", len(records), start, size)
	s.sendResponse(w, http.StatusOK, true, "Logs retrieved successfully", responseData)
}
