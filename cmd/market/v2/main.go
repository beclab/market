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
	"market/internal/v2/client"
	"market/internal/v2/history"
	"market/internal/v2/paymentnew"
	runtimestate "market/internal/v2/runtime"
	"market/internal/v2/settings"
	"market/internal/v2/task"
	"market/internal/v2/types"
	"market/internal/v2/utils"
	"market/internal/v2/watchers"
	"market/pkg/v2/api"

	"github.com/golang/glog"
)

// createAppInfoConfigWithUsers creates AppInfo module configuration with extracted users
func createAppInfoConfigWithUsers(users []string) *appinfo.ModuleConfig {
	// Get default config as base
	config := appinfo.DefaultModuleConfig()

	// If we have extracted users, use them; otherwise fall back to default
	if len(users) > 0 {
		glog.V(2).Infof("Using extracted users: %v", users)
		config.User.UserList = users
	} else {
		glog.V(3).Info("No extracted users found, using default user list")
		// Keep the default user list from DefaultModuleConfig
	}

	return config
}

// loadAppStateDataToUserSource loads app state data from pre-startup step into user's official source
func loadAppStateDataToUserSource(appInfoModule *appinfo.AppInfoModule) {
	// Get all user app state data from pre-startup step
	allUserAppStateData := utils.GetAllUserAppStateData()

	for userID, sourceData := range allUserAppStateData {
		if len(sourceData) == 0 {
			glog.V(2).Info("No app state data found from pre-startup step")
			return
		}

		glog.V(2).Infof("Loading app state data for %d users", len(sourceData))

		// For each user, load their app state data into the official source
		for sourceID, appStateDataList := range sourceData {
			glog.V(2).Infof("Loading %d app states for user: %s, source: %s", len(appStateDataList), userID, sourceID)

			// Set app state data for the user's official source
			err := appInfoModule.SetAppData(userID, sourceID, types.AppStateLatest, map[string]interface{}{
				"app_states": appStateDataList,
			})

			if err != nil {
				glog.Errorf("Failed to load app state data for user %s: %v", userID, err)
			} else {
				glog.V(2).Infof("Successfully loaded %d app states for user %s", len(appStateDataList), userID)
			}
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
	glog.V(1).Info("Verbose logging level 1 enabled")
	glog.V(2).Info("Verbose logging level 2 enabled")
	glog.V(3).Info("Verbose logging level 3 enabled")
	glog.V(4).Info("Verbose logging level 4 enabled")

	// Check dependency service availability before proceeding
	glog.V(2).Info("About to call WaitForDependencyService...")
	utils.WaitForDependencyService()
	glog.V(2).Info("WaitForDependencyService completed")

	if err := client.NewFactory(); err != nil {
		glog.Exitf("Failed to init k8s factory error: %v", err)
	}

	// Start systemenv watcher early and wait for remote service if available
	{
		ctx := context.Background()
		settings.StartSystemEnvWatcher(ctx)
		// Wait up to 20s for OlaresRemoteService from CRD; continue on timeout
		if err := settings.WaitForSystemRemoteService(ctx, 20*time.Second); err != nil {
			glog.Errorf("SystemEnv watcher: %v; continuing startup with fallbacks", err)
		}
	}

	// Upgrade flow: Check and update configurations and cache data (pre-execution)
	glog.V(2).Info("=== Pre-execution: Running upgrade flow ===")
	if err := utils.UpgradeFlow(); err != nil {
		glog.Errorf("Warning: Upgrade flow failed: %v", err)
		// Don't fail the startup, just log the warning
	}
	glog.V(2).Info("=== End pre-execution upgrade flow ===")

	// 0. Initialize Settings Module (Required for API)
	redisHost := utils.GetEnvOrDefault("REDIS_HOST", "localhost")
	redisPort := utils.GetEnvOrDefault("REDIS_PORT", "6379")
	redisPassword := utils.GetEnvOrDefault("REDIS_PASSWORD", "")
	redisDBStr := utils.GetEnvOrDefault("REDIS_DB", "0")
	redisDB, err := strconv.Atoi(redisDBStr)
	if err != nil {
		glog.Exitf("Invalid REDIS_DB value: %v", err)
	}

	var redisClient settings.RedisClient
	if !utils.IsPublicEnvironment() {
		redisClient, err = settings.NewRedisClient(redisHost, redisPort, redisPassword, redisDB)
		if err != nil {
			glog.Exitf("Failed to create Redis client: %v", err)
		}
	}

	// utils.SetRedisClient(redisClient.GetRawClient())

	// Pre-startup step: Setup app service data with retry mechanism
	glog.V(2).Info("=== Pre-startup: Setting up app service data ===")
	for {
		err := utils.SetupAppServiceData()
		if err != nil {
			glog.Errorf("Failed to setup app service data: %v, Retrying in 10 seconds...", err)
			time.Sleep(10 * time.Second)
			continue
		}

		extractedUsers := utils.GetExtractedUsers()
		allUserAppStateData := utils.GetAllUserAppStateData()

		if !utils.IsPublicEnvironment() {

			userCount := len(extractedUsers)
			appCount := 0
			for _, sourceData := range allUserAppStateData {
				for _, appList := range sourceData {
					appCount += len(appList)
				}
			}

			if userCount == 0 || appCount == 0 {
				glog.Warningf("App service data not ready: user count = %d, app count = %d, Retrying in 10 seconds...", userCount, appCount)
				time.Sleep(10 * time.Second)
				continue
			}
		}

		glog.V(2).Info("App service data setup completed successfully")
		break
	}
	glog.V(2).Info("=== End pre-startup step ===")

	// Initialize Settings Manager
	settingsManager := settings.NewSettingsManager(redisClient)
	if err := settingsManager.Initialize(); err != nil {
		glog.Exitf("Failed to initialize Settings module: %v", err)
	}
	glog.V(2).Info("Settings module started successfully")

	// Set the settings manager for API access
	api.SetSettingsManager(settingsManager)

	// 1. Initialize AppInfo Module (Required for cacheManager)
	// Get extracted users from pre-startup step
	extractedUsers := utils.GetExtractedUsers()
	glog.V(2).Infof("Using extracted users for AppInfo module: %v", extractedUsers)

	// Create custom config with extracted users
	appInfoConfig := createAppInfoConfigWithUsers(extractedUsers)
	appInfoModule, err := appinfo.NewAppInfoModule(appInfoConfig)
	if err != nil {
		glog.Exitf("Failed to create AppInfo module: %v", err)
	}

	// Set the settings manager for AppInfo module to use
	appInfoModule.SetSettingsManager(settingsManager)

	if err := appInfoModule.Start(); err != nil {
		glog.Exitf("Failed to start AppInfo module: %v", err)
	}
	glog.V(2).Info("AppInfo module started successfully")

	// Log StatusCorrectionChecker status
	statusChecker := appInfoModule.GetStatusCorrectionChecker()
	if statusChecker != nil {
		glog.V(2).Infof("StatusCorrectionChecker started successfully: %v", statusChecker.IsRunning())
	} else {
		glog.V(2).Info("Warning: StatusCorrectionChecker not available")
	}

	// 1.5. Sync Market Source Configuration with Chart Repository Service
	glog.V(2).Info("=== Step 1.5: Syncing market source configuration with chart repository service ===")
	if err := settings.SyncMarketSourceConfigWithChartRepo(redisClient, settingsManager); err != nil {
		glog.Errorf("Warning: Failed to sync market source configuration: %v", err)
		// Don't fail the startup, just log the warning
	} else {
		glog.V(2).Info("Market source configuration sync completed successfully")
	}
	glog.V(2).Info("=== End Step 1.5 ===")

	// Load app state data into user's official source
	glog.V(2).Info("Loading app state data into user's official source...")
	loadAppStateDataToUserSource(appInfoModule)
	glog.V(2).Info("App state data loaded successfully")

	// Get cacheManager for HTTP server
	glog.V(2).Info("Getting cache manager for HTTP server...")
	cacheManager := appInfoModule.GetCacheManager()
	cacheManager.SetSettingsManager(settingsManager)
	settingsManager.SetCacheManager(cacheManager)
	glog.V(2).Infof("Cache manager obtained successfully: %v", cacheManager != nil)

	// Set cache version getter for GetAppInfoLastInstalled to access app state
	utils.SetCacheVersionGetter(cacheManager)
	glog.V(2).Info("Cache version getter set successfully")

	// Get hydrator for HTTP server
	glog.V(2).Info("Getting hydrator for HTTP server...")
	hydrator := appInfoModule.GetHydrator()
	glog.V(2).Infof("Hydrator obtained successfully: %v", hydrator != nil)

	var taskModule *task.TaskModule
	var historyModule *history.HistoryModule
	if !utils.IsPublicEnvironment() {
		// 1. Init user watch
		var w = watchers.NewWatchers(context.Background(), client.Factory.Config())
		watchers.AddToWatchers[client.User](w, client.UserGVR, cacheManager.HandlerEvent())
		go w.Run(1)

		// 2. Initialize History Module
		historyModule, err = history.NewHistoryModule()
		if err != nil {
			glog.Exitf("Failed to create History module: %v", err)
		}
		glog.V(2).Info("History module started successfully")

		// 3. Initialize Task Module
		taskModule, err = task.NewTaskModule()
		if err != nil {
			glog.Exitf("Failed to create Task module: %v", err)
		}
		// Set history module reference for task recording
		taskModule.SetHistoryModule(historyModule)

		// Set data sender from AppInfo module for system notifications
		dataSender := appInfoModule.GetDataSender()
		if dataSender != nil {
			taskModule.SetDataSender(dataSender)
			glog.V(2).Info("Data sender set in Task module successfully")
		} else {
			glog.V(2).Info("Warning: Data sender not available from AppInfo module, Task module will run without system notifications")
		}

		// Set settings manager for accessing Redis
		if settingsManager != nil {
			taskModule.SetSettingsManager(settingsManager)
			glog.V(2).Info("Settings manager set in Task module successfully")
		} else {
			glog.V(2).Info("Warning: Settings manager not available, Task module will run without Redis access")
		}

		glog.V(2).Info("Task module started successfully")

		// Set task module reference in AppInfo module
		appInfoModule.SetTaskModule(taskModule)
		glog.V(2).Info("Task module reference set in AppInfo module")

		// Set history module reference in AppInfo module
		appInfoModule.SetHistoryModule(historyModule)
		glog.V(2).Info("History module reference set in AppInfo module")

		// 4. Initialize Payment Module Task Manager
		if dataSender != nil {
			paymentnew.InitStateMachine(dataSender, settingsManager)
			glog.V(2).Info("Payment task manager initialized successfully")
		} else {
			glog.V(2).Info("Warning: Data sender not available, payment task manager not initialized")
		}

	}

	// Initialize Runtime State Service
	var runtimeStateService *runtimestate.RuntimeStateService
	// if !utils.IsPublicEnvironment() {
	glog.V(2).Info("=== Initializing Runtime State Service ===")
	stateStore := runtimestate.NewStateStore()
	stateCollector := runtimestate.NewStateCollector(stateStore)

	if taskModule != nil {
		stateCollector.SetTaskModule(taskModule)
	}
	if appInfoModule != nil {
		stateCollector.SetAppInfoModule(appInfoModule)
	}

	runtimeStateService = runtimestate.NewRuntimeStateService(stateStore, stateCollector)

	// Start the service
	if err := runtimeStateService.Start(); err != nil {
		glog.Errorf("Warning: Failed to start runtime state service: %v", err)
	} else {
		glog.V(2).Info("Runtime state service started successfully")
	}
	glog.V(2).Info("=== End Runtime State Service initialization ===")
	// }

	// Create and start the HTTP server
	glog.V(3).Info("Preparing to create HTTP server...")
	glog.V(3).Info("Server configuration before creation:")
	glog.V(3).Info("  - Port: 8080")
	glog.V(3).Infof("  - Cache Manager: %v", cacheManager != nil)
	glog.V(3).Infof("  - Hydrator: %v", hydrator != nil)
	glog.V(3).Infof("  - AppInfo Module: %v", appInfoModule != nil)
	glog.V(3).Infof("  - Task Module: %v", taskModule != nil)
	glog.V(3).Infof("  - History Module: %v", historyModule != nil)
	glog.V(3).Infof("  - Runtime State Service: %v", runtimeStateService != nil)

	server := api.NewServer("8080", cacheManager, hydrator, taskModule, historyModule, runtimeStateService)
	glog.Info("HTTP server instance created successfully")
	// glog.Infof("Task module instance ID: %s", taskModule.GetInstanceID())

	// Lock to system thread
	runtime.LockOSThread()

	go func() {
		glog.V(3).Info("Starting HTTP server goroutine...")
		glog.V(3).Info("Server configuration in goroutine:")
		glog.V(3).Info("  - Port: 8080")
		glog.V(3).Infof("  - Cache Manager: %v", cacheManager != nil)
		glog.V(3).Infof("  - Server instance: %v", server != nil)

		if err := server.Start(); err != nil {
			glog.Errorf("HTTP server failed to start: %v", err)
			glog.Errorf("Error details: %+v", err)
			// Don't use log.Fatal here as it would terminate the program
			// Instead, we'll let the main goroutine handle the shutdown
		}
	}()

	glog.V(2).Info("HTTP server startup initiated")
	glog.V(2).Info("Waiting for server to be ready...")
	time.Sleep(2 * time.Second) // Give the server a moment to start
	glog.V(2).Info("Server startup sequence completed")

	glog.V(2).Info("Starting server on port 8080")

	// Print available endpoints
	glog.V(3).Info("Available endpoints:")
	glog.V(3).Info("  GET    /api/v2/market                    - Get market information")
	glog.V(3).Info("  GET    /api/v2/apps                      - Get specific application information (supports multiple queries)")
	glog.V(3).Info("  GET    /api/v2/apps/{id}/package         - Get rendered installation package for specific application")
	glog.V(3).Info("  PUT    /api/v2/apps/{id}/config          - Update specific application render configuration")
	glog.V(3).Info("  GET    /api/v2/logs                      - Query logs by specific conditions")
	glog.V(3).Info("  POST   /api/v2/apps/{id}/install         - Install application")
	glog.V(3).Info("  DELETE /api/v2/apps/{id}/install         - Cancel installation")
	glog.V(3).Info("  DELETE /api/v2/apps/{id}                 - Uninstall application")
	glog.V(3).Info("  POST   /api/v2/apps/upload               - Upload application installation package")
	glog.V(3).Info("  GET    /api/v2/settings/market-source    - Get market source configuration")
	glog.V(3).Info("  PUT    /api/v2/settings/market-source    - Set market source configuration")
	if !utils.IsPublicEnvironment() {
		glog.V(3).Info("  GET    /api/v2/runtime/state            - Get runtime state (apps, tasks, components)")
	}

	if !utils.IsPublicEnvironment() {
		// Add history record for successful market setup
		glog.V(3).Info("Recording market setup completion in history...")
		historyRecord := &history.HistoryRecord{
			Type:     history.TypeSystem,
			Message:  "market setup finished",
			Time:     time.Now().Unix(),
			App:      "market",
			Account:  "system",
			Extended: "",
		}

		if err := historyModule.StoreRecord(historyRecord); err != nil {
			glog.Errorf("Warning: Failed to record market setup completion: %v", err)
		} else {
			glog.V(3).Infof("Successfully recorded market setup completion with ID: %d", historyRecord.ID)
		}
	}

	glog.V(3).Info("")
	glog.V(3).Info("Helm Repository endpoints (port 82):")
	glog.V(3).Info("  GET    /index.yaml                       - Get Helm repository index (requires X-Market-User and X-Market-Source headers)")
	glog.V(3).Info("  GET    /charts/{filename}.tgz            - Download chart package")
	glog.V(3).Info("  GET    /api/v1/charts                    - List all charts")
	glog.V(3).Info("  POST   /api/v1/charts                    - Upload chart package")
	glog.V(3).Info("  GET    /api/v1/charts/{name}             - Get chart information")
	glog.V(3).Info("  DELETE /api/v1/charts/{name}/{version}   - Delete chart version")
	glog.V(3).Info("  GET    /api/v1/charts/{name}/versions    - Get chart versions")
	glog.V(3).Info("  GET    /api/v1/charts/search             - Search charts")
	glog.V(3).Info("  GET    /api/v1/charts/{name}/{version}/metadata - Get chart metadata")
	glog.V(3).Info("  GET    /api/v1/health                    - Health check")
	glog.V(3).Info("  GET    /api/v1/metrics                   - Get metrics")
	glog.V(3).Info("  GET    /api/v1/config                    - Get repository configuration")
	glog.V(3).Info("  PUT    /api/v1/config                    - Update repository configuration")
	glog.V(3).Info("  POST   /api/v1/index/rebuild             - Rebuild index")
	glog.V(3).Info("")
	glog.V(3).Info("Example Helm Repository usage:")
	glog.V(3).Info("  curl -H 'X-Market-User: user1' -H 'X-Market-Source: web-console' http://localhost:82/index.yaml")
	glog.V(3).Info("  helm repo add myrepo http://localhost:82 --header 'X-Market-User=user1' --header 'X-Market-Source=web-console'")
	glog.V(3).Info("")

	// // Create and start DataWatcherRepo for monitoring state changes
	// glog.Info("=== Creating DataWatcherRepo for state change monitoring ===")

	// // Get Redis client from AppInfo module
	// var redisClientForRepo *appinfo.RedisClient
	// if !utils.IsPublicEnvironment() && appInfoModule != nil {
	// 	redisClientForRepo = appInfoModule.GetRedisClient()
	// } else {
	// 	glog.Info("Public environment detected or AppInfo module not available, skipping DataWatcherRepo creation")
	// }

	// if redisClientForRepo != nil {
	// 	// Create DataWatcherRepo instance
	// 	dataWatcherRepo := appinfo.NewDataWatcherRepo(redisClientForRepo, cacheManager, appInfoModule.GetDataWatcher())

	// 	// Start DataWatcherRepo
	// 	if err := dataWatcherRepo.Start(); err != nil {
	// 		glog.Errorf("Warning: Failed to start DataWatcherRepo: %v", err)
	// 	} else {
	// 		glog.Info("DataWatcherRepo started successfully for monitoring state changes")

	// 		// Set DataWatcherRepo reference in AppInfo module if available
	// 		if appInfoModule != nil {
	// 			// Note: We need to add a method to set DataWatcherRepo in AppInfoModule
	// 			// For now, we'll just log that it's created
	// 			glog.Info("DataWatcherRepo is ready to monitor image info updates and other state changes")
	// 		}
	// 	}
	// }
	// glog.Info("=== End DataWatcherRepo creation ===")

	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-c
	glog.V(3).Info("Shutting down gracefully...")

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
		if runtimeStateService != nil {
			glog.V(3).Info("Stopping Runtime State Service...")
			runtimeStateService.Stop()
		}

		glog.V(3).Info("Stopping Task module...")
		if taskModule != nil {
			taskModule.Stop()
		}

		glog.V(3).Info("Stopping History module...")
		if historyModule != nil {
			if err := historyModule.Close(); err != nil {
				glog.Errorf("Error stopping History module: %v", err)
			}
		}

		glog.V(3).Info("Stopping AppInfo module...")
		if appInfoModule != nil {
			if err := appInfoModule.Stop(); err != nil {
				glog.Errorf("Error stopping AppInfo module: %v", err)
			}
		}

		glog.V(3).Info("Stopping Settings module...")
		if redisClient != nil {
			// Type assert to access Close method
			if closer, ok := redisClient.(interface{ Close() error }); ok {
				if err := closer.Close(); err != nil {
					glog.Errorf("Error stopping Redis client: %v", err)
				}
			}
		}

		glog.V(3).Info("Note: Helm Repository server will stop automatically when main process exits")
	}()

	// Wait for shutdown completion or timeout
	select {
	case <-shutdownCtx.Done():
		glog.V(3).Infof("Shutdown timeout (%v) exceeded, forcing exit", shutdownTimeout)
		os.Exit(1)
	case <-shutdownComplete:
		glog.V(3).Info("All modules stopped. Goodbye!")
	}
}
