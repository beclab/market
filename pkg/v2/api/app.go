package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"market/internal/v2/appinfo"
	"market/internal/v2/types"
	"market/internal/v2/utils"

	"github.com/emicklei/go-restful/v3"
	"github.com/gorilla/mux"
)

// AppsInfoRequest represents the request body for /api/v2/apps
type AppsInfoRequest struct {
	Apps []AppQueryInfo `json:"apps"`
}

// AppQueryInfo represents individual app query information
type AppQueryInfo struct {
	AppID          string `json:"appid"`
	SourceDataName string `json:"sourceDataName"`
}

// AppInfoLatestDataResponse represents AppInfoLatestData for API response without raw_data
type AppInfoLatestDataResponse struct {
	Type            types.AppDataType    `json:"type"`
	Timestamp       int64                `json:"timestamp"`
	Version         string               `json:"version,omitempty"`
	RawPackage      string               `json:"raw_package"`
	Values          []*types.Values      `json:"values"`
	AppInfo         *types.AppInfo       `json:"app_info"`
	RenderedPackage string               `json:"rendered_package"`
	AppSimpleInfo   *types.AppSimpleInfo `json:"app_simple_info"`
}

// AppsInfoResponse represents the response for /api/v2/apps
type AppsInfoResponse struct {
	Apps       []*AppInfoLatestDataResponse `json:"apps"`
	TotalCount int                          `json:"total_count"`
	NotFound   []AppQueryInfo               `json:"not_found,omitempty"`
}

// MarketInfoResponse represents the response structure for market information
type MarketInfoResponse struct {
	UsersData    map[string]*types.UserData `json:"users_data"`
	CacheStats   map[string]interface{}     `json:"cache_stats"`
	TotalUsers   int                        `json:"total_users"`
	TotalSources int                        `json:"total_sources"`
}

// FilteredSourceData represents filtered source data
type FilteredSourceData struct {
	Type           types.SourceDataType         `json:"type"`
	AppStateLatest []*types.AppStateLatestData  `json:"app_state_latest"`
	AppInfoLatest  []*FilteredAppInfoLatestData `json:"app_info_latest"`
	Others         *types.Others                `json:"others,omitempty"`
}

// FilteredAppInfoLatestData contains only AppSimpleInfo from AppInfoLatestData
type FilteredAppInfoLatestData struct {
	Type          types.AppDataType    `json:"type"`
	Timestamp     int64                `json:"timestamp"`
	Version       string               `json:"version,omitempty"`
	AppSimpleInfo *types.AppSimpleInfo `json:"app_simple_info"`
}

// FilteredUserData represents filtered user data
type FilteredUserData struct {
	Sources map[string]*FilteredSourceData `json:"sources"`
	Hash    string                         `json:"hash"`
}

// MarketDataResponse represents the response structure for market data
type MarketDataResponse struct {
	UserData  *FilteredUserData `json:"user_data"`
	UserID    string            `json:"user_id"`
	Timestamp int64             `json:"timestamp"`
}

// 1. Get market information
func (s *Server) getMarketInfo(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/market - Getting market information")

	// Add timeout context
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Create a channel to receive the result
	type result struct {
		data  MarketInfoResponse
		stats map[string]interface{}
		err   error
	}
	resultChan := make(chan result, 1)

	// Run the data retrieval in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getMarketInfo: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get all users data from cache
		allUsersData := s.cacheManager.GetAllUsersData()
		if allUsersData == nil {
			log.Println("Warning: GetAllUsersData returned nil")
			allUsersData = make(map[string]*types.UserData)
		}

		// Get cache statistics
		cacheStats := s.cacheManager.GetCacheStats()
		if cacheStats == nil {
			log.Println("Warning: GetCacheStats returned nil")
			cacheStats = make(map[string]interface{})
		}

		// Prepare response data
		responseData := MarketInfoResponse{
			UsersData:  allUsersData,
			CacheStats: cacheStats,
			TotalUsers: len(allUsersData),
		}

		resultChan <- result{data: responseData, stats: cacheStats}
	}()

	// Wait for result or timeout
	select {
	case <-ctx.Done():
		log.Printf("Request timeout or cancelled for /api/v2/market")
		s.sendResponse(w, http.StatusRequestTimeout, false, "Request timeout - data retrieval took too long", nil)
		return
	case res := <-resultChan:
		if res.err != nil {
			log.Printf("Error retrieving market information: %v", res.err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to retrieve market information", nil)
			return
		}

		log.Printf("Market information retrieved successfully: %d users", len(res.data.UsersData))
		s.sendResponse(w, http.StatusOK, true, "Market information retrieved successfully", res.data)
	}
}

// 2. Get specific application information (supports multiple queries)
func (s *Server) getAppsInfo(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/apps - Getting apps information")

	// Add timeout context
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for apps request: %s", userID)

	// Step 2: Parse JSON request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Failed to read request body", nil)
		return
	}
	defer r.Body.Close()

	var request AppsInfoRequest
	if err := json.Unmarshal(body, &request); err != nil {
		log.Printf("Failed to parse JSON request: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid JSON format", nil)
		return
	}

	if len(request.Apps) == 0 {
		log.Printf("Empty apps array in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Apps array cannot be empty", nil)
		return
	}

	log.Printf("Received request for %d apps from user %s", len(request.Apps), userID)

	// Create a channel to receive the result
	type result struct {
		response AppsInfoResponse
		err      error
	}
	resultChan := make(chan result, 1)

	// Step 3: Process apps information retrieval in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getAppsInfo: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get user data from cache
		userData := s.cacheManager.GetUserData(userID)
		if userData == nil {
			log.Printf("User data not found for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("user data not found")}
			return
		}

		var foundApps []*AppInfoLatestDataResponse
		var notFoundApps []AppQueryInfo

		// Step 4: Find AppInfoLatestData for each requested app
		for _, appQuery := range request.Apps {
			log.Printf("Searching for app: %s in source: %s", appQuery.AppID, appQuery.SourceDataName)

			// Check if source exists
			sourceData, exists := userData.Sources[appQuery.SourceDataName]
			if !exists {
				log.Printf("Source data not found: %s for user: %s", appQuery.SourceDataName, userID)
				notFoundApps = append(notFoundApps, appQuery)
				continue
			}

			// Search for app in AppInfoLatest array
			found := false
			for _, appInfoData := range sourceData.AppInfoLatest {
				if appInfoData == nil {
					continue
				}

				// Check multiple possible ID fields for matching
				var appID string
				if appInfoData.RawData != nil {
					// Priority: ID > AppID > Name
					if appInfoData.RawData.ID != "" {
						appID = appInfoData.RawData.ID
					} else if appInfoData.RawData.AppID != "" {
						appID = appInfoData.RawData.AppID
					} else if appInfoData.RawData.Name != "" {
						appID = appInfoData.RawData.Name
					}
				}

				// Also check AppSimpleInfo if available
				if appID == "" && appInfoData.AppSimpleInfo != nil {
					appID = appInfoData.AppSimpleInfo.AppID
				}

				// Match the requested app ID or name
				if appID == appQuery.AppID || (appInfoData.RawData != nil && appInfoData.RawData.Name == appQuery.AppID) {
					log.Printf("Found app: %s in source: %s", appQuery.AppID, appQuery.SourceDataName)

					// Convert AppInfoLatestData to AppInfoLatestDataResponse (without raw_data)
					responseData := &AppInfoLatestDataResponse{
						Type:            appInfoData.Type,
						Timestamp:       appInfoData.Timestamp,
						Version:         appInfoData.Version,
						RawPackage:      appInfoData.RawPackage,
						Values:          appInfoData.Values,
						AppInfo:         appInfoData.AppInfo,
						RenderedPackage: appInfoData.RenderedPackage,
						AppSimpleInfo:   appInfoData.AppSimpleInfo,
					}

					foundApps = append(foundApps, responseData)
					found = true
					break
				}
			}

			if !found {
				log.Printf("App not found: %s in source: %s", appQuery.AppID, appQuery.SourceDataName)
				notFoundApps = append(notFoundApps, appQuery)
			}
		}

		// Step 5: Prepare response
		response := AppsInfoResponse{
			Apps:       foundApps,
			TotalCount: len(foundApps),
		}

		if len(notFoundApps) > 0 {
			response.NotFound = notFoundApps
		}

		log.Printf("Apps info retrieval completed: %d found, %d not found", len(foundApps), len(notFoundApps))
		resultChan <- result{response: response}
	}()

	// Wait for result or timeout
	select {
	case <-ctx.Done():
		log.Printf("Request timeout or cancelled for /api/v2/apps")
		s.sendResponse(w, http.StatusRequestTimeout, false, "Request timeout - apps retrieval took too long", nil)
		return
	case res := <-resultChan:
		if res.err != nil {
			log.Printf("Error retrieving apps information: %v", res.err)
			if res.err.Error() == "user data not found" {
				s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
			} else {
				s.sendResponse(w, http.StatusInternalServerError, false, "Failed to retrieve apps information", nil)
			}
			return
		}

		log.Printf("Apps information retrieved successfully for user: %s", userID)
		s.sendResponse(w, http.StatusOK, true, "Apps information retrieved successfully", res.response)
	}
}

// 3. Get rendered installation package for specific application (single app only)
func (s *Server) getAppPackage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	log.Printf("GET /api/v2/apps/%s/package - Getting rendered package for app", appID)

	// TODO: Implement business logic for getting rendered app package

	s.sendResponse(w, http.StatusOK, true, "App package retrieved successfully", nil)
}

// 4. Update specific application render configuration (single app only)
func (s *Server) updateAppConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	log.Printf("PUT /api/v2/apps/%s/config - Updating app config", appID)

	// TODO: Parse request body and implement business logic for updating app config

	s.sendResponse(w, http.StatusOK, true, "App config updated successfully", nil)
}

// 9. Upload application installation package
func (s *Server) uploadAppPackage(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/apps/upload - Uploading app package")

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for upload request: %s", userID)

	// Step 2: Check if cache manager is available
	if s.cacheManager == nil {
		log.Printf("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 3: Parse multipart form data
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		log.Printf("Failed to parse multipart form: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Failed to parse form data", nil)
		return
	}

	// Step 4: Get uploaded file
	file, header, err := r.FormFile("chart")
	if err != nil {
		log.Printf("Failed to get uploaded file: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "No chart file found in request", nil)
		return
	}
	defer file.Close()

	// Step 5: Validate file
	if header == nil {
		log.Printf("File header is nil")
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid file header", nil)
		return
	}

	// Check file size (max 100MB)
	if header.Size > 100<<20 {
		log.Printf("File too large: %d bytes", header.Size)
		s.sendResponse(w, http.StatusBadRequest, false, "File too large (max 100MB)", nil)
		return
	}

	// Check file extension
	filename := header.Filename
	if !strings.HasSuffix(strings.ToLower(filename), ".tgz") &&
		!strings.HasSuffix(strings.ToLower(filename), ".tar.gz") {
		log.Printf("Invalid file extension: %s", filename)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid file format (must be .tgz or .tar.gz)", nil)
		return
	}

	// Step 6: Read file content
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Failed to read file content: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to read file content", nil)
		return
	}

	// Step 7: Get source ID from form or use default
	sourceID := r.FormValue("source")
	if sourceID == "" {
		sourceID = "local" // Default source for uploaded packages
	}

	// Step 8: Get token for validation
	token := utils.GetTokenFromRequest(restfulReq)

	// Step 9: Process the uploaded package using LocalRepo
	localRepo := appinfo.NewLocalRepo(s.cacheManager)
	appInfo, err := localRepo.UploadAppPackage(userID, sourceID, fileBytes, filename, token)
	if err != nil {
		log.Printf("Failed to process uploaded package: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, fmt.Sprintf("Failed to process package: %v", err), nil)
		return
	}

	log.Printf("Successfully uploaded and processed chart package: %s for user: %s", filename, userID)

	// Prepare response with app information
	responseData := map[string]interface{}{
		"filename": filename,
		"source":   sourceID,
		"user_id":  userID,
		"app_info": map[string]interface{}{
			"id":           appInfo.ID,
			"app_id":       appInfo.AppID,
			"name":         appInfo.Name,
			"cfg_type":     appInfo.CfgType,
			"chart_name":   appInfo.ChartName,
			"icon":         appInfo.Icon,
			"description":  appInfo.Description,
			"title":        appInfo.Title,
			"version":      appInfo.Version,
			"categories":   appInfo.Categories,
			"version_name": appInfo.VersionName,
			"developer":    appInfo.Developer,
			"rating":       appInfo.Rating,
			"target":       appInfo.Target,
			"create_time":  appInfo.CreateTime,
			"update_time":  appInfo.UpdateTime,
		},
	}

	s.sendResponse(w, http.StatusOK, true, "App package uploaded and processed successfully", responseData)
}

// extractAppDataFromPending extracts app data from the new AppInfoLatestPendingData structure
func extractAppDataFromPending(pendingData *types.AppInfoLatestPendingData) map[string]interface{} {
	data := make(map[string]interface{})

	// Add basic pending data information
	data["type"] = string(pendingData.Type)
	data["timestamp"] = pendingData.Timestamp
	data["version"] = pendingData.Version

	// Add RawData information if available
	if pendingData.RawData != nil {
		data["raw_data"] = map[string]interface{}{
			"id":          pendingData.RawData.ID,
			"name":        pendingData.RawData.Name,
			"appID":       pendingData.RawData.AppID,
			"title":       pendingData.RawData.Title,
			"version":     pendingData.RawData.Version,
			"description": pendingData.RawData.Description,
			"icon":        pendingData.RawData.Icon,
			"categories":  pendingData.RawData.Categories,
			"developer":   pendingData.RawData.Developer,
		}
	}

	// Add package paths
	data["raw_package"] = pendingData.RawPackage
	data["rendered_package"] = pendingData.RenderedPackage

	// Add Values information if available
	if pendingData.Values != nil && len(pendingData.Values) > 0 {
		valuesData := make([]map[string]interface{}, 0, len(pendingData.Values))
		for _, value := range pendingData.Values {
			if value != nil {
				valueMap := map[string]interface{}{
					"file_name":    value.FileName,
					"modify_type":  string(value.ModifyType),
					"modify_key":   value.ModifyKey,
					"modify_value": value.ModifyValue,
				}
				valuesData = append(valuesData, valueMap)
			}
		}
		data["values"] = valuesData
	}

	// Add AppInfo information if available
	if pendingData.AppInfo != nil {
		appInfoData := make(map[string]interface{})

		if pendingData.AppInfo.AppEntry != nil {
			appInfoData["app_entry"] = pendingData.AppInfo.AppEntry
		}

		if pendingData.AppInfo.ImageAnalysis != nil {
			appInfoData["image_analysis"] = pendingData.AppInfo.ImageAnalysis
		}

		data["app_info"] = appInfoData
	}

	return data
}

// Get market hash information
func (s *Server) getMarketHash(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/market/hash - Getting market hash")

	// Add timeout context
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Convert http.Request to restful.Request to reuse utils functions
	restfulReq := s.httpToRestfulRequest(r)

	// Get user information from request using utils module
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for hash request: %s", userID)

	// Create a channel to receive the result
	type result struct {
		hash string
		err  error
	}
	resultChan := make(chan result, 1)

	// Run the data retrieval in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getMarketHash: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get user data from cache
		userData := s.cacheManager.GetUserData(userID)
		if userData == nil {
			log.Printf("User data not found for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("user data not found")}
			return
		}

		// Get hash from user data
		hash := userData.Hash
		log.Printf("Retrieved hash for user %s: %s", userID, hash)

		resultChan <- result{hash: hash}
	}()

	// Wait for result or timeout
	select {
	case <-ctx.Done():
		log.Printf("Request timeout or cancelled for /api/v2/market/hash")
		s.sendResponse(w, http.StatusRequestTimeout, false, "Request timeout - hash retrieval took too long", nil)
		return
	case res := <-resultChan:
		if res.err != nil {
			log.Printf("Error retrieving market hash: %v", res.err)
			if res.err.Error() == "user data not found" {
				s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
			} else {
				s.sendResponse(w, http.StatusInternalServerError, false, "Failed to retrieve market hash", nil)
			}
			return
		}

		// Return hash in response data
		hashData := map[string]string{
			"hash": res.hash,
		}

		log.Printf("Market hash retrieved successfully for user: %s", userID)
		s.sendResponse(w, http.StatusOK, true, "Market hash retrieved successfully", hashData)
	}
}

// Get market data information
func (s *Server) getMarketData(w http.ResponseWriter, r *http.Request) {
	requestStart := time.Now()
	log.Printf("GET /api/v2/market/data - Getting market data, request start: %v", requestStart)

	// Add timeout context
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Convert http.Request to restful.Request to reuse utils functions
	restfulReq := s.httpToRestfulRequest(r)

	// Get user information from request using utils module
	authStart := time.Now()
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("User authentication took %v, retrieved user ID: %s", time.Since(authStart), userID)

	// Create a channel to receive the result
	type result struct {
		data MarketDataResponse
		err  error
	}
	resultChan := make(chan result, 1)

	// Run the data retrieval in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getMarketData: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get user data from cache with timeout check
		start := time.Now()
		userData := s.cacheManager.GetUserData(userID)
		if userData == nil {
			log.Printf("User data not found for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("user data not found")}
			return
		}
		log.Printf("GetUserData took %v for user: %s", time.Since(start), userID)

		// Check if we're still within timeout before filtering
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled during user data retrieval for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("request cancelled")}
			return
		default:
		}

		// Filter the user data to include only required fields with timeout
		filterStart := time.Now()
		filteredUserData := s.filterUserDataWithTimeout(ctx, userData)
		if filteredUserData == nil {
			log.Printf("Data filtering timed out or failed for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("data filtering timeout")}
			return
		}
		log.Printf("Data filtering took %v for user: %s", time.Since(filterStart), userID)

		// Prepare response data
		responseData := MarketDataResponse{
			UserData:  filteredUserData,
			UserID:    userID,
			Timestamp: time.Now().Unix(),
		}

		resultChan <- result{data: responseData}
	}()

	// Wait for result or timeout
	select {
	case <-ctx.Done():
		log.Printf("Request timeout or cancelled for /api/v2/market/data")
		s.sendResponse(w, http.StatusRequestTimeout, false, "Request timeout - data retrieval took too long", nil)
		return
	case res := <-resultChan:
		if res.err != nil {
			log.Printf("Error retrieving market data: %v", res.err)
			if res.err.Error() == "user data not found" {
				s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
			} else {
				s.sendResponse(w, http.StatusInternalServerError, false, "Failed to retrieve market data", nil)
			}
			return
		}

		log.Printf("Market data retrieved successfully for user: %s", userID)
		s.sendResponse(w, http.StatusOK, true, "Market data retrieved successfully", res.data)
	}
}

// httpToRestfulRequest converts http.Request to restful.Request for utils compatibility
func (s *Server) httpToRestfulRequest(r *http.Request) *restful.Request {
	// Create a new restful.Request from the http.Request
	return &restful.Request{Request: r}
}

// filterUserData filters user data to include only required fields
func (s *Server) filterUserData(userData *types.UserData) *FilteredUserData {
	// This function has been replaced by filterUserDataWithTimeout using single global lock
	return s.filterUserDataWithTimeout(context.Background(), userData)
}

// filterSourceData filters source data to include only required fields
func (s *Server) filterSourceData(sourceData *types.SourceData) *FilteredSourceData {
	// This function has been replaced by convertSourceDataToFiltered using single global lock
	return s.convertSourceDataToFiltered(sourceData)
}

// filterUserDataWithTimeout filters user data to include only required fields with timeout
func (s *Server) filterUserDataWithTimeout(ctx context.Context, userData *types.UserData) *FilteredUserData {
	if userData == nil {
		return nil
	}

	log.Printf("DEBUG: Starting filterUserDataWithTimeout with single global lock approach")

	// Use single lock for all data access to avoid deadlocks
	filteredUserData := &FilteredUserData{
		Sources: make(map[string]*FilteredSourceData),
		Hash:    userData.Hash,
	}

	// Check timeout
	select {
	case <-ctx.Done():
		log.Printf("Context cancelled before data processing")
		return nil
	default:
	}

	// Process each source in the user data
	for sourceID, sourceData := range userData.Sources {
		// Check timeout
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled during source processing: %s", sourceID)
			return nil
		default:
		}

		// Convert data directly without additional locks
		filteredSourceData := s.convertSourceDataToFiltered(sourceData)
		if filteredSourceData != nil {
			filteredUserData.Sources[sourceID] = filteredSourceData
		}
	}

	log.Printf("DEBUG: Completed filterUserDataWithTimeout with %d sources processed", len(filteredUserData.Sources))
	return filteredUserData
}

// convertSourceDataToFiltered converts source data to filtered format
func (s *Server) convertSourceDataToFiltered(sourceData *types.SourceData) *FilteredSourceData {
	if sourceData == nil {
		return nil
	}

	var filteredOthers *types.Others
	if sourceData.Others != nil {
		filteredOthers = &types.Others{
			Hash:       sourceData.Others.Hash,
			Version:    sourceData.Others.Version,
			Topics:     sourceData.Others.Topics,
			TopicLists: sourceData.Others.TopicLists,
			Recommends: sourceData.Others.Recommends,
			Pages:      sourceData.Others.Pages,
			Tops:       sourceData.Others.Tops,
			Latest:     sourceData.Others.Latest,
		}
	}

	filteredSourceData := &FilteredSourceData{
		Type:           sourceData.Type,
		AppStateLatest: sourceData.AppStateLatest,
		Others:         filteredOthers,
		AppInfoLatest:  make([]*FilteredAppInfoLatestData, 0),
	}

	// Convert AppInfoLatest data
	if sourceData.AppInfoLatest != nil {
		for _, appInfoData := range sourceData.AppInfoLatest {
			if appInfoData != nil {
				filteredAppInfo := &FilteredAppInfoLatestData{
					Type:          appInfoData.Type,
					Timestamp:     appInfoData.Timestamp,
					Version:       appInfoData.Version,
					AppSimpleInfo: appInfoData.AppSimpleInfo,
				}
				filteredSourceData.AppInfoLatest = append(filteredSourceData.AppInfoLatest, filteredAppInfo)
			}
		}
	}

	return filteredSourceData
}

// 5. Diagnostic endpoint for cache and Redis analysis
func (s *Server) getDiagnostic(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/diagnostic - Getting cache and Redis diagnostic information")

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Perform diagnostic analysis
	err := s.cacheManager.DiagnoseCacheAndRedis()
	if err != nil {
		log.Printf("Diagnostic failed: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Diagnostic failed", nil)
		return
	}

	// Get cache stats
	cacheStats := s.cacheManager.GetCacheStats()

	// Get all users data for detailed analysis
	allUsersData := s.cacheManager.GetAllUsersData()

	diagnosticInfo := map[string]interface{}{
		"cache_stats":   cacheStats,
		"users_data":    allUsersData,
		"total_users":   len(allUsersData),
		"total_sources": cacheStats["total_sources"],
		"is_running":    cacheStats["is_running"],
	}

	log.Println("Diagnostic information retrieved successfully")
	s.sendResponse(w, http.StatusOK, true, "Diagnostic information retrieved successfully", diagnosticInfo)
}

// 6. Force reload from Redis endpoint
func (s *Server) forceReloadFromRedis(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/reload - Force reloading cache data from Redis")

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Force reload from Redis
	err := s.cacheManager.ForceReloadFromRedis()
	if err != nil {
		log.Printf("Force reload failed: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Force reload failed", nil)
		return
	}

	log.Println("Cache data reloaded successfully from Redis")
	s.sendResponse(w, http.StatusOK, true, "Cache data reloaded successfully from Redis", nil)
}

// 7. Cleanup invalid pending data endpoint
func (s *Server) cleanupInvalidPendingData(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/cleanup - Cleaning up invalid pending data")

	// Check if cache manager is available
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Cleanup invalid pending data
	cleanedCount := s.cacheManager.CleanupInvalidPendingData()

	log.Printf("Cleanup completed: removed %d invalid pending data entries", cleanedCount)
	s.sendResponse(w, http.StatusOK, true, "Cleanup completed successfully", map[string]interface{}{
		"cleaned_count": cleanedCount,
	})
}
