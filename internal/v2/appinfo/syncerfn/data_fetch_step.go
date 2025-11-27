package syncerfn

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
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
	version := data.GetVersion()
	if version == "" {
		version = "1.12.0" // fallback version
		log.Printf("No version provided in context, using default: %s", version)
	}

	// Get current market source from context
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		return fmt.Errorf("no market source available in sync context")
	}

	// Build complete URL from market source base URL and endpoint path
	dataURL := d.SettingsManager.BuildAPIURL(marketSource.BaseURL, d.DataEndpointPath)
	log.Printf("Using data URL: %s", dataURL)

	if strings.HasPrefix(dataURL, "file://") {
		return nil
	}

	// Initialize response struct
	response := &AppStoreInfoResponse{}

	// Make request with version parameter and use structured response
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
	data.LatestData = response

	// Extract version from response and update context
	d.extractAndSetVersion(data)

	// Extract app IDs from the latest data
	d.extractAppIDs(data)

	// Extract and update Others data
	d.extractAndUpdateOthers(data)

	log.Printf("Fetched latest data with %d app IDs, version: %s",
		len(data.AppIDs), data.GetVersion())

	return nil
}

// CanSkip determines if this step can be skipped
func (d *DataFetchStep) CanSkip(ctx context.Context, data *SyncContext) bool {
	// Get current market source - only check data for this specific source
	marketSource := data.GetMarketSource()
	if marketSource == nil {
		log.Printf("Executing %s - no market source available in sync context", d.GetStepName())
		return false
	}

	sourceID := marketSource.ID

	// Check if we have existing data in cache for THIS specific source only
	hasExistingData := false
	if data.Cache != nil {
		// Use CacheManager's lock for unified lock strategy
		if data.CacheManager != nil {
			data.CacheManager.RLock()
			for userID, userData := range data.Cache.Users {
				// Only check data for the current market source
				if sourceData, exists := userData.Sources[sourceID]; exists {
					if len(sourceData.AppInfoLatestPending) > 0 || len(sourceData.AppInfoLatest) > 0 {
						hasExistingData = true
						log.Printf("Found existing data for source:%s user:%s (pending:%d latest:%d)",
							sourceID, userID, len(sourceData.AppInfoLatestPending), len(sourceData.AppInfoLatest))
						break
					}
				}
			}
			data.CacheManager.RUnlock()
		} else {
			// Fallback to SyncContext's mutex if CacheManager is not available
			data.mutex.RLock()
			for userID, userData := range data.Cache.Users {
				// Only check data for the current market source
				if sourceData, exists := userData.Sources[sourceID]; exists {
					if len(sourceData.AppInfoLatestPending) > 0 || len(sourceData.AppInfoLatest) > 0 {
						hasExistingData = true
						log.Printf("Found existing data for source:%s user:%s (pending:%d latest:%d)",
							sourceID, userID, len(sourceData.AppInfoLatestPending), len(sourceData.AppInfoLatest))
						break
					}
				}
			}
			data.mutex.RUnlock()
		}
	}

	// Skip only if hashes match AND we have existing data for THIS specific source
	if data.HashMatches && hasExistingData {
		log.Printf("Skipping %s for source %s - hashes match and existing data found, no sync required", d.GetStepName(), sourceID)
		return true
	}

	// Force execution if no existing data for this source, even if hashes match
	if !hasExistingData {
		log.Printf("Executing %s for source %s - no existing data found for this source, forcing data fetch", d.GetStepName(), sourceID)
	} else if !data.HashMatches {
		log.Printf("Executing %s for source %s - hashes don't match, sync required", d.GetStepName(), sourceID)
	}

	return false
}

// extractAndSetVersion extracts version from the response and updates SyncContext
func (d *DataFetchStep) extractAndSetVersion(data *SyncContext) {
	if data.LatestData != nil && data.LatestData.Version != "" {
		data.SetVersion(data.LatestData.Version)
		log.Printf("Updated version from response: %s", data.LatestData.Version)
	} else {
		log.Printf("No version found in response or version is empty")
	}
}

// extractAppIDs extracts app IDs from the fetched data
func (d *DataFetchStep) extractAppIDs(data *SyncContext) {
	// Clear existing app IDs
	data.AppIDs = data.AppIDs[:0]

	// Check if we have valid response data
	if data.LatestData == nil || data.LatestData.Data.Apps == nil {
		log.Printf("Warning: no apps data found in response")
		return
	}

	// Access apps data directly from structured response
	appsMap := data.LatestData.Data.Apps

	// In development environment, limit the original data to only 2 apps
	if isDevelopmentEnvironment() {
		originalCount := len(appsMap)
		if originalCount > 200 {
			log.Printf("Development environment detected, limiting original apps data to 2 (original count: %d)", originalCount)

			// Create a new map with only the first 2 apps
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
			data.LatestData.Data.Apps = limitedAppsMap
			appsMap = limitedAppsMap
		}
	}

	// Iterate through the apps map where keys are app IDs
	for appID, appData := range appsMap {
		// Verify this is a valid app entry by checking if it has required fields
		if appMap, ok := appData.(map[string]interface{}); ok {
			if id, hasID := appMap["id"].(string); hasID && id == appID {
				data.AppIDs = append(data.AppIDs, appID)
			}
		}
	}

	log.Printf("Extracted %d app IDs from response", len(data.AppIDs))

	// Log first few app IDs for debugging
	if len(data.AppIDs) > 0 {
		maxLog := 5
		if len(data.AppIDs) < maxLog {
			maxLog = len(data.AppIDs)
		}
		log.Printf("First %d app IDs: %v", maxLog, data.AppIDs[:maxLog])
	}
}

// isDevelopmentEnvironment checks if the application is running in development mode
func isDevelopmentEnvironment() bool {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	return env == "dev" || env == "development" || env == ""
}

// getMapKeys returns the keys of a map as a slice of strings
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// extractAndUpdateOthers extracts and updates Others data in SourceData
func (d *DataFetchStep) extractAndUpdateOthers(data *SyncContext) {
	log.Printf("DEBUG: Starting extractAndUpdateOthers")
	// Check if we have valid response data
	if data.LatestData == nil {
		log.Printf("Warning: no latest data found for Others extraction")
		return
	}

	// Create Others structure from the response data
	others := &types.Others{
		Hash:       data.LatestData.Hash,
		Version:    data.LatestData.Version,
		Topics:     make([]*types.Topic, 0),
		TopicLists: make([]*types.TopicList, 0),
		Recommends: make([]*types.Recommend, 0),
		Pages:      make([]*types.Page, 0),
		Tops:       make([]*types.AppStoreTopItem, 0),
		Latest:     make([]string, 0),
	}

	// Extract topics data
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
	if data.LatestData.Data.Recommends != nil {
		log.Printf("DEBUG: Processing recommends data, count: %d", len(data.LatestData.Data.Recommends))
		for i, recommendData := range data.LatestData.Data.Recommends {
			if recommendMap, ok := recommendData.(map[string]interface{}); ok {
				log.Printf("DEBUG: Processing recommend[%d], keys: %v", i, getMapKeys(recommendMap))
				if dataField, exists := recommendMap["data"]; exists {
					log.Printf("DEBUG: Recommend[%d] has data field, type: %T, value: %+v", i, dataField, dataField)
				} else {
					log.Printf("DEBUG: Recommend[%d] missing data field", i)
				}
				recommend := d.mapToRecommend(recommendMap)
				if recommend != nil {
					log.Printf("DEBUG: Recommend[%d] mapped successfully, has Data: %v", i, recommend.Data != nil)
					if recommend.Data != nil {
						log.Printf("DEBUG: Recommend[%d] Data.Title count: %d, Data.Description count: %d",
							i, len(recommend.Data.Title), len(recommend.Data.Description))
					}
					others.Recommends = append(others.Recommends, recommend)
				} else {
					log.Printf("DEBUG: Recommend[%d] mapping failed", i)
				}
			} else {
				log.Printf("DEBUG: Recommend[%d] is not a map, type: %T", i, recommendData)
			}
		}
		log.Printf("DEBUG: Extracted %d recommends from response", len(others.Recommends))
	} else {
		log.Printf("DEBUG: No recommends data found in response")
	}

	// Extract pages data
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

	// Extract tops data
	if data.LatestData.Data.Tops != nil {
		for _, topData := range data.LatestData.Data.Tops {
			if topMap, ok := topData.(map[string]interface{}); ok {
				topItem := d.mapToTopItem(topMap)
				if topItem != nil {
					others.Tops = append(others.Tops, topItem)
				}
			}
		}
	}

	// Extract latest data
	if data.LatestData.Data.Latest != nil {
		others.Latest = data.LatestData.Data.Latest
	}

	// Extract tags data - handle object format
	if data.LatestData.Data.Tags != nil {
		keys := make([]string, 0, len(data.LatestData.Data.Tags))
		for k := range data.LatestData.Data.Tags {
			keys = append(keys, k)
		}
		log.Printf("DEBUG: Processing tags data, type: %T, keys: %v", data.LatestData.Data.Tags, keys)
		for tagKey, tagData := range data.LatestData.Data.Tags {
			if tagMap, ok := tagData.(map[string]interface{}); ok {
				tag := d.mapToTag(tagMap)
				if tag != nil {
					others.Tags = append(others.Tags, tag)
					log.Printf("DEBUG: Added tag %s to others", tagKey)
				}
			}
		}
		log.Printf("DEBUG: Extracted %d tags from response", len(others.Tags))
	} else {
		log.Printf("DEBUG: No tags data found in response")
	}

	// Update Others in the cache for current source
	if data.Cache != nil && data.MarketSource != nil {
		d.updateOthersInCache(data, others)
	}

	log.Printf("Extracted Others data: %d topics, %d topic lists, %d recommends, %d pages, %d tops, %d latest, %d tags",
		len(others.Topics), len(others.TopicLists), len(others.Recommends), len(others.Pages), len(others.Tops), len(others.Latest), len(others.Tags))

	// Log detailed summary of recommends data
	if len(others.Recommends) > 0 {
		log.Printf("DEBUG: Final recommends summary - total: %d", len(others.Recommends))
		for i, rec := range others.Recommends {
			log.Printf("DEBUG: Final recommend[%d] '%s', has Data: %v", i, rec.Name, rec.Data != nil)
			if rec.Data != nil {
				log.Printf("DEBUG: Final recommend[%d] Data.Title count: %d, Data.Description count: %d",
					i, len(rec.Data.Title), len(rec.Data.Description))
			}
		}
	} else {
		log.Printf("DEBUG: No recommends data in final Others structure")
	}
}

// mapToTopic converts a map to Topic struct
func (d *DataFetchStep) mapToTopic(m map[string]interface{}) *types.Topic {
	topic := &types.Topic{}

	if id, ok := m["_id"].(string); ok {
		topic.ID = id
	}
	if name, ok := m["name"].(string); ok {
		topic.Name = name
	}
	if data, ok := m["data"].(map[string]interface{}); ok {
		topic.Data = make(map[string]*types.TopicData)
		for lang, topicDataInterface := range data {
			if topicDataMap, ok := topicDataInterface.(map[string]interface{}); ok {
				topicData := &types.TopicData{}

				if group, ok := topicDataMap["group"].(string); ok {
					topicData.Group = group
				}
				if title, ok := topicDataMap["title"].(string); ok {
					topicData.Title = title
				}
				if des, ok := topicDataMap["des"].(string); ok {
					topicData.Des = des
				}
				if iconImg, ok := topicDataMap["iconimg"].(string); ok {
					topicData.IconImg = iconImg
				}
				if detailImg, ok := topicDataMap["detailimg"].(string); ok {
					topicData.DetailImg = detailImg
				}
				if richText, ok := topicDataMap["richtext"].(string); ok {
					topicData.RichText = richText
				}
				if mobileDetailImg, ok := topicDataMap["mobileDetailImg"].(string); ok {
					topicData.MobileDetailImg = mobileDetailImg
				}
				if mobileRichText, ok := topicDataMap["mobileRichtext"].(string); ok {
					topicData.MobileRichText = mobileRichText
				}
				if backgroundColor, ok := topicDataMap["backgroundColor"].(string); ok {
					topicData.BackgroundColor = backgroundColor
				}
				if apps, ok := topicDataMap["apps"].(string); ok {
					topicData.Apps = apps
				}
				if isDelete, ok := topicDataMap["isdelete"].(bool); ok {
					topicData.IsDelete = isDelete
				}

				topic.Data[lang] = topicData
			}
		}
	}
	if source, ok := m["source"].(string); ok {
		topic.Source = source
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
	if title, ok := m["title"].(map[string]interface{}); ok {
		topicList.Title = make(map[string]string)
		for k, v := range title {
			if str, ok := v.(string); ok {
				topicList.Title[k] = str
			}
		}
	}
	if source, ok := m["source"].(string); ok {
		topicList.Source = source
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

	// Handle Data field
	log.Printf("DEBUG: mapToRecommend - checking for data field, available keys: %v", getMapKeys(m))
	if dataField, ok := m["data"].(map[string]interface{}); ok {
		log.Printf("DEBUG: mapToRecommend - found data field, type: %T, keys: %v", dataField, getMapKeys(dataField))
		recommend.Data = &types.RecommendData{}

		if title, ok := dataField["title"].(map[string]interface{}); ok {
			log.Printf("DEBUG: mapToRecommend - found title field, keys: %v", getMapKeys(title))
			recommend.Data.Title = make(map[string]string)
			for k, v := range title {
				if str, ok := v.(string); ok {
					recommend.Data.Title[k] = str
				} else {
					log.Printf("DEBUG: mapToRecommend - title[%s] is not string, type: %T, value: %v", k, v, v)
				}
			}
			log.Printf("DEBUG: mapToRecommend - processed title, count: %d", len(recommend.Data.Title))
		} else {
			log.Printf("DEBUG: mapToRecommend - title field not found or not a map")
		}

		if description, ok := dataField["description"].(map[string]interface{}); ok {
			log.Printf("DEBUG: mapToRecommend - found description field, keys: %v", getMapKeys(description))
			recommend.Data.Description = make(map[string]string)
			for k, v := range description {
				if str, ok := v.(string); ok {
					recommend.Data.Description[k] = str
				} else {
					log.Printf("DEBUG: mapToRecommend - description[%s] is not string, type: %T, value: %v", k, v, v)
				}
			}
			log.Printf("DEBUG: mapToRecommend - processed description, count: %d", len(recommend.Data.Description))
		} else {
			log.Printf("DEBUG: mapToRecommend - description field not found or not a map")
		}
	} else {
		log.Printf("DEBUG: mapToRecommend - data field not found or not a map, type: %T", m["data"])
	}

	// Handle Source field
	if source, ok := m["source"].(string); ok {
		recommend.Source = source
	}

	log.Printf("DEBUG: mapToRecommend - final result, has Data: %v", recommend.Data != nil)
	return recommend
}

// mapToPage converts a map to Page struct
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

// mapToTopItem converts a map to AppStoreTopItem struct
func (d *DataFetchStep) mapToTopItem(m map[string]interface{}) *types.AppStoreTopItem {
	topItem := &types.AppStoreTopItem{}

	if appid, ok := m["appId"].(string); ok {
		topItem.AppID = appid
	}
	if rank, ok := m["rank"].(float64); ok {
		topItem.Rank = int(rank)
	}

	return topItem
}

// mapToTag converts a map to Tag struct
func (d *DataFetchStep) mapToTag(m map[string]interface{}) *types.Tag {
	tag := &types.Tag{}

	if id, ok := m["_id"].(string); ok {
		tag.ID = id
	}
	if name, ok := m["name"].(string); ok {
		tag.Name = name
	}
	if title, ok := m["title"].(map[string]interface{}); ok {
		tag.Title = make(map[string]string)
		for k, v := range title {
			if str, ok := v.(string); ok {
				tag.Title[k] = str
			}
		}
	}
	if icon, ok := m["icon"].(string); ok {
		tag.Icon = icon
	}
	if sort, ok := m["sort"].(float64); ok {
		tag.Sort = int(sort)
	}
	if source, ok := m["source"].(string); ok {
		tag.Source = source
	}
	if createdAt, ok := m["createdAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			tag.CreatedAt = t
		}
	}
	if updatedAt, ok := m["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			tag.UpdatedAt = t
		}
	}

	return tag
}

// updateOthersInCache updates Others data in the cache for the current source
func (d *DataFetchStep) updateOthersInCache(data *SyncContext, others *types.Others) {
	// Get source ID from market source - use Name to match syncer.go behavior
	sourceID := data.MarketSource.ID

	// Use CacheManager's lock for unified lock strategy
	if data.CacheManager != nil {
		data.CacheManager.Lock()
		defer data.CacheManager.Unlock()
	}

	// Get all existing user IDs
	var userIDs []string
	for userID := range data.Cache.Users {
		userIDs = append(userIDs, userID)
	}

	// If no users exist, create a system user as fallback
	if len(userIDs) == 0 {
		systemUserID := "system"
		data.Cache.Users[systemUserID] = types.NewUserData()
		userIDs = append(userIDs, systemUserID)
		log.Printf("No existing users found, created system user as fallback")
	}

	log.Printf("Updating Others data for %d users: %v, sourceID: %s", len(userIDs), userIDs, sourceID)

	// Update Others for each user
	for _, userID := range userIDs {
		userData := data.Cache.Users[userID]

		// Ensure source data exists for this user
		if userData.Sources == nil {
			userData.Sources = make(map[string]*types.SourceData)
		}

		if userData.Sources[sourceID] == nil {
			userData.Sources[sourceID] = types.NewSourceData()
		}

		sourceData := userData.Sources[sourceID]

		// Update Others in SourceData
		sourceData.Others = others

		// Log details about the saved recommends data
		if sourceData.Others != nil && len(sourceData.Others.Recommends) > 0 {
			log.Printf("DEBUG: Saved %d recommends to cache for user %s, source %s",
				len(sourceData.Others.Recommends), userID, sourceID)
			for i, rec := range sourceData.Others.Recommends {
				log.Printf("DEBUG: Saved recommend[%d] '%s', has Data: %v",
					i, rec.Name, rec.Data != nil)
				if rec.Data != nil {
					log.Printf("DEBUG: Saved recommend[%d] Data.Title count: %d, Data.Description count: %d",
						i, len(rec.Data.Title), len(rec.Data.Description))
				}
			}
		} else {
			log.Printf("DEBUG: No recommends data saved to cache for user %s, source %s", userID, sourceID)
		}

		log.Printf("Updated Others data in cache for user %s, source %s", userID, sourceID)
	}

	log.Printf("Successfully updated Others data for all %d users, source %s", len(userIDs), sourceID)
}
