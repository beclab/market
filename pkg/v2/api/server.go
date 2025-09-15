package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"market/internal/v2/appinfo"
	"market/internal/v2/history"
	"market/internal/v2/task"

	"github.com/gorilla/mux"
)

// Server represents the HTTP server
type Server struct {
	router        *mux.Router
	port          string
	cacheManager  *appinfo.CacheManager
	hydrator      *appinfo.Hydrator
	historyModule *history.HistoryModule
	taskModule    *task.TaskModule
	localRepo     *appinfo.LocalRepo
}

// NewServer creates a new server instance
func NewServer(port string, cacheManager *appinfo.CacheManager, hydrator *appinfo.Hydrator, taskModule *task.TaskModule, historyModule *history.HistoryModule) *Server {
	log.Printf("Creating new server instance with port: %s", port)
	log.Printf("Cache manager provided: %v", cacheManager != nil)
	log.Printf("Hydrator provided: %v", hydrator != nil)
	log.Printf("Task module provided: %v", taskModule != nil)
	log.Printf("History module provided: %v", historyModule != nil)

	// Create LocalRepo instance
	localRepo := appinfo.NewLocalRepo(cacheManager)
	if taskModule != nil {
		localRepo.SetTaskModule(taskModule)
	}

	log.Printf("Creating router...")
	s := &Server{
		router:        mux.NewRouter(),
		port:          port,
		cacheManager:  cacheManager,
		hydrator:      hydrator,
		historyModule: historyModule,
		taskModule:    taskModule,
		localRepo:     localRepo,
	}

	log.Printf("Server struct created successfully")

	log.Printf("Setting up routes...")
	s.setupRoutes()
	log.Printf("Routes setup completed")

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// API version prefix
	api := s.router.PathPrefix("/app-store/api/v2").Subrouter()
	log.Printf("Setting up API routes with prefix: /app-store/api/v2")

	// 1. Get market debug memory information
	api.HandleFunc("/market/debug-memory", s.getMarketInfo).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/market/debug-memory")

	// Market hash endpoint
	api.HandleFunc("/market/hash", s.getMarketHash).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/market/hash")

	// Market data endpoint
	api.HandleFunc("/market/data", s.getMarketData).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/market/data")

	// Market state endpoint
	api.HandleFunc("/market/state", s.getMarketState).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/market/state")

	// 2. Get specific application information (supports multiple queries)
	api.HandleFunc("/apps", s.getAppsInfo).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps")

	// 3. Get rendered installation package for specific application (single app only)
	api.HandleFunc("/apps/{id}/package", s.getAppPackage).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/apps/{id}/package")

	// 4. Update specific application render configuration (single app only)
	api.HandleFunc("/apps/{id}/config", s.updateAppConfig).Methods("PUT")
	log.Printf("Route configured: PUT /app-store/api/v2/apps/{id}/config")

	// 5. Query logs by specific conditions
	api.HandleFunc("/logs", s.queryLogs).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/logs")

	// 6. Install application (single)
	api.HandleFunc("/apps/{id}/install", s.installApp).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps/{id}/install")

	// 7. Cancel installation (single)
	api.HandleFunc("/apps/{id}/install", s.cancelInstall).Methods("DELETE")
	log.Printf("Route configured: DELETE /app-store/api/v2/apps/{id}/install")

	// 8. Upgrade application (single)
	api.HandleFunc("/apps/{id}/upgrade", s.upgradeApp).Methods("PUT")
	log.Printf("Route configured: PUT /app-store/api/v2/apps/{id}/upgrade")

	// 9. Uninstall application (single)
	api.HandleFunc("/apps/{id}", s.uninstallApp).Methods("DELETE")
	log.Printf("Route configured: DELETE /app-store/api/v2/apps/{id}")

	// 10. Upload application installation package
	api.HandleFunc("/apps/upload", s.uploadAppPackage).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps/upload")

	// 11. Open application
	api.HandleFunc("/apps/open", s.openApp).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps/open")

	// 12. Resume application
	api.HandleFunc("/apps/resume", s.resumeApp).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps/resume")

	// 13. Stop application
	api.HandleFunc("/apps/stop", s.stopApp).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps/stop")

	// Settings endpoints
	// 14. Get market source configuration
	api.HandleFunc("/settings/market-source", s.getMarketSource).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/settings/market-source")

	// 15. Add market source configuration
	api.HandleFunc("/settings/market-source", s.addMarketSource).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/settings/market-source")

	// 16. Delete market source configuration
	api.HandleFunc("/settings/market-source/{id}", s.deleteMarketSource).Methods("DELETE")
	log.Printf("Route configured: DELETE /app-store/api/v2/settings/market-source/{id}")

	// 17. Get market settings
	api.HandleFunc("/settings/market-settings", s.getMarketSettings).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/settings/market-settings")

	// 18. Update market settings
	api.HandleFunc("/settings/market-settings", s.updateMarketSettings).Methods("PUT")
	log.Printf("Route configured: PUT /app-store/api/v2/settings/market-settings")

	// 19. Get system status aggregation
	api.HandleFunc("/settings/system-status", s.getSystemStatus).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/settings/system-status")

	// Diagnostic endpoints
	// 18. Get cache and Redis diagnostic information
	api.HandleFunc("/diagnostic", s.getDiagnostic).Methods("GET")
	log.Printf("Route configured: GET /app-store/api/v2/diagnostic")

	// 19. Force reload cache data from Redis
	api.HandleFunc("/reload", s.forceReloadFromRedis).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/reload")

	// 20. Cleanup invalid pending data
	api.HandleFunc("/cleanup", s.cleanupInvalidPendingData).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/cleanup")

	// 21. Get application version history
	api.HandleFunc("/apps/version-history", s.getAppVersionHistory).Methods("POST")
	log.Printf("Route configured: POST /app-store/api/v2/apps/version-history")

	// 22. Delete local source application
	api.HandleFunc("/local-apps/delete", s.deleteLocalApp).Methods("DELETE")
	log.Printf("Route configured: DELETE /app-store/api/v2/local-apps/delete")

	log.Printf("All routes configured successfully")
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting server on port %s", s.port)
	log.Printf("Server configuration:")
	log.Printf("  - Port: %s", s.port)
	log.Printf("  - Cache Manager: %v", s.cacheManager != nil)
	log.Printf("  - Hydrator: %v", s.hydrator != nil)
	log.Printf("  - History Module: %v", s.historyModule != nil)

	// Check if port is available
	addr := ":" + s.port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Port %s is not available: %v", s.port, err)
		return fmt.Errorf("port %s is not available: %v", s.port, err)
	}
	listener.Close()
	log.Printf("Port %s is available", s.port)

	// Add middleware for request logging
	s.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			log.Printf("Request started: %s %s", r.Method, r.URL.Path)

			next.ServeHTTP(w, r)

			duration := time.Since(start)
			log.Printf("Request completed: %s %s (took %v)", r.Method, r.URL.Path, duration)
		})
	})

	log.Printf("Server routes configured and ready to accept connections")
	err = http.ListenAndServe(addr, s.router)
	if err != nil {
		log.Printf("Failed to start HTTP server: %v", err)
		return err
	}
	return nil
}

// Close gracefully closes the server and its resources
func (s *Server) Close() error {
	log.Println("Closing server resources")

	if s.historyModule != nil {
		if err := s.historyModule.Close(); err != nil {
			log.Printf("Error closing history module: %v", err)
			return err
		}
	}

	return nil
}

// Response represents a standard API response
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// sendResponse sends a JSON response
func (s *Server) sendResponse(w http.ResponseWriter, statusCode int, success bool, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := Response{
		Success: success,
		Message: message,
		Data:    data,
	}

	// Add debug logging for JSON serialization
	log.Printf("DEBUG: sendResponse - StatusCode: %d, Success: %v, Message: %s", statusCode, success, message)
	log.Printf("DEBUG: sendResponse - Data type: %T", data)

	// Try to marshal the response to check for JSON errors
	jsonData, err := json.Marshal(response)
	if err != nil {
		log.Printf("ERROR: JSON marshaling failed: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("DEBUG: sendResponse - JSON data length: %d bytes", len(jsonData))
	log.Printf("DEBUG: sendResponse - JSON data preview: %s", string(jsonData[:min(len(jsonData), 200)]))

	// Write the JSON data directly
	w.Write(jsonData)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getAppVersionHistory handles POST /api/v2/apps/version-history
func (s *Server) getAppVersionHistory(w http.ResponseWriter, r *http.Request) {
	getAppVersionHistoryHandler(w, r)
}
