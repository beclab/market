package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/types"

	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
)

// RedisClient wraps Redis client with app-specific operations
type RedisClient struct {
	client *redis.Client
	ctx    context.Context
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	Timeout  time.Duration
}

// NewRedisClient creates a new Redis client
func NewRedisClient(config *RedisConfig) (*RedisClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password:     config.Password,
		DB:           config.DB,
		DialTimeout:  config.Timeout,
		ReadTimeout:  config.Timeout,
		WriteTimeout: config.Timeout,
	})

	ctx := context.Background()

	// Test connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		glog.Errorf("Failed to connect to Redis: %v", err)
		return nil, err
	}

	glog.Infof("Connected to Redis successfully")
	return &RedisClient{
		client: rdb,
		ctx:    ctx,
	}, nil
}

// Close closes the Redis connection
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// ClearAllData clears all data from Redis
func (r *RedisClient) ClearAllData() error {
	glog.Infof("Clearing appinfo data from Redis")

	// Get all appinfo keys
	keys, err := r.client.Keys(r.ctx, "appinfo:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get Redis keys: %w", err)
	}

	// Get all settings keys
	settingsKeys, err := r.client.Keys(r.ctx, "settings:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get settings Redis keys: %w", err)
	}

	// Get market sources config key
	marketKeys, err := r.client.Keys(r.ctx, "market:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get market Redis keys: %w", err)
	}

	// Combine all keys
	allKeys := append(keys, settingsKeys...)
	allKeys = append(allKeys, marketKeys...)

	if len(allKeys) == 0 {
		glog.Infof("No data to clear in Redis")
		return nil
	}

	// Delete all keys
	_, err = r.client.Del(r.ctx, allKeys...).Result()
	if err != nil {
		return fmt.Errorf("failed to delete Redis keys: %w", err)
	}

	glog.Infof("Successfully cleared %d keys from Redis (%d appinfo keys, %d settings keys, %d market keys)",
		len(allKeys), len(keys), len(settingsKeys), len(marketKeys))
	return nil
}

// LoadCacheFromRedis loads all cache data from Redis
func (r *RedisClient) LoadCacheFromRedis() (*CacheData, error) {
	glog.Infof("Loading cache data from Redis")

	cache := NewCacheData()

	// Get all user keys
	userKeys, err := r.client.Keys(r.ctx, "appinfo:user:*").Result()
	if err != nil {
		glog.Errorf("Failed to get user keys from Redis: %v", err)
		return cache, err
	}

	for _, userKey := range userKeys {
		// Extract user ID from key (appinfo:user:userID)
		// Use strings.SplitN to handle userIDs that contain colons
		parts := strings.SplitN(userKey, ":", 3)
		if len(parts) < 3 {
			glog.Errorf("Invalid user key format: %s", userKey)
			continue
		}

		// Skip keys that are deeper than user level (contain more colons after userID)
		// We only want user marker keys like "appinfo:user:admin", not data keys like "appinfo:user:admin:source:..."
		userID := parts[2]
		if strings.Contains(userID, ":") {
			// This is a data key, not a user marker key, skip it
			continue
		}

		// Verify this is actually a user marker key by checking its value
		value, err := r.client.Get(r.ctx, userKey).Result()
		if err != nil || value != "1" {
			// Not a user marker key, skip it
			continue
		}

		// Load user data
		userData, err := r.loadUserData(userID)
		if err != nil {
			glog.Errorf("Failed to load user data for %s: %v", userID, err)
			continue
		}

		cache.Users[userID] = userData
	}

	glog.Infof("Loaded cache data for %d users from Redis", len(cache.Users))
	return cache, nil
}

// loadUserData loads all sources for a specific user
func (r *RedisClient) loadUserData(userID string) (*UserData, error) {
	userData := NewUserData()

	// Load user hash from Redis
	userHashKey := fmt.Sprintf("appinfo:user:%s:hash", userID)
	hashValue, err := r.client.Get(r.ctx, userHashKey).Result()
	if err == nil && hashValue != "" {
		userData.Hash = hashValue
		glog.Infof("Loaded user hash from Redis: user=%s, hash=%s", userID, hashValue)
	} else if err != redis.Nil {
		glog.Warningf("Failed to load user hash from Redis: user=%s, error=%v", userID, err)
	}

	// Get all source keys for this user
	sourceKeys, err := r.client.Keys(r.ctx, fmt.Sprintf("appinfo:user:%s:source:*", userID)).Result()
	if err != nil {
		return userData, err
	}

	// Group keys by source ID to handle data types properly
	sourceGroups := make(map[string][]string)
	expectedPrefix := fmt.Sprintf("appinfo:user:%s:source:", userID)

	for _, sourceKey := range sourceKeys {
		if !strings.HasPrefix(sourceKey, expectedPrefix) {
			glog.Errorf("Invalid source key format: %s", sourceKey)
			continue
		}

		// Extract the part after the prefix
		suffix := sourceKey[len(expectedPrefix):]

		// Split by colon to separate sourceID from data type
		parts := strings.SplitN(suffix, ":", 2)
		if len(parts) == 0 {
			glog.Errorf("Invalid source key suffix: %s", suffix)
			continue
		}

		sourceID := parts[0]

		// Skip if this looks like a data type key (contains common data type patterns)
		if strings.Contains(sourceID, "app-info") || strings.Contains(sourceID, "app-state") {
			glog.Warningf("Skipping data type key that was incorrectly parsed as sourceID: %s", sourceKey)
			continue
		}

		// Group keys by sourceID
		if sourceGroups[sourceID] == nil {
			sourceGroups[sourceID] = make([]string, 0)
		}
		sourceGroups[sourceID] = append(sourceGroups[sourceID], sourceKey)
	}

	// Load data for each source
	for sourceID, keys := range sourceGroups {
		glog.Infof("Loading data for source: %s with %d keys", sourceID, len(keys))

		// Load source data
		sourceData, err := r.loadSourceData(userID, sourceID)
		if err != nil {
			glog.Errorf("Failed to load source data for %s/%s: %v", userID, sourceID, err)
			continue
		}

		userData.Sources[sourceID] = sourceData
	}

	return userData, nil
}

// loadSourceData loads all app data for a specific source
func (r *RedisClient) loadSourceData(userID, sourceID string) (*SourceData, error) {
	sourceData := NewSourceData()

	// Load different types of data
	baseKey := fmt.Sprintf("appinfo:user:%s:source:%s", userID, sourceID)

	// Load app-info-history
	historyKey := baseKey + ":app-info-history"
	historyData, err := r.client.Get(r.ctx, historyKey).Result()
	if err == nil {
		var history []*AppInfoHistoryData
		if err := json.Unmarshal([]byte(historyData), &history); err == nil {
			sourceData.AppInfoHistory = history
		}
	}

	// Load app-state-latest
	stateKey := baseKey + ":app-state-latest"
	stateData, err := r.client.Get(r.ctx, stateKey).Result()
	if err == nil {
		var state []*AppStateLatestData
		if err := json.Unmarshal([]byte(stateData), &state); err == nil {
			// Filter out app states that are missing the name field or have any empty URLs
			var validStates []*AppStateLatestData
			removedCount := 0
			for _, appState := range state {
				if appState != nil && appState.Status.Name != "" {
					// Check if any entrance status has empty URL
					hasEmptyUrl := false
					for _, entrance := range appState.Status.EntranceStatuses {
						if entrance.Url == "" {
							hasEmptyUrl = true
							break
						}
					}

					if !hasEmptyUrl {
						validStates = append(validStates, appState)
					} else {
						removedCount++
						glog.V(2).Infof("Removed app state with empty URL for user=%s, source=%s, app=%s", userID, sourceID, appState.Status.Name)
					}
				} else {
					removedCount++
					glog.V(2).Infof("Removed app state with missing name field for user=%s, source=%s", userID, sourceID)
				}
			}
			sourceData.AppStateLatest = validStates
			if removedCount > 0 {
				glog.Infof("Removed %d app states with missing name field or empty URLs for user=%s, source=%s", removedCount, userID, sourceID)
			}
		}
	}

	// Load app-info-latest
	infoKey := baseKey + ":app-info-latest"
	infoData, err := r.client.Get(r.ctx, infoKey).Result()
	if err == nil {
		var info []*AppInfoLatestData
		if err := json.Unmarshal([]byte(infoData), &info); err == nil {
			sourceData.AppInfoLatest = info
		}
	}

	// Load app-info-latest-pending
	infoPendingKey := baseKey + ":app-info-latest-pending"
	infoPendingData, err := r.client.Get(r.ctx, infoPendingKey).Result()
	if err == nil {
		var infoPending []*AppInfoLatestPendingData
		if err := json.Unmarshal([]byte(infoPendingData), &infoPending); err == nil {
			sourceData.AppInfoLatestPending = infoPending
		}
	}

	// Note: Other data is now part of AppInfoLatestPendingData.Others field
	// Legacy "other" data loading is removed as it's now part of the Others structure

	return sourceData, nil
}

// SaveUserDataToRedis saves user data to Redis with optimized performance
func (r *RedisClient) SaveUserDataToRedis(userID string, userData *types.UserData) error {
	glog.Infof("Saving user data to Redis for user: %s", userID)

	// Step 1: Create a snapshot of data (no locks needed as this is called from CacheManager)
	var userHash string
	var sourcesSnapshot map[string]*types.SourceData

	userHash = userData.Hash
	sourcesSnapshot = make(map[string]*types.SourceData, len(userData.Sources))
	for sourceID, sourceData := range userData.Sources {
		sourcesSnapshot[sourceID] = sourceData
	}

	// Step 2: Prepare all Redis operations outside of locks
	pipeline := r.client.Pipeline()

	// Save user hash to Redis
	if userHash != "" {
		userHashKey := fmt.Sprintf("appinfo:user:%s:hash", userID)
		pipeline.Set(r.ctx, userHashKey, userHash, 0)
		glog.V(2).Infof("Prepared user hash for Redis: user=%s, hash=%s", userID, userHash)
	}

	// Process each source with minimal lock time
	for sourceID, sourceData := range sourcesSnapshot {
		if err := r.prepareBatchSourceDataSave(userID, sourceID, sourceData, pipeline); err != nil {
			glog.Errorf("Failed to prepare source data for batch save: user=%s, source=%s, error=%v", userID, sourceID, err)
			continue
		}
	}

	// Step 3: Execute all operations in a single pipeline
	startTime := time.Now()
	results, err := pipeline.Exec(r.ctx)
	duration := time.Since(startTime)

	if err != nil {
		glog.Errorf("Failed to execute Redis pipeline for user %s: %v", userID, err)
		return err
	}

	// Check for any failures in the pipeline
	failedOps := 0
	for i, result := range results {
		if result.Err() != nil {
			glog.Errorf("Redis pipeline operation %d failed for user %s: %v", i, userID, result.Err())
			failedOps++
		}
	}

	if failedOps > 0 {
		glog.Warningf("Redis pipeline completed with %d failed operations out of %d total for user %s", failedOps, len(results), userID)
	}

	// Set user marker
	userKey := fmt.Sprintf("appinfo:user:%s", userID)
	err = r.client.Set(r.ctx, userKey, "1", 0).Err()
	if err != nil {
		glog.Errorf("Failed to set user marker in Redis: %v", err)
		return err
	}

	glog.Infof("Successfully saved user data to Redis for user %s in %v (%d operations, %d failed)",
		userID, duration, len(results), failedOps)
	return nil
}

// prepareBatchSourceDataSave prepares source data for batch Redis operations
func (r *RedisClient) prepareBatchSourceDataSave(userID, sourceID string, sourceData *types.SourceData, pipeline redis.Pipeliner) error {
	// Create a quick snapshot of source data (no locks needed as this is called from CacheManager)
	var appInfoHistory []*types.AppInfoHistoryData
	var appStateLatest []*types.AppStateLatestData
	var appInfoLatest []*types.AppInfoLatestData
	var appInfoLatestPending []*types.AppInfoLatestPendingData

	// Copy slice references (not deep copy for performance)
	if len(sourceData.AppInfoHistory) > 0 {
		appInfoHistory = sourceData.AppInfoHistory
	}
	if len(sourceData.AppStateLatest) > 0 {
		appStateLatest = sourceData.AppStateLatest
	}
	if len(sourceData.AppInfoLatest) > 0 {
		appInfoLatest = sourceData.AppInfoLatest
	}
	if len(sourceData.AppInfoLatestPending) > 0 {
		appInfoLatestPending = sourceData.AppInfoLatestPending
	}

	baseKey := fmt.Sprintf("appinfo:user:%s:source:%s", userID, sourceID)

	// Prepare JSON serialization outside of locks
	if len(appInfoHistory) > 0 {
		historyJSON, err := json.Marshal(appInfoHistory)
		if err != nil {
			glog.Errorf("Failed to marshal app info history for user=%s, source=%s: %v", userID, sourceID, err)
		} else {
			pipeline.Set(r.ctx, baseKey+":app-info-history", historyJSON, 0)
		}
	}

	if len(appStateLatest) > 0 {
		stateJSON, err := json.Marshal(appStateLatest)
		if err != nil {
			glog.Errorf("Failed to marshal app state latest for user=%s, source=%s: %v", userID, sourceID, err)
		} else {
			pipeline.Set(r.ctx, baseKey+":app-state-latest", stateJSON, 0)
		}
	}

	if len(appInfoLatest) > 0 {
		infoJSON, err := json.Marshal(appInfoLatest)
		if err != nil {
			glog.Errorf("Failed to marshal app info latest for user=%s, source=%s: %v", userID, sourceID, err)
		} else {
			pipeline.Set(r.ctx, baseKey+":app-info-latest", infoJSON, 0)
		}
	}

	if len(appInfoLatestPending) > 0 {
		infoPendingJSON, err := json.Marshal(appInfoLatestPending)
		if err != nil {
			glog.Errorf("Failed to marshal app info latest pending for user=%s, source=%s: %v", userID, sourceID, err)
		} else {
			pipeline.Set(r.ctx, baseKey+":app-info-latest-pending", infoPendingJSON, 0)
		}
	}

	glog.V(2).Infof("Prepared batch Redis operations for user=%s, source=%s", userID, sourceID)
	return nil
}

// DeleteUserDataFromRedis removes user data from Redis
func (r *RedisClient) DeleteUserDataFromRedis(userID string) error {
	glog.Infof("Deleting user data from Redis for user: %s", userID)

	// Get all keys for this user
	pattern := fmt.Sprintf("appinfo:user:%s*", userID)
	keys, err := r.client.Keys(r.ctx, pattern).Result()
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		err = r.client.Del(r.ctx, keys...).Err()
		if err != nil {
			glog.Errorf("Failed to delete user data from Redis: %v", err)
			return err
		}
	}

	return nil
}

// SaveSourceDataToRedis saves source data to Redis (legacy method for compatibility)
func (r *RedisClient) SaveSourceDataToRedis(userID, sourceID string, sourceData *types.SourceData) error {
	// Use the optimized batch method with a single-source pipeline
	pipeline := r.client.Pipeline()
	err := r.prepareBatchSourceDataSave(userID, sourceID, sourceData, pipeline)
	if err != nil {
		return err
	}

	startTime := time.Now()
	results, err := pipeline.Exec(r.ctx)
	duration := time.Since(startTime)

	if err != nil {
		glog.Errorf("Failed to execute Redis pipeline for source data: user=%s, source=%s, error=%v", userID, sourceID, err)
		return err
	}

	// Check for any failures
	failedOps := 0
	for i, result := range results {
		if result.Err() != nil {
			glog.Errorf("Redis pipeline operation %d failed for user=%s, source=%s: %v", i, userID, sourceID, result.Err())
			failedOps++
		}
	}

	if failedOps > 0 {
		glog.Warningf("Redis pipeline completed with %d failed operations out of %d total for user=%s, source=%s", failedOps, len(results), userID, sourceID)
	} else {
		glog.V(2).Infof("Successfully saved source data to Redis for user=%s, source=%s in %v (%d operations)", userID, sourceID, duration, len(results))
	}

	return nil
}
