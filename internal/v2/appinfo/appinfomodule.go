package appinfo

import (
	"context"
	"fmt"
	"market/internal/v2/settings"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"market/internal/v2/utils"

	"github.com/golang/glog"
)

// AppInfoModule represents the main application info module
// AppInfoModule 代表主要的应用信息模块
type AppInfoModule struct {
	config       *ModuleConfig
	cacheManager *CacheManager
	redisClient  *RedisClient
	syncer       *Syncer
	hydrator     *Hydrator
	ctx          context.Context
	cancel       context.CancelFunc
	mutex        sync.RWMutex
	isStarted    bool
}

// ModuleConfig holds configuration for the AppInfo module
// ModuleConfig 保存 AppInfo 模块的配置
type ModuleConfig struct {
	Redis          *RedisConfig    `json:"redis"`
	Syncer         *SyncerConfig   `json:"syncer"`
	Cache          *CacheConfig    `json:"cache"`
	User           *UserConfig     `json:"user"`
	Hydrator       *HydratorConfig `json:"hydrator"`
	EnableSync     bool            `json:"enable_sync"`
	EnableCache    bool            `json:"enable_cache"`
	EnableHydrator bool            `json:"enable_hydrator"`
	StartTimeout   time.Duration   `json:"start_timeout"`
}

// CacheConfig holds cache-specific configuration
// CacheConfig 保存缓存相关的配置
type CacheConfig struct {
	SyncBufferSize int           `json:"sync_buffer_size"`
	ForceSync      bool          `json:"force_sync"`
	SyncTimeout    time.Duration `json:"sync_timeout"`
}

// UserConfig holds user-specific configuration
// UserConfig 保存用户相关的配置
type UserConfig struct {
	UserList              []string      `json:"user_list"`
	AdminList             []string      `json:"admin_list"`
	DefaultRole           string        `json:"default_role"`
	GuestEnabled          bool          `json:"guest_enabled"`
	DataRetentionDays     int           `json:"data_retention_days"`
	MaxSourcesPerUser     int           `json:"max_sources_per_user"`
	CacheExpiryHours      int           `json:"cache_expiry_hours"`
	SessionTimeout        time.Duration `json:"session_timeout"`
	MaxConcurrentSessions int           `json:"max_concurrent_sessions"`
	AuthEnabled           bool          `json:"auth_enabled"`
	AuthTimeout           time.Duration `json:"auth_timeout"`
	DefaultPermissions    []string      `json:"default_permissions"`
}

// RedisClientAdapter adapts appinfo.RedisClient to settings.RedisClient interface
// RedisClientAdapter 将 appinfo.RedisClient 适配为 settings.RedisClient 接口
type RedisClientAdapter struct {
	client *RedisClient
}

// Get implements settings.RedisClient.Get
func (r *RedisClientAdapter) Get(key string) (string, error) {
	return r.client.client.Get(r.client.ctx, key).Result()
}

// Set implements settings.RedisClient.Set
func (r *RedisClientAdapter) Set(key string, value interface{}, expiration time.Duration) error {
	return r.client.client.Set(r.client.ctx, key, value, expiration).Err()
}

// HSet implements settings.RedisClient.HSet
func (r *RedisClientAdapter) HSet(key string, fields map[string]interface{}) error {
	return r.client.client.HSet(r.client.ctx, key, fields).Err()
}

// HGetAll implements settings.RedisClient.HGetAll
func (r *RedisClientAdapter) HGetAll(key string) (map[string]string, error) {
	return r.client.client.HGetAll(r.client.ctx, key).Result()
}

// NewAppInfoModule creates a new AppInfo module instance
// NewAppInfoModule 创建一个新的 AppInfo 模块实例
func NewAppInfoModule(config *ModuleConfig) (*AppInfoModule, error) {
	if config == nil {
		config = DefaultModuleConfig()
	}

	// Validate configuration
	if err := validateConfig(config); err != nil {
		glog.Errorf("Invalid module configuration: %v", err)
		return nil, fmt.Errorf("invalid module configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	module := &AppInfoModule{
		config:    config,
		ctx:       ctx,
		cancel:    cancel,
		isStarted: false,
	}

	glog.Infof("AppInfo module created successfully")
	return module, nil
}

// Start initializes and starts all module components
// Start 初始化并启动所有模块组件
func (m *AppInfoModule) Start() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.isStarted {
		return fmt.Errorf("module is already started")
	}

	glog.Infof("Starting AppInfo module...")

	// Initialize Redis client if enabled
	if m.config.EnableCache {
		if err := m.initRedisClient(); err != nil {
			return fmt.Errorf("failed to initialize Redis client: %w", err)
		}
	}

	// Initialize cache manager
	if m.config.EnableCache {
		if err := m.initCacheManager(); err != nil {
			return fmt.Errorf("failed to initialize cache manager: %w", err)
		}
	}

	// Initialize syncer if enabled
	if m.config.EnableSync {
		if err := m.initSyncer(); err != nil {
			return fmt.Errorf("failed to initialize syncer: %w", err)
		}
	}

	// Initialize hydrator if enabled
	if m.config.EnableHydrator {
		if err := m.initHydrator(); err != nil {
			return fmt.Errorf("failed to initialize hydrator: %w", err)
		}
	}

	// Set up hydration notifier connection if both cache and hydrator are enabled
	if m.config.EnableCache && m.config.EnableHydrator && m.cacheManager != nil && m.hydrator != nil {
		m.cacheManager.SetHydrationNotifier(m.hydrator)
		glog.Infof("Hydration notifier connection established between cache manager and hydrator")
	}

	m.isStarted = true
	glog.Infof("AppInfo module started successfully")
	return nil
}

// Stop gracefully shuts down the module
// Stop 优雅地关闭模块
func (m *AppInfoModule) Stop() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.isStarted {
		return nil
	}

	glog.Infof("Stopping AppInfo module...")

	// Stop syncer
	if m.syncer != nil {
		m.syncer.Stop()
		glog.Infof("Syncer stopped")
	}

	// Stop hydrator
	if m.hydrator != nil {
		m.hydrator.Stop()
		glog.Infof("Hydrator stopped")
	}

	// Stop cache manager
	if m.cacheManager != nil {
		m.cacheManager.Stop()
		glog.Infof("Cache manager stopped")
	}

	// Close Redis client
	if m.redisClient != nil {
		if err := m.redisClient.Close(); err != nil {
			glog.Errorf("Failed to close Redis client: %v", err)
		} else {
			glog.Infof("Redis client closed")
		}
	}

	// Cancel context
	m.cancel()

	m.isStarted = false
	glog.Infof("AppInfo module stopped successfully")
	return nil
}

// GetCacheManager returns the cache manager instance
// GetCacheManager 返回缓存管理器实例
func (m *AppInfoModule) GetCacheManager() *CacheManager {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.cacheManager
}

// GetSyncer returns the syncer instance
// GetSyncer 返回同步器实例
func (m *AppInfoModule) GetSyncer() *Syncer {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.syncer
}

// GetHydrator returns the hydrator instance
// GetHydrator 返回水合器实例
func (m *AppInfoModule) GetHydrator() *Hydrator {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.hydrator
}

// GetRedisClient returns the Redis client instance
// GetRedisClient 返回 Redis 客户端实例
func (m *AppInfoModule) GetRedisClient() *RedisClient {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.redisClient
}

// IsStarted returns whether the module is currently running
// IsStarted 返回模块是否正在运行
func (m *AppInfoModule) IsStarted() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.isStarted
}

// GetModuleStatus returns the current status of the module and all components
// GetModuleStatus 返回模块和所有组件的当前状态
func (m *AppInfoModule) GetModuleStatus() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	status := map[string]interface{}{
		"is_started":      m.isStarted,
		"enable_sync":     m.config.EnableSync,
		"enable_cache":    m.config.EnableCache,
		"enable_hydrator": m.config.EnableHydrator,
		"components": map[string]interface{}{
			"redis_client":  m.redisClient != nil,
			"cache_manager": m.cacheManager != nil,
			"syncer":        m.syncer != nil,
			"hydrator":      m.hydrator != nil,
		},
	}

	// Add cache manager status
	if m.cacheManager != nil {
		status["cache_stats"] = m.cacheManager.GetCacheStats()
	}

	// Add syncer status
	if m.syncer != nil {
		status["syncer_running"] = m.syncer.IsRunning()
	}

	// Add hydrator status
	// 添加水合器状态
	if m.hydrator != nil {
		status["hydrator_running"] = m.hydrator.IsRunning()
		status["hydrator_metrics"] = m.hydrator.GetMetrics()
	}

	return status
}

// initRedisClient initializes the Redis client
// initRedisClient 初始化 Redis 客户端
func (m *AppInfoModule) initRedisClient() error {
	glog.Infof("Initializing Redis client...")

	client, err := NewRedisClient(m.config.Redis)
	if err != nil {
		return fmt.Errorf("failed to create Redis client: %w", err)
	}

	m.redisClient = client
	glog.Infof("Redis client initialized successfully")
	return nil
}

// initCacheManager initializes the cache manager
// initCacheManager 初始化缓存管理器
func (m *AppInfoModule) initCacheManager() error {
	glog.Infof("Initializing cache manager...")

	if m.redisClient == nil {
		return fmt.Errorf("Redis client is required for cache manager")
	}

	m.cacheManager = NewCacheManager(m.redisClient, m.config.User)

	// Start cache manager
	if err := m.cacheManager.Start(); err != nil {
		return fmt.Errorf("failed to start cache manager: %w", err)
	}

	glog.Infof("Cache manager initialized successfully")
	return nil
}

// initSyncer initializes the syncer
// initSyncer 初始化同步器
func (m *AppInfoModule) initSyncer() error {
	glog.Infof("Initializing syncer...")

	if m.cacheManager == nil {
		return fmt.Errorf("cache manager is required for syncer")
	}

	// Get the actual cache data from cache manager instead of creating a new one
	// 从缓存管理器获取实际的缓存数据，而不是创建新的
	cacheData := m.cacheManager.cache

	// Create settings manager for syncer
	// 为同步器创建设置管理器
	redisAdapter := &RedisClientAdapter{client: m.redisClient}
	settingsManager := settings.NewSettingsManager(redisAdapter)
	if err := settingsManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize settings manager: %w", err)
	}

	m.syncer = CreateDefaultSyncer(cacheData, *m.config.Syncer, settingsManager)

	// Set cache manager reference for hydration notifications
	// 设置缓存管理器引用以进行水合通知
	if m.cacheManager != nil {
		m.syncer.SetCacheManager(m.cacheManager)
		glog.Infof("Cache manager reference set in syncer for hydration notifications")
	}

	// Start syncer
	if err := m.syncer.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start syncer: %w", err)
	}

	glog.Infof("Syncer initialized successfully")
	return nil
}

// initHydrator initializes the hydrator
// initHydrator 初始化水合器
func (m *AppInfoModule) initHydrator() error {
	glog.Infof("Initializing hydrator...")

	if m.cacheManager == nil {
		return fmt.Errorf("cache manager is required for hydrator")
	}

	// Get the actual cache data from cache manager
	// 从缓存管理器获取实际的缓存数据
	cacheData := m.cacheManager.cache

	// Create settings manager for hydrator
	// 为水合器创建设置管理器
	redisAdapter := &RedisClientAdapter{client: m.redisClient}
	settingsManager := settings.NewSettingsManager(redisAdapter)
	if err := settingsManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize settings manager: %w", err)
	}

	// Use hydrator config from module config, or default if not specified
	// 使用模块配置中的水合器配置，如果未指定则使用默认配置
	hydratorConfig := DefaultHydratorConfig()
	if m.config.Hydrator != nil {
		hydratorConfig = *m.config.Hydrator
	}

	m.hydrator = NewHydrator(cacheData, settingsManager, hydratorConfig)

	// Start hydrator with context
	// 使用上下文启动水合器
	if err := m.hydrator.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start hydrator: %w", err)
	}

	glog.Infof("Hydrator initialized successfully")
	return nil
}

// validateConfig validates the module configuration
// validateConfig 验证模块配置
func validateConfig(config *ModuleConfig) error {
	if config.EnableCache && config.Redis == nil {
		return fmt.Errorf("Redis configuration is required when cache is enabled")
	}

	if config.EnableSync && config.Syncer == nil {
		return fmt.Errorf("Syncer configuration is required when sync is enabled")
	}

	if config.StartTimeout <= 0 {
		config.StartTimeout = 30 * time.Second
	}

	// Validate user configuration
	if config.User != nil {
		if len(config.User.UserList) == 0 {
			return fmt.Errorf("user list cannot be empty")
		}

		if config.User.DataRetentionDays <= 0 {
			return fmt.Errorf("data retention days must be positive")
		}

		if config.User.MaxSourcesPerUser <= 0 {
			return fmt.Errorf("max sources per user must be positive")
		}

		if config.User.CacheExpiryHours <= 0 {
			return fmt.Errorf("cache expiry hours must be positive")
		}

		if config.User.SessionTimeout <= 0 {
			return fmt.Errorf("session timeout must be positive")
		}

		if config.User.MaxConcurrentSessions <= 0 {
			return fmt.Errorf("max concurrent sessions must be positive")
		}

		if config.User.AuthTimeout <= 0 {
			return fmt.Errorf("auth timeout must be positive")
		}
	}

	return nil
}

// DefaultModuleConfig returns a default module configuration
// DefaultModuleConfig 返回默认的模块配置
func DefaultModuleConfig() *ModuleConfig {
	// Parse Redis configuration from environment variables
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort, err := strconv.Atoi(os.Getenv("REDIS_PORT"))
	if err != nil || redisPort == 0 {
		redisPort = 6379
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisDB, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil || redisDB < 0 {
		redisDB = 0
	}

	redisTimeout, err := time.ParseDuration(os.Getenv("REDIS_TIMEOUT"))
	if err != nil || redisTimeout <= 0 {
		redisTimeout = 5 * time.Second
	}

	// Parse Syncer configuration from environment variables
	remoteHashURL := os.Getenv("SYNCER_REMOTE_HASH_URL")
	if remoteHashURL == "" {
		remoteHashURL = "http://localhost:8080/api/hash"
	}

	remoteDataURL := os.Getenv("SYNCER_REMOTE_DATA_URL")
	if remoteDataURL == "" {
		remoteDataURL = "http://localhost:8080/api/data"
	}

	detailURLTemplate := os.Getenv("SYNCER_DETAIL_URL_TEMPLATE")
	if detailURLTemplate == "" {
		detailURLTemplate = "http://localhost:8080/api/detail/%s"
	}

	syncInterval, err := time.ParseDuration(os.Getenv("SYNCER_SYNC_INTERVAL"))
	if err != nil || syncInterval <= 0 {
		syncInterval = 5 * time.Minute
	}

	// Parse Cache configuration from environment variables
	syncBufferSize, err := strconv.Atoi(os.Getenv("CACHE_SYNC_BUFFER_SIZE"))
	if err != nil || syncBufferSize <= 0 {
		syncBufferSize = 1000
	}

	forceSync, err := strconv.ParseBool(os.Getenv("CACHE_FORCE_SYNC"))
	if err != nil {
		forceSync = false
	}

	syncTimeout, err := time.ParseDuration(os.Getenv("CACHE_SYNC_TIMEOUT"))
	if err != nil || syncTimeout <= 0 {
		syncTimeout = 10 * time.Second
	}

	// Parse Module configuration from environment variables
	enableSync, err := strconv.ParseBool(os.Getenv("MODULE_ENABLE_SYNC"))
	if err != nil {
		enableSync = true
	}

	enableCache, err := strconv.ParseBool(os.Getenv("MODULE_ENABLE_CACHE"))
	if err != nil {
		enableCache = true
	}

	enableHydrator, err := strconv.ParseBool(os.Getenv("MODULE_ENABLE_HYDRATOR"))
	if err != nil {
		enableHydrator = true
	}

	startTimeout, err := time.ParseDuration(os.Getenv("MODULE_START_TIMEOUT"))
	if err != nil || startTimeout <= 0 {
		startTimeout = 30 * time.Second
	}

	// Parse Hydrator configuration from environment variables
	// 从环境变量解析水合器配置
	hydratorQueueSize, err := strconv.Atoi(os.Getenv("HYDRATOR_QUEUE_SIZE"))
	if err != nil || hydratorQueueSize <= 0 {
		hydratorQueueSize = 1000
	}

	hydratorWorkerCount, err := strconv.Atoi(os.Getenv("HYDRATOR_WORKER_COUNT"))
	if err != nil || hydratorWorkerCount <= 0 {
		hydratorWorkerCount = 5
	}

	// Parse User configuration from environment variables
	// 从环境变量解析用户配置
	userListStr := os.Getenv("USER_LIST")
	var userList []string
	if userListStr != "" {
		userList = strings.Split(userListStr, ",")
		// Trim spaces from user IDs
		for i, user := range userList {
			userList[i] = strings.TrimSpace(user)
		}
	} else {
		userList = []string{"admin", "user1", "user2", "test_user"}
	}

	adminListStr := os.Getenv("USER_ADMIN_LIST")
	var adminList []string
	if adminListStr != "" {
		adminList = strings.Split(adminListStr, ",")
		// Trim spaces from admin IDs
		for i, admin := range adminList {
			adminList[i] = strings.TrimSpace(admin)
		}
	} else {
		adminList = []string{"admin"}
	}

	defaultRole := os.Getenv("USER_DEFAULT_ROLE")
	if defaultRole == "" {
		defaultRole = "user"
	}

	guestEnabled, err := strconv.ParseBool(os.Getenv("USER_GUEST_ENABLED"))
	if err != nil {
		guestEnabled = false
	}

	dataRetentionDays, err := strconv.Atoi(os.Getenv("USER_DATA_RETENTION_DAYS"))
	if err != nil || dataRetentionDays <= 0 {
		dataRetentionDays = 30
	}

	maxSourcesPerUser, err := strconv.Atoi(os.Getenv("USER_MAX_SOURCES_PER_USER"))
	if err != nil || maxSourcesPerUser <= 0 {
		maxSourcesPerUser = 10
	}

	cacheExpiryHours, err := strconv.Atoi(os.Getenv("USER_CACHE_EXPIRY_HOURS"))
	if err != nil || cacheExpiryHours <= 0 {
		cacheExpiryHours = 24
	}

	sessionTimeout, err := time.ParseDuration(os.Getenv("USER_SESSION_TIMEOUT"))
	if err != nil || sessionTimeout <= 0 {
		sessionTimeout = 3600 * time.Second
	}

	maxConcurrentSessions, err := strconv.Atoi(os.Getenv("USER_MAX_CONCURRENT_SESSIONS"))
	if err != nil || maxConcurrentSessions <= 0 {
		maxConcurrentSessions = 5
	}

	authEnabled, err := strconv.ParseBool(os.Getenv("USER_AUTH_ENABLED"))
	if err != nil {
		authEnabled = false
	}

	authTimeout, err := time.ParseDuration(os.Getenv("USER_AUTH_TIMEOUT"))
	if err != nil || authTimeout <= 0 {
		authTimeout = 300 * time.Second
	}

	defaultPermissionsStr := os.Getenv("USER_DEFAULT_PERMISSIONS")
	var defaultPermissions []string
	if defaultPermissionsStr != "" {
		defaultPermissions = strings.Split(defaultPermissionsStr, ",")
		// Trim spaces from permissions
		for i, perm := range defaultPermissions {
			defaultPermissions[i] = strings.TrimSpace(perm)
		}
	} else {
		defaultPermissions = []string{"read", "write"}
	}

	return &ModuleConfig{
		Redis: &RedisConfig{
			Host:     redisHost,
			Port:     redisPort,
			Password: redisPassword,
			DB:       redisDB,
			Timeout:  redisTimeout,
		},
		Syncer: &SyncerConfig{
			SyncInterval: syncInterval,
		},
		Cache: &CacheConfig{
			SyncBufferSize: syncBufferSize,
			ForceSync:      forceSync,
			SyncTimeout:    syncTimeout,
		},
		Hydrator: &HydratorConfig{
			QueueSize:   hydratorQueueSize,
			WorkerCount: hydratorWorkerCount,
		},
		User: &UserConfig{
			UserList:              userList,
			AdminList:             adminList,
			DefaultRole:           defaultRole,
			GuestEnabled:          guestEnabled,
			DataRetentionDays:     dataRetentionDays,
			MaxSourcesPerUser:     maxSourcesPerUser,
			CacheExpiryHours:      cacheExpiryHours,
			SessionTimeout:        sessionTimeout,
			MaxConcurrentSessions: maxConcurrentSessions,
			AuthEnabled:           authEnabled,
			AuthTimeout:           authTimeout,
			DefaultPermissions:    defaultPermissions,
		},
		EnableSync:     enableSync,
		EnableCache:    enableCache,
		EnableHydrator: enableHydrator,
		StartTimeout:   startTimeout,
	}
}

// GetDockerImageInfo 是一个便捷函数，用于获取 Docker 镜像信息
// GetDockerImageInfo is a convenience function to get Docker image information
func (m *AppInfoModule) GetDockerImageInfo(imageName string) (*utils.DockerImageInfo, error) {
	return utils.GetDockerImageInfo(imageName)
}

// GetLayerDownloadProgress 是一个便捷函数，用于获取层下载进度
// GetLayerDownloadProgress is a convenience function to get layer download progress
func (m *AppInfoModule) GetLayerDownloadProgress(layerDigest string) (*utils.LayerInfo, error) {
	return utils.GetLayerDownloadProgress(layerDigest)
}

// SetAppData 是一个便捷函数，用于设置应用数据
// SetAppData is a convenience function to set app data
func (m *AppInfoModule) SetAppData(userID, sourceID string, dataType AppDataType, data map[string]interface{}) error {
	if !m.isStarted || m.cacheManager == nil {
		return fmt.Errorf("module is not started or cache manager is not available")
	}
	return m.cacheManager.SetAppData(userID, sourceID, dataType, data)
}

// GetAppData 是一个便捷函数，用于获取应用数据
// GetAppData is a convenience function to get app data
func (m *AppInfoModule) GetAppData(userID, sourceID string, dataType AppDataType) interface{} {
	if !m.isStarted || m.cacheManager == nil {
		return nil
	}
	return m.cacheManager.GetAppData(userID, sourceID, dataType)
}

// GetUserConfig returns the user configuration
// GetUserConfig 返回用户配置
func (m *AppInfoModule) GetUserConfig() *UserConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.config.User
}

// IsValidUser checks if a user ID is in the configured user list
// IsValidUser 检查用户ID是否在配置的用户列表中
func (m *AppInfoModule) IsValidUser(userID string) bool {
	if m.config.User == nil {
		return false
	}

	for _, user := range m.config.User.UserList {
		if user == userID {
			return true
		}
	}
	return false
}

// IsAdminUser checks if a user ID is in the admin list
// IsAdminUser 检查用户ID是否在管理员列表中
func (m *AppInfoModule) IsAdminUser(userID string) bool {
	if m.config.User == nil {
		return false
	}

	for _, admin := range m.config.User.AdminList {
		if admin == userID {
			return true
		}
	}
	return false
}

// GetMaxSourcesForUser returns the maximum number of sources allowed per user
// GetMaxSourcesForUser 返回每个用户允许的最大源数量
func (m *AppInfoModule) GetMaxSourcesForUser(userID string) int {
	if m.config.User == nil {
		return 10 // default value
	}

	// Admin users might have different limits in the future
	if m.IsAdminUser(userID) {
		return m.config.User.MaxSourcesPerUser * 2 // Admins get double the limit
	}

	return m.config.User.MaxSourcesPerUser
}

// ValidateUserAccess validates if a user can access the system
// ValidateUserAccess 验证用户是否可以访问系统
func (m *AppInfoModule) ValidateUserAccess(userID string) error {
	if m.config.User == nil {
		return fmt.Errorf("user configuration not available")
	}

	// Check if user is in the valid user list
	if !m.IsValidUser(userID) {
		// Check if guest access is enabled
		if !m.config.User.GuestEnabled {
			return fmt.Errorf("user '%s' is not authorized to access the system", userID)
		}
		glog.Infof("Guest user '%s' granted access", userID)
	}

	return nil
}

// GetUserRole returns the role for a given user
// GetUserRole 返回给定用户的角色
func (m *AppInfoModule) GetUserRole(userID string) string {
	if m.config.User == nil {
		return "unknown"
	}

	if m.IsAdminUser(userID) {
		return "admin"
	}

	if m.IsValidUser(userID) {
		return m.config.User.DefaultRole
	}

	if m.config.User.GuestEnabled {
		return "guest"
	}

	return "unauthorized"
}

// GetUserPermissions returns the permissions for a given user
// GetUserPermissions 返回给定用户的权限
func (m *AppInfoModule) GetUserPermissions(userID string) []string {
	if m.config.User == nil {
		return []string{}
	}

	role := m.GetUserRole(userID)
	switch role {
	case "admin":
		return []string{"read", "write", "delete", "admin"}
	case "guest":
		return []string{"read"}
	case "user":
		return m.config.User.DefaultPermissions
	default:
		return []string{}
	}
}

// UpdateUserConfig updates the user configuration for the module and cache manager
// UpdateUserConfig 更新模块和缓存管理器的用户配置
func (m *AppInfoModule) UpdateUserConfig(newUserConfig *UserConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.isStarted {
		return fmt.Errorf("module is not started")
	}

	if newUserConfig == nil {
		return fmt.Errorf("user config cannot be nil")
	}

	glog.Infof("Updating module user configuration")

	// Update module configuration
	m.config.User = newUserConfig

	// Update cache manager configuration if available
	if m.cacheManager != nil {
		if err := m.cacheManager.UpdateUserConfig(newUserConfig); err != nil {
			return fmt.Errorf("failed to update cache manager user config: %w", err)
		}
	}

	glog.Infof("Module user configuration updated successfully")
	return nil
}

// SyncUserListToCache synchronizes the current user list to cache
// SyncUserListToCache 将当前用户列表同步到缓存
func (m *AppInfoModule) SyncUserListToCache() error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if !m.isStarted {
		return fmt.Errorf("module is not started")
	}

	if m.cacheManager == nil {
		return fmt.Errorf("cache manager is not available")
	}

	return m.cacheManager.SyncUserListToCache()
}

// RefreshUserDataStructures ensures all configured users have proper data structures
// RefreshUserDataStructures 确保所有配置的用户都有适当的数据结构
func (m *AppInfoModule) RefreshUserDataStructures() error {
	if !m.isStarted {
		return fmt.Errorf("module is not started")
	}

	glog.Infof("Refreshing user data structures")

	// First sync user list to cache
	if err := m.SyncUserListToCache(); err != nil {
		return fmt.Errorf("failed to sync user list to cache: %w", err)
	}

	// Force sync to Redis to ensure persistence
	if m.cacheManager != nil {
		if err := m.cacheManager.ForceSync(); err != nil {
			glog.Warningf("Failed to force sync after refreshing user data structures: %v", err)
		}
	}

	glog.Infof("User data structures refreshed successfully")
	return nil
}

// GetConfiguredUsers returns the list of configured users
// GetConfiguredUsers 返回配置的用户列表
func (m *AppInfoModule) GetConfiguredUsers() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.config.User == nil {
		return []string{}
	}

	// Return a copy to prevent external modification
	users := make([]string, len(m.config.User.UserList))
	copy(users, m.config.User.UserList)
	return users
}

// GetCachedUsers returns the list of users currently in cache
// GetCachedUsers 返回当前在缓存中的用户列表
func (m *AppInfoModule) GetCachedUsers() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if !m.isStarted || m.cacheManager == nil {
		return []string{}
	}

	allUsersData := m.cacheManager.GetAllUsersData()
	users := make([]string, 0, len(allUsersData))
	for userID := range allUsersData {
		users = append(users, userID)
	}

	return users
}
