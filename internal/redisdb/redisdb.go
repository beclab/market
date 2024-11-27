package redisdb

import (
	"os"
	"strconv"

	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
	"golang.org/x/net/context"
)

type Client struct {
	rdb *redis.Client
}

const (
	LocalDevAppsDbName     = "localDevApps"
	LocalDevNsfwBucketName = "LocalDevNsfwBucket"

	NsfwKey = "nsfw"
)

var (
	redisClient *Client
	ctx         = context.Background()
)

func Init() error {
	// Initialize Redis client
	redisDbNumberStr := os.Getenv("REDIS_DB_NUMBER")

	redisDbNumber, errAtoi := strconv.Atoi(redisDbNumberStr)
	if errAtoi != nil {
		glog.Errorf("Error converting REDIS_DB_NUMBER to int:", errAtoi)
		return errAtoi
	}

	redisClient = &Client{
		rdb: redis.NewClient(&redis.Options{
			Addr:     os.Getenv("REDIS_ADDRESS"),  // Redis server address
			Password: os.Getenv("REDIS_PASSWORD"), // No password set
			DB:       redisDbNumber,               // Use default DB
		}),
	}

	// Test connection
	_, err := redisClient.rdb.Ping(ctx).Result()
	if err != nil {
		glog.Errorf("Redis connection error: %s", err.Error())
		return err
	}

	// Create buckets (Redis does not have buckets, but we can use keys)
	return nil
}
