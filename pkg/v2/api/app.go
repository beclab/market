package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"

	"github.com/emicklei/go-restful/v3"
	"github.com/gorilla/mux"
)

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
	log.Println("GET /api/v2/apps - Getting apps information")

	// Get query parameters for filtering
	appIDs := r.URL.Query()["id"]
	log.Printf("Requested app IDs: %v", appIDs)

	// TODO: Implement business logic for getting apps information

	s.sendResponse(w, http.StatusOK, true, "Apps information retrieved successfully", nil)
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

	// TODO: Implement business logic for getting market hash
	// TODO: 实现获取市场哈希的业务逻辑

	s.sendResponse(w, http.StatusOK, true, "Market hash retrieved successfully", nil)
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
	if userData == nil {
		return nil
	}

	filteredUserData := &FilteredUserData{
		Sources: make(map[string]*FilteredSourceData),
	}

	// First, get the basic user data with minimal lock time
	// 首先，用最小锁时间获取基本用户数据
	userData.Mutex.RLock()
	hash := userData.Hash
	sourceIDs := make([]string, 0, len(userData.Sources))
	for sourceID := range userData.Sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	userData.Mutex.RUnlock()

	filteredUserData.Hash = hash

	// Process each source individually to avoid holding locks for too long
	// 单独处理每个源以避免长时间持有锁
	for _, sourceID := range sourceIDs {
		userData.Mutex.RLock()
		sourceData := userData.Sources[sourceID]
		userData.Mutex.RUnlock()

		if sourceData != nil {
			filteredSourceData := s.filterSourceData(sourceData)
			if filteredSourceData != nil {
				filteredUserData.Sources[sourceID] = filteredSourceData
			}
		}
	}

	return filteredUserData
}

// filterSourceData filters source data to include only required fields
// filterSourceData 过滤源数据，仅包含必需字段
func (s *Server) filterSourceData(sourceData *types.SourceData) *FilteredSourceData {
	if sourceData == nil {
		return nil
	}

	// Lock source data for reading
	// 锁定源数据进行读取
	sourceData.Mutex.RLock()
	defer sourceData.Mutex.RUnlock()

	filteredSourceData := &FilteredSourceData{
		Type:           sourceData.Type,
		AppStateLatest: sourceData.AppStateLatest,
		Others:         sourceData.Others,
	}

	// Filter AppInfoLatest to include only AppSimpleInfo
	// 过滤 AppInfoLatest，仅包含 AppSimpleInfo
	if sourceData.AppInfoLatest != nil {
		filteredAppInfoLatest := make([]*FilteredAppInfoLatestData, 0, len(sourceData.AppInfoLatest))

		for _, appInfoData := range sourceData.AppInfoLatest {
			if appInfoData != nil {
				filteredAppInfo := &FilteredAppInfoLatestData{
					Type:          appInfoData.Type,
					Timestamp:     appInfoData.Timestamp,
					Version:       appInfoData.Version,
					AppSimpleInfo: appInfoData.AppSimpleInfo,
				}
				filteredAppInfoLatest = append(filteredAppInfoLatest, filteredAppInfo)
			}
		}

		filteredSourceData.AppInfoLatest = filteredAppInfoLatest
	}

	return filteredSourceData
}

// filterUserDataWithTimeout filters user data to include only required fields with timeout
// filterUserDataWithTimeout 带超时过滤用户数据，仅包含必需字段
func (s *Server) filterUserDataWithTimeout(ctx context.Context, userData *types.UserData) *FilteredUserData {
	if userData == nil {
		return nil
	}

	// First, get the basic user data with minimal lock time
	// 首先，用最小锁时间获取基本用户数据
	userData.Mutex.RLock()
	hash := userData.Hash
	sourceIDs := make([]string, 0, len(userData.Sources))
	for sourceID := range userData.Sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	userData.Mutex.RUnlock()

	// Check if we're still within timeout before filtering
	// 在过滤前检查是否仍在超时范围内
	select {
	case <-ctx.Done():
		log.Printf("Context cancelled during user data retrieval for user: %s", hash)
		return nil
	default:
	}

	filteredUserData := &FilteredUserData{
		Sources: make(map[string]*FilteredSourceData),
		Hash:    hash,
	}

	// Process each source individually to avoid holding locks for too long
	// 单独处理每个源以避免长时间持有锁
	for _, sourceID := range sourceIDs {
		userData.Mutex.RLock()
		sourceData := userData.Sources[sourceID]
		userData.Mutex.RUnlock()

		if sourceData != nil {
			filteredSourceData := s.filterSourceData(sourceData)
			if filteredSourceData != nil {
				filteredUserData.Sources[sourceID] = filteredSourceData
			}
		}
	}

	return filteredUserData
}
