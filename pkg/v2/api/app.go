package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"market/internal/v2/types"

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
