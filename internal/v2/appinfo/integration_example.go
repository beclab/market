package appinfo

import (
	"context"
	"log"
	"sync"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"
)

// IntegrationExample demonstrates how to set up the hydration process with image analysis and database sync
// IntegrationExample 演示如何设置带有镜像分析和数据库同步的水合过程
func IntegrationExample() error {
	// 1. Setup Redis client
	// 1. 设置Redis客户端
	redisConfig := &RedisConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
		Timeout:  5 * time.Second,
	}

	redisClient, err := NewRedisClient(redisConfig)
	if err != nil {
		return err
	}
	defer redisClient.Close()

	// 2. Setup user configuration
	// 2. 设置用户配置
	userConfig := &UserConfig{
		UserList:          []string{"admin", "user1", "user2"},
		AdminList:         []string{"admin"},
		GuestEnabled:      true,
		MaxSourcesPerUser: 10,
	}

	// 3. Create cache manager
	// 3. 创建缓存管理器
	cacheManager := NewCacheManager(redisClient, userConfig)

	// 4. Start cache manager
	// 4. 启动缓存管理器
	if err := cacheManager.Start(); err != nil {
		return err
	}
	defer cacheManager.Stop()

	// 5. Create settings manager (assume it exists)
	// 5. 创建设置管理器（假设它存在）
	settingsManager := &settings.SettingsManager{} // Initialize appropriately

	// 6. Get cache data from cache manager
	// 6. 从缓存管理器获取缓存数据
	cacheData := &types.CacheData{
		Users: make(map[string]*types.UserData),
		Mutex: sync.RWMutex{},
	}

	// 7. Create hydrator with default config
	// 7. 使用默认配置创建水合器
	hydratorConfig := DefaultHydratorConfig()
	hydrator := NewHydrator(cacheData, settingsManager, hydratorConfig)

	// 8. Set cache manager for database synchronization
	// 8. 设置缓存管理器以进行数据库同步
	hydrator.SetCacheManager(cacheManager)

	// 9. Set hydrator as hydration notifier for cache manager
	// 9. 将水合器设置为缓存管理器的水合通知器
	cacheManager.SetHydrationNotifier(hydrator)

	// 10. Start hydrator
	// 10. 启动水合器
	ctx := context.Background()
	if err := hydrator.Start(ctx); err != nil {
		return err
	}
	defer hydrator.Stop()

	log.Println("Hydration system with image analysis and database sync started successfully")

	// Example: Add some pending app data that will trigger hydration tasks
	// 示例：添加一些将触发水合任务的待处理应用数据
	pendingAppData := map[string]interface{}{
		"data": map[string]interface{}{
			"apps": map[string]interface{}{
				"nginx": map[string]interface{}{
					"name":        "nginx",
					"version":     "1.21.0",
					"description": "High performance web server",
					"chart_url":   "https://charts.example.com/nginx-1.21.0.tgz",
				},
				"redis": map[string]interface{}{
					"name":        "redis",
					"version":     "6.0.0",
					"description": "In-memory data structure store",
					"chart_url":   "https://charts.example.com/redis-6.0.0.tgz",
				},
			},
		},
	}

	// This will create hydration tasks that include:
	// 这将创建包含以下内容的水合任务：
	// 1. Source chart download
	// 1. 源chart下载
	// 2. Chart rendering
	// 2. Chart渲染
	// 3. Docker image analysis
	// 3. Docker镜像分析
	// 4. Database update with image information
	// 4. 使用镜像信息更新数据库
	if err := cacheManager.SetAppData("admin", "default-source", AppInfoLatestPending, pendingAppData); err != nil {
		log.Printf("Error setting pending app data: %v", err)
		return err
	}

	// Wait for some processing to occur
	// 等待一些处理发生
	time.Sleep(time.Second * 60)

	// Get metrics to see the processing results
	// 获取指标以查看处理结果
	metrics := hydrator.GetMetrics()
	log.Printf("Hydration metrics: %+v", metrics)

	// Get cache stats
	// 获取缓存统计
	cacheStats := cacheManager.GetCacheStats()
	log.Printf("Cache stats: %+v", cacheStats)

	return nil
}

// SetupHydratorWithImageAnalysis sets up a complete hydrator system with image analysis
// SetupHydratorWithImageAnalysis 设置完整的带有镜像分析的水合器系统
func SetupHydratorWithImageAnalysis(
	redisClient *RedisClient,
	userConfig *UserConfig,
	settingsManager *settings.SettingsManager,
) (*CacheManager, *Hydrator, error) {

	// Create and start cache manager
	// 创建并启动缓存管理器
	cacheManager := NewCacheManager(redisClient, userConfig)
	if err := cacheManager.Start(); err != nil {
		return nil, nil, err
	}

	// Create cache data structure
	// 创建缓存数据结构
	cacheData := &types.CacheData{
		Users: make(map[string]*types.UserData),
		Mutex: sync.RWMutex{},
	}

	// Create hydrator
	// 创建水合器
	hydratorConfig := HydratorConfig{
		QueueSize:   1000,
		WorkerCount: 3, // Use 3 workers for better performance
	}
	hydrator := NewHydrator(cacheData, settingsManager, hydratorConfig)

	// Connect cache manager and hydrator
	// 连接缓存管理器和水合器
	hydrator.SetCacheManager(cacheManager)
	cacheManager.SetHydrationNotifier(hydrator)

	return cacheManager, hydrator, nil
}
