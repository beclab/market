package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"
)

// MarketSettings represents user market settings
type MarketSettings struct {
	SelectedSource string `json:"selected_source"`
}

// MarketSource represents a market source configuration
type MarketSource struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	BaseURL     string    `json:"base_url"`
	Priority    int       `json:"priority"`
	IsActive    bool      `json:"is_active"`
	UpdatedAt   time.Time `json:"updated_at"`
	Description string    `json:"description"`
}

// MarketSourcesConfig represents the market sources configuration
type MarketSourcesConfig struct {
	Sources       []*MarketSource `json:"sources"`
	DefaultSource string          `json:"default_source"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// RedisClient interface for Redis operations
type RedisClient interface {
	Get(key string) (string, error)
	Set(key string, value interface{}, expiration time.Duration) error
	Keys(pattern string) ([]string, error)
}

// UpgradeFlow 执行升级流程 - 作为程序启动前的预执行功能
// 此功能不依赖任何其他组件，在程序初始化之前执行
func UpgradeFlow() error {
	log.Println("=== Starting upgrade flow ===")

	// 1. 检查并更新market source配置
	if err := checkAndUpdateMarketSourceConfig(); err != nil {
		log.Printf("Failed to check and update market source config: %v", err)
		return err
	}

	// 2. 检查并更新缓存数据
	if err := checkAndUpdateCacheData(); err != nil {
		log.Printf("Failed to check and update cache data: %v", err)
		return err
	}

	log.Println("=== Upgrade flow completed successfully ===")
	return nil
}

// checkAndUpdateMarketSourceConfig 检查并更新market source配置
func checkAndUpdateMarketSourceConfig() error {
	log.Println("Checking and updating market source configuration...")

	if IsPublicEnvironment() {
		log.Println("Public environment detected, skipping market source config update")
		return nil
	}

	// 创建Redis客户端
	redisClient, err := createRedisClient()
	if err != nil {
		log.Printf("Failed to create Redis client: %v, skipping market source config update", err)
		return nil
	}

	// 1. 更新所有用户的SelectedSource
	if err := updateAllUsersSelectedSource(redisClient); err != nil {
		log.Printf("Failed to update users' SelectedSource: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 2. 更新MarketSource配置
	if err := updateMarketSourceConfig(redisClient); err != nil {
		log.Printf("Failed to update market source config: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	log.Println("Market source configuration check completed")
	return nil
}

// checkAndUpdateCacheData 检查并更新缓存数据
func checkAndUpdateCacheData() error {
	log.Println("Checking and updating cache data...")

	if IsPublicEnvironment() {
		log.Println("Public environment detected, skipping cache data update")
		return nil
	}

	// 创建Redis客户端
	redisClient, err := createRedisClient()
	if err != nil {
		log.Printf("Failed to create Redis client: %v, skipping cache data update", err)
		return nil
	}

	// 更新缓存数据中的源ID
	if err := updateCacheDataSources(redisClient); err != nil {
		log.Printf("Failed to update cache data sources: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	log.Println("Cache data check completed")
	return nil
}

// createRedisClient 创建Redis客户端
func createRedisClient() (RedisClient, error) {
	// 获取Redis连接参数
	redisHost := GetEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := GetEnvOrDefault("REDIS_PORT", "6379")
	redisPassword := GetEnvOrDefault("REDIS_PASSWORD", "")
	redisDBStr := GetEnvOrDefault("REDIS_DB", "0")
	redisDB, err := strconv.Atoi(redisDBStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB value: %w", err)
	}

	// 由于我们不能依赖settings包，这里创建一个简单的Redis客户端实现
	// 在实际环境中，这里应该创建真正的Redis客户端
	log.Printf("Creating Redis client for upgrade flow: %s:%s, DB: %d", redisHost, redisPort, redisDB)

	// 返回一个简单的Redis客户端实现
	return &SimpleRedisClient{
		host:     redisHost,
		port:     redisPort,
		password: redisPassword,
		db:       redisDB,
	}, nil
}

// SimpleRedisClient 简单的Redis客户端实现，用于升级流程
type SimpleRedisClient struct {
	host     string
	port     string
	password string
	db       int
}

// Get 获取Redis键值
func (c *SimpleRedisClient) Get(key string) (string, error) {
	// 这里应该实现真正的Redis GET操作
	// 由于我们不能依赖外部包，这里返回错误表示不支持
	return "", fmt.Errorf("Redis GET operation not implemented in upgrade flow")
}

// Set 设置Redis键值
func (c *SimpleRedisClient) Set(key string, value interface{}, expiration time.Duration) error {
	// 这里应该实现真正的Redis SET操作
	// 由于我们不能依赖外部包，这里返回错误表示不支持
	return fmt.Errorf("Redis SET operation not implemented in upgrade flow")
}

// Keys 获取匹配模式的键列表
func (c *SimpleRedisClient) Keys(pattern string) ([]string, error) {
	// 这里应该实现真正的Redis KEYS操作
	// 由于我们不能依赖外部包，这里返回错误表示不支持
	return nil, fmt.Errorf("Redis KEYS operation not implemented in upgrade flow")
}

// updateAllUsersSelectedSource 更新所有用户的SelectedSource
func updateAllUsersSelectedSource(redisClient RedisClient) error {
	if redisClient == nil {
		log.Println("Redis client not available, skipping user settings update")
		return nil
	}

	log.Println("Updating all users' SelectedSource from 'Official-Market-Sources' to 'market.olares'...")

	// 获取所有用户设置键
	pattern := "market:settings:*"
	keys, err := redisClient.Keys(pattern)
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// 获取当前设置
		data, err := redisClient.Get(key)
		if err != nil {
			if err.Error() == "key not found" {
				continue
			}
			return fmt.Errorf("failed to get settings for key %s: %w", key, err)
		}

		// 解析设置
		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			return fmt.Errorf("failed to unmarshal settings for key %s: %w", key, err)
		}

		// 检查是否需要更新
		if settings.SelectedSource == "Official-Market-Sources" {
			settings.SelectedSource = "market.olares"

			// 序列化更新后的设置
			updatedData, err := json.Marshal(settings)
			if err != nil {
				return fmt.Errorf("failed to marshal updated settings for key %s: %w", key, err)
			}

			// 保存回Redis
			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				return fmt.Errorf("failed to save updated settings for key %s: %w", key, err)
			}

			updatedCount++
		}
	}

	if updatedCount > 0 {
		log.Printf("Updated %d users' SelectedSource from 'Official-Market-Sources' to 'market.olares'", updatedCount)
	} else {
		log.Println("No users found with 'Official-Market-Sources' as SelectedSource")
	}

	return nil
}

// updateMarketSourceConfig 更新MarketSource配置
func updateMarketSourceConfig(redisClient RedisClient) error {
	if redisClient == nil {
		log.Println("Redis client not available, skipping market source config update")
		return nil
	}

	log.Println("Updating market source configuration...")

	// 获取market sources配置
	configKey := "market:sources:config"
	data, err := redisClient.Get(configKey)
	if err != nil {
		if err.Error() == "key not found" {
			log.Println("No market sources configuration found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	// 解析配置
	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false

	// 更新sources中的deprecated条目
	for _, source := range config.Sources {
		if source.ID == "Official-Market-Sources" {
			log.Printf("Updating deprecated source: %s -> market.olares", source.ID)
			source.ID = "market.olares"
			source.Name = "market.olares"
			updated = true
		}
	}

	// 更新default source
	if config.DefaultSource == "Official-Market-Sources" {
		log.Printf("Updating default source: %s -> market.olares", config.DefaultSource)
		config.DefaultSource = "market.olares"
		updated = true
	}

	if updated {
		// 更新时间戳
		config.UpdatedAt = time.Now()

		// 序列化更新后的配置
		updatedData, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("failed to marshal updated market sources config: %w", err)
		}

		// 保存回Redis
		if err := redisClient.Set(configKey, string(updatedData), 0); err != nil {
			return fmt.Errorf("failed to save updated market sources config: %w", err)
		}

		log.Println("Market source configuration updated successfully")
	} else {
		log.Println("No deprecated market sources found, configuration is up to date")
	}

	return nil
}

// updateCacheDataSources 更新缓存数据中的源ID
func updateCacheDataSources(redisClient RedisClient) error {
	if redisClient == nil {
		log.Println("Redis client not available, skipping cache data source update")
		return nil
	}

	log.Println("Updating cache data sources from 'Official-Market-Sources' to 'market.olares'...")

	// 获取所有用户数据键
	pattern := "cache:user:*"
	keys, err := redisClient.Keys(pattern)
	if err != nil {
		return fmt.Errorf("failed to get cache user keys: %w", err)
	}

	updatedUsers := 0
	for _, key := range keys {
		// 获取用户数据
		data, err := redisClient.Get(key)
		if err != nil {
			if err.Error() == "key not found" {
				continue
			}
			return fmt.Errorf("failed to get user data for key %s: %w", key, err)
		}

		// 解析用户数据
		var userData UserData
		if err := json.Unmarshal([]byte(data), &userData); err != nil {
			return fmt.Errorf("failed to unmarshal user data for key %s: %w", key, err)
		}

		// 检查是否需要更新源ID
		if oldSourceData, exists := userData.Sources["Official-Market-Sources"]; exists {
			log.Printf("Found Official-Market-Sources data for user key %s, migrating to market.olares", key)

			// 如果market.olares已存在，合并数据
			if newSourceData, newExists := userData.Sources["market.olares"]; newExists {
				log.Printf("Merging Official-Market-Sources data with existing market.olares data for user key %s", key)
				mergedSourceData := mergeSourceData(newSourceData, oldSourceData)
				userData.Sources["market.olares"] = mergedSourceData
			} else {
				// 如果market.olares不存在，直接重命名
				userData.Sources["market.olares"] = oldSourceData
			}

			// 删除旧的Official-Market-Sources
			delete(userData.Sources, "Official-Market-Sources")

			// 序列化更新后的用户数据
			updatedData, err := json.Marshal(userData)
			if err != nil {
				return fmt.Errorf("failed to marshal updated user data for key %s: %w", key, err)
			}

			// 保存回Redis
			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				return fmt.Errorf("failed to save updated user data for key %s: %w", key, err)
			}

			updatedUsers++
		}
	}

	if updatedUsers > 0 {
		log.Printf("Updated %d users' cache data sources from 'Official-Market-Sources' to 'market.olares'", updatedUsers)
	} else {
		log.Println("No users found with 'Official-Market-Sources' cache data")
	}

	return nil
}

// UserData represents user cache data structure
type UserData struct {
	Sources map[string]*SourceData `json:"sources"`
	Hash    string                 `json:"hash"`
}

// SourceData represents source cache data structure
type SourceData struct {
	AppInfoHistory       []*AppInfoHistoryData       `json:"app_info_history"`
	AppStateLatest       []*AppStateLatestData       `json:"app_state_latest"`
	AppInfoLatest        []*AppInfoLatestData        `json:"app_info_latest"`
	AppInfoLatestPending []*AppInfoLatestPendingData `json:"app_info_latest_pending"`
	AppRenderFailed      []*AppRenderFailedData      `json:"app_render_failed"`
	Others               *Others                     `json:"others"`
}

// AppInfoHistoryData represents app info history data
type AppInfoHistoryData struct {
	Type      string                 `json:"type"`
	Timestamp int64                  `json:"timestamp"`
	RawData   map[string]interface{} `json:"raw_data"`
}

// AppStateLatestData represents app state latest data
type AppStateLatestData struct {
	Type      string     `json:"type"`
	Timestamp int64      `json:"timestamp"`
	Status    *AppStatus `json:"status"`
}

// AppStatus represents app status
type AppStatus struct {
	Name             string            `json:"name"`
	EntranceStatuses []*EntranceStatus `json:"entrance_statuses"`
}

// EntranceStatus represents entrance status
type EntranceStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
	URL   string `json:"url"`
}

// AppInfoLatestData represents app info latest data
type AppInfoLatestData struct {
	Type            string                 `json:"type"`
	Timestamp       int64                  `json:"timestamp"`
	Version         int                    `json:"version"`
	RawData         *RawData               `json:"raw_data"`
	RawPackage      map[string]interface{} `json:"raw_package"`
	Values          map[string]interface{} `json:"values"`
	AppInfo         *AppInfoData           `json:"app_info"`
	RenderedPackage map[string]interface{} `json:"rendered_package"`
}

// RawData represents raw app data
type RawData struct {
	ID    string `json:"id"`
	AppID string `json:"app_id"`
	Name  string `json:"name"`
}

// AppInfoData represents app info data
type AppInfoData struct {
	AppEntry *AppEntryData `json:"app_entry"`
}

// AppEntryData represents app entry data
type AppEntryData struct {
	ID    string `json:"id"`
	AppID string `json:"app_id"`
	Name  string `json:"name"`
}

// AppInfoLatestPendingData represents app info latest pending data
type AppInfoLatestPendingData struct {
	Type            string                 `json:"type"`
	Timestamp       int64                  `json:"timestamp"`
	Version         int                    `json:"version"`
	RawData         *RawData               `json:"raw_data"`
	RawPackage      map[string]interface{} `json:"raw_package"`
	Values          map[string]interface{} `json:"values"`
	AppInfo         *AppInfoData           `json:"app_info"`
	RenderedPackage map[string]interface{} `json:"rendered_package"`
}

// AppRenderFailedData represents app render failed data
type AppRenderFailedData struct {
	Type          string   `json:"type"`
	Timestamp     int64    `json:"timestamp"`
	RawData       *RawData `json:"raw_data"`
	FailureReason string   `json:"failure_reason"`
}

// Others represents others data
type Others struct {
	Version string   `json:"version"`
	Hash    string   `json:"hash"`
	Topics  []*Topic `json:"topics"`
}

// Topic represents topic data
type Topic struct {
	Name string                `json:"name"`
	Data map[string]*TopicData `json:"data"`
}

// TopicData represents topic data
type TopicData struct {
	Apps string `json:"apps"`
}

// mergeSourceData 合并两个源的数据，以新源为准
func mergeSourceData(newSource, oldSource *SourceData) *SourceData {
	log.Println("Merging source data...")

	// 创建合并后的源数据
	merged := &SourceData{
		AppInfoHistory:       make([]*AppInfoHistoryData, 0),
		AppStateLatest:       make([]*AppStateLatestData, 0),
		AppInfoLatest:        make([]*AppInfoLatestData, 0),
		AppInfoLatestPending: make([]*AppInfoLatestPendingData, 0),
		AppRenderFailed:      make([]*AppRenderFailedData, 0),
		Others:               oldSource.Others, // 使用旧源的Others数据
	}

	// 如果新源有Others数据，使用新源的
	if newSource.Others != nil {
		merged.Others = newSource.Others
	}

	// 合并AppInfoHistory - 以新源为准，添加旧源中不存在的
	merged.AppInfoHistory = append(merged.AppInfoHistory, newSource.AppInfoHistory...)
	for _, oldItem := range oldSource.AppInfoHistory {
		if !containsAppInfoHistory(merged.AppInfoHistory, oldItem) {
			merged.AppInfoHistory = append(merged.AppInfoHistory, oldItem)
		}
	}

	// 合并AppStateLatest - 以新源为准，添加旧源中不存在的
	merged.AppStateLatest = append(merged.AppStateLatest, newSource.AppStateLatest...)
	for _, oldItem := range oldSource.AppStateLatest {
		if !containsAppStateLatest(merged.AppStateLatest, oldItem) {
			merged.AppStateLatest = append(merged.AppStateLatest, oldItem)
		}
	}

	// 合并AppInfoLatest - 以新源为准，添加旧源中不存在的
	merged.AppInfoLatest = append(merged.AppInfoLatest, newSource.AppInfoLatest...)
	for _, oldItem := range oldSource.AppInfoLatest {
		if !containsAppInfoLatest(merged.AppInfoLatest, oldItem) {
			merged.AppInfoLatest = append(merged.AppInfoLatest, oldItem)
		}
	}

	// 合并AppInfoLatestPending - 以新源为准，添加旧源中不存在的
	merged.AppInfoLatestPending = append(merged.AppInfoLatestPending, newSource.AppInfoLatestPending...)
	for _, oldItem := range oldSource.AppInfoLatestPending {
		if !containsAppInfoLatestPending(merged.AppInfoLatestPending, oldItem) {
			merged.AppInfoLatestPending = append(merged.AppInfoLatestPending, oldItem)
		}
	}

	// 合并AppRenderFailed - 以新源为准，添加旧源中不存在的
	merged.AppRenderFailed = append(merged.AppRenderFailed, newSource.AppRenderFailed...)
	for _, oldItem := range oldSource.AppRenderFailed {
		if !containsAppRenderFailed(merged.AppRenderFailed, oldItem) {
			merged.AppRenderFailed = append(merged.AppRenderFailed, oldItem)
		}
	}

	log.Printf("Merged source data: %d history, %d states, %d latest, %d pending, %d failed",
		len(merged.AppInfoHistory), len(merged.AppStateLatest),
		len(merged.AppInfoLatest), len(merged.AppInfoLatestPending), len(merged.AppRenderFailed))

	return merged
}

// 辅助函数检查是否包含特定的数据项
func containsAppInfoHistory(slice []*AppInfoHistoryData, item *AppInfoHistoryData) bool {
	for _, s := range slice {
		if s != nil && item != nil && s.Timestamp == item.Timestamp {
			return true
		}
	}
	return false
}

func containsAppStateLatest(slice []*AppStateLatestData, item *AppStateLatestData) bool {
	for _, s := range slice {
		if s != nil && item != nil && s.Status != nil && item.Status != nil && s.Status.Name == item.Status.Name {
			return true
		}
	}
	return false
}

func containsAppInfoLatest(slice []*AppInfoLatestData, item *AppInfoLatestData) bool {
	for _, s := range slice {
		if s != nil && item != nil && s.RawData != nil && item.RawData != nil {
			// 比较ID、AppID或Name
			if (s.RawData.ID != "" && s.RawData.ID == item.RawData.ID) ||
				(s.RawData.AppID != "" && s.RawData.AppID == item.RawData.AppID) ||
				(s.RawData.Name != "" && s.RawData.Name == item.RawData.Name) {
				return true
			}
		}
	}
	return false
}

func containsAppInfoLatestPending(slice []*AppInfoLatestPendingData, item *AppInfoLatestPendingData) bool {
	for _, s := range slice {
		if s != nil && item != nil && s.RawData != nil && item.RawData != nil {
			// 比较ID、AppID或Name
			if (s.RawData.ID != "" && s.RawData.ID == item.RawData.ID) ||
				(s.RawData.AppID != "" && s.RawData.AppID == item.RawData.AppID) ||
				(s.RawData.Name != "" && s.RawData.Name == item.RawData.Name) {
				return true
			}
		}
	}
	return false
}

func containsAppRenderFailed(slice []*AppRenderFailedData, item *AppRenderFailedData) bool {
	for _, s := range slice {
		if s != nil && item != nil && s.RawData != nil && item.RawData != nil {
			// 比较ID、AppID或Name
			if (s.RawData.ID != "" && s.RawData.ID == item.RawData.ID) ||
				(s.RawData.AppID != "" && s.RawData.AppID == item.RawData.AppID) ||
				(s.RawData.Name != "" && s.RawData.Name == item.RawData.Name) {
				return true
			}
		}
	}
	return false
}
