package helm

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"market/internal/v2/types"

	"github.com/gorilla/mux"
)

// ==================== Enhanced Management API ====================

// ApplicationInfoEntry represents the application information
type ApplicationInfoEntry struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description map[string]string `json:"description"`
	Developer   string            `json:"developer"`
	Website     string            `json:"website"`
	Icon        string            `json:"icon"`
	Categories  []string          `json:"categories"`
	Timestamp   int64             `json:"timestamp"`
}

// ChartInfo represents basic information about a chart
type ChartInfo struct {
	Name          string            `json:"name"`
	LatestVersion string            `json:"latest_version"`
	AppVersion    string            `json:"app_version"`
	Description   string            `json:"description"`
	Home          string            `json:"home,omitempty"`
	Icon          string            `json:"icon,omitempty"`
	Keywords      []string          `json:"keywords,omitempty"`
	Maintainers   []ChartMaintainer `json:"maintainers,omitempty"`
	TotalVersions int               `json:"total_versions"`
	Created       string            `json:"created"`
	Updated       string            `json:"updated"`
	Deprecated    bool              `json:"deprecated"`
}

// listCharts handles GET /api/v1/charts
//
// Function: Lists all available charts in repository with pagination and filtering support
//
// Query Parameters:
//   - page: Page number (default: 1)
//   - page_size: Items per page (default: 20, max: 100)
//   - keyword: Filter by keyword in name/description
//   - maintainer: Filter by maintainer
//   - deprecated: Include deprecated charts (true/false, default: false)
//   - sort: Sort order (name, created, updated, version)
//   - order: Sort direction (asc, desc, default: asc)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Charts retrieved successfully",
//	  "data": {
//	    "charts": [
//	      {
//	        "name": "nginx",
//	        "latest_version": "1.2.3",
//	        "app_version": "1.21.0",
//	        "description": "NGINX web server",
//	        "home": "https://nginx.org",
//	        "icon": "https://example.com/nginx-icon.png",
//	        "keywords": ["web", "server"],
//	        "maintainers": [
//	          {
//	            "name": "John Doe",
//	            "email": "john@example.com"
//	          }
//	        ],
//	        "total_versions": 5,
//	        "created": "2023-01-01T00:00:00Z",
//	        "updated": "2023-12-01T00:00:00Z",
//	        "deprecated": false
//	      }
//	    ],
//	    "total_count": 50,
//	    "page": 1,
//	    "page_size": 20
//	  }
//	}
//
// HTTP Status Codes:
//   - 200: Success - charts list returned
//   - 400: Bad Request - invalid query parameters or missing headers
//   - 401: Unauthorized - invalid user context
//   - 500: Internal Server Error - failed to retrieve charts
func (hr *HelmRepository) listCharts(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Log user action for audit
	logUserAction(userCtx, "LIST", "charts")

	// Check if cache manager is available
	if globalCacheManager == nil {
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		http.Error(w, "User data not found", http.StatusUnauthorized)
		return
	}

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		http.Error(w, "Source data not found", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	keyword := strings.ToLower(r.URL.Query().Get("keyword"))
	maintainer := strings.ToLower(r.URL.Query().Get("maintainer"))

	// Prepare response
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Charts     []ChartInfo `json:"charts"`
			TotalCount int         `json:"total_count"`
			Page       int         `json:"page"`
			PageSize   int         `json:"page_size"`
		} `json:"data"`
	}{
		Success: true,
		Message: "Charts retrieved successfully",
	}

	// Convert AppInfoLatest to ChartInfo
	charts := make([]ChartInfo, 0)
	for _, appData := range sourceData.AppInfoLatest {
		if appData == nil || appData.RawData == nil {
			continue
		}

		app := appData.RawData

		// Skip if app doesn't have essential information
		if app.Name == "" || app.Version == "" {
			continue
		}

		// Apply filters
		if keyword != "" {
			matched := false
			if strings.Contains(strings.ToLower(app.Name), keyword) {
				matched = true
			}
			if !matched && len(app.Description) > 0 {
				for _, desc := range app.Description {
					if strings.Contains(strings.ToLower(desc), keyword) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}

		if maintainer != "" && !strings.Contains(strings.ToLower(app.Developer), maintainer) {
			continue
		}

		// Create chart info
		chart := ChartInfo{
			Name:          app.Name,
			LatestVersion: app.Version,
			AppVersion:    app.Version,
			Created:       time.Unix(appData.Timestamp, 0).Format(time.RFC3339),
			Updated:       time.Unix(appData.Timestamp, 0).Format(time.RFC3339),
			TotalVersions: 1,
			Deprecated:    false,
		}

		// Add description
		if len(app.Description) > 0 {
			if desc, ok := app.Description["en-US"]; ok && desc != "" {
				chart.Description = desc
			} else {
				for _, desc := range app.Description {
					if desc != "" {
						chart.Description = desc
						break
					}
				}
			}
		}

		// Add icon
		if app.Icon != "" {
			chart.Icon = app.Icon
		}

		// Add home URL
		if app.Website != "" {
			chart.Home = app.Website
		}

		// Add maintainers
		if app.Developer != "" {
			chart.Maintainers = []ChartMaintainer{
				{
					Name: app.Developer,
				},
			}
		}

		// Add keywords from categories
		if len(app.Categories) > 0 {
			chart.Keywords = app.Categories
		}

		charts = append(charts, chart)
	}

	// Apply pagination
	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= len(charts) {
		start = len(charts)
	}
	if end > len(charts) {
		end = len(charts)
	}

	// Set response data
	response.Data.Charts = charts[start:end]
	response.Data.TotalCount = len(charts)
	response.Data.Page = page
	response.Data.PageSize = pageSize

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Write response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// uploadChart handles POST /api/v1/charts
//
// Function: Uploads new chart packages to the repository
//
// Request Content-Type: multipart/form-data
// Form Fields:
//   - chart: Chart package file (.tgz format, required)
//   - force: Overwrite existing version (true/false, default: false)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Request Size Limit: 100MB per chart package
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Chart uploaded successfully",
//	  "data": {
//	    "chart_name": "nginx",
//	    "version": "1.2.3",
//	    "filename": "nginx-1.2.3.tgz",
//	    "size": 2048576,
//	    "digest": "sha256:1234567890abcdef...",
//	    "uploaded_at": "2023-12-01T10:00:00Z"
//	  }
//	}
//
// HTTP Status Codes:
//   - 201: Created - chart uploaded successfully
//   - 400: Bad Request - invalid file format, missing file, or missing headers
//   - 401: Unauthorized - invalid user context
//   - 403: Forbidden - user doesn't have upload permission
//   - 409: Conflict - chart version already exists (when force=false)
//   - 413: Request Entity Too Large - file size exceeds limit
//   - 422: Unprocessable Entity - invalid chart structure
//   - 500: Internal Server Error - failed to save chart
func (hr *HelmRepository) uploadChart(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Log user action for audit
	logUserAction(userCtx, "UPLOAD", "chart")

	// Return not implemented response with explanation
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": "Chart upload is not supported via this endpoint",
		"error":   "Please use /api/v2/apps/upload endpoint for uploading application packages",
		"details": map[string]string{
			"reason": "This is a dynamic repository that uses the main application upload API",
			"type":   "dynamic_repository",
			"note":   "All chart packages should be uploaded through the main application upload API at /api/v2/apps/upload",
			"docs":   "Please refer to the API documentation for the correct upload endpoint",
		},
	})
}

// getChartInfo handles GET /api/v1/charts/{name}
//
// Function: Gets detailed information for a specific chart, including all versions
//
// URL Parameters:
//   - name: Chart name (required)
//
// Query Parameters:
//   - include_deprecated: Include deprecated versions (true/false, default: false)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Chart information retrieved successfully",
//	  "data": {
//	    "name": "nginx",
//	    "versions": [
//	      {
//	        "version": "1.2.3",
//	        "app_version": "1.21.0",
//	        "description": "NGINX web server",
//	        "created": "2023-12-01T00:00:00Z",
//	        "digest": "sha256:1234567890abcdef...",
//	        "size": 2048576,
//	        "urls": ["https://repo.example.com/charts/nginx-1.2.3.tgz"],
//	        "maintainers": [...],
//	        "keywords": ["web", "server"],
//	        "home": "https://nginx.org",
//	        "sources": ["https://github.com/nginx/nginx"],
//	        "icon": "https://example.com/nginx-icon.png",
//	        "deprecated": false
//	      }
//	    ],
//	    "total_count": 5
//	  }
//	}
//
// HTTP Status Codes:
//   - 200: Success - chart information returned
//   - 400: Bad Request - missing headers
//   - 401: Unauthorized - invalid user context
//   - 403: Forbidden - user doesn't have access to this chart
//   - 404: Not Found - chart not found
//   - 500: Internal Server Error - failed to retrieve chart information
func (hr *HelmRepository) getChartInfo(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Extract chart name from URL parameters
	vars := mux.Vars(r)
	chartName := vars["name"]

	// Log user action for audit
	logUserAction(userCtx, "GET_INFO", chartName)

	// Check if cache manager is available
	if globalCacheManager == nil {
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		http.Error(w, "User data not found", http.StatusUnauthorized)
		return
	}

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		http.Error(w, "Source data not found", http.StatusUnauthorized)
		return
	}

	// Find the chart in AppInfoLatest
	var chartData *types.ApplicationInfoEntry
	for _, appData := range sourceData.AppInfoLatest {
		if appData != nil && appData.RawData != nil && appData.RawData.Name == chartName {
			chartData = appData.RawData
			break
		}
	}

	// If chart not found, return 404
	if chartData == nil {
		http.Error(w, "Chart not found", http.StatusNotFound)
		return
	}

	// Prepare response
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Name     string `json:"name"`
			Versions []struct {
				Version     string            `json:"version"`
				AppVersion  string            `json:"app_version"`
				Description string            `json:"description"`
				Created     string            `json:"created"`
				Updated     string            `json:"updated"`
				Maintainers []ChartMaintainer `json:"maintainers,omitempty"`
				Keywords    []string          `json:"keywords,omitempty"`
				Home        string            `json:"home,omitempty"`
				Icon        string            `json:"icon,omitempty"`
				Deprecated  bool              `json:"deprecated"`
			} `json:"versions"`
			TotalCount int `json:"total_count"`
		} `json:"data"`
	}{
		Success: true,
		Message: "Chart information retrieved successfully",
	}

	// Set chart name
	response.Data.Name = chartData.Name

	// Add version information
	version := struct {
		Version     string            `json:"version"`
		AppVersion  string            `json:"app_version"`
		Description string            `json:"description"`
		Created     string            `json:"created"`
		Updated     string            `json:"updated"`
		Maintainers []ChartMaintainer `json:"maintainers,omitempty"`
		Keywords    []string          `json:"keywords,omitempty"`
		Home        string            `json:"home,omitempty"`
		Icon        string            `json:"icon,omitempty"`
		Deprecated  bool              `json:"deprecated"`
	}{
		Version:    chartData.Version,
		AppVersion: chartData.Version,
		Created:    time.Unix(chartData.CreateTime, 0).Format(time.RFC3339),
		Updated:    time.Unix(chartData.UpdateTime, 0).Format(time.RFC3339),
		Deprecated: false,
	}

	// Add description
	if len(chartData.Description) > 0 {
		if desc, ok := chartData.Description["en-US"]; ok && desc != "" {
			version.Description = desc
		} else {
			for _, desc := range chartData.Description {
				if desc != "" {
					version.Description = desc
					break
				}
			}
		}
	}

	// Add maintainers
	if chartData.Developer != "" {
		version.Maintainers = []ChartMaintainer{
			{
				Name: chartData.Developer,
			},
		}
	}

	// Add keywords from categories
	if len(chartData.Categories) > 0 {
		version.Keywords = chartData.Categories
	}

	// Add home URL
	if chartData.Website != "" {
		version.Home = chartData.Website
	}

	// Add icon
	if chartData.Icon != "" {
		version.Icon = chartData.Icon
	}

	// Add version to response
	response.Data.Versions = append(response.Data.Versions, version)
	response.Data.TotalCount = 1

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Write response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// deleteChartVersion handles DELETE /api/v1/charts/{name}/{version}
//
// Function: Deletes a specific version of a chart
//
// URL Parameters:
//   - name: Chart name (required)
//   - version: Chart version (required)
//
// Query Parameters:
//   - force: Force deletion even if it's the last version (true/false, default: false)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Chart version deleted successfully",
//	  "data": {
//	    "chart_name": "nginx",
//	    "version": "1.2.3",
//	    "deleted_at": "2023-12-01T10:00:00Z"
//	  }
//	}
//
// HTTP Status Codes:
//   - 200: Success - chart version deleted
//   - 400: Bad Request - cannot delete last version without force flag or missing headers
//   - 401: Unauthorized - invalid user context
//   - 403: Forbidden - user doesn't have delete permission for this chart
//   - 404: Not Found - chart or version not found
//   - 409: Conflict - version is referenced by other resources
//   - 500: Internal Server Error - failed to delete chart version
func (hr *HelmRepository) deleteChartVersion(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Extract chart name and version from URL parameters
	vars := mux.Vars(r)
	chartName := vars["name"]
	version := vars["version"]

	// Log user action for audit
	logUserAction(userCtx, "DELETE", chartName+":"+version)

	// Return not implemented response with explanation
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": "Chart version deletion is not supported via this endpoint",
		"error":   "Please use /api/v2/apps/delete endpoint for deleting application versions",
		"details": map[string]string{
			"reason": "This is a dynamic repository that uses the main application management API",
			"type":   "dynamic_repository",
			"note":   "All chart version deletions should be performed through the main application management API at /api/v2/apps/delete",
			"docs":   "Please refer to the API documentation for the correct deletion endpoint",
		},
	})
}

// getChartVersions handles GET /api/v1/charts/{name}/versions
//
// Function: Gets all versions list for a specific chart
//
// URL Parameters:
//   - name: Chart name (required)
//
// Query Parameters:
//   - include_deprecated: Include deprecated versions (true/false, default: false)
//   - sort: Sort order (version, created, default: version)
//   - order: Sort direction (asc, desc, default: desc)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Chart versions retrieved successfully",
//	  "data": {
//	    "chart_name": "nginx",
//	    "versions": ["1.2.3", "1.2.2", "1.2.1"],
//	    "total_count": 3
//	  }
//	}
//
// HTTP Status Codes:
//   - 200: Success - chart versions returned
//   - 400: Bad Request - missing headers
//   - 401: Unauthorized - invalid user context
//   - 403: Forbidden - user doesn't have access to this chart
//   - 404: Not Found - chart not found
//   - 500: Internal Server Error - failed to retrieve versions
func (hr *HelmRepository) getChartVersions(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Extract chart name from URL parameters
	vars := mux.Vars(r)
	chartName := vars["name"]

	// Log user action for audit
	logUserAction(userCtx, "GET_VERSIONS", chartName)

	// Check if cache manager is available
	if globalCacheManager == nil {
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		http.Error(w, "User data not found", http.StatusUnauthorized)
		return
	}

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		http.Error(w, "Source data not found", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "version"
	}
	order := r.URL.Query().Get("order")
	if order == "" {
		order = "desc"
	}

	// Find the chart in AppInfoLatest
	var chartData *types.ApplicationInfoEntry
	for _, appData := range sourceData.AppInfoLatest {
		if appData != nil && appData.RawData != nil && appData.RawData.Name == chartName {
			chartData = appData.RawData
			break
		}
	}

	// If chart not found, return 404
	if chartData == nil {
		http.Error(w, "Chart not found", http.StatusNotFound)
		return
	}

	// Prepare response
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ChartName  string   `json:"chart_name"`
			Versions   []string `json:"versions"`
			TotalCount int      `json:"total_count"`
		} `json:"data"`
	}{
		Success: true,
		Message: "Chart versions retrieved successfully",
	}

	// Set chart name
	response.Data.ChartName = chartData.Name

	// Add version information
	// Note: Currently we only have the latest version in AppInfoLatest
	response.Data.Versions = []string{chartData.Version}
	response.Data.TotalCount = 1

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Write response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// searchCharts handles GET /api/v1/charts/search
//
// Function: Searches for charts in the repository
//
// Query Parameters:
//   - q: Search query (required)
//   - page: Page number (default: 1)
//   - page_size: Items per page (default: 20)
//   - include_deprecated: Include deprecated charts (true/false, default: false)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Search Fields: name, description, keywords, maintainer names
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Search completed successfully",
//	  "data": {
//	    "results": [...],
//	    "total_count": 10,
//	    "query": "nginx",
//	    "page": 1,
//	    "page_size": 20
//	  }
//	}
//
// HTTP Status Codes:
//   - 200: Success - search results returned
//   - 400: Bad Request - missing or invalid search query or missing headers
//   - 401: Unauthorized - invalid user context
//   - 500: Internal Server Error - search failed
func (hr *HelmRepository) searchCharts(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Get search query
	query := strings.ToLower(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "Missing required query parameter: q", http.StatusBadRequest)
		return
	}

	// Parse pagination parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Log user action for audit
	logUserAction(userCtx, "SEARCH", query)

	// Check if cache manager is available
	if globalCacheManager == nil {
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		http.Error(w, "User data not found", http.StatusUnauthorized)
		return
	}

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		http.Error(w, "Source data not found", http.StatusUnauthorized)
		return
	}

	// Search in AppInfoLatest
	var results []ChartInfo
	for _, appData := range sourceData.AppInfoLatest {
		if appData == nil || appData.RawData == nil {
			continue
		}

		app := appData.RawData

		// Skip if app doesn't have essential information
		if app.Name == "" || app.Version == "" {
			continue
		}

		// Search in name, description, and categories
		matched := false

		// Search in name
		if strings.Contains(strings.ToLower(app.Name), query) {
			matched = true
		}

		// Search in description
		if !matched && len(app.Description) > 0 {
			for _, desc := range app.Description {
				if strings.Contains(strings.ToLower(desc), query) {
					matched = true
					break
				}
			}
		}

		// Search in categories
		if !matched && len(app.Categories) > 0 {
			for _, category := range app.Categories {
				if strings.Contains(strings.ToLower(category), query) {
					matched = true
					break
				}
			}
		}

		// Skip if no match
		if !matched {
			continue
		}

		// Create chart info
		chart := ChartInfo{
			Name:          app.Name,
			LatestVersion: app.Version,
			AppVersion:    app.Version,
			Created:       time.Unix(appData.Timestamp, 0).Format(time.RFC3339),
			Updated:       time.Unix(appData.Timestamp, 0).Format(time.RFC3339),
			TotalVersions: 1,
			Deprecated:    false,
		}

		// Add description
		if len(app.Description) > 0 {
			if desc, ok := app.Description["en-US"]; ok && desc != "" {
				chart.Description = desc
			} else {
				for _, desc := range app.Description {
					if desc != "" {
						chart.Description = desc
						break
					}
				}
			}
		}

		// Add icon
		if app.Icon != "" {
			chart.Icon = app.Icon
		}

		// Add home URL
		if app.Website != "" {
			chart.Home = app.Website
		}

		// Add maintainers
		if app.Developer != "" {
			chart.Maintainers = []ChartMaintainer{
				{
					Name: app.Developer,
				},
			}
		}

		// Add keywords from categories
		if len(app.Categories) > 0 {
			chart.Keywords = app.Categories
		}

		results = append(results, chart)
	}

	// If no results found, return empty list instead of 404
	if len(results) == 0 {
		response := struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Data    struct {
				Results    []ChartInfo `json:"results"`
				TotalCount int         `json:"total_count"`
				Query      string      `json:"query"`
				Page       int         `json:"page"`
				PageSize   int         `json:"page_size"`
			} `json:"data"`
		}{
			Success: true,
			Message: "Search completed successfully",
		}

		response.Data.Results = []ChartInfo{}
		response.Data.TotalCount = 0
		response.Data.Query = query
		response.Data.Page = page
		response.Data.PageSize = pageSize

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Apply pagination
	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= len(results) {
		start = len(results)
	}
	if end > len(results) {
		end = len(results)
	}

	// Prepare response
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Results    []ChartInfo `json:"results"`
			TotalCount int         `json:"total_count"`
			Query      string      `json:"query"`
			Page       int         `json:"page"`
			PageSize   int         `json:"page_size"`
		} `json:"data"`
	}{
		Success: true,
		Message: "Search completed successfully",
	}

	// Set response data
	response.Data.Results = results[start:end]
	response.Data.TotalCount = len(results)
	response.Data.Query = query
	response.Data.Page = page
	response.Data.PageSize = pageSize

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Write response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// getChartMetadata handles GET /api/v1/charts/{name}/{version}/metadata
//
// Function: Gets detailed metadata for a specific chart version
//
// URL Parameters:
//   - name: Chart name (required)
//   - version: Chart version (required)
//
// Request Headers:
//   - X-Market-User: User ID for authentication (required)
//   - X-Market-Source: Source system identifier (required)
//
// Response Format: JSON
// Response Structure:
//
//	{
//	  "success": true,
//	  "message": "Chart metadata retrieved successfully",
//	  "data": {
//	    "apiVersion": "v2",
//	    "name": "nginx",
//	    "version": "1.2.3",
//	    "kubeVersion": ">=1.19.0",
//	    "description": "NGINX web server",
//	    "type": "application",
//	    "keywords": ["web", "server"],
//	    "home": "https://nginx.org",
//	    "sources": ["https://github.com/nginx/nginx"],
//	    "dependencies": [...],
//	    "maintainers": [...],
//	    "icon": "https://example.com/nginx-icon.png",
//	    "appVersion": "1.21.0",
//	    "deprecated": false,
//	    "annotations": {}
//	  }
//	}
//
// HTTP Status Codes:
//   - 200: Success - metadata returned
//   - 400: Bad Request - missing headers
//   - 401: Unauthorized - invalid user context
//   - 403: Forbidden - user doesn't have access to this chart
//   - 404: Not Found - chart or version not found
//   - 500: Internal Server Error - failed to extract metadata
func (hr *HelmRepository) getChartMetadata(w http.ResponseWriter, r *http.Request) {
	// Extract user context from headers
	userCtx := extractUserContext(r)

	// Validate user context
	if !validateUserContext(userCtx) {
		http.Error(w, "Missing required headers: X-Market-User and X-Market-Source", http.StatusBadRequest)
		return
	}

	// Extract chart name and version from URL parameters
	vars := mux.Vars(r)
	chartName := vars["name"]
	version := vars["version"]

	// Log user action for audit
	logUserAction(userCtx, "GET_METADATA", chartName+":"+version)

	// Check if cache manager is available
	if globalCacheManager == nil {
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	// Get user data from cache
	userData := globalCacheManager.GetUserData(userCtx.UserID)
	if userData == nil {
		http.Error(w, "User data not found", http.StatusUnauthorized)
		return
	}

	// Get source data from user data
	sourceData := userData.Sources[userCtx.Source]
	if sourceData == nil {
		http.Error(w, "Source data not found", http.StatusUnauthorized)
		return
	}

	// Find the chart in AppInfoLatest
	var chartData *types.ApplicationInfoEntry
	for _, appData := range sourceData.AppInfoLatest {
		if appData != nil && appData.RawData != nil && appData.RawData.Name == chartName && appData.RawData.Version == version {
			chartData = appData.RawData
			break
		}
	}

	// If chart not found, return 404
	if chartData == nil {
		http.Error(w, "Chart or version not found", http.StatusNotFound)
		return
	}

	// Prepare response
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			APIVersion   string            `json:"apiVersion"`
			Name         string            `json:"name"`
			Version      string            `json:"version"`
			KubeVersion  string            `json:"kubeVersion"`
			Description  string            `json:"description"`
			Type         string            `json:"type"`
			Keywords     []string          `json:"keywords,omitempty"`
			Home         string            `json:"home,omitempty"`
			Sources      []string          `json:"sources,omitempty"`
			Dependencies []struct{}        `json:"dependencies,omitempty"`
			Maintainers  []ChartMaintainer `json:"maintainers,omitempty"`
			Icon         string            `json:"icon,omitempty"`
			AppVersion   string            `json:"appVersion"`
			Deprecated   bool              `json:"deprecated"`
			Annotations  map[string]string `json:"annotations,omitempty"`
		} `json:"data"`
	}{
		Success: true,
		Message: "Chart metadata retrieved successfully",
	}

	// Set basic metadata
	response.Data.APIVersion = "v2"
	response.Data.Name = chartData.Name
	response.Data.Version = chartData.Version
	response.Data.KubeVersion = ">=1.19.0"
	response.Data.Type = "application"
	response.Data.AppVersion = chartData.Version
	response.Data.Deprecated = false

	// Add description
	if len(chartData.Description) > 0 {
		if desc, ok := chartData.Description["en-US"]; ok && desc != "" {
			response.Data.Description = desc
		} else {
			for _, desc := range chartData.Description {
				if desc != "" {
					response.Data.Description = desc
					break
				}
			}
		}
	}

	// Add keywords from categories
	if len(chartData.Categories) > 0 {
		response.Data.Keywords = chartData.Categories
	}

	// Add home URL
	if chartData.Website != "" {
		response.Data.Home = chartData.Website
	}

	// Add icon
	if chartData.Icon != "" {
		response.Data.Icon = chartData.Icon
	}

	// Add maintainers
	if chartData.Developer != "" {
		response.Data.Maintainers = []ChartMaintainer{
			{
				Name: chartData.Developer,
			},
		}
	}

	// Add annotations
	response.Data.Annotations = map[string]string{
		"created": time.Unix(chartData.CreateTime, 0).Format(time.RFC3339),
		"updated": time.Unix(chartData.UpdateTime, 0).Format(time.RFC3339),
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Write response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
