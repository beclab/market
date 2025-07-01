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

	"market/internal/v2/history"
	"market/internal/v2/task"
	"market/internal/v2/utils"

	"github.com/golang/glog"
)

// AppInfoModule represents the main application info module
type AppInfoModule struct {
	config           *ModuleConfig
	cacheManager     *CacheManager
	redisClient      *RedisClient
	syncer           *Syncer
	hydrator         *Hydrator
	dataWatcher      *DataWatcher
	dataWatcherState *DataWatcherState
	dataWatcherUser  *DataWatcherUser
	dataSender       *DataSender
	ctx              context.Context
	cancel           context.CancelFunc
	mutex            sync.RWMutex
	isStarted        bool
	taskModule       *task.TaskModule
	historyModule    *history.HistoryModule
}

// ModuleConfig holds configuration for the AppInfo module
type ModuleConfig struct {
	Redis                  *RedisConfig    `json:"redis"`
	Syncer                 *SyncerConfig   `json:"syncer"`
	Cache                  *CacheConfig    `json:"cache"`
	User                   *UserConfig     `json:"user"`
	Hydrator               *HydratorConfig `json:"hydrator"`
	EnableSync             bool            `json:"enable_sync"`
	EnableCache            bool            `json:"enable_cache"`
	EnableHydrator         bool            `json:"enable_hydrator"`
	EnableDataWatcher      bool            `json:"enable_data_watcher"`
	EnableDataWatcherState bool            `json:"enable_data_watcher_state"`
	EnableDataWatcherUser  bool            `json:"enable_data_watcher_user"`
	StartTimeout           time.Duration   `json:"start_timeout"`
}

// CacheConfig holds cache-specific configuration
type CacheConfig struct {
	SyncBufferSize int           `json:"sync_buffer_size"`
	ForceSync      bool          `json:"force_sync"`
	SyncTimeout    time.Duration `json:"sync_timeout"`
	ClearCache     bool          `json:"clear_cache"`
}

// UserConfig holds user-specific configuration
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
	ClearCache            bool          `json:"clear_cache"`
}

// RedisClientAdapter adapts appinfo.RedisClient to settings.RedisClient interface
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

	// Initialize DataWatcher if enabled and dependencies are available
	if m.config.EnableDataWatcher && m.config.EnableCache && m.config.EnableHydrator {
		if err := m.initDataWatcher(); err != nil {
			return fmt.Errorf("failed to initialize DataWatcher: %w", err)
		}
	}

	// Initialize DataWatcherState if enabled
	if m.config.EnableDataWatcherState {
		if err := m.initDataWatcherState(); err != nil {
			return fmt.Errorf("failed to initialize DataWatcherState: %w", err)
		}
	}

	// Initialize DataWatcherUser if enabled
	if m.config.EnableDataWatcherUser {
		if err := m.initDataWatcherUser(); err != nil {
			return fmt.Errorf("failed to initialize DataWatcherUser: %w", err)
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
func (m *AppInfoModule) Stop() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.isStarted {
		return nil
	}

	glog.Infof("Stopping AppInfo module...")

	// Stop components in reverse order
	if m.hydrator != nil {
		m.hydrator.Stop()
	}

	// Stop DataWatcher
	if m.dataWatcher != nil {
		m.dataWatcher.Stop()
	}

	// Stop DataWatcherState
	if m.dataWatcherState != nil {
		if err := m.dataWatcherState.Stop(); err != nil {
			glog.Errorf("Failed to stop DataWatcherState: %v", err)
		}
	}

	// Stop DataWatcherUser
	if m.dataWatcherUser != nil {
		m.dataWatcherUser.Stop()
		glog.Infof("DataWatcherUser stopped")
	}

	// Close DataSender
	if m.dataSender != nil {
		m.dataSender.Close()
		glog.Infof("DataSender closed")
	}

	if m.syncer != nil {
		m.syncer.Stop()
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
func (m *AppInfoModule) GetCacheManager() *CacheManager {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.cacheManager
}

// GetSyncer returns the syncer instance
func (m *AppInfoModule) GetSyncer() *Syncer {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.syncer
}

// GetHydrator returns the hydrator instance (can nil)
func (m *AppInfoModule) GetHydrator() *Hydrator {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.hydrator
}

// GetDataWatcher returns the DataWatcher instance (can nil)
func (m *AppInfoModule) GetDataWatcher() *DataWatcher {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.dataWatcher
}

// GetDataWatcherState returns the DataWatcherState instance (can nil)
func (m *AppInfoModule) GetDataWatcherState() *DataWatcherState {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.dataWatcherState
}

// GetDataWatcherUser returns the DataWatcherUser instance (can nil)
func (m *AppInfoModule) GetDataWatcherUser() *DataWatcherUser {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.dataWatcherUser
}

// GetDataSender returns the DataSender instance (can nil)
func (m *AppInfoModule) GetDataSender() *DataSender {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.dataSender
}

// GetRedisClient returns the Redis client instance
func (m *AppInfoModule) GetRedisClient() *RedisClient {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.redisClient
}

// GetRedisConfig returns the Redis configuration
func (m *AppInfoModule) GetRedisConfig() *RedisConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.config.Redis
}

// IsStarted returns whether the module is currently running
func (m *AppInfoModule) IsStarted() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.isStarted
}

// GetModuleStatus returns the current status of the module and all components
func (m *AppInfoModule) GetModuleStatus() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	status := map[string]interface{}{
		"is_started":                m.isStarted,
		"enable_sync":               m.config.EnableSync,
		"enable_cache":              m.config.EnableCache,
		"enable_hydrator":           m.config.EnableHydrator,
		"enable_data_watcher":       m.config.EnableDataWatcher,
		"enable_data_watcher_state": m.config.EnableDataWatcherState,
		"enable_data_watcher_user":  m.config.EnableDataWatcherUser,
		"components": map[string]interface{}{
			"redis_client":       m.redisClient != nil,
			"cache_manager":      m.cacheManager != nil,
			"syncer":             m.syncer != nil,
			"hydrator":           m.hydrator != nil,
			"data_watcher":       m.dataWatcher != nil,
			"data_watcher_state": m.dataWatcherState != nil,
			"data_watcher_user":  m.dataWatcherUser != nil,
			"data_sender":        m.dataSender != nil,
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
	if m.hydrator != nil {
		status["hydrator_running"] = m.hydrator.IsRunning()
		status["hydrator_metrics"] = m.hydrator.GetMetrics()
	}

	// Add DataWatcher status
	if m.dataWatcher != nil {
		status["data_watcher_running"] = m.dataWatcher.IsRunning()
		status["data_watcher_metrics"] = m.dataWatcher.GetMetrics()
	}

	// Add DataWatcherUser status
	if m.dataWatcherUser != nil {
		status["data_watcher_user_healthy"] = m.dataWatcherUser.IsHealthy()
		status["data_watcher_user_status"] = m.dataWatcherUser.GetStatus()
	}

	return status
}

// initRedisClient initializes the Redis client
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
func (m *AppInfoModule) initSyncer() error {
	glog.Infof("Initializing syncer...")

	if m.cacheManager == nil {
		return fmt.Errorf("cache manager is required for syncer")
	}

	// Get the actual cache data from cache manager instead of creating a new one
	cacheData := m.cacheManager.cache

	// Create settings manager for syncer
	redisAdapter := &RedisClientAdapter{client: m.redisClient}
	settingsManager := settings.NewSettingsManager(redisAdapter)
	if err := settingsManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize settings manager: %w", err)
	}

	m.syncer = CreateDefaultSyncer(cacheData, *m.config.Syncer, settingsManager)

	// Set cache manager reference for hydration notifications
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
func (m *AppInfoModule) initHydrator() error {
	glog.Infof("Initializing hydrator...")

	if m.cacheManager == nil {
		return fmt.Errorf("cache manager is required for hydrator")
	}

	// Get the actual cache data from cache manager
	cacheData := m.cacheManager.cache

	// Create settings manager for hydrator
	redisAdapter := &RedisClientAdapter{client: m.redisClient}
	settingsManager := settings.NewSettingsManager(redisAdapter)
	if err := settingsManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize settings manager: %w", err)
	}

	// Use hydrator config from module config, or default if not specified
	hydratorConfig := DefaultHydratorConfig()
	if m.config.Hydrator != nil {
		hydratorConfig = *m.config.Hydrator
	}

	m.hydrator = NewHydrator(cacheData, settingsManager, hydratorConfig)

	// Start hydrator with context
	if err := m.hydrator.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start hydrator: %w", err)
	}

	glog.Infof("Hydrator initialized successfully")
	return nil
}

// initDataWatcher initializes the DataWatcher
func (m *AppInfoModule) initDataWatcher() error {
	glog.Infof("Initializing DataWatcher...")

	if m.cacheManager == nil {
		return fmt.Errorf("cache manager is required for DataWatcher")
	}

	if m.hydrator == nil {
		return fmt.Errorf("hydrator is required for DataWatcher")
	}

	// Initialize DataSender for system notifications
	dataSender, err := NewDataSender()
	if err != nil {
		glog.Warningf("Failed to initialize DataSender: %v, DataWatcher will run without system notifications", err)
		dataSender = nil
	} else {
		m.dataSender = dataSender
		glog.Infof("DataSender initialized successfully")
	}

	// Create DataWatcher instance
	m.dataWatcher = NewDataWatcher(m.cacheManager, m.hydrator, m.dataSender)

	// Start DataWatcher
	if err := m.dataWatcher.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start DataWatcher: %w", err)
	}

	glog.Infof("DataWatcher initialized successfully")
	return nil
}

// initDataWatcherState initializes the DataWatcherState
func (m *AppInfoModule) initDataWatcherState() error {
	glog.Infof("Initializing DataWatcherState...")

	// Create DataWatcherState instance with cache manager, task module, history module, and data watcher
	m.dataWatcherState = NewDataWatcherState(m.cacheManager, m.taskModule, m.historyModule, m.dataWatcher)

	// Start DataWatcherState
	if err := m.dataWatcherState.Start(); err != nil {
		return fmt.Errorf("failed to start DataWatcherState: %w", err)
	}

	glog.Infof("DataWatcherState initialized successfully")
	return nil
}

// initDataWatcherUser initializes the DataWatcherUser
func (m *AppInfoModule) initDataWatcherUser() error {
	glog.Infof("Initializing DataWatcherUser...")

	// Create DataWatcherUser instance
	m.dataWatcherUser = NewDataWatcherUser()

	// Set history module reference if available
	if m.historyModule != nil {
		m.dataWatcherUser.SetHistoryModule(m.historyModule)
		glog.Infof("History module reference set in DataWatcherUser")
	} else {
		glog.Warningf("History module not available when initializing DataWatcherUser")
	}

	// Start DataWatcherUser
	if err := m.dataWatcherUser.Start(m.ctx); err != nil {
		return fmt.Errorf("failed to start DataWatcherUser: %w", err)
	}

	glog.Infof("DataWatcherUser initialized successfully")
	return nil
}

// validateConfig validates the module configuration
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

	clearCache, err := strconv.ParseBool(os.Getenv("CLEAR_CACHE"))
	if err != nil {
		clearCache = false
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

	enableDataWatcher, err := strconv.ParseBool(os.Getenv("MODULE_ENABLE_DATA_WATCHER"))
	if err != nil {
		enableDataWatcher = true
	}

	enableDataWatcherState, err := strconv.ParseBool(os.Getenv("MODULE_ENABLE_DATA_WATCHER_STATE"))
	if err != nil {
		enableDataWatcherState = true
	}

	enableDataWatcherUser, err := strconv.ParseBool(os.Getenv("MODULE_ENABLE_DATA_WATCHER_USER"))
	if err != nil {
		enableDataWatcherUser = true
	}

	startTimeout, err := time.ParseDuration(os.Getenv("MODULE_START_TIMEOUT"))
	if err != nil || startTimeout <= 0 {
		startTimeout = 30 * time.Second
	}

	// Parse Hydrator configuration from environment variables
	hydratorQueueSize, err := strconv.Atoi(os.Getenv("HYDRATOR_QUEUE_SIZE"))
	if err != nil || hydratorQueueSize <= 0 {
		hydratorQueueSize = 1000
	}

	hydratorWorkerCount, err := strconv.Atoi(os.Getenv("HYDRATOR_WORKER_COUNT"))
	if err != nil || hydratorWorkerCount <= 0 {
		hydratorWorkerCount = 1
	}

	// Parse User configuration from environment variables
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
			ClearCache:     clearCache,
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
			ClearCache:            clearCache,
		},
		EnableSync:             enableSync,
		EnableCache:            enableCache,
		EnableHydrator:         enableHydrator,
		EnableDataWatcher:      enableDataWatcher,
		EnableDataWatcherState: enableDataWatcherState,
		EnableDataWatcherUser:  enableDataWatcherUser,
		StartTimeout:           startTimeout,
	}
}

// GetDockerImageInfo is a convenience function to get Docker image information
func (m *AppInfoModule) GetDockerImageInfo(imageName string) (*utils.DockerImageInfo, error) {
	return utils.GetDockerImageInfo(imageName)
}

// GetLayerDownloadProgress is a convenience function to get layer download progress
func (m *AppInfoModule) GetLayerDownloadProgress(layerDigest string) (*utils.LayerInfo, error) {
	return utils.GetLayerDownloadProgress(layerDigest)
}

// SetAppData is a convenience function to set app data
func (m *AppInfoModule) SetAppData(userID, sourceID string, dataType AppDataType, data map[string]interface{}) error {
	if !m.isStarted || m.cacheManager == nil {
		return fmt.Errorf("module is not started or cache manager is not available")
	}
	return m.cacheManager.SetAppData(userID, sourceID, dataType, data)
}

// GetAppData is a convenience function to get app data
func (m *AppInfoModule) GetAppData(userID, sourceID string, dataType AppDataType) interface{} {
	if !m.isStarted || m.cacheManager == nil {
		return nil
	}
	return m.cacheManager.GetAppData(userID, sourceID, dataType)
}

// GetUserConfig returns the user configuration
func (m *AppInfoModule) GetUserConfig() *UserConfig {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.config.User
}

// IsValidUser checks if a user ID is in the configured user list
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

// CleanupInvalidData cleans up invalid pending data entries from cache
func (m *AppInfoModule) CleanupInvalidData() (int, error) {
	if m.cacheManager == nil {
		return 0, fmt.Errorf("cache manager not available")
	}

	cleanedCount := m.cacheManager.CleanupInvalidPendingData()
	glog.Infof("Cleaned up %d invalid pending data entries", cleanedCount)

	return cleanedCount, nil
}

// GetInvalidDataReport returns a detailed report of invalid pending data entries
func (m *AppInfoModule) GetInvalidDataReport() map[string]interface{} {
	if m.cacheManager == nil {
		return map[string]interface{}{
			"error": "cache manager not available",
		}
	}

	report := map[string]interface{}{
		"users": make(map[string]interface{}),
		"totals": map[string]int{
			"total_users":        0,
			"total_sources":      0,
			"total_pending_data": 0,
			"total_invalid_data": 0,
		},
	}

	m.cacheManager.mutex.RLock()
	defer m.cacheManager.mutex.RUnlock()

	totalUsers := 0
	totalSources := 0
	totalPendingData := 0
	totalInvalidData := 0

	for userID, userData := range m.cacheManager.cache.Users {
		totalUsers++
		userReport := map[string]interface{}{
			"sources": make(map[string]interface{}),
			"totals": map[string]int{
				"total_sources":      0,
				"total_pending_data": 0,
				"total_invalid_data": 0,
			},
		}

		for sourceID, sourceData := range userData.Sources {
			totalSources++

			sourceReport := map[string]interface{}{
				"total_pending_data": len(sourceData.AppInfoLatestPending),
				"invalid_entries":    make([]map[string]interface{}, 0),
			}

			invalidCount := 0
			for i, pendingData := range sourceData.AppInfoLatestPending {
				isValid := false
				invalidReasons := make([]string, 0)

				if pendingData.RawData != nil {
					if (pendingData.RawData.ID != "" && pendingData.RawData.ID != "0") ||
						(pendingData.RawData.AppID != "" && pendingData.RawData.AppID != "0") ||
						(pendingData.RawData.Name != "" && pendingData.RawData.Name != "unknown") {
						isValid = true
					} else {
						if pendingData.RawData.ID == "" || pendingData.RawData.ID == "0" {
							invalidReasons = append(invalidReasons, "empty or zero ID")
						}
						if pendingData.RawData.AppID == "" || pendingData.RawData.AppID == "0" {
							invalidReasons = append(invalidReasons, "empty or zero AppID")
						}
						if pendingData.RawData.Name == "" || pendingData.RawData.Name == "unknown" {
							invalidReasons = append(invalidReasons, "empty or unknown Name")
						}
					}
				} else {
					invalidReasons = append(invalidReasons, "null RawData")
				}

				if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil && !isValid {
					if (pendingData.AppInfo.AppEntry.ID != "" && pendingData.AppInfo.AppEntry.ID != "0") ||
						(pendingData.AppInfo.AppEntry.AppID != "" && pendingData.AppInfo.AppEntry.AppID != "0") ||
						(pendingData.AppInfo.AppEntry.Name != "" && pendingData.AppInfo.AppEntry.Name != "unknown") {
						isValid = true
						invalidReasons = make([]string, 0) // Clear reasons if AppInfo is valid
					}
				}

				if !isValid {
					invalidCount++
					invalidEntry := map[string]interface{}{
						"index":   i,
						"reasons": invalidReasons,
						"data": map[string]interface{}{
							"timestamp": pendingData.Timestamp,
							"version":   pendingData.Version,
						},
					}

					if pendingData.RawData != nil {
						invalidEntry["raw_data"] = map[string]interface{}{
							"id":     pendingData.RawData.ID,
							"app_id": pendingData.RawData.AppID,
							"name":   pendingData.RawData.Name,
							"title":  pendingData.RawData.Title,
						}
					}

					sourceReport["invalid_entries"] = append(sourceReport["invalid_entries"].([]map[string]interface{}), invalidEntry)
				}
			}

			sourceReport["invalid_count"] = invalidCount
			totalPendingData += len(sourceData.AppInfoLatestPending)
			totalInvalidData += invalidCount

			userReport["sources"].(map[string]interface{})[sourceID] = sourceReport
			userReport["totals"].(map[string]int)["total_sources"]++
			userReport["totals"].(map[string]int)["total_pending_data"] += len(sourceData.AppInfoLatestPending)
			userReport["totals"].(map[string]int)["total_invalid_data"] += invalidCount
		}

		report["users"].(map[string]interface{})[userID] = userReport
	}

	report["totals"].(map[string]int)["total_users"] = totalUsers
	report["totals"].(map[string]int)["total_sources"] = totalSources
	report["totals"].(map[string]int)["total_pending_data"] = totalPendingData
	report["totals"].(map[string]int)["total_invalid_data"] = totalInvalidData

	return report
}

// SetTaskModule sets the task module for recording task events
func (m *AppInfoModule) SetTaskModule(taskModule *task.TaskModule) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.taskModule = taskModule
	glog.Infof("Task module reference set in AppInfo module")

	// Re-initialize DataWatcherState if it exists and module is started
	if m.isStarted && m.dataWatcherState != nil {
		glog.Infof("Re-initializing DataWatcherState with task module")

		// Stop existing DataWatcherState
		if err := m.dataWatcherState.Stop(); err != nil {
			glog.Errorf("Failed to stop existing DataWatcherState: %v", err)
		}

		// Create new DataWatcherState with task module
		m.dataWatcherState = NewDataWatcherState(m.cacheManager, m.taskModule, m.historyModule, m.dataWatcher)

		// Start new DataWatcherState
		if err := m.dataWatcherState.Start(); err != nil {
			glog.Errorf("Failed to start new DataWatcherState: %v", err)
		} else {
			glog.Infof("DataWatcherState re-initialized successfully with task module")
		}
	}
}

// SetHistoryModule sets the history module reference
func (m *AppInfoModule) SetHistoryModule(historyModule *history.HistoryModule) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.historyModule = historyModule
	glog.Infof("History module reference set in AppInfo module")

	// Re-initialize DataWatcherState if it exists and module is started
	if m.isStarted && m.dataWatcherState != nil {
		glog.Infof("Re-initializing DataWatcherState with history module")

		// Stop existing DataWatcherState
		if err := m.dataWatcherState.Stop(); err != nil {
			glog.Errorf("Failed to stop existing DataWatcherState: %v", err)
		}

		// Create new DataWatcherState with history module
		m.dataWatcherState = NewDataWatcherState(m.cacheManager, m.taskModule, m.historyModule, m.dataWatcher)

		// Start new DataWatcherState
		if err := m.dataWatcherState.Start(); err != nil {
			glog.Errorf("Failed to start new DataWatcherState: %v", err)
		} else {
			glog.Infof("DataWatcherState re-initialized successfully with history module")
		}
	}

	// Re-initialize DataWatcherUser if it exists and module is started
	if m.isStarted && m.dataWatcherUser != nil {
		glog.Infof("Re-initializing DataWatcherUser with history module")

		// Stop existing DataWatcherUser
		m.dataWatcherUser.Stop()

		// Create new DataWatcherUser
		m.dataWatcherUser = NewDataWatcherUser()

		// Set history module reference
		m.dataWatcherUser.SetHistoryModule(m.historyModule)

		// Start new DataWatcherUser
		if err := m.dataWatcherUser.Start(m.ctx); err != nil {
			glog.Errorf("Failed to start new DataWatcherUser: %v", err)
		} else {
			glog.Infof("DataWatcherUser re-initialized successfully with history module")
		}
	}
}

// GetTaskModule returns the task module instance
func (m *AppInfoModule) GetTaskModule() *task.TaskModule {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.taskModule
}
