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
	"market/internal/v2/types"
	"market/internal/v2/utils"
	"market/pkg/v2/api"
	"market/pkg/v2/helm"

	"github.com/golang/glog"
)

// createAppInfoConfigWithUsers creates AppInfo module configuration with extracted users
func createAppInfoConfigWithUsers(users []string) *appinfo.ModuleConfig {
	// Get default config as base
	config := appinfo.DefaultModuleConfig()

	// If we have extracted users, use them; otherwise fall back to default
	if len(users) > 0 {
		log.Printf("Using extracted users: %v", users)
		config.User.UserList = users
	} else {
		log.Printf("No extracted users found, using default user list")
		// Keep the default user list from DefaultModuleConfig
	}

	return config
}

// loadAppStateDataToUserSource loads app state data from pre-startup step into user's official source
func loadAppStateDataToUserSource(appInfoModule *appinfo.AppInfoModule) {
	// Get all user app state data from pre-startup step
	allUserAppStateData := utils.GetAllUserAppStateData()

	if len(allUserAppStateData) == 0 {
		log.Println("No app state data found from pre-startup step")
		return
	}

	log.Printf("Loading app state data for %d users", len(allUserAppStateData))

	// For each user, load their app state data into the official source
	for userID, appStateDataList := range allUserAppStateData {
		log.Printf("Loading %d app states for user: %s", len(appStateDataList), userID)

		// Set app state data for the user's official source
		err := appInfoModule.SetAppData(userID, "Official-Market-Sources", types.AppStateLatest, map[string]interface{}{
			"app_states": appStateDataList,
		})

		if err != nil {
			log.Printf("Failed to load app state data for user %s: %v", userID, err)
		} else {
			log.Printf("Successfully loaded %d app states for user %s", len(appStateDataList), userID)
		}
	}
}

func main() {
	log.Printf("Starting market application...")

	// Initialize glog for debug logging
	flag.Set("logtostderr", "true")
	flag.Set("v", "2")
	flag.Parse()
	defer glog.Flush()

	log.Println("Starting Market API Server on port 8080...")
	glog.Info("glog initialized for debug logging")

	// Pre-startup step: Setup app service data
	log.Println("=== Pre-startup: Setting up app service data ===")
	if err := utils.SetupAppServiceData(); err != nil {
		log.Printf("Warning: Failed to setup app service data: %v", err)
		log.Println("Continuing with startup process...")
	} else {
		log.Println("App service data setup completed successfully")
	}
	log.Println("=== End pre-startup step ===")

	// 0. Initialize Settings Module (Required for API)
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
	api.SetSettingsManager(settingsManager)

	// 1. Initialize AppInfo Module (Required for cacheManager)
	// Get extracted users from pre-startup step
	extractedUsers := utils.GetExtractedUsers()
	log.Printf("Using extracted users for AppInfo module: %v", extractedUsers)

	// Create custom config with extracted users
	appInfoConfig := createAppInfoConfigWithUsers(extractedUsers)
	appInfoModule, err := appinfo.NewAppInfoModule(appInfoConfig)
	if err != nil {
		log.Fatalf("Failed to create AppInfo module: %v", err)
	}

	if err := appInfoModule.Start(); err != nil {
		log.Fatalf("Failed to start AppInfo module: %v", err)
	}
	log.Println("AppInfo module started successfully")

	// Load app state data into user's official source
	log.Println("Loading app state data into user's official source...")
	loadAppStateDataToUserSource(appInfoModule)
	log.Println("App state data loaded successfully")

	// Get cacheManager for HTTP server
	log.Printf("Getting cache manager for HTTP server...")
	cacheManager := appInfoModule.GetCacheManager()
	log.Printf("Cache manager obtained successfully: %v", cacheManager != nil)

	// Create and start the HTTP server
	log.Printf("Preparing to create HTTP server...")
	log.Printf("Server configuration before creation:")
	log.Printf("  - Port: 8080")
	log.Printf("  - Cache Manager: %v", cacheManager != nil)
	log.Printf("  - AppInfo Module: %v", appInfoModule != nil)

	server := api.NewServer("8080", cacheManager)
	log.Printf("HTTP server instance created successfully")

	// Lock to system thread
	runtime.LockOSThread()

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
	historyModule, err := history.NewHistoryModule()
	if err != nil {
		log.Fatalf("Failed to create History module: %v", err)
	}
	log.Println("History module started successfully")

	// 3. Initialize Task Module
	taskModule := task.NewTaskModule()
	// Set history module reference for task recording
	taskModule.SetHistoryModule(historyModule)
	log.Println("Task module started successfully")

	// 4. Initialize Helm Repository Service
	// Start Helm Repository server in a goroutine
	go func() {
		log.Println("Starting Helm Repository server on port 82...")
		if err := helm.StartHelmRepositoryServerWithCacheManager(cacheManager); err != nil {
			log.Printf("Failed to start Helm Repository server: %v", err)
		}
	}()
	log.Println("Helm Repository service initialized successfully")

	// Add history record for successful market setup
	log.Println("Recording market setup completion in history...")
	historyRecord := &history.HistoryRecord{
		Type:     history.TypeSystem,
		Message:  "market setup finished",
		Time:     time.Now().Unix(),
		App:      "market",
		Account:  "system",
		Extended: "",
	}

	if err := historyModule.StoreRecord(historyRecord); err != nil {
		log.Printf("Warning: Failed to record market setup completion: %v", err)
	} else {
		log.Printf("Successfully recorded market setup completion with ID: %d", historyRecord.ID)
	}

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
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-c
	log.Println("Shutting down gracefully...")

	// Set a timeout for graceful shutdown
	shutdownTimeout := 30 * time.Second
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Channel to signal completion of shutdown
	shutdownComplete := make(chan struct{})

	// Run shutdown in a goroutine
	go func() {
		defer close(shutdownComplete)

		// Stop all modules in reverse order
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
	select {
	case <-shutdownCtx.Done():
		log.Printf("Shutdown timeout (%v) exceeded, forcing exit", shutdownTimeout)
		os.Exit(1)
	case <-shutdownComplete:
		log.Println("All modules stopped. Goodbye!")
	}
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
