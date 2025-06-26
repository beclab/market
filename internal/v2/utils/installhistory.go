package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/go-redis/redis/v8"
)

// Global Redis client instance for download tracking
var globalRedisClient *redis.Client

// SetRedisClient sets the global Redis client instance
func SetRedisClient(redisClient *redis.Client) {
	globalRedisClient = redisClient
	log.Printf("Install History: Redis client set successfully")
}

// DownloadRecord represents a download record stored in Redis
type DownloadRecord struct {
	Source  string `json:"source"`
	Version string `json:"version"`
}

// saveDownloadRecord saves download record to Redis
func saveDownloadRecord(userID, appName, source, version string) error {
	if globalRedisClient == nil {
		log.Printf("Install History: Redis client not available, skipping download record save")
		return nil
	}

	// Create download record
	record := DownloadRecord{
		Source:  source,
		Version: version,
	}

	// Marshal to JSON
	recordJSON, err := json.Marshal(record)
	if err != nil {
		log.Printf("Install History: Failed to marshal download record: %v", err)
		return err
	}

	// Create Redis key: user_appname
	redisKey := fmt.Sprintf("%s_%s", userID, appName)

	// Save to Redis with no expiration (persistent storage)
	ctx := context.Background()
	err = globalRedisClient.Set(ctx, redisKey, recordJSON, 0).Err()
	if err != nil {
		log.Printf("Install History: Failed to save download record to Redis: %v", err)
		return err
	}

	log.Printf("Install History: Saved download record to Redis - Key: %s, Source: %s, Version: %s",
		redisKey, source, version)
	return nil
}

// getDownloadRecord retrieves download record from Redis
func getDownloadRecord(userID, appName string) (*DownloadRecord, error) {
	if globalRedisClient == nil {
		log.Printf("Install History: Redis client not available, cannot retrieve download record")
		return nil, fmt.Errorf("Redis client not available")
	}

	// Create Redis key: user_appname
	redisKey := fmt.Sprintf("%s_%s", userID, appName)

	// Get from Redis
	ctx := context.Background()
	recordJSON, err := globalRedisClient.Get(ctx, redisKey).Result()
	if err != nil {
		if err == redis.Nil {
			log.Printf("Install History: No download record found for key: %s", redisKey)
			return nil, nil // Return nil record, not error
		}
		log.Printf("Install History: Failed to get download record from Redis: %v", err)
		return nil, err
	}

	// Unmarshal JSON
	var record DownloadRecord
	err = json.Unmarshal([]byte(recordJSON), &record)
	if err != nil {
		log.Printf("Install History: Failed to unmarshal download record: %v", err)
		return nil, err
	}

	log.Printf("Install History: Retrieved download record from Redis - Key: %s, Source: %s, Version: %s",
		redisKey, record.Source, record.Version)
	return &record, nil
}

// GetDownloadRecord retrieves download record from Redis (public function)
func GetDownloadRecord(userID, appName string) (*DownloadRecord, error) {
	return getDownloadRecord(userID, appName)
}

// GetAppVersionFromDownloadRecord retrieves app version from download record
func GetAppVersionFromDownloadRecord(userID, appName string) (string, error) {
	record, err := getDownloadRecord(userID, appName)
	if err != nil {
		return "", err
	}
	if record != nil {
		return record.Version, nil
	}
	return "", nil
}

// SaveDownloadRecord saves download record to Redis (public function)
func SaveDownloadRecord(userID, appName, source, version string) error {
	return saveDownloadRecord(userID, appName, source, version)
}
