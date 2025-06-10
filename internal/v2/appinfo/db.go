package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

	// Get all source keys for this user
	sourceKeys, err := r.client.Keys(r.ctx, fmt.Sprintf("appinfo:user:%s:source:*", userID)).Result()
	if err != nil {
		return userData, err
	}

	for _, sourceKey := range sourceKeys {
		// Extract source ID from key
		// Use strings.SplitN to handle sourceIDs that contain colons
		expectedPrefix := fmt.Sprintf("appinfo:user:%s:source:", userID)
		if !strings.HasPrefix(sourceKey, expectedPrefix) {
			glog.Errorf("Invalid source key format: %s", sourceKey)
			continue
		}
		sourceID := sourceKey[len(expectedPrefix):]

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
			sourceData.AppStateLatest = state
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
	// 注意：Other数据现在是AppInfoLatestPendingData.Others字段的一部分
	// 传统的"other"数据加载已被移除，因为它现在是Others结构的一部分

	return sourceData, nil
}

// SaveUserDataToRedis saves user data to Redis
func (r *RedisClient) SaveUserDataToRedis(userID string, userData *UserData) error {
	glog.Infof("Saving user data to Redis for user: %s", userID)

	userData.Mutex.RLock()
	defer userData.Mutex.RUnlock()

	for sourceID, sourceData := range userData.Sources {
		if err := r.SaveSourceDataToRedis(userID, sourceID, sourceData); err != nil {
			return err
		}
	}

	// Set user marker
	userKey := fmt.Sprintf("appinfo:user:%s", userID)
	err := r.client.Set(r.ctx, userKey, "1", 0).Err()
	if err != nil {
		glog.Errorf("Failed to set user marker in Redis: %v", err)
		return err
	}

	return nil
}

// SaveSourceDataToRedis saves source data to Redis
func (r *RedisClient) SaveSourceDataToRedis(userID, sourceID string, sourceData *SourceData) error {
	sourceData.Mutex.RLock()
	defer sourceData.Mutex.RUnlock()

	baseKey := fmt.Sprintf("appinfo:user:%s:source:%s", userID, sourceID)

	// Save app-info-history
	if len(sourceData.AppInfoHistory) > 0 {
		historyJSON, err := json.Marshal(sourceData.AppInfoHistory)
		if err == nil {
			r.client.Set(r.ctx, baseKey+":app-info-history", historyJSON, 0)
		}
	}

	// Save app-state-latest
	if len(sourceData.AppStateLatest) > 0 {
		stateJSON, err := json.Marshal(sourceData.AppStateLatest)
		if err == nil {
			r.client.Set(r.ctx, baseKey+":app-state-latest", stateJSON, 0)
		}
	}

	// Save app-info-latest
	if len(sourceData.AppInfoLatest) > 0 {
		infoJSON, err := json.Marshal(sourceData.AppInfoLatest)
		if err == nil {
			r.client.Set(r.ctx, baseKey+":app-info-latest", infoJSON, 0)
		}
	}

	// Save app-info-latest-pending
	if len(sourceData.AppInfoLatestPending) > 0 {
		infoPendingJSON, err := json.Marshal(sourceData.AppInfoLatestPending)
		if err == nil {
			r.client.Set(r.ctx, baseKey+":app-info-latest-pending", infoPendingJSON, 0)
		}
	}

	// Note: Other data is now part of AppInfoLatestPendingData.Others field
	// Legacy "other" data saving is removed as it's now part of the Others structure
	// 注意：Other数据现在是AppInfoLatestPendingData.Others字段的一部分
	// 传统的"other"数据保存已被移除，因为它现在是Others结构的一部分

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
