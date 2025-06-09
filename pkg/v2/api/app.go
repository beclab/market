package api

import (
	"log"
	"net/http"
	"strconv"

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
	Applications []ApplicationEntry         `json:"applications"`
}

// ApplicationEntry represents a simplified application entry for market display
// 应用条目，用于市场展示的简化应用信息
type ApplicationEntry struct {
	UserID    string                 `json:"user_id"`
	SourceID  string                 `json:"source_id"`
	AppData   map[string]interface{} `json:"app_data"`
	DataType  string                 `json:"data_type"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// 1. Get market information
func (s *Server) getMarketInfo(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/market - Getting market information")

	// Check if cache manager is available
	// 检查缓存管理器是否可用
	if s.cacheManager == nil {
		log.Println("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Get query parameters for filtering
	// 获取过滤查询参数
	userID := r.URL.Query().Get("user_id")
	sourceID := r.URL.Query().Get("source_id")
	dataType := r.URL.Query().Get("data_type")
	limitStr := r.URL.Query().Get("limit")

	limit := 100 // Default limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Get all users data from cache
	// 从缓存获取所有用户数据
	allUsersData := s.cacheManager.GetAllUsersData()

	// Get cache statistics
	// 获取缓存统计信息
	cacheStats := s.cacheManager.GetCacheStats()

	// Extract applications data based on filters
	// 根据过滤条件提取应用数据
	var applications []ApplicationEntry
	totalSources := 0
	count := 0

	for currentUserID, userData := range allUsersData {
		// Filter by user ID if specified
		// 如果指定了用户ID则过滤
		if userID != "" && currentUserID != userID {
			continue
		}

		userData.Mutex.RLock()
		for currentSourceID, sourceData := range userData.Sources {
			// Filter by source ID if specified
			// 如果指定了源ID则过滤
			if sourceID != "" && currentSourceID != sourceID {
				continue
			}

			totalSources++
			sourceData.Mutex.RLock()

			// Add AppInfoLatest if available and matches filter
			// 如果可用且符合过滤条件，添加AppInfoLatest
			if sourceData.AppInfoLatest != nil &&
				(dataType == "" || dataType == string(types.AppInfoLatest)) {
				if count < limit {
					applications = append(applications, ApplicationEntry{
						UserID:    currentUserID,
						SourceID:  currentSourceID,
						AppData:   sourceData.AppInfoLatest.Data,
						DataType:  string(sourceData.AppInfoLatest.Type),
						Timestamp: sourceData.AppInfoLatest.Timestamp,
						Version:   sourceData.AppInfoLatest.Version,
					})
					count++
				}
			}

			// Add AppStateLatest if available and matches filter
			// 如果可用且符合过滤条件，添加AppStateLatest
			if sourceData.AppStateLatest != nil &&
				(dataType == "" || dataType == string(types.AppStateLatest)) {
				if count < limit {
					applications = append(applications, ApplicationEntry{
						UserID:    currentUserID,
						SourceID:  currentSourceID,
						AppData:   sourceData.AppStateLatest.Data,
						DataType:  string(sourceData.AppStateLatest.Type),
						Timestamp: sourceData.AppStateLatest.Timestamp,
						Version:   sourceData.AppStateLatest.Version,
					})
					count++
				}
			}

			// Add AppInfoLatestPending if available and matches filter
			// 如果可用且符合过滤条件，添加AppInfoLatestPending
			if sourceData.AppInfoLatestPending != nil &&
				(dataType == "" || dataType == string(types.AppInfoLatestPending)) {
				if count < limit {
					applications = append(applications, ApplicationEntry{
						UserID:    currentUserID,
						SourceID:  currentSourceID,
						AppData:   sourceData.AppInfoLatestPending.Data,
						DataType:  string(sourceData.AppInfoLatestPending.Type),
						Timestamp: sourceData.AppInfoLatestPending.Timestamp,
						Version:   sourceData.AppInfoLatestPending.Version,
					})
					count++
				}
			}

			// Add history entries if matches filter
			// 如果符合过滤条件，添加历史记录条目
			if dataType == "" || dataType == string(types.AppInfoHistory) {
				for _, historyEntry := range sourceData.AppInfoHistory {
					if count < limit && historyEntry != nil {
						applications = append(applications, ApplicationEntry{
							UserID:    currentUserID,
							SourceID:  currentSourceID,
							AppData:   historyEntry.Data,
							DataType:  string(historyEntry.Type),
							Timestamp: historyEntry.Timestamp,
							Version:   historyEntry.Version,
						})
						count++
					}
				}
			}

			// Add other data entries if matches filter
			// 如果符合过滤条件，添加其他数据条目
			if dataType == "" || dataType == string(types.Other) {
				for subType, otherData := range sourceData.Other {
					if count < limit && otherData != nil {
						// Add sub_type to the data for identification
						// 为标识添加sub_type到数据中
						dataWithSubType := make(map[string]interface{})
						for k, v := range otherData.Data {
							dataWithSubType[k] = v
						}
						dataWithSubType["sub_type"] = subType

						applications = append(applications, ApplicationEntry{
							UserID:    currentUserID,
							SourceID:  currentSourceID,
							AppData:   dataWithSubType,
							DataType:  string(otherData.Type),
							Timestamp: otherData.Timestamp,
							Version:   otherData.Version,
						})
						count++
					}
				}
			}

			sourceData.Mutex.RUnlock()

			// Break if we've reached the limit
			// 如果已达到限制则跳出
			if count >= limit {
				break
			}
		}
		userData.Mutex.RUnlock()

		// Break if we've reached the limit
		// 如果已达到限制则跳出
		if count >= limit {
			break
		}
	}

	// Prepare response data
	// 准备响应数据
	responseData := MarketInfoResponse{
		UsersData:    allUsersData,
		CacheStats:   cacheStats,
		TotalUsers:   len(allUsersData),
		TotalSources: totalSources,
		Applications: applications,
	}

	log.Printf("Market information retrieved: %d users, %d sources, %d applications (limited to %d)",
		len(allUsersData), totalSources, len(applications), limit)

	s.sendResponse(w, http.StatusOK, true, "Market information retrieved successfully", responseData)
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
