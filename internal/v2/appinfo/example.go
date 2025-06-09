package appinfo

import (
	"context"
	"fmt"
	"log"
	"time"

	"market/internal/v2/appinfo/syncerfn"
	"market/internal/v2/settings"

	"github.com/golang/glog"
)

// ExampleRedisClient is a simple example implementation of RedisClient interface
// ExampleRedisClient 是 RedisClient 接口的简单示例实现
type ExampleRedisClient struct {
	data map[string]string // In-memory storage for demonstration
}

// NewExampleRedisClient creates a new example Redis client
func NewExampleRedisClient() *ExampleRedisClient {
	return &ExampleRedisClient{
		data: make(map[string]string),
	}
}

// Get implements RedisClient interface
func (e *ExampleRedisClient) Get(key string) (string, error) {
	if value, exists := e.data[key]; exists {
		return value, nil
	}
	return "", fmt.Errorf("key not found: %s", key)
}

// Set implements RedisClient interface
func (e *ExampleRedisClient) Set(key string, value interface{}, expiration time.Duration) error {
	e.data[key] = fmt.Sprintf("%v", value)
	return nil
}

// HSet implements RedisClient interface
func (e *ExampleRedisClient) HSet(key string, fields map[string]interface{}) error {
	// For simplicity, store as single key-value pair
	// 为了简单起见，存储为单个键值对
	for field, value := range fields {
		e.data[fmt.Sprintf("%s:%s", key, field)] = fmt.Sprintf("%v", value)
	}
	return nil
}

// HGetAll implements RedisClient interface
func (e *ExampleRedisClient) HGetAll(key string) (map[string]string, error) {
	result := make(map[string]string)
	prefix := key + ":"
	for k, v := range e.data {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			field := k[len(prefix):]
			result[field] = v
		}
	}
	return result, nil
}

// Example demonstrates how to use the cache system
func Example() {
	// 1. Create Redis configuration
	redisConfig := &RedisConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
		Timeout:  10 * time.Second,
	}

	// 2. Create Redis client
	redisClient, err := NewRedisClient(redisConfig)
	if err != nil {
		glog.Errorf("Failed to create Redis client: %v", err)
		return
	}
	defer redisClient.Close()

	// 3. Create cache manager
	cacheManager := NewCacheManager(redisClient, nil)

	// 4. Start cache manager (loads data from Redis)
	if err := cacheManager.Start(); err != nil {
		glog.Errorf("Failed to start cache manager: %v", err)
		return
	}
	defer cacheManager.Stop()

	// 5. Set some app data
	appData := map[string]interface{}{
		"app_name":    "MyApp",
		"version":     "1.0.0",
		"status":      "running",
		"update_time": time.Now().Unix(),
	}

	// Set app-info-latest data
	err = cacheManager.SetAppData("user123", "source456", AppInfoLatest, appData)
	if err != nil {
		glog.Errorf("Failed to set app data: %v", err)
		return
	}

	// Set app-state-latest data
	stateData := map[string]interface{}{
		"cpu_usage":    45.2,
		"memory_usage": 67.8,
		"disk_usage":   23.1,
	}
	err = cacheManager.SetAppData("user123", "source456", AppStateLatest, stateData)
	if err != nil {
		glog.Errorf("Failed to set state data: %v", err)
		return
	}

	// Add to app-info-history
	historyData := map[string]interface{}{
		"event":     "app_started",
		"timestamp": time.Now().Unix(),
		"details":   "Application started successfully",
	}
	err = cacheManager.SetAppData("user123", "source456", AppInfoHistory, historyData)
	if err != nil {
		glog.Errorf("Failed to set history data: %v", err)
		return
	}

	// Set other type data
	otherData := map[string]interface{}{
		"sub_type":    "custom_metric",
		"metric_name": "response_time",
		"value":       150.5,
	}
	err = cacheManager.SetAppData("user123", "source456", Other, otherData)
	if err != nil {
		glog.Errorf("Failed to set other data: %v", err)
		return
	}

	// 6. Retrieve data from cache
	retrievedAppInfo := cacheManager.GetAppData("user123", "source456", AppInfoLatest)
	if retrievedAppInfo != nil {
		glog.Infof("Retrieved app info: %+v", retrievedAppInfo)
	}

	retrievedState := cacheManager.GetAppData("user123", "source456", AppStateLatest)
	if retrievedState != nil {
		glog.Infof("Retrieved app state: %+v", retrievedState)
	}

	retrievedHistory := cacheManager.GetAppData("user123", "source456", AppInfoHistory)
	if retrievedHistory != nil {
		glog.Infof("Retrieved app history: %+v", retrievedHistory)
	}

	// 7. Get cache statistics
	stats := cacheManager.GetCacheStats()
	glog.Infof("Cache stats: %+v", stats)

	// 8. Force sync all data to Redis (optional)
	if err := cacheManager.ForceSync(); err != nil {
		glog.Errorf("Failed to force sync: %v", err)
	}

	glog.Infof("Example completed successfully")
}

// ExampleCustomStep demonstrates how to create a custom sync step
type ExampleCustomStep struct {
	Name string
}

func NewExampleCustomStep(name string) *ExampleCustomStep {
	return &ExampleCustomStep{
		Name: name,
	}
}

func (e *ExampleCustomStep) GetStepName() string {
	return fmt.Sprintf("Custom Step: %s", e.Name)
}

func (e *ExampleCustomStep) Execute(ctx context.Context, data *syncerfn.SyncContext) error {
	log.Printf("Executing %s", e.GetStepName())

	// Custom logic here
	// For example, data processing, validation, etc.

	log.Printf("Custom step %s completed successfully", e.Name)
	return nil
}

func (e *ExampleCustomStep) CanSkip(ctx context.Context, data *syncerfn.SyncContext) bool {
	// Custom skip logic
	return false
}

// ExampleUsage demonstrates how to use the syncer system
func ExampleUsage() {
	// Create Redis client and settings manager
	// 创建Redis客户端和设置管理器
	redisClient := NewExampleRedisClient()
	settingsManager := settings.NewSettingsManager(redisClient)

	if err := settingsManager.Initialize(); err != nil {
		log.Printf("Failed to initialize settings manager: %v", err)
		return
	}

	// Create cache
	cache := NewCacheData()

	// Create syncer with 5-minute interval
	syncer := NewSyncer(cache, 5*time.Minute, settingsManager)

	// Get API endpoints
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints == nil {
		log.Printf("No API endpoints configuration found")
		return
	}

	// Add default steps
	syncer.AddStep(syncerfn.NewHashComparisonStep(endpoints.HashPath, settingsManager))
	syncer.AddStep(syncerfn.NewDataFetchStep(endpoints.DataPath, settingsManager))
	version := getVersionForSync()
	syncer.AddStep(syncerfn.NewDetailFetchStep(endpoints.DetailPath, version, settingsManager))

	// Add custom steps
	syncer.AddStep(NewExampleCustomStep("Data Validation"))
	syncer.AddStep(NewExampleCustomStep("Cache Update"))

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Start syncer
	if err := syncer.Start(ctx); err != nil {
		log.Printf("Failed to start syncer: %v", err)
		return
	}

	// Let it run for demonstration
	time.Sleep(30 * time.Second)

	// Stop syncer
	syncer.Stop()

	log.Println("Example completed")
}

// ExampleWithConfig demonstrates using CreateDefaultSyncer with configuration
func ExampleWithConfig() {
	// Create Redis client and settings manager
	// 创建Redis客户端和设置管理器
	redisClient := NewExampleRedisClient()
	settingsManager := settings.NewSettingsManager(redisClient)

	if err := settingsManager.Initialize(); err != nil {
		log.Printf("Failed to initialize settings manager: %v", err)
		return
	}

	// Create cache
	cache := NewCacheData()

	// Create configuration - now only needs sync interval
	// 创建配置 - 现在只需要同步间隔
	config := SyncerConfig{
		SyncInterval: 3 * time.Minute,
	}

	// Create syncer with default steps
	syncer := CreateDefaultSyncer(cache, config, settingsManager)

	// Optionally add more custom steps
	syncer.AddStep(NewExampleCustomStep("Post Processing"))

	// Create context
	ctx := context.Background()

	// Start syncer
	if err := syncer.Start(ctx); err != nil {
		log.Printf("Failed to start syncer: %v", err)
		return
	}

	log.Println("Syncer started successfully")

	// In a real application, you would keep the syncer running
	// For this example, we'll stop it after a short time
	time.Sleep(10 * time.Second)
	syncer.Stop()
}

// ExampleStepManagement demonstrates how to manage steps dynamically
func ExampleStepManagement() {
	// Create Redis client and settings manager
	// 创建Redis客户端和设置管理器
	redisClient := NewExampleRedisClient()
	settingsManager := settings.NewSettingsManager(redisClient)

	if err := settingsManager.Initialize(); err != nil {
		log.Printf("Failed to initialize settings manager: %v", err)
		return
	}

	cache := NewCacheData()
	syncer := NewSyncer(cache, 1*time.Minute, settingsManager)

	// Get API endpoints
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints == nil {
		log.Printf("No API endpoints configuration found")
		return
	}

	// Add initial steps
	syncer.AddStep(syncerfn.NewHashComparisonStep(endpoints.HashPath, settingsManager))
	syncer.AddStep(syncerfn.NewDataFetchStep(endpoints.DataPath, settingsManager))

	log.Printf("Initial steps count: %d", len(syncer.GetSteps()))

	// Add more steps
	version := getVersionForSync()
	syncer.AddStep(syncerfn.NewDetailFetchStep(endpoints.DetailPath, version, settingsManager))
	syncer.AddStep(NewExampleCustomStep("Validation"))

	log.Printf("After adding steps: %d", len(syncer.GetSteps()))

	// Remove a step (index 1)
	if err := syncer.RemoveStep(1); err != nil {
		log.Printf("Failed to remove step: %v", err)
	} else {
		log.Printf("After removing step: %d", len(syncer.GetSteps()))
	}

	// Get all steps for inspection
	steps := syncer.GetSteps()
	for i, step := range steps {
		log.Printf("Step %d: %s", i+1, step.GetStepName())
	}
}

// ExampleErrorHandling demonstrates error handling in sync steps
func ExampleErrorHandling() {
	// Create Redis client and settings manager
	// 创建Redis客户端和设置管理器
	redisClient := NewExampleRedisClient()
	settingsManager := settings.NewSettingsManager(redisClient)

	if err := settingsManager.Initialize(); err != nil {
		log.Printf("Failed to initialize settings manager: %v", err)
		return
	}

	// Add a source that will likely fail (invalid URL)
	// 添加一个可能失败的源（无效URL）
	failingSource := &settings.MarketSource{
		ID:          "failing",
		Name:        "Failing Market Source",
		BaseURL:     "https://invalid-url-that-will-fail.com",
		Priority:    200, // High priority so it tries first
		IsActive:    true,
		Description: "Source for testing error handling",
	}

	if err := settingsManager.AddMarketSource(failingSource); err != nil {
		log.Printf("Failed to add failing market source: %v", err)
	}

	cache := NewCacheData()
	syncer := NewSyncer(cache, 1*time.Minute, settingsManager)

	// Get API endpoints
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints == nil {
		log.Printf("No API endpoints configuration found")
		return
	}

	// Add steps that will fail with the invalid source but may succeed with backup sources
	syncer.AddStep(syncerfn.NewHashComparisonStep(endpoints.HashPath, settingsManager))
	syncer.AddStep(syncerfn.NewDataFetchStep(endpoints.DataPath, settingsManager))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start syncer - it will handle errors gracefully and try backup sources
	if err := syncer.Start(ctx); err != nil {
		log.Printf("Failed to start syncer: %v", err)
		return
	}

	// Let it run to see error handling
	time.Sleep(5 * time.Second)
	syncer.Stop()
}

// ExampleUsageWithMultipleSources demonstrates how to use the syncer with multiple market sources
// 演示如何使用多市场源的同步器
func ExampleUsageWithMultipleSources() {
	// Create settings manager with Redis client
	// 创建带有Redis客户端的设置管理器
	redisClient := NewExampleRedisClient() // Your Redis implementation
	settingsManager := settings.NewSettingsManager(redisClient)

	// Initialize settings manager
	// 初始化设置管理器
	if err := settingsManager.Initialize(); err != nil {
		log.Fatalf("Failed to initialize settings manager: %v", err)
	}

	// Add additional market sources programmatically
	// 通过编程方式添加额外的市场源
	secondarySource := &settings.MarketSource{
		ID:          "secondary",
		Name:        "Secondary Market Source",
		BaseURL:     "https://appstore-backup.example.com",
		Priority:    50, // Lower priority than default
		IsActive:    true,
		Description: "Secondary market source for failover",
	}

	if err := settingsManager.AddMarketSource(secondarySource); err != nil {
		log.Printf("Failed to add secondary market source: %v", err)
	}

	// Update API endpoints if needed
	// 如果需要，更新API端点
	customEndpoints := &settings.APIEndpointsConfig{
		HashPath:   "/api/v2/appstore/hash",     // Custom API version
		DataPath:   "/api/v2/appstore/info",     // Custom API version
		DetailPath: "/api/v2/applications/info", // Custom API version
	}

	if err := settingsManager.UpdateAPIEndpoints(customEndpoints); err != nil {
		log.Printf("Failed to update API endpoints: %v", err)
	}

	// Create cache
	// 创建缓存
	cache := NewCacheData()

	// Create syncer with new multiple sources support
	// 使用新的多源支持创建同步器
	config := SyncerConfig{
		SyncInterval: 3 * time.Minute, // Custom sync interval
	}

	syncer := CreateDefaultSyncer(cache, config, settingsManager)

	// Start syncing
	// 开始同步
	ctx := context.Background()
	if err := syncer.Start(ctx); err != nil {
		log.Fatalf("Failed to start syncer: %v", err)
	}

	// The syncer will now:
	// 同步器现在将：
	// 1. Get all active market sources sorted by priority
	// 1. 获取所有按优先级排序的活跃市场源
	// 2. Try each source in order until one succeeds
	// 2. 按顺序尝试每个源，直到其中一个成功
	// 3. Use the configured API endpoint paths with each source's base URL
	// 3. 使用配置的API端点路径与每个源的基础URL组合

	log.Println("Syncer started with multiple sources support")

	// Let it run for some time, then stop
	// 让它运行一段时间，然后停止
	time.Sleep(30 * time.Second)

	syncer.Stop()
	log.Println("Syncer stopped")
}

// ExampleManageMarketSources demonstrates how to manage market sources
// 演示如何管理市场源
func ExampleManageMarketSources() {
	redisClient := NewExampleRedisClient()
	settingsManager := settings.NewSettingsManager(redisClient)

	if err := settingsManager.Initialize(); err != nil {
		log.Fatalf("Failed to initialize settings manager: %v", err)
	}

	// Get current market sources
	// 获取当前市场源
	sources := settingsManager.GetMarketSources()
	log.Printf("Current market sources: %d", len(sources.Sources))

	for _, source := range sources.Sources {
		log.Printf("Source: %s (%s) - %s, Priority: %d, Active: %t",
			source.ID, source.Name, source.BaseURL, source.Priority, source.IsActive)
	}

	// Get only active sources
	// 仅获取活跃源
	activeSources := settingsManager.GetActiveMarketSources()
	log.Printf("Active market sources: %d", len(activeSources))

	// Get default source
	// 获取默认源
	defaultSource := settingsManager.GetDefaultMarketSource()
	if defaultSource != nil {
		log.Printf("Default source: %s (%s)", defaultSource.ID, defaultSource.BaseURL)
	}

	// Get API endpoints configuration
	// 获取API端点配置
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints != nil {
		log.Printf("API Endpoints - Hash: %s, Data: %s, Detail: %s",
			endpoints.HashPath, endpoints.DataPath, endpoints.DetailPath)
	}

	// Example of building complete URLs
	// 构建完整URL的示例
	if defaultSource != nil && endpoints != nil {
		hashURL := settingsManager.BuildAPIURL(defaultSource.BaseURL, endpoints.HashPath)
		dataURL := settingsManager.BuildAPIURL(defaultSource.BaseURL, endpoints.DataPath)
		detailURL := settingsManager.BuildAPIURL(defaultSource.BaseURL, endpoints.DetailPath)

		log.Printf("Complete URLs for default source:")
		log.Printf("  Hash: %s", hashURL)
		log.Printf("  Data: %s", dataURL)
		log.Printf("  Detail: %s", detailURL)
	}
}

// Example of how to customize syncer with specific steps
// 如何使用特定步骤自定义同步器的示例
func ExampleCustomSyncer() {
	redisClient := NewExampleRedisClient()
	settingsManager := settings.NewSettingsManager(redisClient)

	if err := settingsManager.Initialize(); err != nil {
		log.Fatalf("Failed to initialize settings manager: %v", err)
	}

	cache := NewCacheData()
	config := DefaultSyncerConfig()

	// Create syncer without default steps
	// 创建不带默认步骤的同步器
	syncer := NewSyncer(cache, config.SyncInterval, settingsManager)

	// Add custom steps with different configurations
	// 添加具有不同配置的自定义步骤
	endpoints := settingsManager.GetAPIEndpoints()
	if endpoints == nil {
		log.Fatal("No API endpoints configuration found")
	}

	// Custom hash comparison step
	// 自定义hash比较步骤
	hashStep := syncerfn.NewHashComparisonStep(endpoints.HashPath, settingsManager)
	syncer.AddStep(hashStep)

	// Custom data fetch step
	// 自定义数据获取步骤
	dataStep := syncerfn.NewDataFetchStep(endpoints.DataPath, settingsManager)
	syncer.AddStep(dataStep)

	// Custom detail fetch step with larger batch size
	// 具有更大批处理大小的自定义详情获取步骤
	version := getVersionForSync()
	detailStep := syncerfn.NewDetailFetchStepWithBatchSize(
		endpoints.DetailPath, version, settingsManager, 20) // Batch size of 20
	syncer.AddStep(detailStep)

	log.Printf("Custom syncer created with %d steps", len(syncer.GetSteps()))

	// Use the custom syncer
	// 使用自定义同步器
	ctx := context.Background()
	if err := syncer.Start(ctx); err != nil {
		log.Fatalf("Failed to start custom syncer: %v", err)
	}

	log.Println("Custom syncer started")
}

// ExampleUserConfiguration demonstrates user configuration features
// 演示用户配置功能
func ExampleUserConfiguration() {
	glog.Infof("====== Starting User Configuration Example ======")

	// Load configuration from environment variables
	config := DefaultModuleConfig()

	// Display user configuration
	if config.User != nil {
		glog.Infof("User Configuration:")
		glog.Infof("  User List: %v", config.User.UserList)
		glog.Infof("  Admin List: %v", config.User.AdminList)
		glog.Infof("  Default Role: %s", config.User.DefaultRole)
		glog.Infof("  Guest Enabled: %v", config.User.GuestEnabled)
		glog.Infof("  Max Sources Per User: %d", config.User.MaxSourcesPerUser)
		glog.Infof("  Data Retention Days: %d", config.User.DataRetentionDays)
		glog.Infof("  Session Timeout: %v", config.User.SessionTimeout)
		glog.Infof("  Default Permissions: %v", config.User.DefaultPermissions)
	}

	// Create AppInfo module with user configuration
	module, err := NewAppInfoModule(config)
	if err != nil {
		glog.Errorf("Failed to create AppInfo module: %v", err)
		return
	}

	// Test user validation
	testUsers := []string{"admin", "user1", "user2", "guest_user", "unauthorized_user"}

	for _, userID := range testUsers {
		glog.Infof("Testing user: %s", userID)

		// Check if user is valid
		isValid := module.IsValidUser(userID)
		glog.Infof("  Is valid user: %v", isValid)

		// Check if user is admin
		isAdmin := module.IsAdminUser(userID)
		glog.Infof("  Is admin user: %v", isAdmin)

		// Get user role
		role := module.GetUserRole(userID)
		glog.Infof("  User role: %s", role)

		// Get user permissions
		permissions := module.GetUserPermissions(userID)
		glog.Infof("  User permissions: %v", permissions)

		// Get max sources for user
		maxSources := module.GetMaxSourcesForUser(userID)
		glog.Infof("  Max sources: %d", maxSources)

		// Validate user access
		if err := module.ValidateUserAccess(userID); err != nil {
			glog.Warningf("  Access validation failed: %v", err)
		} else {
			glog.Infof("  Access validation: PASSED")
		}

		glog.Infof("")
	}

	// Start the module to test cache operations
	if err := module.Start(); err != nil {
		glog.Errorf("Failed to start module: %v", err)
		return
	}
	defer module.Stop()

	// Test setting app data with user limits
	cacheManager := module.GetCacheManager()
	if cacheManager != nil {
		// Test with valid user
		testData := map[string]interface{}{
			"app_name": "test-app",
			"version":  "1.0.0",
		}

		glog.Infof("Testing app data operations:")

		// Try to set data for authorized user
		err := cacheManager.SetAppData("admin", "source1", AppInfoLatest, testData)
		if err != nil {
			glog.Errorf("Failed to set data for admin: %v", err)
		} else {
			glog.Infof("Successfully set data for admin user")
		}

		// Try to exceed source limit for regular user
		glog.Infof("Testing source limits for user1:")
		for i := 1; i <= 12; i++ { // Try to exceed the limit of 10
			sourceID := fmt.Sprintf("source%d", i)
			err := cacheManager.SetAppData("user1", sourceID, AppInfoLatest, testData)
			if err != nil {
				glog.Warningf("Failed to set data for source %s: %v", sourceID, err)
				break
			} else {
				glog.Infof("Successfully set data for source %s", sourceID)
			}
		}

		// Try with unauthorized user
		err = cacheManager.SetAppData("unauthorized_user", "source1", AppInfoLatest, testData)
		if err != nil {
			glog.Warningf("Failed to set data for unauthorized user (expected): %v", err)
		}
	}

	glog.Infof("====== User Configuration Example Completed ======")
}

// ExampleWithHydrationNotification demonstrates real-time hydration task creation
// ExampleWithHydrationNotification 演示实时水合任务创建
func ExampleWithHydrationNotification() {
	log.Println("Starting example with hydration notification")

	// Create module configuration with hydration enabled
	// 创建启用水合功能的模块配置
	config := DefaultModuleConfig()
	config.EnableSync = true
	config.EnableCache = true
	config.EnableHydrator = true

	// Create and start module
	// 创建并启动模块
	module, err := NewAppInfoModule(config)
	if err != nil {
		log.Printf("Failed to create module: %v", err)
		return
	}

	if err := module.Start(); err != nil {
		log.Printf("Failed to start module: %v", err)
		return
	}
	defer module.Stop()

	// Wait for initialization
	// 等待初始化
	time.Sleep(2 * time.Second)

	// Check that hydration notifier is connected
	// 检查水合通知器是否已连接
	status := module.GetModuleStatus()
	log.Printf("Module status: %+v", status)

	// Simulate setting AppInfoLatestPending data which should trigger hydration
	// 模拟设置AppInfoLatestPending数据，这应该触发水合
	testData := map[string]interface{}{
		"version": "1.0.0",
		"data": map[string]interface{}{
			"apps": map[string]interface{}{
				"test-app-1": map[string]interface{}{
					"name":        "Test Application 1",
					"version":     "1.0.0",
					"description": "A test application for hydration",
					"icon":        "test-icon.png",
				},
				"test-app-2": map[string]interface{}{
					"name":        "Test Application 2",
					"version":     "2.0.0",
					"description": "Another test application for hydration",
					"icon":        "test-icon-2.png",
				},
			},
		},
	}

	log.Println("Setting AppInfoLatestPending data to trigger hydration...")

	// Set the data which should immediately trigger hydration task creation
	// 设置数据，这应该立即触发水合任务创建
	err = module.SetAppData("test-user", "test-source", AppInfoLatestPending, testData)
	if err != nil {
		log.Printf("Failed to set app data: %v", err)
		return
	}

	log.Println("AppInfoLatestPending data set successfully")

	// Wait for hydration tasks to be processed
	// 等待水合任务被处理
	time.Sleep(5 * time.Second)

	// Check hydrator metrics to see if tasks were created and processed
	// 检查水合器指标以查看是否创建和处理了任务
	if hydrator := module.GetHydrator(); hydrator != nil {
		metrics := hydrator.GetMetrics()
		log.Printf("Hydrator metrics after data update:")
		log.Printf("  Total tasks processed: %d", metrics.TotalTasksProcessed)
		log.Printf("  Total tasks succeeded: %d", metrics.TotalTasksSucceeded)
		log.Printf("  Total tasks failed: %d", metrics.TotalTasksFailed)
		log.Printf("  Active tasks count: %d", metrics.ActiveTasksCount)
		log.Printf("  Queue length: %d", metrics.QueueLength)
	}

	log.Println("Hydration notification example completed")
}

// TestHydrationNotification tests the hydration notification mechanism
// TestHydrationNotification 测试水合通知机制
func TestHydrationNotification() {
	log.Println("=== Testing Hydration Notification Mechanism ===")

	// Create module configuration
	config := DefaultModuleConfig()
	config.EnableSync = true
	config.EnableCache = true
	config.EnableHydrator = true

	log.Printf("Config - EnableSync: %t, EnableCache: %t, EnableHydrator: %t",
		config.EnableSync, config.EnableCache, config.EnableHydrator)

	// Create and start module
	module, err := NewAppInfoModule(config)
	if err != nil {
		log.Printf("Failed to create module: %v", err)
		return
	}

	if err := module.Start(); err != nil {
		log.Printf("Failed to start module: %v", err)
		return
	}
	defer module.Stop()

	// Wait for initialization
	time.Sleep(2 * time.Second)

	// Get components
	cacheManager := module.GetCacheManager()
	hydrator := module.GetHydrator()
	syncer := module.GetSyncer()

	log.Printf("Components - CacheManager: %t, Hydrator: %t, Syncer: %t",
		cacheManager != nil, hydrator != nil, syncer != nil)

	// Check module status to verify connections
	status := module.GetModuleStatus()
	log.Printf("Module status: %+v", status)

	if hydrator != nil {
		log.Printf("Hydrator running: %t", hydrator.IsRunning())
		initialMetrics := hydrator.GetMetrics()
		log.Printf("Initial hydrator metrics:")
		log.Printf("  Active tasks: %d", initialMetrics.ActiveTasksCount)
		log.Printf("  Queue length: %d", initialMetrics.QueueLength)
		log.Printf("  Total processed: %d", initialMetrics.TotalTasksProcessed)
	}

	// Test direct cache manager notification
	if cacheManager != nil {
		log.Println("\n--- Testing Direct CacheManager.SetAppData Call ---")

		testData := map[string]interface{}{
			"version": "1.0.0",
			"data": map[string]interface{}{
				"apps": map[string]interface{}{
					"direct-test-app-1": map[string]interface{}{
						"name":        "Direct Test App 1",
						"version":     "1.0.0",
						"description": "App for testing direct notification",
					},
					"direct-test-app-2": map[string]interface{}{
						"name":        "Direct Test App 2",
						"version":     "2.0.0",
						"description": "Another app for testing direct notification",
					},
				},
			},
		}

		log.Println("Calling CacheManager.SetAppData...")
		err := cacheManager.SetAppData("test-user", "direct-test-source", AppInfoLatestPending, testData)
		if err != nil {
			log.Printf("❌ Failed to set app data directly: %v", err)
		} else {
			log.Println("✅ Successfully set app data directly via CacheManager")
		}

		// Wait and check if tasks were created
		log.Println("Waiting 3 seconds for hydration tasks to be processed...")
		time.Sleep(3 * time.Second)

		if hydrator != nil {
			metrics := hydrator.GetMetrics()
			log.Printf("Hydrator metrics after direct call:")
			log.Printf("  Active tasks: %d", metrics.ActiveTasksCount)
			log.Printf("  Queue length: %d", metrics.QueueLength)
			log.Printf("  Total processed: %d", metrics.TotalTasksProcessed)
			log.Printf("  Total succeeded: %d", metrics.TotalTasksSucceeded)
			log.Printf("  Total failed: %d", metrics.TotalTasksFailed)

			if metrics.TotalTasksProcessed > 0 {
				log.Println("✅ Hydration notification mechanism is working!")
			} else {
				log.Println("❌ No tasks were processed, notification might not be working")
			}
		}
	}

	log.Println("=== Test Completed ===")
}
