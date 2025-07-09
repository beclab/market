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
	Apps       []map[string]interface{} `json:"apps"`
	TotalCount int                      `json:"total_count"`
	NotFound   []AppQueryInfo           `json:"not_found,omitempty"`
}

// MarketInfoResponse represents the response structure for market information
type MarketInfoResponse struct {
	UsersData    interface{}            `json:"users_data"`
	CacheStats   map[string]interface{} `json:"cache_stats"`
	TotalUsers   int                    `json:"total_users"`
	TotalSources int                    `json:"total_sources"`
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

		// Create safe copies to avoid circular references
		safeUsersData := s.createSafeUsersDataCopy(allUsersData)

		// Prepare response data
		responseData := MarketInfoResponse{
			UsersData:  safeUsersData,
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

		log.Printf("Market information retrieved successfully: %d users", res.data.TotalUsers)
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
		response map[string]interface{}
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

		var foundApps []map[string]interface{}
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

					// 使用 safe copy，避免循环引用
					safeCopy := s.createSafeAppInfoLatestCopy(appInfoData)
					foundApps = append(foundApps, safeCopy)
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
		response := map[string]interface{}{
			"apps":        foundApps,
			"total_count": len(foundApps),
		}
		if len(notFoundApps) > 0 {
			response["not_found"] = notFoundApps
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

		// Add debug logging to check response data
		if apps, ok := res.response["apps"]; ok {
			log.Printf("DEBUG: - Total apps found: %d", len(apps.([]map[string]interface{})))
		}
		if total, ok := res.response["total_count"]; ok {
			log.Printf("DEBUG: - Total count: %d", total)
		}
		if notFound, ok := res.response["not_found"]; ok {
			log.Printf("DEBUG: - Not found count: %d", len(notFound.([]AppQueryInfo)))
		}

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
	if s.hydrator != nil {
		localRepo.SetHydrator(s.hydrator)
	}
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

// createSafeUsersDataCopy creates a safe copy of users data to avoid circular references
func (s *Server) createSafeUsersDataCopy(usersData map[string]*types.UserData) map[string]interface{} {
	if usersData == nil {
		return nil
	}

	safeUsersData := make(map[string]interface{})
	for userID, userData := range usersData {
		if userData == nil {
			continue
		}

		safeUserData := map[string]interface{}{
			"hash": userData.Hash,
		}

		// Create safe copies of sources
		if userData.Sources != nil {
			safeSources := make(map[string]interface{})
			for sourceID, sourceData := range userData.Sources {
				if sourceData == nil {
					continue
				}
				safeSources[sourceID] = s.createSafeSourceDataCopy(sourceData)
			}
			safeUserData["sources"] = safeSources
		}

		safeUsersData[userID] = safeUserData
	}

	return safeUsersData
}

// createSafeSourceDataCopy creates a safe copy of source data to avoid circular references
func (s *Server) createSafeSourceDataCopy(sourceData *types.SourceData) map[string]interface{} {
	if sourceData == nil {
		return nil
	}

	safeSourceData := map[string]interface{}{
		"type": sourceData.Type,
	}

	// Create safe copies of AppStateLatest
	if len(sourceData.AppStateLatest) > 0 {
		safeAppStateLatest := make([]map[string]interface{}, len(sourceData.AppStateLatest))
		for i, data := range sourceData.AppStateLatest {
			safeAppStateLatest[i] = s.createSafeAppStateLatestCopy(data)
		}
		safeSourceData["app_state_latest"] = safeAppStateLatest
	}

	// Create safe copies of AppInfoLatest
	if len(sourceData.AppInfoLatest) > 0 {
		safeAppInfoLatest := make([]map[string]interface{}, len(sourceData.AppInfoLatest))
		for i, data := range sourceData.AppInfoLatest {
			safeAppInfoLatest[i] = s.createSafeAppInfoLatestCopy(data)
		}
		safeSourceData["app_info_latest"] = safeAppInfoLatest
	}

	// Create safe copies of AppInfoLatestPending
	if len(sourceData.AppInfoLatestPending) > 0 {
		safeAppInfoLatestPending := make([]map[string]interface{}, len(sourceData.AppInfoLatestPending))
		for i, data := range sourceData.AppInfoLatestPending {
			safeAppInfoLatestPending[i] = s.createSafePendingDataCopy(data)
		}
		safeSourceData["app_info_latest_pending"] = safeAppInfoLatestPending
	}

	// Create safe copies of Others data
	if sourceData.Others != nil {
		safeSourceData["others"] = s.createSafeOthersCopy(sourceData.Others)
	}

	return safeSourceData
}

// createSafeAppStateLatestCopy creates a safe copy of AppStateLatestData to avoid circular references
func (s *Server) createSafeAppStateLatestCopy(data *types.AppStateLatestData) map[string]interface{} {
	if data == nil {
		return nil
	}

	return map[string]interface{}{
		"type":    data.Type,
		"version": data.Version,
		"status": map[string]interface{}{
			"name":               data.Status.Name,
			"state":              data.Status.State,
			"updateTime":         data.Status.UpdateTime,
			"statusTime":         data.Status.StatusTime,
			"lastTransitionTime": data.Status.LastTransitionTime,
			"progress":           data.Status.Progress,
			"entranceStatuses":   data.Status.EntranceStatuses,
		},
	}
}

// createSafeAppInfoLatestCopy creates a safe copy of AppInfoLatestData to avoid circular references
func (s *Server) createSafeAppInfoLatestCopy(data *types.AppInfoLatestData) map[string]interface{} {
	if data == nil {
		return nil
	}

	safeCopy := map[string]interface{}{
		"type":             data.Type,
		"timestamp":        data.Timestamp,
		"version":          data.Version,
		"raw_package":      data.RawPackage,
		"rendered_package": data.RenderedPackage,
	}

	// Only include basic information from RawData to avoid cycles
	if data.RawData != nil {
		safeCopy["raw_data"] = s.createSafeApplicationInfoEntryCopy(data.RawData)
	}

	// Only include basic information from AppInfo to avoid cycles
	if data.AppInfo != nil {
		safeCopy["app_info"] = s.createSafeAppInfoCopy(data.AppInfo)
	}

	// Include Values if they exist
	if data.Values != nil {
		safeCopy["values"] = data.Values
	}

	// Include AppSimpleInfo if it exists
	if data.AppSimpleInfo != nil {
		safeCopy["app_simple_info"] = s.createSafeAppSimpleInfoCopy(data.AppSimpleInfo)
	}

	return safeCopy
}

// createSafePendingDataCopy creates a safe copy of pending data to avoid circular references
func (s *Server) createSafePendingDataCopy(pendingData *types.AppInfoLatestPendingData) map[string]interface{} {
	if pendingData == nil {
		return nil
	}

	safeCopy := map[string]interface{}{
		"type":             pendingData.Type,
		"timestamp":        pendingData.Timestamp,
		"version":          pendingData.Version,
		"raw_package":      pendingData.RawPackage,
		"rendered_package": pendingData.RenderedPackage,
	}

	// Only include basic information from RawData to avoid cycles
	if pendingData.RawData != nil {
		safeCopy["raw_data"] = s.createSafeApplicationInfoEntryCopy(pendingData.RawData)
	}

	// Only include basic information from AppInfo to avoid cycles
	if pendingData.AppInfo != nil {
		safeCopy["app_info"] = s.createSafeAppInfoCopy(pendingData.AppInfo)
	}

	// Include Values if they exist
	if pendingData.Values != nil {
		safeCopy["values"] = pendingData.Values
	}

	return safeCopy
}

// createSafeApplicationInfoEntryCopy creates a safe copy of ApplicationInfoEntry to avoid circular references
func (s *Server) createSafeApplicationInfoEntryCopy(entry *types.ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		return nil
	}

	return map[string]interface{}{
		"id":                 entry.ID,
		"name":               entry.Name,
		"cfgType":            entry.CfgType,
		"chartName":          entry.ChartName,
		"icon":               entry.Icon,
		"description":        entry.Description,
		"appID":              entry.AppID,
		"title":              entry.Title,
		"version":            entry.Version,
		"categories":         entry.Categories,
		"versionName":        entry.VersionName,
		"fullDescription":    entry.FullDescription,
		"upgradeDescription": entry.UpgradeDescription,
		"promoteImage":       entry.PromoteImage,
		"promoteVideo":       entry.PromoteVideo,
		"subCategory":        entry.SubCategory,
		"locale":             entry.Locale,
		"developer":          entry.Developer,
		"requiredMemory":     entry.RequiredMemory,
		"requiredDisk":       entry.RequiredDisk,
		"supportArch":        entry.SupportArch,
		"requiredGPU":        entry.RequiredGPU,
		"requiredCPU":        entry.RequiredCPU,
		"rating":             entry.Rating,
		"target":             entry.Target,
		"submitter":          entry.Submitter,
		"doc":                entry.Doc,
		"website":            entry.Website,
		"featuredImage":      entry.FeaturedImage,
		"sourceCode":         entry.SourceCode,
		"modelSize":          entry.ModelSize,
		"namespace":          entry.Namespace,
		"onlyAdmin":          entry.OnlyAdmin,
		"lastCommitHash":     entry.LastCommitHash,
		"createTime":         entry.CreateTime,
		"updateTime":         entry.UpdateTime,
		"appLabels":          entry.AppLabels,
		"screenshots":        entry.Screenshots,
		"tags":               entry.Tags,
		"updated_at":         entry.UpdatedAt,
		// Skip complex interface{} fields that might cause cycles
		// "supportClient", "permission", "entrances", "middleware", "license", "legal", "i18n", "count", "versionHistory", "metadata"
		"options": entry.Options,
	}
}

// createSafeAppInfoCopy creates a safe copy of AppInfo to avoid circular references
func (s *Server) createSafeAppInfoCopy(appInfo *types.AppInfo) map[string]interface{} {
	if appInfo == nil {
		return nil
	}

	safeCopy := map[string]interface{}{}

	// Only include basic information from AppEntry to avoid cycles
	if appInfo.AppEntry != nil {
		safeCopy["app_entry"] = s.createSafeApplicationInfoEntryCopy(appInfo.AppEntry)
	}

	// Deep copy ImageAnalysis, including Images map
	if appInfo.ImageAnalysis != nil {
		imageAnalysisCopy := map[string]interface{}{
			"app_id":       appInfo.ImageAnalysis.AppID,
			"user_id":      appInfo.ImageAnalysis.UserID,
			"source_id":    appInfo.ImageAnalysis.SourceID,
			"analyzed_at":  appInfo.ImageAnalysis.AnalyzedAt,
			"total_images": appInfo.ImageAnalysis.TotalImages,
		}
		// Deep copy Images map
		if appInfo.ImageAnalysis.Images != nil {
			imagesMap := make(map[string]interface{}, len(appInfo.ImageAnalysis.Images))
			for k, v := range appInfo.ImageAnalysis.Images {
				if v != nil {
					imagesMap[k] = s.createSafeImageInfoCopy(v)
				}
			}
			imageAnalysisCopy["images"] = imagesMap
		}
		safeCopy["image_analysis"] = imageAnalysisCopy
	}

	return safeCopy
}

// createSafeImageInfoCopy creates a safe copy of ImageInfo to avoid circular references
func (s *Server) createSafeImageInfoCopy(info *types.ImageInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	copy := map[string]interface{}{
		"name":              info.Name,
		"tag":               info.Tag,
		"architecture":      info.Architecture,
		"total_size":        info.TotalSize,
		"downloaded_size":   info.DownloadedSize,
		"download_progress": info.DownloadProgress,
		"layer_count":       info.LayerCount,
		"downloaded_layers": info.DownloadedLayers,
		"created_at":        info.CreatedAt,
		"analyzed_at":       info.AnalyzedAt,
		"status":            info.Status,
		"error_message":     info.ErrorMessage,
	}
	// Deep copy Nodes
	if info.Nodes != nil {
		nodesArr := make([]map[string]interface{}, 0, len(info.Nodes))
		for _, n := range info.Nodes {
			if n != nil {
				nodesArr = append(nodesArr, s.createSafeNodeInfoCopy(n))
			}
		}
		copy["nodes"] = nodesArr
	}
	return copy
}

// createSafeNodeInfoCopy creates a safe copy of NodeInfo to avoid circular references
func (s *Server) createSafeNodeInfoCopy(node *types.NodeInfo) map[string]interface{} {
	if node == nil {
		return nil
	}
	copy := map[string]interface{}{
		"node_name":         node.NodeName,
		"architecture":      node.Architecture,
		"variant":           node.Variant,
		"os":                node.OS,
		"downloaded_size":   node.DownloadedSize,
		"downloaded_layers": node.DownloadedLayers,
		"total_size":        node.TotalSize,
		"layer_count":       node.LayerCount,
	}
	// Deep copy Layers
	if node.Layers != nil {
		layersArr := make([]map[string]interface{}, 0, len(node.Layers))
		for _, l := range node.Layers {
			if l != nil {
				layersArr = append(layersArr, s.createSafeLayerInfoCopy(l))
			}
		}
		copy["layers"] = layersArr
	}
	return copy
}

// createSafeLayerInfoCopy creates a safe copy of LayerInfo to avoid circular references
func (s *Server) createSafeLayerInfoCopy(layer *types.LayerInfo) map[string]interface{} {
	if layer == nil {
		return nil
	}
	return map[string]interface{}{
		"digest":     layer.Digest,
		"size":       layer.Size,
		"media_type": layer.MediaType,
		"offset":     layer.Offset,
		"downloaded": layer.Downloaded,
		"progress":   layer.Progress,
		"local_path": layer.LocalPath,
	}
}

// createSafeAppSimpleInfoCopy creates a safe copy of AppSimpleInfo to avoid circular references
func (s *Server) createSafeAppSimpleInfoCopy(info *types.AppSimpleInfo) map[string]interface{} {
	if info == nil {
		return nil
	}

	return map[string]interface{}{
		"app_id":          info.AppID,
		"app_name":        info.AppName,
		"app_icon":        info.AppIcon,
		"app_description": info.AppDescription,
		"app_version":     info.AppVersion,
		"app_title":       info.AppTitle,
		"categories":      info.Categories,
	}
}

// createSafeOthersCopy creates a safe copy of Others data to avoid circular references
func (s *Server) createSafeOthersCopy(others *types.Others) map[string]interface{} {
	if others == nil {
		return nil
	}

	safeCopy := map[string]interface{}{
		"hash":    others.Hash,
		"version": others.Version,
		"latest":  others.Latest,
	}

	// Create safe copies of Topics
	if len(others.Topics) > 0 {
		safeTopics := make([]map[string]interface{}, len(others.Topics))
		for i, topic := range others.Topics {
			safeTopics[i] = s.createSafeTopicCopy(topic)
		}
		safeCopy["topics"] = safeTopics
	}

	// Create safe copies of TopicLists
	if len(others.TopicLists) > 0 {
		safeTopicLists := make([]map[string]interface{}, len(others.TopicLists))
		for i, topicList := range others.TopicLists {
			safeTopicLists[i] = s.createSafeTopicListCopy(topicList)
		}
		safeCopy["topic_lists"] = safeTopicLists
	}

	// Create safe copies of Recommends
	if len(others.Recommends) > 0 {
		safeRecommends := make([]map[string]interface{}, len(others.Recommends))
		for i, recommend := range others.Recommends {
			safeRecommends[i] = s.createSafeRecommendCopy(recommend)
		}
		safeCopy["recommends"] = safeRecommends
	}

	// Create safe copies of Pages
	if len(others.Pages) > 0 {
		safePages := make([]map[string]interface{}, len(others.Pages))
		for i, page := range others.Pages {
			safePages[i] = s.createSafePageCopy(page)
		}
		safeCopy["pages"] = safePages
	}

	// Create safe copies of Tops
	if len(others.Tops) > 0 {
		safeTops := make([]map[string]interface{}, len(others.Tops))
		for i, top := range others.Tops {
			safeTops[i] = map[string]interface{}{
				"appid": top.AppID,
				"rank":  top.Rank,
			}
		}
		safeCopy["tops"] = safeTops
	}

	return safeCopy
}

// createSafeTopicCopy creates a safe copy of Topic to avoid circular references
func (s *Server) createSafeTopicCopy(topic *types.Topic) map[string]interface{} {
	if topic == nil {
		return nil
	}

	return map[string]interface{}{
		"_id":        topic.ID,
		"name":       topic.Name,
		"data":       topic.Data,
		"source":     topic.Source,
		"createdAt":  topic.CreatedAt,
		"updated_at": topic.UpdatedAt,
	}
}

// createSafeTopicListCopy creates a safe copy of TopicList to avoid circular references
func (s *Server) createSafeTopicListCopy(topicList *types.TopicList) map[string]interface{} {
	if topicList == nil {
		return nil
	}

	return map[string]interface{}{
		"name":        topicList.Name,
		"type":        topicList.Type,
		"description": topicList.Description,
		"content":     topicList.Content,
		"title":       topicList.Title,
		"source":      topicList.Source,
		"createdAt":   topicList.CreatedAt,
		"updated_at":  topicList.UpdatedAt,
	}
}

// createSafeRecommendCopy creates a safe copy of Recommend to avoid circular references
func (s *Server) createSafeRecommendCopy(recommend *types.Recommend) map[string]interface{} {
	if recommend == nil {
		return nil
	}

	copy := map[string]interface{}{
		"name":        recommend.Name,
		"description": recommend.Description,
		"content":     recommend.Content,
		"createdAt":   recommend.CreatedAt,
		"updated_at":  recommend.UpdatedAt,
	}

	// Add I18n field if present
	if recommend.I18n != nil {
		copy["i18n"] = map[string]interface{}{
			"title":       recommend.I18n.Title,
			"description": recommend.I18n.Description,
		}
	}

	// Add Source field if present
	if recommend.Source != "" {
		copy["source"] = recommend.Source
	}

	return copy
}

// createSafePageCopy creates a safe copy of Page to avoid circular references
func (s *Server) createSafePageCopy(page *types.Page) map[string]interface{} {
	if page == nil {
		return nil
	}

	return map[string]interface{}{
		"category":   page.Category,
		"content":    page.Content,
		"createdAt":  page.CreatedAt,
		"updated_at": page.UpdatedAt,
	}
}
