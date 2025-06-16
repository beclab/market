package settings

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// NewSettingsManager creates a new settings manager instance
// 创建新的设置管理器实例
func NewSettingsManager(redisClient RedisClient) *SettingsManager {
	return &SettingsManager{
		redisClient: redisClient,
	}
}

// Initialize initializes the settings manager
// 初始化设置管理器
func (sm *SettingsManager) Initialize() error {
	log.Println("Initializing settings manager...")

	// Initialize market sources
	// 初始化市场源
	if err := sm.initializeMarketSources(); err != nil {
		return fmt.Errorf("failed to initialize market sources: %w", err)
	}

	// Initialize API endpoints
	// 初始化API端点
	if err := sm.initializeAPIEndpoints(); err != nil {
		return fmt.Errorf("failed to initialize API endpoints: %w", err)
	}

	return nil
}

// initializeMarketSources initializes market sources configuration
// 初始化市场源配置
func (sm *SettingsManager) initializeMarketSources() error {
	// Try to load from Redis first
	// 首先尝试从Redis加载
	config, err := sm.loadMarketSourcesFromRedis()
	if err != nil {
		log.Printf("Failed to load market sources from Redis: %v", err)

		// Create default configuration from environment variables
		// 从环境变量创建默认配置
		config = sm.createDefaultMarketSources()

		// Save default config to Redis
		// 将默认配置保存到Redis
		if err := sm.saveMarketSourcesToRedis(config); err != nil {
			log.Printf("Failed to save Official Market Sources to Redis: %v", err)
		}

		log.Printf("Loaded Official Market Sources from environment")
	} else {
		log.Printf("Loaded market sources from Redis: %d sources", len(config.Sources))
	}

	// Set in memory
	// 设置到内存中
	sm.mu.Lock()
	sm.marketSources = config
	sm.mu.Unlock()

	return nil
}

// initializeAPIEndpoints initializes API endpoints configuration
// 初始化API端点配置
func (sm *SettingsManager) initializeAPIEndpoints() error {
	// Try to load from Redis first
	// 首先尝试从Redis加载
	config, err := sm.loadAPIEndpointsFromRedis()
	if err != nil {
		log.Printf("Failed to load API endpoints from Redis: %v", err)

		// Create default configuration from environment variables
		// 从环境变量创建默认配置
		config = sm.createDefaultAPIEndpoints()

		// Save default config to Redis
		// 将默认配置保存到Redis
		if err := sm.saveAPIEndpointsToRedis(config); err != nil {
			log.Printf("Failed to save default API endpoints to Redis: %v", err)
		}

		log.Printf("Loaded default API endpoints from environment")
	} else {
		log.Printf("Loaded API endpoints from Redis")
	}

	// Set in memory
	// 设置到内存中
	sm.mu.Lock()
	sm.apiEndpoints = config
	sm.mu.Unlock()

	return nil
}

// createDefaultMarketSources creates Official Market Sources from environment
// 从环境变量创建默认市场源
func (sm *SettingsManager) createDefaultMarketSources() *MarketSourcesConfig {
	baseURL := os.Getenv("SYNCER_REMOTE")
	if baseURL == "" {
		baseURL = "https://appstore-server-prod.bttcdn.com"
	}

	// Remove trailing slash
	// 移除末尾的斜杠
	baseURL = strings.TrimSuffix(baseURL, "/")

	defaultSource := &MarketSource{
		ID:          "default",
		Name:        "Official-Market-Sources",
		BaseURL:     baseURL,
		Priority:    100,
		IsActive:    true,
		UpdatedAt:   time.Now(),
		Description: "Official Market Sources loaded from environment",
	}

	return &MarketSourcesConfig{
		Sources:       []*MarketSource{defaultSource},
		DefaultSource: "default",
		UpdatedAt:     time.Now(),
	}
}

// createDefaultAPIEndpoints creates default API endpoints from environment
// 从环境变量创建默认API端点
func (sm *SettingsManager) createDefaultAPIEndpoints() *APIEndpointsConfig {
	hashPath := os.Getenv("API_HASH_PATH")
	if hashPath == "" {
		hashPath = "/api/v1/appstore/hash"
	}

	dataPath := os.Getenv("API_DATA_PATH")
	if dataPath == "" {
		dataPath = "/api/v1/appstore/info"
	}

	detailPath := os.Getenv("API_DETAIL_PATH")
	if detailPath == "" {
		detailPath = "/api/v1/applications/info"
	}

	return &APIEndpointsConfig{
		HashPath:   hashPath,
		DataPath:   dataPath,
		DetailPath: detailPath,
		UpdatedAt:  time.Now(),
	}
}

// GetMarketSources gets all market sources
// 获取所有市场源
func (sm *SettingsManager) GetMarketSources() *MarketSourcesConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.marketSources == nil {
		return nil
	}

	// Return a deep copy to prevent external modification
	// 返回深拷贝以防止外部修改
	sources := make([]*MarketSource, len(sm.marketSources.Sources))
	for i, src := range sm.marketSources.Sources {
		sources[i] = &MarketSource{
			ID:          src.ID,
			Name:        src.Name,
			BaseURL:     src.BaseURL,
			Priority:    src.Priority,
			IsActive:    src.IsActive,
			UpdatedAt:   src.UpdatedAt,
			Description: src.Description,
		}
	}

	return &MarketSourcesConfig{
		Sources:       sources,
		DefaultSource: sm.marketSources.DefaultSource,
		UpdatedAt:     sm.marketSources.UpdatedAt,
	}
}

// GetActiveMarketSources gets all active market sources sorted by priority
// 获取所有活跃的市场源，按优先级排序
func (sm *SettingsManager) GetActiveMarketSources() []*MarketSource {
	config := sm.GetMarketSources()
	if config == nil {
		return nil
	}

	var activeSources []*MarketSource
	for _, src := range config.Sources {
		if src.IsActive {
			activeSources = append(activeSources, src)
		}
	}

	// Sort by priority (descending)
	// 按优先级排序（降序）
	for i := 0; i < len(activeSources)-1; i++ {
		for j := i + 1; j < len(activeSources); j++ {
			if activeSources[i].Priority < activeSources[j].Priority {
				activeSources[i], activeSources[j] = activeSources[j], activeSources[i]
			}
		}
	}

	return activeSources
}

// GetDefaultMarketSource gets the Official Market Sources
// 获取默认市场源
func (sm *SettingsManager) GetDefaultMarketSource() *MarketSource {
	config := sm.GetMarketSources()
	if config == nil {
		return nil
	}

	// Find default source by ID
	// 通过ID查找默认源
	for _, source := range config.Sources {
		if source.ID == config.DefaultSource {
			return &MarketSource{
				ID:          source.ID,
				Name:        source.Name,
				BaseURL:     source.BaseURL,
				Priority:    source.Priority,
				IsActive:    source.IsActive,
				UpdatedAt:   source.UpdatedAt,
				Description: source.Description,
			}
		}
	}

	// If no default found, return first active source
	// 如果没有找到默认源，返回第一个活跃源
	for _, source := range config.Sources {
		if source.IsActive {
			return &MarketSource{
				ID:          source.ID,
				Name:        source.Name,
				BaseURL:     source.BaseURL,
				Priority:    source.Priority,
				IsActive:    source.IsActive,
				UpdatedAt:   source.UpdatedAt,
				Description: source.Description,
			}
		}
	}

	return nil
}

// GetMarketSource gets a single market source (for API compatibility)
// 获取单个市场源（为了API兼容性）
func (sm *SettingsManager) GetMarketSource() *MarketSource {
	return sm.GetDefaultMarketSource()
}

// SetMarketSource sets the Official Market Sources URL (for API compatibility)
// 设置默认市场源URL（为了API兼容性）
func (sm *SettingsManager) SetMarketSource(url string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.marketSources == nil {
		sm.marketSources = &MarketSourcesConfig{
			Sources:       []*MarketSource{},
			DefaultSource: "default",
			UpdatedAt:     time.Now(),
		}
	}

	// Update the default source or create it if it doesn't exist
	// 更新默认源或者如果不存在则创建它
	defaultUpdated := false
	for _, source := range sm.marketSources.Sources {
		if source.ID == "default" {
			source.BaseURL = strings.TrimSuffix(url, "/")
			source.UpdatedAt = time.Now()
			defaultUpdated = true
			break
		}
	}

	if !defaultUpdated {
		// Create new default source
		// 创建新的默认源
		newSource := &MarketSource{
			ID:          "default",
			Name:        "Official-Market-Sources",
			BaseURL:     strings.TrimSuffix(url, "/"),
			Priority:    100,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Official Market Sources set via API",
		}
		sm.marketSources.Sources = append(sm.marketSources.Sources, newSource)
	}

	sm.marketSources.UpdatedAt = time.Now()

	// Save to Redis
	// 保存到Redis
	return sm.saveMarketSourcesToRedis(sm.marketSources)
}

// GetAPIEndpoints gets the API endpoints configuration
// 获取API端点配置
func (sm *SettingsManager) GetAPIEndpoints() *APIEndpointsConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.apiEndpoints == nil {
		return nil
	}

	// Return a copy to prevent external modification
	// 返回副本以防止外部修改
	return &APIEndpointsConfig{
		HashPath:   sm.apiEndpoints.HashPath,
		DataPath:   sm.apiEndpoints.DataPath,
		DetailPath: sm.apiEndpoints.DetailPath,
		UpdatedAt:  sm.apiEndpoints.UpdatedAt,
	}
}

// BuildAPIURL builds a complete API URL from base URL and endpoint path
// 从基础URL和端点路径构建完整的API URL
func (sm *SettingsManager) BuildAPIURL(baseURL, endpointPath string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	endpointPath = strings.TrimPrefix(endpointPath, "/")
	return fmt.Sprintf("%s/%s", baseURL, endpointPath)
}

// AddMarketSource adds a new market source
// 添加新的市场源
func (sm *SettingsManager) AddMarketSource(source *MarketSource) error {
	if source.ID == "" {
		return fmt.Errorf("source ID cannot be empty")
	}
	if source.BaseURL == "" {
		return fmt.Errorf("source base URL cannot be empty")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if source with same ID already exists
	// 检查是否已存在相同ID的源
	for _, existing := range sm.marketSources.Sources {
		if existing.ID == source.ID {
			return fmt.Errorf("source with ID %s already exists", source.ID)
		}
	}

	// Add new source
	// 添加新源
	source.UpdatedAt = time.Now()
	sm.marketSources.Sources = append(sm.marketSources.Sources, source)
	sm.marketSources.UpdatedAt = time.Now()

	// Save to Redis
	// 保存到Redis
	if err := sm.saveMarketSourcesToRedis(sm.marketSources); err != nil {
		return fmt.Errorf("failed to save market sources to Redis: %w", err)
	}

	log.Printf("Added new market source: %s (%s)", source.Name, source.ID)
	return nil
}

// UpdateAPIEndpoints updates the API endpoints configuration
// 更新API端点配置
func (sm *SettingsManager) UpdateAPIEndpoints(endpoints *APIEndpointsConfig) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	endpoints.UpdatedAt = time.Now()
	sm.apiEndpoints = endpoints

	// Save to Redis
	// 保存到Redis
	if err := sm.saveAPIEndpointsToRedis(endpoints); err != nil {
		return fmt.Errorf("failed to save API endpoints to Redis: %w", err)
	}

	log.Printf("Updated API endpoints configuration")
	return nil
}

// loadMarketSourcesFromRedis loads market sources from Redis
// 从Redis加载市场源
func (sm *SettingsManager) loadMarketSourcesFromRedis() (*MarketSourcesConfig, error) {
	data, err := sm.redisClient.Get(RedisKeyMarketSources)
	if err != nil {
		return nil, fmt.Errorf("failed to get market sources from Redis: %w", err)
	}

	if data == "" {
		return nil, fmt.Errorf("no market sources config found in Redis")
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	return &config, nil
}

// saveMarketSourcesToRedis saves market sources to Redis
// 将市场源保存到Redis
func (sm *SettingsManager) saveMarketSourcesToRedis(config *MarketSourcesConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal market sources config: %w", err)
	}

	return sm.redisClient.Set(RedisKeyMarketSources, string(data), 0)
}

// loadAPIEndpointsFromRedis loads API endpoints from Redis
// 从Redis加载API端点
func (sm *SettingsManager) loadAPIEndpointsFromRedis() (*APIEndpointsConfig, error) {
	data, err := sm.redisClient.Get(RedisKeyAPIEndpoints)
	if err != nil {
		return nil, fmt.Errorf("failed to get API endpoints from Redis: %w", err)
	}

	if data == "" {
		return nil, fmt.Errorf("no API endpoints config found in Redis")
	}

	var config APIEndpointsConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal API endpoints config: %w", err)
	}

	return &config, nil
}

// saveAPIEndpointsToRedis saves API endpoints to Redis
// 将API端点保存到Redis
func (sm *SettingsManager) saveAPIEndpointsToRedis(config *APIEndpointsConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal API endpoints config: %w", err)
	}

	return sm.redisClient.Set(RedisKeyAPIEndpoints, string(data), 0)
}
