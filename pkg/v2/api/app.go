package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"

	"github.com/emicklei/go-restful/v3"
	"github.com/gorilla/mux"
)

// AppsInfoRequest represents the request body for /api/v2/apps
// AppsInfoRequest 表示 /api/v2/apps 接口的请求体
type AppsInfoRequest struct {
	Apps []AppQueryInfo `json:"apps"`
}

// AppQueryInfo represents individual app query information
// AppQueryInfo 表示单个应用查询信息
type AppQueryInfo struct {
	AppID          string `json:"appid"`
	SourceDataName string `json:"sourceDataName"`
}

// AppInfoLatestDataResponse represents AppInfoLatestData for API response without raw_data
// AppInfoLatestDataResponse 表示API响应应用的AppInfoLatestData，不包含raw_data字段
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
// AppsInfoResponse 表示 /api/v2/apps 接口的响应
type AppsInfoResponse struct {
	Apps       []*AppInfoLatestDataResponse `json:"apps"`
	TotalCount int                          `json:"total_count"`
	NotFound   []AppQueryInfo               `json:"not_found,omitempty"`
}

// MarketInfoResponse represents the response structure for market information
// 市场信息响应结构
type MarketInfoResponse struct {
	UsersData    map[string]*types.UserData `json:"users_data"`
	CacheStats   map[string]interface{}     `json:"cache_stats"`
	TotalUsers   int                        `json:"total_users"`
	TotalSources int                        `json:"total_sources"`
}

// FilteredSourceData 表示过滤后的源数据
// FilteredSourceData represents filtered source data
type FilteredSourceData struct {
	Type           types.SourceDataType         `json:"type"`
	AppStateLatest []*types.AppStateLatestData  `json:"app_state_latest"`
	AppInfoLatest  []*FilteredAppInfoLatestData `json:"app_info_latest"`
	Others         *types.Others                `json:"others,omitempty"`
}

// FilteredAppInfoLatestData contains only AppSimpleInfo from AppInfoLatestData
// FilteredAppInfoLatestData 仅包含 AppInfoLatestData 中的 AppSimpleInfo
type FilteredAppInfoLatestData struct {
	Type          types.AppDataType    `json:"type"`
	Timestamp     int64                `json:"timestamp"`
	Version       string               `json:"version,omitempty"`
	AppSimpleInfo *types.AppSimpleInfo `json:"app_simple_info"`
}

// FilteredUserData 表示过滤后的用户数据
// FilteredUserData represents filtered user data
type FilteredUserData struct {
	Sources map[string]*FilteredSourceData `json:"sources"`
	Hash    string                         `json:"hash"`
}

// MarketDataResponse 表示市场数据响应结构
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
	// 添加超时上下文
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	// 检查缓存管理器是否可用
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Create a channel to receive the result
	// 创建通道接收结果
	type result struct {
		data  MarketInfoResponse
		stats map[string]interface{}
		err   error
	}
	resultChan := make(chan result, 1)

	// Run the data retrieval in a goroutine
	// 在协程中运行数据检索
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getMarketInfo: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get all users data from cache
		// 从缓存获取所有用户数据
		allUsersData := s.cacheManager.GetAllUsersData()
		if allUsersData == nil {
			log.Println("Warning: GetAllUsersData returned nil")
			allUsersData = make(map[string]*types.UserData)
		}

		// Get cache statistics
		// 获取缓存统计信息
		cacheStats := s.cacheManager.GetCacheStats()
		if cacheStats == nil {
			log.Println("Warning: GetCacheStats returned nil")
			cacheStats = make(map[string]interface{})
		}

		// Prepare response data
		// 准备响应数据
		responseData := MarketInfoResponse{
			UsersData:  allUsersData,
			CacheStats: cacheStats,
			TotalUsers: len(allUsersData),
		}

		resultChan <- result{data: responseData, stats: cacheStats}
	}()

	// Wait for result or timeout
	// 等待结果或超时
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
	// 添加超时上下文
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	// 检查缓存管理器是否可用
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 1: Get user information from request
	// 步骤1：从请求中获取用户信息
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for apps request: %s", userID)

	// Step 2: Parse JSON request body
	// 步骤2：解析JSON请求体
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
	// 创建通道接收结果
	type result struct {
		response AppsInfoResponse
		err      error
	}
	resultChan := make(chan result, 1)

	// Step 3: Process apps information retrieval in goroutine
	// 步骤3：在goroutine中处理应用信息检索
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getAppsInfo: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get user data from cache
		// 从缓存获取用户数据
		userData := s.cacheManager.GetUserData(userID)
		if userData == nil {
			log.Printf("User data not found for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("user data not found")}
			return
		}

		var foundApps []*AppInfoLatestDataResponse
		var notFoundApps []AppQueryInfo

		// Step 4: Find AppInfoLatestData for each requested app
		// 步骤4：为每个请求的应用查找AppInfoLatestData
		for _, appQuery := range request.Apps {
			log.Printf("Searching for app: %s in source: %s", appQuery.AppID, appQuery.SourceDataName)

			// Check if source exists
			// 检查源是否存在
			sourceData, exists := userData.Sources[appQuery.SourceDataName]
			if !exists {
				log.Printf("Source data not found: %s for user: %s", appQuery.SourceDataName, userID)
				notFoundApps = append(notFoundApps, appQuery)
				continue
			}

			// Search for app in AppInfoLatest array
			// 在AppInfoLatest数组中搜索应用
			found := false
			for _, appInfoData := range sourceData.AppInfoLatest {
				if appInfoData == nil {
					continue
				}

				// Check multiple possible ID fields for matching
				// 检查多个可能的ID字段进行匹配
				var appID string
				if appInfoData.RawData != nil {
					// Priority: ID > AppID > Name
					// 优先级：ID > AppID > Name
					if appInfoData.RawData.ID != "" {
						appID = appInfoData.RawData.ID
					} else if appInfoData.RawData.AppID != "" {
						appID = appInfoData.RawData.AppID
					} else if appInfoData.RawData.Name != "" {
						appID = appInfoData.RawData.Name
					}
				}

				// Also check AppSimpleInfo if available
				// 如果可用，也检查AppSimpleInfo
				if appID == "" && appInfoData.AppSimpleInfo != nil {
					appID = appInfoData.AppSimpleInfo.AppID
				}

				// Match the requested app ID
				// 匹配请求的应用ID
				if appID == appQuery.AppID {
					log.Printf("Found app: %s in source: %s", appQuery.AppID, appQuery.SourceDataName)

					// Convert AppInfoLatestData to AppInfoLatestDataResponse (without raw_data)
					// 将AppInfoLatestData转换为AppInfoLatestDataResponse（不包含raw_data）
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
		// 步骤5：准备响应
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
	// 等待结果或超时
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

	// TODO: Implement business logic for uploading app package
	// Handle multipart form data for file upload

	s.sendResponse(w, http.StatusOK, true, "App package uploaded successfully", nil)
}

// extractAppDataFromPending extracts app data from the new AppInfoLatestPendingData structure
// extractAppDataFromPending 从新的AppInfoLatestPendingData结构中提取应用数据
func extractAppDataFromPending(pendingData *types.AppInfoLatestPendingData) map[string]interface{} {
	data := make(map[string]interface{})

	// Add basic pending data information
	// 添加基本的待处理数据信息
	data["type"] = string(pendingData.Type)
	data["timestamp"] = pendingData.Timestamp
	data["version"] = pendingData.Version

	// Add RawData information if available
	// 如果可用，添加RawData信息
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
	// 添加包路径
	data["raw_package"] = pendingData.RawPackage
	data["rendered_package"] = pendingData.RenderedPackage

	// Add Values information if available
	// 如果可用，添加Values信息
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
	// 如果可用，添加AppInfo信息
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
// 获取市场哈希信息
func (s *Server) getMarketHash(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/market/hash - Getting market hash")

	// Add timeout context
	// 添加超时上下文
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	// 检查缓存管理器是否可用
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Convert http.Request to restful.Request to reuse utils functions
	// 将 http.Request 转换为 restful.Request 以复用 utils 函数
	restfulReq := s.httpToRestfulRequest(r)

	// Get user information from request using utils module
	// 使用 utils 模块从请求中获取用户信息
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for hash request: %s", userID)

	// Create a channel to receive the result
	// 创建通道接收结果
	type result struct {
		hash string
		err  error
	}
	resultChan := make(chan result, 1)

	// Run the data retrieval in a goroutine
	// 在协程中运行数据检索
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getMarketHash: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get user data from cache
		// 从缓存获取用户数据
		userData := s.cacheManager.GetUserData(userID)
		if userData == nil {
			log.Printf("User data not found for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("user data not found")}
			return
		}

		// Get hash from user data
		// 从用户数据中获取哈希值
		hash := userData.Hash
		log.Printf("Retrieved hash for user %s: %s", userID, hash)

		resultChan <- result{hash: hash}
	}()

	// Wait for result or timeout
	// 等待结果或超时
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
		// 在响应数据中返回哈希值
		hashData := map[string]string{
			"hash": res.hash,
		}

		log.Printf("Market hash retrieved successfully for user: %s", userID)
		s.sendResponse(w, http.StatusOK, true, "Market hash retrieved successfully", hashData)
	}
}

// Get market data information
// 获取市场数据信息
func (s *Server) getMarketData(w http.ResponseWriter, r *http.Request) {
	requestStart := time.Now()
	log.Printf("GET /api/v2/market/data - Getting market data, request start: %v", requestStart)

	// Add timeout context
	// 添加超时上下文
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Check if cache manager is available
	// 检查缓存管理器是否可用
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Convert http.Request to restful.Request to reuse utils functions
	// 将 http.Request 转换为 restful.Request 以复用 utils 函数
	restfulReq := s.httpToRestfulRequest(r)

	// Get user information from request using utils module
	// 使用 utils 模块从请求中获取用户信息
	authStart := time.Now()
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("User authentication took %v, retrieved user ID: %s", time.Since(authStart), userID)

	// Create a channel to receive the result
	// 创建通道接收结果
	type result struct {
		data MarketDataResponse
		err  error
	}
	resultChan := make(chan result, 1)

	// Run the data retrieval in a goroutine
	// 在协程中运行数据检索
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in getMarketData: %v", r)
				resultChan <- result{err: fmt.Errorf("internal error occurred")}
			}
		}()

		// Get user data from cache with timeout check
		// 带超时检查从缓存获取用户数据
		start := time.Now()
		userData := s.cacheManager.GetUserData(userID)
		if userData == nil {
			log.Printf("User data not found for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("user data not found")}
			return
		}
		log.Printf("GetUserData took %v for user: %s", time.Since(start), userID)

		// Check if we're still within timeout before filtering
		// 在过滤前检查是否仍在超时范围内
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled during user data retrieval for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("request cancelled")}
			return
		default:
		}

		// Filter the user data to include only required fields with timeout
		// 带超时过滤用户数据，仅包含必需字段
		filterStart := time.Now()
		filteredUserData := s.filterUserDataWithTimeout(ctx, userData)
		if filteredUserData == nil {
			log.Printf("Data filtering timed out or failed for user: %s", userID)
			resultChan <- result{err: fmt.Errorf("data filtering timeout")}
			return
		}
		log.Printf("Data filtering took %v for user: %s", time.Since(filterStart), userID)

		// Prepare response data
		// 准备响应数据
		responseData := MarketDataResponse{
			UserData:  filteredUserData,
			UserID:    userID,
			Timestamp: time.Now().Unix(),
		}

		resultChan <- result{data: responseData}
	}()

	// Wait for result or timeout
	// 等待结果或超时
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
// httpToRestfulRequest 将 http.Request 转换为 restful.Request 以与 utils 兼容
func (s *Server) httpToRestfulRequest(r *http.Request) *restful.Request {
	// Create a new restful.Request from the http.Request
	// 从 http.Request 创建新的 restful.Request
	return &restful.Request{Request: r}
}

// filterUserData filters user data to include only required fields
// filterUserData 过滤用户数据，仅包含必需字段
func (s *Server) filterUserData(userData *types.UserData) *FilteredUserData {
	// 这个函数已被 filterUserDataWithTimeout 替代，使用单一全局锁
	// This function has been replaced by filterUserDataWithTimeout using single global lock
	return s.filterUserDataWithTimeout(context.Background(), userData)
}

// filterSourceData filters source data to include only required fields
// filterSourceData 过滤源数据，仅包含必需字段
func (s *Server) filterSourceData(sourceData *types.SourceData) *FilteredSourceData {
	// 这个函数已被 convertSourceDataToFiltered 替代，使用单一全局锁
	// This function has been replaced by convertSourceDataToFiltered using single global lock
	return s.convertSourceDataToFiltered(sourceData)
}

// filterUserDataWithTimeout filters user data to include only required fields with timeout
// filterUserDataWithTimeout 带超时过滤用户数据，仅包含必需字段
func (s *Server) filterUserDataWithTimeout(ctx context.Context, userData *types.UserData) *FilteredUserData {
	if userData == nil {
		return nil
	}

	log.Printf("DEBUG: Starting filterUserDataWithTimeout with single global lock approach")

	// 使用单一锁进行所有数据访问，避免死锁
	// Use single lock for all data access to avoid deadlocks
	filteredUserData := &FilteredUserData{
		Sources: make(map[string]*FilteredSourceData),
	}

	// 检查超时
	// Check timeout
	select {
	case <-ctx.Done():
		log.Printf("Context cancelled before data processing")
		return nil
	default:
	}

	// 步骤1：一次性获取所有需要的数据（使用全局锁）
	// Step 1: Get all needed data in one go (using global lock)
	var (
		hash      string
		sources   map[string]*FilteredSourceData
		sourceIDs []string
	)

	// 通过CacheManager获取数据，它会处理全局锁
	// Get data through CacheManager which handles the global lock
	if s.cacheManager != nil {
		// 获取用户数据的快照
		// Get snapshot of user data
		allUsersData := s.cacheManager.GetAllUsersData()
		if userDataSnapshot, exists := allUsersData[s.getCurrentUserID()]; exists {
			hash = userDataSnapshot.Hash
			sources = make(map[string]*FilteredSourceData)

			for sourceID, sourceData := range userDataSnapshot.Sources {
				sourceIDs = append(sourceIDs, sourceID)

				// 检查超时
				// Check timeout
				select {
				case <-ctx.Done():
					log.Printf("Context cancelled during source processing: %s", sourceID)
					return nil
				default:
				}

				// 直接转换数据，无需额外锁
				// Convert data directly without additional locks
				filteredSourceData := s.convertSourceDataToFiltered(sourceData)
				if filteredSourceData != nil {
					sources[sourceID] = filteredSourceData
				}
			}
		}
	}

	filteredUserData.Hash = hash
	filteredUserData.Sources = sources

	log.Printf("DEBUG: Completed filterUserDataWithTimeout with %d sources processed", len(sources))
	return filteredUserData
}

// getCurrentUserID 获取当前用户ID（简化版本）
// getCurrentUserID gets current user ID (simplified version)
func (s *Server) getCurrentUserID() string {
	// 在实际实现中，这应该从请求上下文中获取
	// In actual implementation, this should be obtained from request context
	return "admin" // 临时硬编码，实际应该从认证信息获取
}

// convertSourceDataToFiltered 转换源数据为过滤后的格式
// convertSourceDataToFiltered converts source data to filtered format
func (s *Server) convertSourceDataToFiltered(sourceData *types.SourceData) *FilteredSourceData {
	if sourceData == nil {
		return nil
	}

	filteredSourceData := &FilteredSourceData{
		Type:           sourceData.Type,
		AppStateLatest: sourceData.AppStateLatest,
		Others:         sourceData.Others,
		AppInfoLatest:  make([]*FilteredAppInfoLatestData, 0),
	}

	// 转换AppInfoLatest数据
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
