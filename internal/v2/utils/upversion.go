package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"context"

	"github.com/go-redis/redis/v8"
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
	Del(keys ...string) error
	ScanAllKeys(pattern string, count int) ([]string, error)
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

	// 1. 更新所有用户的SelectedSource (Official-Market-Sources -> market.olares)
	if err := updateAllUsersSelectedSource(redisClient); err != nil {
		log.Printf("Failed to update users' SelectedSource: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 1.1 更新所有用户的SelectedSource (market-local -> upload)
	if err := updateAllUsersSelectedSourceFromLocal(redisClient); err != nil {
		log.Printf("Failed to update users' SelectedSource from local: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 1.2 更新所有用户的SelectedSource (dev-local -> studio)
	if err := updateAllUsersSelectedSourceFromDevLocal(redisClient); err != nil {
		log.Printf("Failed to update users' SelectedSource from dev-local: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 2. 更新MarketSource配置 (Official-Market-Sources -> market.olares)
	if err := updateMarketSourceConfig(redisClient); err != nil {
		log.Printf("Failed to update market source config: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 3. 更新MarketSource配置 (market-local -> upload)
	if err := updateMarketSourceConfigFromLocal(redisClient); err != nil {
		log.Printf("Failed to update market source config from local: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 4. 更新MarketSource配置 (dev-local -> studio)
	if err := updateMarketSourceConfigFromDevLocal(redisClient); err != nil {
		log.Printf("Failed to update market source config from dev-local: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 5. 更新所有用户的SelectedSource (local -> upload)
	if err := updateAllUsersSelectedSourceFromLocal2(redisClient); err != nil {
		log.Printf("Failed to update users' SelectedSource from local: %v", err)
		// 不返回错误，继续执行其他升级步骤
	}

	// 6. 更新MarketSource配置 (local -> upload)
	if err := updateMarketSourceConfigFromLocal2(redisClient); err != nil {
		log.Printf("Failed to update market source config from local: %v", err)
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

	// 追加：迁移 appinfo 键空间的源数据（旧 -> 新），并清理旧键
	if err := migrateAppinfoSources(redisClient, "Official-Market-Sources", "market.olares"); err != nil {
		log.Printf("Failed to migrate appinfo sources from 'Official-Market-Sources' to 'market.olares': %v", err)
	}
	if err := migrateAppinfoSources(redisClient, "market-local", "upload"); err != nil {
		log.Printf("Failed to migrate appinfo sources from 'market-local' to 'upload': %v", err)
	}
	if err := migrateAppinfoSources(redisClient, "dev-local", "studio"); err != nil {
		log.Printf("Failed to migrate appinfo sources from 'dev-local' to 'studio': %v", err)
	}
	if err := migrateAppinfoSources(redisClient, "local", "upload"); err != nil {
		log.Printf("Failed to migrate appinfo sources from 'local' to 'upload': %v", err)
	}

	// 自动扫描处理：对大小写/空格等变体做统一迁移
	deprecatedMap := map[string]string{
		"official-market-sources": "market.olares",
		"market-local":            "upload",
		"dev-local":               "studio",
		"local":                   "upload",
	}
	if err := migrateAppinfoSourcesAuto(redisClient, deprecatedMap); err != nil {
		log.Printf("Failed to auto-migrate appinfo sources by normalization: %v", err)
	}

	// 扩展迁移：处理其他废弃源并在迁移后删除旧源
	// market-local -> upload
	if err := migrateCacheDataSources(redisClient, "market-local", "upload"); err != nil {
		log.Printf("Failed to migrate cache data sources from 'market-local' to 'upload': %v", err)
	}

	// dev-local -> studio
	if err := migrateCacheDataSources(redisClient, "dev-local", "studio"); err != nil {
		log.Printf("Failed to migrate cache data sources from 'dev-local' to 'studio': %v", err)
	}

	// local -> upload
	if err := migrateCacheDataSources(redisClient, "local", "upload"); err != nil {
		log.Printf("Failed to migrate cache data sources from 'local' to 'upload': %v", err)
	}

	log.Println("Cache data check completed")
	return nil
}

// migrateAppinfoSources 迁移 appinfo 键空间中某个源到新源
// 将 appinfo:user:<uid>:source:<old>:(app-info-history|app-state-latest|app-info-latest|app-info-latest-pending|app-render-failed)
// 迁移为对应的新源键，并在合并后删除旧键。
func migrateAppinfoSources(redisClient RedisClient, oldSourceID, newSourceID string) error {
	if redisClient == nil {
		log.Println("Redis client not available, skipping appinfo source migration")
		return nil
	}
	if oldSourceID == "" || newSourceID == "" || oldSourceID == newSourceID {
		return nil
	}

	log.Printf("Migrating appinfo sources from '%s' to '%s'...", oldSourceID, newSourceID)

	// 遍历所有用户的该源键
	userSourcePattern := fmt.Sprintf("appinfo:user:*:source:%s*", oldSourceID)
	keys, err := redisClient.Keys(userSourcePattern)
	if err != nil {
		return fmt.Errorf("failed to list appinfo keys for source '%s': %w", oldSourceID, err)
	}
	if len(keys) == 0 {
		log.Printf("No appinfo keys found for source '%s'", oldSourceID)
		return nil
	}

	// 支持的数据段后缀
	dataSuffixes := []string{
		":app-info-history", ":app_state_history",
		":app-state-latest", ":app_state_latest",
		":app-info-latest", ":app_info_latest",
		":app-info-latest-pending", ":app_info_latest_pending",
		":app-render-failed", ":app_render_failed",
	}

	migratedUsers := make(map[string]bool)
	for _, oldKey := range keys {
		// oldKey 形如 appinfo:user:<uid>:source:<oldSourceID>[:suffix]
		// 计算基础新key前缀
		newBase := strings.Replace(oldKey, ":source:"+oldSourceID, ":source:"+newSourceID, 1)

		// 检查是否是具体数据段键，否则跳过
		isDataKey := false
		for _, suffix := range dataSuffixes {
			if strings.HasSuffix(oldKey, suffix) {
				isDataKey = true
				break
			}
		}
		if !isDataKey {
			// 可能是用户标记等，跳过
			continue
		}

		// 读取旧值
		val, err := redisClient.Get(oldKey)
		if err != nil {
			continue
		}

		// 合并策略：如果新键有值，按 JSON 合并（数组拼接去重），否则直接写入
		// 先确定新键名
		newKey := newBase
		// 读取新值（可能不存在）
		newVal, err := redisClient.Get(newKey)
		if err == nil && newVal != "" {
			merged := mergeJSONArraysIfNeeded(newKey, newVal, val)
			if err := redisClient.Set(newKey, merged, 0); err != nil {
				return fmt.Errorf("failed to write merged data to %s: %w", newKey, err)
			}
		} else {
			if err := redisClient.Set(newKey, val, 0); err != nil {
				return fmt.Errorf("failed to write data to %s: %w", newKey, err)
			}
		}

		// 删除旧键
		if err := redisClient.Del(oldKey); err != nil {
			log.Printf("Warning: failed to delete old key %s: %v", oldKey, err)
		}

		// 标记该用户已迁移
		// 提取 userID
		// 形如 appinfo:user:<uid>:source:...
		parts := strings.Split(oldKey, ":")
		if len(parts) >= 3 {
			migratedUsers[parts[2]] = true
		}
	}

	log.Printf("Migrated appinfo sources for %d users: %s -> %s", len(migratedUsers), oldSourceID, newSourceID)
	return nil
}

// mergeJSONArraysIfNeeded 针对数组类型的 JSON 做合并；非数组则以新值覆盖为准
func mergeJSONArraysIfNeeded(key string, existing, incoming string) string {
	// 快速判断：如果不是 '[' 开头，则直接覆盖
	if len(existing) == 0 || existing[0] != '[' || len(incoming) == 0 || incoming[0] != '[' {
		return incoming
	}
	var arr1 []map[string]interface{}
	var arr2 []map[string]interface{}
	if err := json.Unmarshal([]byte(existing), &arr1); err != nil {
		return incoming
	}
	if err := json.Unmarshal([]byte(incoming), &arr2); err != nil {
		return incoming
	}
	// 合并并做简单去重（依据 timestamp + type）
	seen := make(map[string]bool)
	merged := make([]map[string]interface{}, 0, len(arr1)+len(arr2))
	for _, it := range arr1 {
		key := fmt.Sprintf("%v-%v", it["timestamp"], it["type"])
		if !seen[key] {
			seen[key] = true
			merged = append(merged, it)
		}
	}
	for _, it := range arr2 {
		key := fmt.Sprintf("%v-%v", it["timestamp"], it["type"])
		if !seen[key] {
			seen[key] = true
			merged = append(merged, it)
		}
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return incoming
	}
	return string(out)
}

// normalizeSourceID 规范化源ID（小写+去首尾空白）
func normalizeSourceID(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// migrateAppinfoSourcesAuto 扫描 appinfo 键空间，按规范化后的源名映射做迁移
func migrateAppinfoSourcesAuto(redisClient RedisClient, mapping map[string]string) error {
	if redisClient == nil || len(mapping) == 0 {
		return nil
	}

	log.Println("Auto-migrating appinfo sources by normalized mapping...")

	// 仅扫描包含 app* 段的数据键（兼容 app-*/app_*），避免非数据键
	keys, err := redisClient.Keys("appinfo:user:*:source:*:app*")
	if err != nil {
		return fmt.Errorf("failed to list appinfo data keys: %w", err)
	}
	if len(keys) == 0 {
		log.Println("No appinfo data keys found for auto migration")
		return nil
	}

	migratedUsers := make(map[string]bool)
	for _, oldKey := range keys {
		parts := strings.Split(oldKey, ":")
		// 期待格式 appinfo:user:<uid>:source:<sourceID>:<segment>
		if len(parts) < 6 || parts[0] != "appinfo" || parts[1] != "user" || parts[3] != "source" {
			continue
		}
		userID := parts[2]
		sourceID := parts[4]
		norm := normalizeSourceID(sourceID)
		newSourceID, ok := mapping[norm]
		if !ok || newSourceID == sourceID {
			continue
		}

		// 构造新键（仅替换 :source:<old>：带冒号避免部分匹配）
		needle := ":source:" + sourceID + ":"
		replacement := ":source:" + newSourceID + ":"
		if !strings.Contains(oldKey, needle) {
			continue
		}
		newKey := strings.Replace(oldKey, needle, replacement, 1)

		// 读取旧值
		oldVal, err := redisClient.Get(oldKey)
		if err != nil {
			continue
		}

		// 读取新值并合并（数组JSON走合并）
		if newVal, err := redisClient.Get(newKey); err == nil && newVal != "" {
			merged := mergeJSONArraysIfNeeded(newKey, newVal, oldVal)
			if err := redisClient.Set(newKey, merged, 0); err != nil {
				return fmt.Errorf("failed to write merged data to %s: %w", newKey, err)
			}
		} else {
			if err := redisClient.Set(newKey, oldVal, 0); err != nil {
				return fmt.Errorf("failed to write data to %s: %w", newKey, err)
			}
		}

		// 删除旧键
		if err := redisClient.Del(oldKey); err != nil {
			log.Printf("Warning: failed to delete old key %s: %v", oldKey, err)
		}

		migratedUsers[userID] = true
	}

	if len(migratedUsers) > 0 {
		log.Printf("Auto-migrated appinfo sources for %d users by normalization", len(migratedUsers))
	} else {
		log.Println("No appinfo sources required auto migration by normalization")
	}
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

	log.Printf("Creating Redis client for upgrade flow: %s:%s, DB: %d", redisHost, redisPort, redisDB)

	// 创建真正的Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisHost, redisPort),
		Password: redisPassword,
		DB:       redisDB,
	})

	// 测试连接
	ctx := context.Background()
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Println("Redis client connected successfully")

	// 返回包装的Redis客户端
	return &RedisClientWrapper{
		client: rdb,
		ctx:    ctx,
	}, nil
}

// RedisClientWrapper Redis客户端包装器，实现真正的Redis操作
type RedisClientWrapper struct {
	client *redis.Client
	ctx    context.Context
}

// Get 获取Redis键值
func (c *RedisClientWrapper) Get(key string) (string, error) {
	result, err := c.client.Get(c.ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found")
	}
	return result, err
}

// Set 设置Redis键值
func (c *RedisClientWrapper) Set(key string, value interface{}, expiration time.Duration) error {
	return c.client.Set(c.ctx, key, value, expiration).Err()
}

// Keys 获取匹配模式的键列表
func (c *RedisClientWrapper) Keys(pattern string) ([]string, error) {
	return c.client.Keys(c.ctx, pattern).Result()
}

// Del 删除一个或多个键
func (c *RedisClientWrapper) Del(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(c.ctx, keys...).Err()
}

// ScanAllKeys 使用 SCAN 游标遍历收集所有匹配的键
func (c *RedisClientWrapper) ScanAllKeys(pattern string, count int) ([]string, error) {
	var (
		cursor uint64
		all    []string
	)
	if count <= 0 {
		count = 1000
	}
	for {
		keys, cur, err := c.client.Scan(c.ctx, cursor, pattern, int64(count)).Result()
		if err != nil {
			return nil, err
		}
		if len(keys) > 0 {
			all = append(all, keys...)
		}
		cursor = cur
		if cursor == 0 {
			break
		}
	}
	return all, nil
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

// migrateCacheDataSources 迁移缓存数据中的源ID（通用）
func migrateCacheDataSources(redisClient RedisClient, oldSourceID, newSourceID string) error {
	if redisClient == nil {
		log.Println("Redis client not available, skipping cache data migration")
		return nil
	}

	if oldSourceID == "" || newSourceID == "" || oldSourceID == newSourceID {
		return nil
	}

	log.Printf("Migrating cache data sources from '%s' to '%s'...", oldSourceID, newSourceID)

	// 获取所有用户数据键
	keys, err := redisClient.Keys("cache:user:*")
	if err != nil {
		return fmt.Errorf("failed to get cache user keys: %w", err)
	}

	updatedUsers := 0
	for _, key := range keys {
		data, err := redisClient.Get(key)
		if err != nil {
			if err.Error() == "key not found" {
				continue
			}
			return fmt.Errorf("failed to get user data for key %s: %w", key, err)
		}

		var userData UserData
		if err := json.Unmarshal([]byte(data), &userData); err != nil {
			return fmt.Errorf("failed to unmarshal user data for key %s: %w", key, err)
		}

		if userData.Sources == nil {
			continue
		}

		// 如果旧源存在，则进行迁移
		if oldSourceData, exists := userData.Sources[oldSourceID]; exists {
			// 合并或直接移动到新源
			if newSourceData, newExists := userData.Sources[newSourceID]; newExists {
				merged := mergeSourceData(newSourceData, oldSourceData)
				userData.Sources[newSourceID] = merged
			} else {
				userData.Sources[newSourceID] = oldSourceData
			}

			// 删除旧源
			delete(userData.Sources, oldSourceID)

			// 保存回Redis
			updatedData, err := json.Marshal(userData)
			if err != nil {
				return fmt.Errorf("failed to marshal updated user data for key %s: %w", key, err)
			}
			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				return fmt.Errorf("failed to save updated user data for key %s: %w", key, err)
			}

			updatedUsers++
			log.Printf("Migrated cache data for key %s: %s -> %s", key, oldSourceID, newSourceID)
		}
	}

	if updatedUsers > 0 {
		log.Printf("Updated %d users' cache data sources from '%s' to '%s'", updatedUsers, oldSourceID, newSourceID)
	} else {
		log.Printf("No users found with '%s' cache data", oldSourceID)
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

// updateAllUsersSelectedSourceFromLocal 更新所有用户的SelectedSource从market-local到upload
func updateAllUsersSelectedSourceFromLocal(redisClient RedisClient) error {
	log.Println("Updating all users' SelectedSource from 'market-local' to 'upload'...")

	// 获取所有market settings的键
	keys, err := redisClient.Keys("market:settings:*")
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// 获取用户设置
		data, err := redisClient.Get(key)
		if err != nil {
			log.Printf("Failed to get settings for key %s: %v", key, err)
			continue
		}

		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			log.Printf("Failed to unmarshal settings for key %s: %v", key, err)
			continue
		}

		// 检查是否需要更新
		if settings.SelectedSource == "market-local" {
			settings.SelectedSource = "upload"

			// 更新设置
			updatedData, err := json.Marshal(settings)
			if err != nil {
				log.Printf("Failed to marshal updated settings for key %s: %v", key, err)
				continue
			}

			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				log.Printf("Failed to update settings for key %s: %v", key, err)
				continue
			}

			updatedCount++
			log.Printf("Updated user settings for key %s: market-local -> upload", key)
		}
	}

	if updatedCount == 0 {
		log.Println("No users found with 'market-local' as SelectedSource")
	} else {
		log.Printf("Updated %d users' SelectedSource from 'market-local' to 'upload'", updatedCount)
	}

	return nil
}

// updateMarketSourceConfigFromLocal 更新MarketSource配置，将market-local改为upload
func updateMarketSourceConfigFromLocal(redisClient RedisClient) error {
	log.Println("Updating market source configuration from 'market-local' to 'upload'...")

	// 获取market sources配置
	configData, err := redisClient.Get("market:sources:config")
	if err != nil {
		if err.Error() == "key not found" {
			log.Println("No market sources config found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false
	// 查找并更新market-local源
	for _, source := range config.Sources {
		if source.ID == "market-local" {
			log.Printf("Found deprecated market source: %s (%s), updating to 'upload'", source.Name, source.ID)
			source.ID = "upload"
			source.Name = "upload"
			source.UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		log.Println("No deprecated market sources found, configuration is up to date")
		return nil
	}

	// 保存更新后的配置
	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}

	if err := redisClient.Set("market:sources:config", string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	log.Println("Successfully updated market source configuration from 'market-local' to 'upload'")
	return nil
}

// updateAllUsersSelectedSourceFromDevLocal 更新所有用户的SelectedSource从dev-local到studio
func updateAllUsersSelectedSourceFromDevLocal(redisClient RedisClient) error {
	log.Println("Updating all users' SelectedSource from 'dev-local' to 'studio'...")

	// 获取所有market settings的键
	keys, err := redisClient.Keys("market:settings:*")
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// 获取用户设置
		data, err := redisClient.Get(key)
		if err != nil {
			log.Printf("Failed to get settings for key %s: %v", key, err)
			continue
		}

		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			log.Printf("Failed to unmarshal settings for key %s: %v", key, err)
			continue
		}

		// 检查是否需要更新
		if settings.SelectedSource == "dev-local" {
			settings.SelectedSource = "studio"

			// 更新设置
			updatedData, err := json.Marshal(settings)
			if err != nil {
				log.Printf("Failed to marshal updated settings for key %s: %v", key, err)
				continue
			}

			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				log.Printf("Failed to update settings for key %s: %v", key, err)
				continue
			}

			updatedCount++
			log.Printf("Updated user settings for key %s: dev-local -> studio", key)
		}
	}

	if updatedCount == 0 {
		log.Println("No users found with 'dev-local' as SelectedSource")
	} else {
		log.Printf("Updated %d users' SelectedSource from 'dev-local' to 'studio'", updatedCount)
	}

	return nil
}

// updateMarketSourceConfigFromDevLocal 更新MarketSource配置，将dev-local改为studio
func updateMarketSourceConfigFromDevLocal(redisClient RedisClient) error {
	log.Println("Updating market source configuration from 'dev-local' to 'studio'...")

	// 获取market sources配置
	configData, err := redisClient.Get("market:sources:config")
	if err != nil {
		if err.Error() == "key not found" {
			log.Println("No market sources config found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false
	// 查找并更新dev-local源
	for _, source := range config.Sources {
		if source.ID == "dev-local" {
			log.Printf("Found deprecated market source: %s (%s), updating to 'studio'", source.Name, source.ID)
			source.ID = "studio"
			source.Name = "studio"
			source.UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		log.Println("No deprecated market sources found, configuration is up to date")
		return nil
	}

	// 保存更新后的配置
	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}

	if err := redisClient.Set("market:sources:config", string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	log.Println("Successfully updated market source configuration from 'dev-local' to 'studio'")
	return nil
}

// updateAllUsersSelectedSourceFromLocal2 更新所有用户的SelectedSource从local到upload
func updateAllUsersSelectedSourceFromLocal2(redisClient RedisClient) error {
	log.Println("Updating all users' SelectedSource from 'local' to 'upload'...")

	// 获取所有market settings的键
	keys, err := redisClient.Keys("market:settings:*")
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// 获取用户设置
		data, err := redisClient.Get(key)
		if err != nil {
			log.Printf("Failed to get settings for key %s: %v", key, err)
			continue
		}

		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			log.Printf("Failed to unmarshal settings for key %s: %v", key, err)
			continue
		}

		// 检查是否需要更新
		if settings.SelectedSource == "local" {
			settings.SelectedSource = "upload"

			// 更新设置
			updatedData, err := json.Marshal(settings)
			if err != nil {
				log.Printf("Failed to marshal updated settings for key %s: %v", key, err)
				continue
			}

			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				log.Printf("Failed to update settings for key %s: %v", key, err)
				continue
			}

			updatedCount++
			log.Printf("Updated user settings for key %s: local -> upload", key)
		}
	}

	if updatedCount == 0 {
		log.Println("No users found with 'local' as SelectedSource")
	} else {
		log.Printf("Updated %d users' SelectedSource from 'local' to 'upload'", updatedCount)
	}

	return nil
}

// updateMarketSourceConfigFromLocal2 更新MarketSource配置，将local改为upload
func updateMarketSourceConfigFromLocal2(redisClient RedisClient) error {
	log.Println("Updating market source configuration from 'local' to 'upload'...")

	// 获取market sources配置
	configData, err := redisClient.Get("market:sources:config")
	if err != nil {
		if err.Error() == "key not found" {
			log.Println("No market sources config found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false
	// 查找并更新local源
	for _, source := range config.Sources {
		if source.ID == "local" {
			log.Printf("Found deprecated market source: %s (%s), updating to 'upload'", source.Name, source.ID)
			source.ID = "upload"
			source.Name = "upload"
			source.UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		log.Println("No deprecated market sources found, configuration is up to date")
		return nil
	}

	// 保存更新后的配置
	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}

	if err := redisClient.Set("market:sources:config", string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	log.Println("Successfully updated market source configuration from 'local' to 'upload'")
	return nil
}
