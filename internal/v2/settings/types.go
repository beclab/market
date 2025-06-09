package settings

import (
	"sync"
	"time"
)

// MarketSource represents a single market source
// 单个应用市场源
type MarketSource struct {
	ID          string    `json:"id" redis:"id"`               // Unique identifier for the source
	Name        string    `json:"name" redis:"name"`           // Display name of the source
	BaseURL     string    `json:"base_url" redis:"base_url"`   // Base URL of the market source
	Priority    int       `json:"priority" redis:"priority"`   // Priority for selection (higher = more priority)
	IsActive    bool      `json:"is_active" redis:"is_active"` // Whether this source is active
	UpdatedAt   time.Time `json:"updated_at" redis:"updated_at"`
	Description string    `json:"description" redis:"description"` // Description of the source
}

// MarketSourcesConfig represents the market sources configuration
// 应用市场源配置结构，支持多个数据源
type MarketSourcesConfig struct {
	Sources       []*MarketSource `json:"sources" redis:"sources"`
	DefaultSource string          `json:"default_source" redis:"default_source"` // ID of default source
	UpdatedAt     time.Time       `json:"updated_at" redis:"updated_at"`
}

// APIEndpointsConfig represents the API endpoint paths configuration
// API端点路径配置
type APIEndpointsConfig struct {
	HashPath   string    `json:"hash_path" redis:"hash_path"`     // e.g., "/api/v1/appstore/hash"
	DataPath   string    `json:"data_path" redis:"data_path"`     // e.g., "/api/v1/appstore/info"
	DetailPath string    `json:"detail_path" redis:"detail_path"` // e.g., "/api/v1/applications/info"
	UpdatedAt  time.Time `json:"updated_at" redis:"updated_at"`
}

// SettingsManager manages application settings
// 设置管理器，管理应用程序设置
type SettingsManager struct {
	mu            sync.RWMutex
	marketSources *MarketSourcesConfig
	apiEndpoints  *APIEndpointsConfig
	redisClient   RedisClient
}

// RedisClient interface for Redis operations
// Redis操作接口
type RedisClient interface {
	Get(key string) (string, error)
	Set(key string, value interface{}, expiration time.Duration) error
	HSet(key string, fields map[string]interface{}) error
	HGetAll(key string) (map[string]string, error)
}

// Constants for Redis keys
// Redis键常量
const (
	RedisKeyMarketSources = "market:sources:config"
	RedisKeyAPIEndpoints  = "market:api:endpoints"
)
