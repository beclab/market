package utils

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"context"

	"market/internal/v2/helper"

	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
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

// UpgradeFlow performs the upgrade flow - executed as a pre-startup function before the program runs
// This function does not depend on any other components and runs before program initialization
func UpgradeFlow() error {
	glog.Info("=== Starting upgrade flow ===")

	// 1. Check and update market source configuration
	if err := checkAndUpdateMarketSourceConfig(); err != nil {
		glog.Errorf("Failed to check and update market source config: %v", err)
		return err
	}

	// 2. Check and update cache data
	if err := checkAndUpdateCacheData(); err != nil {
		glog.Errorf("Failed to check and update cache data: %v", err)
		return err
	}

	glog.Info("=== Upgrade flow completed successfully ===")
	return nil
}

// checkAndUpdateMarketSourceConfig checks and updates the market source configuration
func checkAndUpdateMarketSourceConfig() error {
	glog.Info("Checking and updating market source configuration...")

	if helper.IsPublicEnvironment() {
		glog.Info("Public environment detected, skipping market source config update")
		return nil
	}

	// Create Redis client
	redisClient, err := createRedisClient()
	if err != nil {
		glog.Errorf("Failed to create Redis client: %v, skipping market source config update", err)
		return nil
	}

	// 1. Update SelectedSource for all users (Official-Market-Sources -> market.olares)
	if err := updateAllUsersSelectedSource(redisClient); err != nil {
		glog.Errorf("Failed to update users' SelectedSource: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 1.x Clean up the 'default' source in the configuration (handled once here)
	if err := removeDefaultSourceInConfig(redisClient); err != nil {
		glog.Errorf("Failed to remove 'default' source from config: %v", err)
	}

	// 1.1 Update SelectedSource for all users (market-local -> upload)
	if err := updateAllUsersSelectedSourceFromLocal(redisClient); err != nil {
		glog.Errorf("Failed to update users' SelectedSource from local: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 1.2 Update SelectedSource for all users (dev-local -> studio)
	if err := updateAllUsersSelectedSourceFromDevLocal(redisClient); err != nil {
		glog.Errorf("Failed to update users' SelectedSource from dev-local: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 2. Update MarketSource configuration (Official-Market-Sources -> market.olares)
	if err := updateMarketSourceConfig(redisClient); err != nil {
		glog.Errorf("Failed to update market source config: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 3. Update MarketSource configuration (market-local -> upload)
	if err := updateMarketSourceConfigFromLocal(redisClient); err != nil {
		glog.Errorf("Failed to update market source config from local: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 4. Update MarketSource configuration (dev-local -> studio)
	if err := updateMarketSourceConfigFromDevLocal(redisClient); err != nil {
		glog.Errorf("Failed to update market source config from dev-local: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 5. Update SelectedSource for all users (local -> upload)
	if err := updateAllUsersSelectedSourceFromLocal2(redisClient); err != nil {
		glog.Errorf("Failed to update users' SelectedSource from local: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	// 6. Update MarketSource configuration (local -> upload)
	if err := updateMarketSourceConfigFromLocal2(redisClient); err != nil {
		glog.Errorf("Failed to update market source config from local: %v", err)
		// Do not return the error; continue with other upgrade steps
	}

	glog.Info("Market source configuration check completed")
	return nil
}

// checkAndUpdateCacheData checks and updates cache data
func checkAndUpdateCacheData() error {
	glog.Info("Checking and updating cache data...")

	if helper.IsPublicEnvironment() {
		glog.Info("Public environment detected, skipping cache data update")
		return nil
	}

	// Create Redis client
	redisClient, err := createRedisClient()
	if err != nil {
		glog.Errorf("Failed to create Redis client: %v, skipping cache data update", err)
		return nil
	}

	// Additional: migrate source data in the appinfo keyspace (old -> new) and clean up old keys
	if err := migrateAppinfoSources(redisClient, "Official-Market-Sources", "market.olares"); err != nil {
		glog.Errorf("Failed to migrate appinfo sources from 'Official-Market-Sources' to 'market.olares': %v", err)
	}
	if err := migrateAppinfoSources(redisClient, "market-local", "upload"); err != nil {
		glog.Errorf("Failed to migrate appinfo sources from 'market-local' to 'upload': %v", err)
	}
	if err := migrateAppinfoSources(redisClient, "dev-local", "studio"); err != nil {
		glog.Errorf("Failed to migrate appinfo sources from 'dev-local' to 'studio': %v", err)
	}
	if err := migrateAppinfoSources(redisClient, "local", "upload"); err != nil {
		glog.Errorf("Failed to migrate appinfo sources from 'local' to 'upload': %v", err)
	}
	if err := removePreOlaresAppStateLatest(redisClient); err != nil {
		glog.Errorf("Failed to remove last olares-app states key: %v", err)
	}

	deprecatedMap := map[string]string{
		"official-market-sources": "market.olares",
		"market-local":            "upload",
		"dev-local":               "studio",
		"local":                   "upload",
	}
	if err := migrateAppinfoSourcesAuto(redisClient, deprecatedMap); err != nil {
		glog.Errorf("Failed to auto-migrate appinfo sources by normalization: %v", err)
	}

	glog.Info("Cache data check completed")
	return nil
}

// migrateAppinfoSources migrates a source in the appinfo keyspace to a new source
// It migrates appinfo:user:<uid>:source:<old>:(app-info-history|app-state-latest|app-info-latest|app-info-latest-pending|app-render-failed)
// to the corresponding new source keys and deletes the old keys after merging.
func migrateAppinfoSources(redisClient RedisClient, oldSourceID, newSourceID string) error {
	if redisClient == nil {
		glog.Info("Redis client not available, skipping appinfo source migration")
		return nil
	}
	if oldSourceID == "" || newSourceID == "" || oldSourceID == newSourceID {
		return nil
	}

	glog.Infof("Migrating appinfo sources from '%s' to '%s'...", oldSourceID, newSourceID)

	// Scan all keys and filter precisely in the program to avoid relying on Redis MATCH
	keys, err := redisClient.ScanAllKeys("*", 5000)
	if err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}
	glog.Infof("Found %d keys to migrate", len(keys))

	// Supported data segment suffixes
	dataSuffixes := []string{
		":app-info-history", ":app_state_history",
		":app-state-latest", ":app_state_latest",
		":app-info-latest", ":app_info_latest",
		":app-info-latest-pending", ":app_info_latest_pending",
		":app-render-failed", ":app_render_failed",
	}

	migratedUsers := make(map[string]bool)
	for _, oldKey := range keys {
		// Limit to the appinfo keyspace and keys containing the specified old source segment
		if !strings.HasPrefix(oldKey, "appinfo:user:") {
			continue
		}
		if !strings.Contains(oldKey, ":source:"+oldSourceID+":") {
			continue
		}
		// oldKey looks like appinfo:user:<uid>:source:<oldSourceID>[:suffix]
		// Compute the base new-key prefix
		newBase := strings.Replace(oldKey, ":source:"+oldSourceID, ":source:"+newSourceID, 1)

		// Check whether it is a concrete data segment key; otherwise skip
		isDataKey := false
		for _, suffix := range dataSuffixes {
			if strings.HasSuffix(oldKey, suffix) {
				isDataKey = true
				break
			}
		}
		if !isDataKey {
			// May be a user marker, etc.; skip
			continue
		}

		// Read the old value
		val, err := redisClient.Get(oldKey)
		if err != nil {
			continue
		}

		// Merge strategy: if the new key has a value, merge as JSON (array concat with dedup); otherwise write directly
		// Determine the new key name first
		newKey := newBase
		// Read the new value (may not exist)
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

		// Delete the old key
		if err := redisClient.Del(oldKey); err != nil {
			glog.Errorf("Warning: failed to delete old key %s: %v", oldKey, err)
		}

		// Mark this user as migrated
		// Extract userID
		// Looks like appinfo:user:<uid>:source:...
		parts := strings.Split(oldKey, ":")
		if len(parts) >= 3 {
			migratedUsers[parts[2]] = true
		}
	}

	glog.Infof("Migrated appinfo sources for %d users: %s -> %s", len(migratedUsers), oldSourceID, newSourceID)
	return nil
}

func removePreOlaresAppStateLatest(redisClient RedisClient) error {
	if redisClient == nil {
		glog.Info("Redis client not available, skipping system app states remove")
		return nil
	}

	glog.Infof("Remove last version system app states key")

	keys, err := redisClient.ScanAllKeys("appinfo:user:*:source::app-state-latest", 500)
	if err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}
	glog.Infof("Found %d keys to remove", len(keys))

	for _, removeKey := range keys {
		err := redisClient.Del(removeKey)
		if err != nil {
			glog.Errorf("Warning: failed to delete old key %s: %v", removeKey, err)
			continue
		} else {
			glog.Infof("Remove last version system app states key: %s", removeKey)
		}
	}

	return nil
}

func mergeJSONArraysIfNeeded(key string, existing, incoming string) string {
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

// normalizeSourceID normalizes a source ID (lowercase + trim leading/trailing whitespace)
func normalizeSourceID(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// migrateAppinfoSourcesAuto scans the appinfo keyspace and migrates based on the normalized source-name mapping
func migrateAppinfoSourcesAuto(redisClient RedisClient, mapping map[string]string) error {
	if redisClient == nil || len(mapping) == 0 {
		return nil
	}

	glog.Info("Auto-migrating appinfo sources by normalized mapping...")

	// Only scan data keys containing the app* segment (compatible with app-*/app_*) to avoid non-data keys, using SCAN
	keys, err := redisClient.ScanAllKeys("appinfo:user:*:source:*:app*", 2000)
	if err != nil {
		return fmt.Errorf("failed to list appinfo data keys: %w", err)
	}
	if len(keys) == 0 {
		glog.Info("No appinfo data keys found for auto migration")
		return nil
	}

	migratedUsers := make(map[string]bool)
	for _, oldKey := range keys {
		parts := strings.Split(oldKey, ":")
		// Expected format: appinfo:user:<uid>:source:<sourceID>:<segment>
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

		// Build the new key (only replace :source:<old>: with surrounding colons to avoid partial matches)
		needle := ":source:" + sourceID + ":"
		replacement := ":source:" + newSourceID + ":"
		if !strings.Contains(oldKey, needle) {
			continue
		}
		newKey := strings.Replace(oldKey, needle, replacement, 1)

		// Read the old value
		oldVal, err := redisClient.Get(oldKey)
		if err != nil {
			continue
		}

		// Read the new value and merge (array JSON is merged)
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

		// Delete the old key
		if err := redisClient.Del(oldKey); err != nil {
			glog.Errorf("Warning: failed to delete old key %s: %v", oldKey, err)
		}

		migratedUsers[userID] = true
	}

	if len(migratedUsers) > 0 {
		glog.Infof("Auto-migrated appinfo sources for %d users by normalization", len(migratedUsers))
	} else {
		glog.Info("No appinfo sources required auto migration by normalization")
	}
	return nil
}

// removeDefaultSourceInConfig reads and removes sources whose id == "default" from the market sources configuration
func removeDefaultSourceInConfig(redisClient RedisClient) error {
	if redisClient == nil {
		return nil
	}

	configKey := "market:sources:config"
	data, err := redisClient.Get(configKey)
	if err != nil {
		if err.Error() == "key not found" {
			// Configuration does not exist; skip
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	if len(config.Sources) == 0 {
		return nil
	}

	filtered := make([]*MarketSource, 0, len(config.Sources))
	removed := false
	for _, src := range config.Sources {
		if src != nil && src.ID == "default" {
			removed = true
			continue
		}
		filtered = append(filtered, src)
	}

	if !removed {
		return nil
	}

	config.Sources = filtered
	if config.DefaultSource == "default" {
		config.DefaultSource = ""
	}
	config.UpdatedAt = time.Now()

	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}
	if err := redisClient.Set(configKey, string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	glog.Info("Removed deprecated source with id 'default'")
	return nil
}

// createRedisClient creates a Redis client
func createRedisClient() (RedisClient, error) {
	// Get Redis connection parameters
	redisHost := helper.GetEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := helper.GetEnvOrDefault("REDIS_PORT", "6379")
	redisPassword := helper.GetEnvOrDefault("REDIS_PASSWORD", "")
	redisDBStr := helper.GetEnvOrDefault("REDIS_DB", "0")
	redisDB, err := strconv.Atoi(redisDBStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_DB value: %w", err)
	}

	glog.Errorf("Creating Redis client for upgrade flow: %s:%s, DB: %d", redisHost, redisPort, redisDB)

	// Create the real Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisHost, redisPort),
		Password: redisPassword,
		DB:       redisDB,
	})

	// Test the connection
	ctx := context.Background()
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	glog.Info("Redis client connected successfully")

	// Return the wrapped Redis client
	return &RedisClientWrapper{
		client: rdb,
		ctx:    ctx,
	}, nil
}

// RedisClientWrapper wraps a Redis client and implements the real Redis operations
type RedisClientWrapper struct {
	client *redis.Client
	ctx    context.Context
}

// Get retrieves the value for a Redis key
func (c *RedisClientWrapper) Get(key string) (string, error) {
	result, err := c.client.Get(c.ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found")
	}
	return result, err
}

// Set sets the value for a Redis key
func (c *RedisClientWrapper) Set(key string, value interface{}, expiration time.Duration) error {
	return c.client.Set(c.ctx, key, value, expiration).Err()
}

// Keys returns the list of keys matching a pattern
func (c *RedisClientWrapper) Keys(pattern string) ([]string, error) {
	return c.client.Keys(c.ctx, pattern).Result()
}

// Del deletes one or more keys
func (c *RedisClientWrapper) Del(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(c.ctx, keys...).Err()
}

// ScanAllKeys collects all matching keys by iterating with the SCAN cursor
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

// updateAllUsersSelectedSource updates the SelectedSource for all users
func updateAllUsersSelectedSource(redisClient RedisClient) error {
	if redisClient == nil {
		glog.Info("Redis client not available, skipping user settings update")
		return nil
	}

	glog.Info("Updating all users' SelectedSource from 'Official-Market-Sources' to 'market.olares'...")

	// Get all user settings keys
	pattern := "market:settings:*"
	keys, err := redisClient.Keys(pattern)
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// Get the current settings
		data, err := redisClient.Get(key)
		if err != nil {
			if err.Error() == "key not found" {
				continue
			}
			return fmt.Errorf("failed to get settings for key %s: %w", key, err)
		}

		// Parse the settings
		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			return fmt.Errorf("failed to unmarshal settings for key %s: %w", key, err)
		}

		// Check whether an update is needed
		if settings.SelectedSource == "Official-Market-Sources" {
			settings.SelectedSource = "market.olares"

			// Serialize the updated settings
			updatedData, err := json.Marshal(settings)
			if err != nil {
				return fmt.Errorf("failed to marshal updated settings for key %s: %w", key, err)
			}

			// Save back to Redis
			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				return fmt.Errorf("failed to save updated settings for key %s: %w", key, err)
			}

			updatedCount++
		}
	}

	if updatedCount > 0 {
		glog.Infof("Updated %d users' SelectedSource from 'Official-Market-Sources' to 'market.olares'", updatedCount)
	} else {
		glog.Info("No users found with 'Official-Market-Sources' as SelectedSource")
	}

	return nil
}

// updateMarketSourceConfig updates the MarketSource configuration
func updateMarketSourceConfig(redisClient RedisClient) error {
	if redisClient == nil {
		glog.Info("Redis client not available, skipping market source config update")
		return nil
	}

	glog.Info("Updating market source configuration...")

	// Get the market sources configuration
	configKey := "market:sources:config"
	data, err := redisClient.Get(configKey)
	if err != nil {
		if err.Error() == "key not found" {
			glog.Error("No market sources configuration found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	// Parse the configuration
	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false

	// Update deprecated entries in sources
	for _, source := range config.Sources {
		if source.ID == "Official-Market-Sources" {
			glog.Infof("Updating deprecated source: %s -> market.olares", source.ID)
			source.ID = "market.olares"
			source.Name = "market.olares"
			updated = true
		}
	}

	// Update default source
	if config.DefaultSource == "Official-Market-Sources" {
		glog.Infof("Updating default source: %s -> market.olares", config.DefaultSource)
		config.DefaultSource = "market.olares"
		updated = true
	}

	if updated {
		// Update timestamp
		config.UpdatedAt = time.Now()

		// Serialize the updated configuration
		updatedData, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("failed to marshal updated market sources config: %w", err)
		}

		// Save back to Redis
		if err := redisClient.Set(configKey, string(updatedData), 0); err != nil {
			return fmt.Errorf("failed to save updated market sources config: %w", err)
		}

		glog.Info("Market source configuration updated successfully")
	} else {
		glog.Info("No deprecated market sources found, configuration is up to date")
	}

	return nil
}

// updateAllUsersSelectedSourceFromLocal updates the SelectedSource for all users from market-local to upload
func updateAllUsersSelectedSourceFromLocal(redisClient RedisClient) error {
	glog.Info("Updating all users' SelectedSource from 'market-local' to 'upload'...")

	// Get all market settings keys
	keys, err := redisClient.Keys("market:settings:*")
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// Get the user settings
		data, err := redisClient.Get(key)
		if err != nil {
			glog.Errorf("Failed to get settings for key %s: %v", key, err)
			continue
		}

		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			glog.Errorf("Failed to unmarshal settings for key %s: %v", key, err)
			continue
		}

		// Check whether an update is needed
		if settings.SelectedSource == "market-local" {
			settings.SelectedSource = "upload"

			// Update the settings
			updatedData, err := json.Marshal(settings)
			if err != nil {
				glog.Errorf("Failed to marshal updated settings for key %s: %v", key, err)
				continue
			}

			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				glog.Errorf("Failed to update settings for key %s: %v", key, err)
				continue
			}

			updatedCount++
			glog.Infof("Updated user settings for key %s: market-local -> upload", key)
		}
	}

	if updatedCount == 0 {
		glog.Info("No users found with 'market-local' as SelectedSource")
	} else {
		glog.Infof("Updated %d users' SelectedSource from 'market-local' to 'upload'", updatedCount)
	}

	return nil
}

// updateMarketSourceConfigFromLocal updates the MarketSource configuration, changing market-local to upload
func updateMarketSourceConfigFromLocal(redisClient RedisClient) error {
	glog.Info("Updating market source configuration from 'market-local' to 'upload'...")

	// Get the market sources configuration
	configData, err := redisClient.Get("market:sources:config")
	if err != nil {
		if err.Error() == "key not found" {
			glog.Error("No market sources config found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false
	// Find and update the market-local source
	for _, source := range config.Sources {
		if source.ID == "market-local" {
			glog.Infof("Found deprecated market source: %s (%s), updating to 'upload'", source.Name, source.ID)
			source.ID = "upload"
			source.Name = "upload"
			source.UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		glog.Info("No deprecated market sources found, configuration is up to date")
		return nil
	}

	// Save the updated configuration
	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}

	if err := redisClient.Set("market:sources:config", string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	glog.Info("Successfully updated market source configuration from 'market-local' to 'upload'")
	return nil
}

// updateAllUsersSelectedSourceFromDevLocal updates the SelectedSource for all users from dev-local to studio
func updateAllUsersSelectedSourceFromDevLocal(redisClient RedisClient) error {
	glog.Info("Updating all users' SelectedSource from 'dev-local' to 'studio'...")

	// Get all market settings keys
	keys, err := redisClient.Keys("market:settings:*")
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// Get the user settings
		data, err := redisClient.Get(key)
		if err != nil {
			glog.Errorf("Failed to get settings for key %s: %v", key, err)
			continue
		}

		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			glog.Errorf("Failed to unmarshal settings for key %s: %v", key, err)
			continue
		}

		// Check whether an update is needed
		if settings.SelectedSource == "dev-local" {
			settings.SelectedSource = "studio"

			// Update the settings
			updatedData, err := json.Marshal(settings)
			if err != nil {
				glog.Errorf("Failed to marshal updated settings for key %s: %v", key, err)
				continue
			}

			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				glog.Errorf("Failed to update settings for key %s: %v", key, err)
				continue
			}

			updatedCount++
			glog.Infof("Updated user settings for key %s: dev-local -> studio", key)
		}
	}

	if updatedCount == 0 {
		glog.Info("No users found with 'dev-local' as SelectedSource")
	} else {
		glog.Infof("Updated %d users' SelectedSource from 'dev-local' to 'studio'", updatedCount)
	}

	return nil
}

// updateMarketSourceConfigFromDevLocal updates the MarketSource configuration, changing dev-local to studio
func updateMarketSourceConfigFromDevLocal(redisClient RedisClient) error {
	glog.Info("Updating market source configuration from 'dev-local' to 'studio'...")

	// Get the market sources configuration
	configData, err := redisClient.Get("market:sources:config")
	if err != nil {
		if err.Error() == "key not found" {
			glog.Error("No market sources config found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false
	// Find and update the dev-local source
	for _, source := range config.Sources {
		if source.ID == "dev-local" {
			glog.Infof("Found deprecated market source: %s (%s), updating to 'studio'", source.Name, source.ID)
			source.ID = "studio"
			source.Name = "studio"
			source.UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		glog.Info("No deprecated market sources found, configuration is up to date")
		return nil
	}

	// Save the updated configuration
	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}

	if err := redisClient.Set("market:sources:config", string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	glog.Info("Successfully updated market source configuration from 'dev-local' to 'studio'")
	return nil
}

// updateAllUsersSelectedSourceFromLocal2 updates the SelectedSource for all users from local to upload
func updateAllUsersSelectedSourceFromLocal2(redisClient RedisClient) error {
	glog.Info("Updating all users' SelectedSource from 'local' to 'upload'...")

	// Get all market settings keys
	keys, err := redisClient.Keys("market:settings:*")
	if err != nil {
		return fmt.Errorf("failed to get market settings keys: %w", err)
	}

	updatedCount := 0
	for _, key := range keys {
		// Get the user settings
		data, err := redisClient.Get(key)
		if err != nil {
			glog.Errorf("Failed to get settings for key %s: %v", key, err)
			continue
		}

		var settings MarketSettings
		if err := json.Unmarshal([]byte(data), &settings); err != nil {
			glog.Errorf("Failed to unmarshal settings for key %s: %v", key, err)
			continue
		}

		// Check whether an update is needed
		if settings.SelectedSource == "local" {
			settings.SelectedSource = "upload"

			// Update the settings
			updatedData, err := json.Marshal(settings)
			if err != nil {
				glog.Errorf("Failed to marshal updated settings for key %s: %v", key, err)
				continue
			}

			if err := redisClient.Set(key, string(updatedData), 0); err != nil {
				glog.Errorf("Failed to update settings for key %s: %v", key, err)
				continue
			}

			updatedCount++
			glog.Infof("Updated user settings for key %s: local -> upload", key)
		}
	}

	if updatedCount == 0 {
		glog.Info("No users found with 'local' as SelectedSource")
	} else {
		glog.Infof("Updated %d users' SelectedSource from 'local' to 'upload'", updatedCount)
	}

	return nil
}

// updateMarketSourceConfigFromLocal2 updates the MarketSource configuration, changing local to upload
func updateMarketSourceConfigFromLocal2(redisClient RedisClient) error {
	glog.Info("Updating market source configuration from 'local' to 'upload'...")

	// Get the market sources configuration
	configData, err := redisClient.Get("market:sources:config")
	if err != nil {
		if err.Error() == "key not found" {
			glog.Error("No market sources config found, skipping update")
			return nil
		}
		return fmt.Errorf("failed to get market sources config: %w", err)
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(configData), &config); err != nil {
		return fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	updated := false
	// Find and update the local source
	for _, source := range config.Sources {
		if source.ID == "local" {
			glog.Infof("Found deprecated market source: %s (%s), updating to 'upload'", source.Name, source.ID)
			source.ID = "upload"
			source.Name = "upload"
			source.UpdatedAt = time.Now()
			updated = true
			break
		}
	}

	if !updated {
		glog.Info("No deprecated market sources found, configuration is up to date")
		return nil
	}

	// Save the updated configuration
	updatedData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated market sources config: %w", err)
	}

	if err := redisClient.Set("market:sources:config", string(updatedData), 0); err != nil {
		return fmt.Errorf("failed to save updated market sources config: %w", err)
	}

	glog.Info("Successfully updated market source configuration from 'local' to 'upload'")
	return nil
}
