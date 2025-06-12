package syncerfn

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// DataFetchStep implements the second step: fetch latest data from remote
type DataFetchStep struct {
	DataEndpointPath string                    // Relative path like "/api/v1/appstore/info"
	SettingsManager  *settings.SettingsManager // Settings manager to build complete URLs
}

// NewDataFetchStep creates a new data fetch step
func NewDataFetchStep(dataEndpointPath string, settingsManager *settings.SettingsManager) *DataFetchStep {
	return &DataFetchStep{
		DataEndpointPath: dataEndpointPath,
		SettingsManager:  settingsManager,
	}
}

// GetStepName returns the name of this step
func (d *DataFetchStep) GetStepName() string {
	return "Data Fetch Step"
}

// Execute performs the data fetching logic
func (d *DataFetchStep) Execute(ctx context.Context, data *SyncContext) error {
	log.Printf("Executing %s", d.GetStepName())

	// Get version from SyncContext for API request
	// 从SyncContext获取版本用于API请求
	version := data.GetVersion()
	if version == "" {
		version = "1.12.0" // fallback version
		log.Printf("No version provided in context, using default: %s", version)
	}

	// Get current market source from context
	// 从上下文获取当前市场源
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	// 从市场源基础URL和端点路径构建完整URL
	dataURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DataEndpointPath)
	log.Printf("Using data URL: %s", dataURL)

	// Initialize response struct
	// 初始化响应结构体
	response := &AppStoreInfoResponse{}

	// Make request with version parameter and use structured response
	// 携带version参数发起请求并使用结构化响应
	resp, err := data.Client.R().
		SetContext(ctx).
		SetQueryParam("version", version).
		SetResult(response).
		Get(dataURL)

	if err != nil {
		return fmt.Errorf("failed to fetch latest data from %s: %w", dataURL, err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("remote data API returned status %d from %s", resp.StatusCode(), dataURL)
	}

	// Update SyncContext with structured data
	// 使用结构化数据更新SyncContext
	data.LatestData = response

	// Extract version from response and update context
	// 从响应中提取version并更新上下文
	d.extractAndSetVersion(data)

	// Extract app IDs from the latest data
	// 从最新数据中提取app ID列表
	d.extractAppIDs(data)

	// Extract and update Others data
	// 提取和更新Others数据
	d.extractAndUpdateOthers(data)

	log.Printf("Fetched latest data with %d app IDs, version: %s",
		len(data.AppIDs), data.GetVersion())

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DataFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Check if we have any existing data in cache
	// 检查缓存中是否有现有数据
	hasExistingData := false
	if data.Cache != nil {
		data.Cache.Mutex.RLock()
		for _, userData := range data.Cache.Users {
			// 不再需要嵌套锁，因为我们已经持有全局锁
			// No nested locks needed since we already hold the global lock
			for _, sourceData := range userData.Sources {
				// 不再需要嵌套锁，因为我们已经持有全局锁
				// No nested locks needed since we already hold the global lock
				if len(sourceData.AppInfoLatestPending) > 0 || len(sourceData.AppInfoLatest) > 0 {
					hasExistingData = true
				}
				if hasExistingData {
					break
				}
			}
			if hasExistingData {
				break
			}
		}
		data.Cache.Mutex.RUnlock()
	}

	// Skip only if hashes match AND we have existing data
	// 只有在hash匹配且有现有数据时才跳过
	if data.HashMatches && hasExistingData {
		log.Printf("Skipping %s - hashes match and existing data found, no sync required", d.GetStepName())
		return true
	}

	// Force execution if no existing data, even if hashes match
	// 如果没有现有数据，即使hash匹配也强制执行
	if !hasExistingData {
		log.Printf("Executing %s - no existing data found, forcing data fetch", d.GetStepName())
	} else if !data.HashMatches {
		log.Printf("Executing %s - hashes don't match, sync required", d.GetStepName())
	}

	return false
}

// extractAndSetVersion extracts version from the response and updates SyncContext
// extractAndSetVersion 从响应中提取version并更新SyncContext
func (d *DataFetchStep) extractAndSetVersion(data *SyncContext) {
	if data.LatestData != nil && data.LatestData.Version != "" {
		data.SetVersion(data.LatestData.Version)
		log.Printf("Updated version from response: %s", data.LatestData.Version)
	} else {
		log.Printf("No version found in response or version is empty")
	}
}

// extractAppIDs extracts app IDs from the fetched data
// extractAppIDs 从获取的数据中提取app ID列表
func (d *DataFetchStep) extractAppIDs(data *SyncContext) {
	// Clear existing app IDs
	// 清空现有的app ID列表
	data.AppIDs = data.AppIDs[:0]

	// Check if we have valid response data
	// 检查是否有有效的响应数据
	if data.LatestData == nil || data.LatestData.Data.Apps == nil {
		log.Printf("Warning: no apps data found in response")
		return
	}

	// Access apps data directly from structured response
	// 直接从结构化响应访问apps数据
	appsMap := data.LatestData.Data.Apps

	// In development environment, limit the original data to only 2 apps
	// 在开发环境下，将原始数据限制为只有2个应用
	if isDevelopmentEnvironment() {
		originalCount := len(appsMap)
		if originalCount > 2 {
			log.Printf("Development environment detected, limiting original apps data to 2 (original count: %d)", originalCount)

			// Create a new map with only the first 2 apps
			// 创建一个只包含前2个应用的新map
			limitedAppsMap := make(map[string]interface{})
			count := 0
			for appID, appData := range appsMap {
				if count >= 2 {
					break
				}
				limitedAppsMap[appID] = appData
				count++
			}

			// Replace the original apps data with limited data
			// 用限制后的数据替换原始应用数据
			data.LatestData.Data.Apps = limitedAppsMap
			appsMap = limitedAppsMap
		}
	}

	// Iterate through the apps map where keys are app IDs
	// 遍历apps映射，其中键是app ID
	for appID, appData := range appsMap {
		// Verify this is a valid app entry by checking if it has required fields
		// 通过检查是否有必需字段来验证这是一个有效的app条目
		if appMap, ok := appData.(map[string]interface{}); ok {
			if id, hasID := appMap["id"].(string); hasID && id == appID {
				data.AppIDs = append(data.AppIDs, appID)
			}
		}
	}

	log.Printf("Extracted %d app IDs from response", len(data.AppIDs))

	// Log first few app IDs for debugging
	// 记录前几个app ID用于调试
	if len(data.AppIDs) > 0 {
		maxLog := 5
		if len(data.AppIDs) < maxLog {
			maxLog = len(data.AppIDs)
		}
		log.Printf("First %d app IDs: %v", maxLog, data.AppIDs[:maxLog])
	}
}

// isDevelopmentEnvironment checks if the application is running in development mode
// 检查应用是否在开发模式下运行
func isDevelopmentEnvironment() bool {
	// Check DEV_MODE environment variable
	// 检查 DEV_MODE 环境变量
	devMode := os.Getenv("DEV_MODE")
	if devMode == "true" {
		return true
	}

	// Check ENVIRONMENT environment variable
	// 检查 ENVIRONMENT 环境变量
	environment := os.Getenv("ENVIRONMENT")
	if environment == "development" {
		return true
	}

	// Check DEBUG_MODE environment variable
	// 检查 DEBUG_MODE 环境变量
	debugMode := os.Getenv("DEBUG_MODE")
	if debugMode == "true" {
		return true
	}

	return false
}

// extractAndUpdateOthers extracts and updates Others data in SourceData
// extractAndUpdateOthers 提取并更新SourceData中的Others数据
func (d *DataFetchStep) extractAndUpdateOthers(data *SyncContext) {
	// Check if we have valid response data
	// 检查是否有有效的响应数据
	if data.LatestData == nil {
		log.Printf("Warning: no latest data found for Others extraction")
		return
	}

	// Create Others structure from the response data
	// 从响应数据创建Others结构
	others := &types.Others{
		Hash:       data.LatestData.Hash,
		Version:    data.LatestData.Version,
		Topics:     make([]*types.Topic, 0),
		TopicLists: make([]*types.TopicList, 0),
		Recommends: make([]*types.Recommend, 0),
		Pages:      make([]*types.Page, 0),
	}

	// Extract topics data
	// 提取topics数据
	if data.LatestData.Data.Topics != nil {
		for _, topicData := range data.LatestData.Data.Topics {
			if topicMap, ok := topicData.(map[string]interface{}); ok {
				topic := d.mapToTopic(topicMap)
				if topic != nil {
					others.Topics = append(others.Topics, topic)
				}
			}
		}
	}

	// Extract topic lists data
	// 提取topic lists数据
	if data.LatestData.Data.TopicLists != nil {
		for _, topicListData := range data.LatestData.Data.TopicLists {
			if topicListMap, ok := topicListData.(map[string]interface{}); ok {
				topicList := d.mapToTopicList(topicListMap)
				if topicList != nil {
					others.TopicLists = append(others.TopicLists, topicList)
				}
			}
		}
	}

	// Extract recommends data
	// 提取recommends数据
	if data.LatestData.Data.Recommends != nil {
		for _, recommendData := range data.LatestData.Data.Recommends {
			if recommendMap, ok := recommendData.(map[string]interface{}); ok {
				recommend := d.mapToRecommend(recommendMap)
				if recommend != nil {
					others.Recommends = append(others.Recommends, recommend)
				}
			}
		}
	}

	// Extract pages data
	// 提取pages数据
	if data.LatestData.Data.Pages != nil {
		for _, pageData := range data.LatestData.Data.Pages {
			if pageMap, ok := pageData.(map[string]interface{}); ok {
				page := d.mapToPage(pageMap)
				if page != nil {
					others.Pages = append(others.Pages, page)
				}
			}
		}
	}

	// Update Others in the cache for current source
	// 为当前源在缓存中更新Others
	if data.Cache != nil && data.MarketSource != nil {
		d.updateOthersInCache(data, others)
	}

	log.Printf("Extracted Others data: %d topics, %d topic lists, %d recommends, %d pages",
		len(others.Topics), len(others.TopicLists), len(others.Recommends), len(others.Pages))
}

// mapToTopic converts a map to Topic struct
// mapToTopic 将map转换为Topic结构体
func (d *DataFetchStep) mapToTopic(m map[string]interface{}) *types.Topic {
	topic := &types.Topic{}

	if name, ok := m["name"].(string); ok {
		topic.Name = name
	}
	if name2, ok := m["name2"].(map[string]interface{}); ok {
		topic.Name2 = make(map[string]string)
		for k, v := range name2 {
			if str, ok := v.(string); ok {
				topic.Name2[k] = str
			}
		}
	}
	if introduction, ok := m["introduction"].(string); ok {
		topic.Introduction = introduction
	}
	if introduction2, ok := m["introduction2"].(map[string]interface{}); ok {
		topic.Introduction2 = make(map[string]string)
		for k, v := range introduction2 {
			if str, ok := v.(string); ok {
				topic.Introduction2[k] = str
			}
		}
	}
	if des, ok := m["des"].(string); ok {
		topic.Des = des
	}
	if des2, ok := m["des2"].(map[string]interface{}); ok {
		topic.Des2 = make(map[string]string)
		for k, v := range des2 {
			if str, ok := v.(string); ok {
				topic.Des2[k] = str
			}
		}
	}
	if iconImg, ok := m["iconimg"].(string); ok {
		topic.IconImg = iconImg
	}
	if detailImg, ok := m["detailimg"].(string); ok {
		topic.DetailImg = detailImg
	}
	if richText, ok := m["richtext"].(string); ok {
		topic.RichText = richText
	}
	if richText2, ok := m["richtext2"].(map[string]interface{}); ok {
		topic.RichText2 = make(map[string]string)
		for k, v := range richText2 {
			if str, ok := v.(string); ok {
				topic.RichText2[k] = str
			}
		}
	}
	if apps, ok := m["apps"].(string); ok {
		topic.Apps = apps
	}
	if isDelete, ok := m["isdelete"].(bool); ok {
		topic.IsDelete = isDelete
	}
	if createdAt, ok := m["createdAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			topic.CreatedAt = t
		}
	}
	if updatedAt, ok := m["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			topic.UpdatedAt = t
		}
	}

	return topic
}

// mapToTopicList converts a map to TopicList struct
// mapToTopicList 将map转换为TopicList结构体
func (d *DataFetchStep) mapToTopicList(m map[string]interface{}) *types.TopicList {
	topicList := &types.TopicList{}

	if name, ok := m["name"].(string); ok {
		topicList.Name = name
	}
	if typ, ok := m["type"].(string); ok {
		topicList.Type = typ
	}
	if description, ok := m["description"].(string); ok {
		topicList.Description = description
	}
	if content, ok := m["content"].(string); ok {
		topicList.Content = content
	}
	if createdAt, ok := m["createdAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			topicList.CreatedAt = t
		}
	}
	if updatedAt, ok := m["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			topicList.UpdatedAt = t
		}
	}

	return topicList
}

// mapToRecommend converts a map to Recommend struct
// mapToRecommend 将map转换为Recommend结构体
func (d *DataFetchStep) mapToRecommend(m map[string]interface{}) *types.Recommend {
	recommend := &types.Recommend{}

	if name, ok := m["name"].(string); ok {
		recommend.Name = name
	}
	if description, ok := m["description"].(string); ok {
		recommend.Description = description
	}
	if content, ok := m["content"].(string); ok {
		recommend.Content = content
	}
	if createdAt, ok := m["createdAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			recommend.CreatedAt = t
		}
	}
	if updatedAt, ok := m["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			recommend.UpdatedAt = t
		}
	}

	return recommend
}

// mapToPage converts a map to Page struct
// mapToPage 将map转换为Page结构体
func (d *DataFetchStep) mapToPage(m map[string]interface{}) *types.Page {
	page := &types.Page{}

	if category, ok := m["category"].(string); ok {
		page.Category = category
	}
	if content, ok := m["content"].(string); ok {
		page.Content = content
	}
	if createdAt, ok := m["createdAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			page.CreatedAt = t
		}
	}
	if updatedAt, ok := m["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			page.UpdatedAt = t
		}
	}

	return page
}

// updateOthersInCache updates Others data in the cache for the current source
// updateOthersInCache 为当前源在缓存中更新Others数据
func (d *DataFetchStep) updateOthersInCache(data *SyncContext, others *types.Others) {
	// Get source ID from market source - use Name to match syncer.go behavior
	// 从市场源获取源ID - 使用Name以匹配syncer.go的行为
	sourceID := data.MarketSource.Name

	// Get all existing user IDs with minimal locking
	// 用最小锁定获取所有现有的用户ID
	data.Cache.Mutex.RLock()
	var userIDs []string
	for userID := range data.Cache.Users {
		userIDs = append(userIDs, userID)
	}
	data.Cache.Mutex.RUnlock()

	// If no users exist, create a system user as fallback
	// 如果没有用户存在，创建系统用户作为回退
	if len(userIDs) == 0 {
		data.Cache.Mutex.Lock()
		// Double-check after acquiring write lock
		if len(data.Cache.Users) == 0 {
			systemUserID := "system"
			data.Cache.Users[systemUserID] = types.NewUserData()
			userIDs = append(userIDs, systemUserID)
			log.Printf("No existing users found, created system user as fallback")
		} else {
			// Users were added by another goroutine
			for userID := range data.Cache.Users {
				userIDs = append(userIDs, userID)
			}
		}
		data.Cache.Mutex.Unlock()
	}

	log.Printf("Updating Others data for %d users: %v, sourceID: %s", len(userIDs), userIDs, sourceID)

	// Update Others for each user using global lock
	// 使用全局锁为每个用户更新Others
	data.Cache.Mutex.Lock()
	defer data.Cache.Mutex.Unlock()

	for _, userID := range userIDs {
		userData := data.Cache.Users[userID]
		// 不再需要嵌套锁，因为我们已经持有全局锁
		// No nested locks needed since we already hold the global lock

		// Ensure source data exists for this user
		// 确保此用户的源数据存在
		if userData.Sources == nil {
			userData.Sources = make(map[string]*types.SourceData)
		}

		if userData.Sources[sourceID] == nil {
			userData.Sources[sourceID] = types.NewSourceData()
		}

		sourceData := userData.Sources[sourceID]
		// 不再需要嵌套锁，因为我们已经持有全局锁
		// No nested locks needed since we already hold the global lock

		// Update Others in SourceData
		// 更新SourceData中的Others
		sourceData.Others = others

		log.Printf("Updated Others data in cache for user %s, source %s", userID, sourceID)
	}

	log.Printf("Successfully updated Others data for all %d users, source %s", len(userIDs), sourceID)
}
