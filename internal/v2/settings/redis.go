package settings

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisClientImpl implements the RedisClient interface using go-redis
type RedisClientImpl struct {
	client *redis.Client
	ctx    context.Context
}

// NewRedisClient creates a new Redis client instance
func NewRedisClient(host, port, password string, db int) (*RedisClientImpl, error) {
	addr := fmt.Sprintf("%s:%s", host, port)

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx := context.Background()

	// Test the connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Printf("Connected to Redis at %s", addr)

	return &RedisClientImpl{
		client: rdb,
		ctx:    ctx,
	}, nil
}

// Get retrieves a value from Redis
func (r *RedisClientImpl) Get(key string) (string, error) {
	val, err := r.client.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found")
	}
	return val, err
}

// Set stores a value in Redis
func (r *RedisClientImpl) Set(key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(r.ctx, key, value, expiration).Err()
}

// HSet stores hash fields in Redis
func (r *RedisClientImpl) HSet(key string, fields map[string]interface{}) error {
	return r.client.HSet(r.ctx, key, fields).Err()
}

// HGetAll retrieves all hash fields from Redis
func (r *RedisClientImpl) HGetAll(key string) (map[string]string, error) {
	result, err := r.client.HGetAll(r.ctx, key).Result()
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("key not found or empty")
	}

	return result, nil
}

// Del deletes a key from Redis
func (r *RedisClientImpl) Del(key string) error {
	return r.client.Del(r.ctx, key).Err()
}

// Close closes the Redis connection
func (r *RedisClientImpl) Close() error {
	return r.client.Close()
}

// GetRawClient returns the underlying *redis.Client instance
func (r *RedisClientImpl) GetRawClient() *redis.Client {
	return r.client
}
