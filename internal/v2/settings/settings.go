package settings

import (
	"encoding/json"
	"fmt"
	"market/internal/v2/types"
	"market/internal/v2/utils"
)

type MarketSettings = types.MarketSettings

// Redis key for market settings
const RedisKeyMarketSettings = "market:settings"

// getMarketSettings retrieves market settings from Redis for a specific user
// This is an internal method that will be called by the manager
func getMarketSettings(redisClient RedisClient, userID string) (*MarketSettings, error) {
	// Create user-specific Redis key
	userRedisKey := fmt.Sprintf("%s:%s", RedisKeyMarketSettings, userID)

	if utils.IsPublicEnvironment() {
		return &MarketSettings{
			SelectedSource: "market.olares", // Default selected source
		}, nil
	}

	// Get settings from Redis
	data, err := redisClient.Get(userRedisKey)
	if err != nil {
		// If key not found, return default settings
		if err.Error() == "key not found" {
			return &MarketSettings{
				SelectedSource: "market.olares", // Default selected source
			}, nil
		}
		return nil, err
	}

	// Unmarshal JSON data
	var settings MarketSettings
	if err := json.Unmarshal([]byte(data), &settings); err != nil {
		return nil, err
	}

	return &settings, nil
}

// updateMarketSettings updates market settings in Redis for a specific user
// This is an internal method that will be called by the manager
func updateMarketSettings(redisClient RedisClient, userID string, settings *MarketSettings) error {
	// Create user-specific Redis key
	userRedisKey := fmt.Sprintf("%s:%s", RedisKeyMarketSettings, userID)

	// Marshal settings to JSON
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	// Store in Redis with no expiration (persistent)
	return redisClient.Set(userRedisKey, string(data), 0)
}
