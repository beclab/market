package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"market/internal/v2/appinfo"
	"market/internal/v2/history"
	"market/internal/v2/settings"
	"market/internal/v2/task"
	"market/pkg/v2/api"
)

func main() {
	log.Println("Starting Market API Server on port 8080...")

	// Initialize core modules
	// 初始化核心模块

	// 0. Initialize Settings Module
	// 0. 初始化设置模块
	redisHost := getEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := getEnvOrDefault("REDIS_PORT", "6379")
	redisPassword := getEnvOrDefault("REDIS_PASSWORD", "")
	redisDBStr := getEnvOrDefault("REDIS_DB", "0")
	redisDB, err := strconv.Atoi(redisDBStr)
	if err != nil {
		log.Fatalf("Invalid REDIS_DB value: %v", err)
	}

	redisClient, err := settings.NewRedisClient(redisHost, redisPort, redisPassword, redisDB)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}

	settingsManager := settings.NewSettingsManager(redisClient)
	if err := settingsManager.Initialize(); err != nil {
		log.Fatalf("Failed to initialize Settings module: %v", err)
	}
	log.Println("Settings module started successfully")

	// Set the settings manager for API access
	// 为API访问设置设置管理器
	api.SetSettingsManager(settingsManager)

	// 1. Initialize AppInfo Module
	// 1. 初始化应用信息模块
	appInfoConfig := appinfo.DefaultModuleConfig()
	appInfoModule, err := appinfo.NewAppInfoModule(appInfoConfig)
	if err != nil {
		log.Fatalf("Failed to create AppInfo module: %v", err)
	}

	if err := appInfoModule.Start(); err != nil {
		log.Fatalf("Failed to start AppInfo module: %v", err)
	}
	log.Println("AppInfo module started successfully")

	// 2. Initialize History Module
	// 2. 初始化历史记录模块
	historyModule, err := history.NewHistoryModule()
	if err != nil {
		log.Fatalf("Failed to create History module: %v", err)
	}
	log.Println("History module started successfully")

	// 3. Initialize Task Module
	// 3. 初始化任务模块
	taskModule := task.NewTaskModule()
	log.Println("Task module started successfully")

	// Create and start the HTTP server
	// 创建并启动HTTP服务器
	cacheManager := appInfoModule.GetCacheManager()
	server := api.NewServer("8080", cacheManager)

	log.Println("Available endpoints:")
	log.Println("  GET    /api/v2/market                    - Get market information")
	log.Println("  GET    /api/v2/apps                      - Get specific application information (supports multiple queries)")
	log.Println("  GET    /api/v2/apps/{id}/package         - Get rendered installation package for specific application")
	log.Println("  PUT    /api/v2/apps/{id}/config          - Update specific application render configuration")
	log.Println("  GET    /api/v2/logs                      - Query logs by specific conditions")
	log.Println("  POST   /api/v2/apps/{id}/install         - Install application")
	log.Println("  DELETE /api/v2/apps/{id}/install         - Cancel installation")
	log.Println("  DELETE /api/v2/apps/{id}                 - Uninstall application")
	log.Println("  POST   /api/v2/apps/upload               - Upload application installation package")
	log.Println("  GET    /api/v2/settings/market-source    - Get market source configuration")
	log.Println("  PUT    /api/v2/settings/market-source    - Set market source configuration")
	log.Println("")

	// Start HTTP server in a goroutine
	// 在协程中启动HTTP服务器
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal("Failed to start HTTP server:", err)
		}
	}()

	// Setup graceful shutdown
	// 设置优雅关闭
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	// 等待中断信号
	<-c
	log.Println("Shutting down gracefully...")

	// Stop all modules in reverse order
	// 按相反顺序停止所有模块
	log.Println("Stopping Task module...")
	taskModule.Stop()

	log.Println("Stopping History module...")
	if err := historyModule.Close(); err != nil {
		log.Printf("Error stopping History module: %v", err)
	}

	log.Println("Stopping AppInfo module...")
	if err := appInfoModule.Stop(); err != nil {
		log.Printf("Error stopping AppInfo module: %v", err)
	}

	log.Println("Stopping Settings module...")
	if err := redisClient.Close(); err != nil {
		log.Printf("Error stopping Redis client: %v", err)
	}

	log.Println("All modules stopped. Goodbye!")
}

// getEnvOrDefault gets environment variable or returns default value
// 获取环境变量或返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
