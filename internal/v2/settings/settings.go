package settings

import (
	"encoding/json"
	"market/internal/v2/types"
)

type MarketSettings = types.MarketSettings

// Redis key for market settings
const RedisKeyMarketSettings = "market:settings"

// getMarketSettings retrieves market settings from Redis
// This is an internal method that will be called by the manager
func getMarketSettings(redisClient RedisClient) (*MarketSettings, error) {
	// Get settings from Redis
	data, err := redisClient.Get(RedisKeyMarketSettings)
	if err != nil {
		// If key not found, return default settings
		if err.Error() == "key not found" {
			return &MarketSettings{
				SelectedSource: "market.olares", // Default empty selected source
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

// updateMarketSettings updates market settings in Redis
// This is an internal method that will be called by the manager
func updateMarketSettings(redisClient RedisClient, settings *MarketSettings) error {
	// Marshal settings to JSON
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	// Store in Redis with no expiration (persistent)
	return redisClient.Set(RedisKeyMarketSettings, string(data), 0)
}
