package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"market/internal/v2/appinfo"
	"market/internal/v2/history"
	"market/internal/v2/settings"
	"market/internal/v2/task"
	"market/pkg/v2/api"
	"market/pkg/v2/helm"

	"github.com/golang/glog"
)

func main() {
	log.Printf("Starting market application...")

	// Initialize glog for debug logging
	// 初始化glog用于调试日志
	flag.Set("logtostderr", "true")
	flag.Set("v", "2")
	flag.Parse()
	defer glog.Flush()

	log.Println("Starting Market API Server on port 8080...")
	glog.Info("glog initialized for debug logging")

	// 0. Initialize Settings Module (Required for API)
	// 0. 初始化设置模块（API所需）
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

	// 1. Initialize AppInfo Module (Required for cacheManager)
	// 1. 初始化应用信息模块（cacheManager所需）
	appInfoConfig := appinfo.DefaultModuleConfig()
	appInfoModule, err := appinfo.NewAppInfoModule(appInfoConfig)
	if err != nil {
		log.Fatalf("Failed to create AppInfo module: %v", err)
	}

	if err := appInfoModule.Start(); err != nil {
		log.Fatalf("Failed to start AppInfo module: %v", err)
	}
	log.Println("AppInfo module started successfully")

	// Get cacheManager for HTTP server
	// 获取HTTP服务器所需的cacheManager
	log.Printf("Getting cache manager for HTTP server...")
	cacheManager := appInfoModule.GetCacheManager()
	log.Printf("Cache manager obtained successfully: %v", cacheManager != nil)

	// Create and start the HTTP server
	// 创建并启动HTTP服务器
	log.Printf("Preparing to create HTTP server...")
	log.Printf("Server configuration before creation:")
	log.Printf("  - Port: 8080")
	log.Printf("  - Cache Manager: %v", cacheManager != nil)
	log.Printf("  - AppInfo Module: %v", appInfoModule != nil)

	server := api.NewServer("8080", cacheManager)
	log.Printf("HTTP server instance created successfully")

	// 锁定到系统线程
	runtime.LockOSThread()

	// 启动HTTP服务器
	go func() {
		log.Printf("Starting HTTP server goroutine...")
		log.Printf("Server configuration in goroutine:")
		log.Printf("  - Port: 8080")
		log.Printf("  - Cache Manager: %v", cacheManager != nil)
		log.Printf("  - Server instance: %v", server != nil)

		if err := server.Start(); err != nil {
			log.Printf("HTTP server failed to start: %v", err)
			log.Printf("Error details: %+v", err)
			// Don't use log.Fatal here as it would terminate the program
			// Instead, we'll let the main goroutine handle the shutdown
		}
	}()

	log.Printf("HTTP server startup initiated")
	log.Printf("Waiting for server to be ready...")
	time.Sleep(2 * time.Second) // Give the server a moment to start
	log.Printf("Server startup sequence completed")

	log.Println("Starting server on port 8080")

	// Print available endpoints
	// 打印可用的端点
	log.Printf("Available endpoints:")
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

	// 4. Initialize Helm Repository Service
	// 4. 初始化 Helm Repository 服务
	// Start Helm Repository server in a goroutine
	// 在协程中启动 Helm Repository 服务器
	go func() {
		log.Println("Starting Helm Repository server on port 82...")
		if err := helm.StartHelmRepositoryServerWithCacheManager(cacheManager); err != nil {
			log.Printf("Failed to start Helm Repository server: %v", err)
		}
	}()
	log.Println("Helm Repository service initialized successfully")

	log.Println("")
	log.Println("Helm Repository endpoints (port 82):")
	log.Println("  GET    /index.yaml                       - Get Helm repository index (requires X-Market-User and X-Market-Source headers)")
	log.Println("  GET    /charts/{filename}.tgz            - Download chart package")
	log.Println("  GET    /api/v1/charts                    - List all charts")
	log.Println("  POST   /api/v1/charts                    - Upload chart package")
	log.Println("  GET    /api/v1/charts/{name}             - Get chart information")
	log.Println("  DELETE /api/v1/charts/{name}/{version}   - Delete chart version")
	log.Println("  GET    /api/v1/charts/{name}/versions    - Get chart versions")
	log.Println("  GET    /api/v1/charts/search             - Search charts")
	log.Println("  GET    /api/v1/charts/{name}/{version}/metadata - Get chart metadata")
	log.Println("  GET    /api/v1/health                    - Health check")
	log.Println("  GET    /api/v1/metrics                   - Get metrics")
	log.Println("  GET    /api/v1/config                    - Get repository configuration")
	log.Println("  PUT    /api/v1/config                    - Update repository configuration")
	log.Println("  POST   /api/v1/index/rebuild             - Rebuild index")
	log.Println("")
	log.Println("Example Helm Repository usage:")
	log.Println("  curl -H 'X-Market-User: user1' -H 'X-Market-Source: web-console' http://localhost:82/index.yaml")
	log.Println("  helm repo add myrepo http://localhost:82 --header 'X-Market-User=user1' --header 'X-Market-Source=web-console'")
	log.Println("")

	// Setup graceful shutdown
	// 设置优雅关闭
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	// 等待中断信号
	<-c
	log.Println("Shutting down gracefully...")

	// Set a timeout for graceful shutdown
	// 为优雅关闭设置超时
	shutdownTimeout := 30 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Channel to signal completion of shutdown
	// 用于标记关闭完成的通道
	shutdownComplete := make(chan struct{})

	// Run shutdown in a goroutine
	// 在协程中运行关闭过程
	go func() {
		defer close(shutdownComplete)

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

		log.Println("Note: Helm Repository server will stop automatically when main process exits")
	}()

	// Wait for shutdown completion or timeout
	// 等待关闭完成或超时
	select {
	case <-shutdownCtx.Done():
		log.Printf("Shutdown timeout (%v) exceeded, forcing exit", shutdownTimeout)
		os.Exit(1)
	case <-shutdownComplete:
		log.Println("All modules stopped. Goodbye!")
	}
}

// getEnvOrDefault gets environment variable or returns default value
// 获取环境变量或返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
