package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"market/internal/v2/appinfo"
	"market/internal/v2/history"
	"market/internal/v2/runtime"
	"market/internal/v2/task"

	"github.com/golang/glog"
	"github.com/gorilla/mux"
)

// Server represents the HTTP server
type Server struct {
	router              *mux.Router
	port                string
	cacheManager        *appinfo.CacheManager
	hydrator            *appinfo.Hydrator
	historyModule       *history.HistoryModule
	taskModule          *task.TaskModule
	localRepo           *appinfo.LocalRepo
	runtimeStateService *runtime.RuntimeStateService
}

// NewServer creates a new server instance
func NewServer(port string, cacheManager *appinfo.CacheManager, hydrator *appinfo.Hydrator, taskModule *task.TaskModule, historyModule *history.HistoryModule, runtimeStateService *runtime.RuntimeStateService) *Server {
	glog.V(2).Infof("Creating new server instance with port: %s", port)
	glog.V(3).Infof("Cache manager provided: %v", cacheManager != nil)
	glog.V(3).Infof("Hydrator provided: %v", hydrator != nil)
	glog.V(3).Infof("Task module provided: %v", taskModule != nil)
	glog.V(3).Infof("History module provided: %v", historyModule != nil)
	glog.V(3).Infof("Runtime state service provided: %v", runtimeStateService != nil)

	// Create LocalRepo instance
	localRepo := appinfo.NewLocalRepo(cacheManager)
	if taskModule != nil {
		localRepo.SetTaskModule(taskModule)
	}

	glog.V(3).Infof("Creating router...")
	s := &Server{
		router:              mux.NewRouter(),
		port:                port,
		cacheManager:        cacheManager,
		hydrator:            hydrator,
		historyModule:       historyModule,
		taskModule:          taskModule,
		localRepo:           localRepo,
		runtimeStateService: runtimeStateService,
	}

	glog.V(2).Info("Server struct created successfully")

	glog.V(3).Info("Setting up routes...")
	s.setupRoutes()
	glog.V(3).Info("Routes setup completed")

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// API version prefix
	api := s.router.PathPrefix("/app-store/api/v2").Subrouter()
	glog.V(3).Info("Setting up API routes with prefix: /app-store/api/v2")

	// 1. Get market debug memory information
	api.HandleFunc("/market/debug-memory", s.getMarketInfo).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/market/debug-memory")

	// Market hash endpoint
	api.HandleFunc("/market/hash", s.getMarketHash).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/market/hash")

	// Market data endpoint
	api.HandleFunc("/market/data", s.getMarketData).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/market/data")

	// Market state endpoint
	api.HandleFunc("/market/state", s.getMarketState).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/market/state")

	// 2. Get specific application information (supports multiple queries)
	api.HandleFunc("/apps", s.getAppsInfo).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps")

	// 3. Get rendered installation package for specific application (single app only)
	api.HandleFunc("/apps/{id}/package", s.getAppPackage).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/apps/{id}/package")

	// 4. Update specific application render configuration (single app only)
	api.HandleFunc("/apps/{id}/config", s.updateAppConfig).Methods("PUT")
	glog.V(3).Info("Route configured: PUT /app-store/api/v2/apps/{id}/config")

	// 5. Query logs by specific conditions
	api.HandleFunc("/logs", s.queryLogs).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/logs")

	// 6. Install application (single)
	api.HandleFunc("/apps/{id}/install", s.installApp).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/{id}/install")

	// 6.1. Clone application (single)
	api.HandleFunc("/apps/{id}/clone", s.cloneApp).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/{id}/clone")

	// 7. Cancel installation (single)
	api.HandleFunc("/apps/{id}/install", s.cancelInstall).Methods("DELETE")
	glog.V(3).Info("Route configured: DELETE /app-store/api/v2/apps/{id}/install")

	// 8. Upgrade application (single)
	api.HandleFunc("/apps/{id}/upgrade", s.upgradeApp).Methods("PUT")
	glog.V(3).Info("Route configured: PUT /app-store/api/v2/apps/{id}/upgrade")

	// 9. Uninstall application (single)
	api.HandleFunc("/apps/{id}", s.uninstallApp).Methods("DELETE")
	glog.V(3).Info("Route configured: DELETE /app-store/api/v2/apps/{id}")

	// 10. Upload application installation package
	api.HandleFunc("/apps/upload", s.uploadAppPackage).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/upload")

	// 11. Open application
	api.HandleFunc("/apps/open", s.openApp).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/open")

	// 12. Resume application
	api.HandleFunc("/apps/resume", s.resumeApp).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/resume")

	// 13. Stop application
	api.HandleFunc("/apps/stop", s.stopApp).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/stop")

	// Settings endpoints
	// 14. Get market source configuration
	api.HandleFunc("/settings/market-source", s.getMarketSource).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/settings/market-source")

	// 15. Add market source configuration
	api.HandleFunc("/settings/market-source", s.addMarketSource).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/settings/market-source")

	// 16. Delete market source configuration
	api.HandleFunc("/settings/market-source/{id}", s.deleteMarketSource).Methods("DELETE")
	glog.V(3).Info("Route configured: DELETE /app-store/api/v2/settings/market-source/{id}")

	// 17. Get market settings
	api.HandleFunc("/settings/market-settings", s.getMarketSettings).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/settings/market-settings")

	// 18. Update market settings
	api.HandleFunc("/settings/market-settings", s.updateMarketSettings).Methods("PUT")
	glog.V(3).Info("Route configured: PUT /app-store/api/v2/settings/market-settings")

	// 19. Get system status aggregation
	api.HandleFunc("/settings/system-status", s.getSystemStatus).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/settings/system-status")

	// Diagnostic endpoints
	// 18. Get cache and Redis diagnostic information
	api.HandleFunc("/diagnostic", s.getDiagnostic).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/diagnostic")

	// 19. Force reload cache data from Redis
	api.HandleFunc("/reload", s.forceReloadFromRedis).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/reload")

	// 20. Cleanup invalid pending data
	api.HandleFunc("/cleanup", s.cleanupInvalidPendingData).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/cleanup")

	// 21. Get application version history
	api.HandleFunc("/apps/version-history", s.getAppVersionHistory).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/apps/version-history")

	// 22. Delete local source application
	api.HandleFunc("/local-apps/delete", s.deleteLocalApp).Methods("DELETE")
	glog.V(3).Info("Route configured: DELETE /app-store/api/v2/local-apps/delete")

	// 23. Submit signature for payment processing
	api.HandleFunc("/payment/submit-signature", s.submitSignature).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/payment/submit-signature")

	// 24. Check app payment status
	// New route with source parameter (recommended)
	api.HandleFunc("/sources/{source}/apps/{id}/payment-status", s.getAppPaymentStatus).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/sources/{source}/apps/{id}/payment-status")

	// 24.1 Purchase app
	api.HandleFunc("/sources/{source}/apps/{id}/purchase", s.purchaseApp).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/sources/{source}/apps/{id}/purchase")

	// 24.2 Restore purchase
	api.HandleFunc("/sources/{source}/apps/{id}/restore-purchase", s.restorePurchase).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/sources/{source}/apps/{id}/restore-purchase")

	// Legacy route for backward compatibility (searches all sources)
	api.HandleFunc("/apps/{id}/payment-status", s.getAppPaymentStatusLegacy).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/apps/{id}/payment-status (legacy)")

	// 25. Start payment polling after frontend payment completion
	api.HandleFunc("/payment/start-polling", s.startPaymentPolling).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/payment/start-polling")

	// 26. Frontend signals payment readiness
	api.HandleFunc("/payment/frontend-start", s.startFrontendPayment).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/payment/frontend-start")

	// 26.1 Resend VC to LarePass for persistence
	api.HandleFunc("/payment/resend-vc", s.resendPaymentVC).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/payment/resend-vc")

	// 27. Fetch signature callback (from LarePass)
	api.HandleFunc("/payment/fetch-signature-callback", s.fetchSignatureCallback).Methods("POST")
	glog.V(3).Info("Route configured: POST /app-store/api/v2/payment/fetch-signature-callback")

	// 28. Get runtime state
	api.HandleFunc("/runtime/state", s.getRuntimeState).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/runtime/state")

	// 29. Get runtime dashboard (HTML page)
	api.HandleFunc("/runtime/dashboard", s.getRuntimeDashboard).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/runtime/dashboard")

	// 30. Get runtime dashboard-app (HTML page for app processing flow)
	api.HandleFunc("/runtime/dashboard-app", s.getRuntimeDashboardApp).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/runtime/dashboard-app")

	// 31. Get payment state machine states
	api.HandleFunc("/payment/states", s.getPaymentStates).Methods("GET")
	glog.V(3).Info("Route configured: GET /app-store/api/v2/payment/states")

	glog.V(2).Info("All routes configured successfully")
}

// Start starts the HTTP server
func (s *Server) Start() error {
	glog.V(2).Infof("Starting server on port %s", s.port)
	glog.V(3).Infof("Server configuration:")
	glog.V(3).Infof("  - Port: %s", s.port)
	glog.V(3).Infof("  - Cache Manager: %v", s.cacheManager != nil)
	glog.V(3).Infof("  - Hydrator: %v", s.hydrator != nil)
	glog.V(3).Infof("  - History Module: %v", s.historyModule != nil)

	// Check if port is available
	addr := ":" + s.port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		glog.Errorf("Port %s is not available: %v", s.port, err)
		return fmt.Errorf("port %s is not available: %v", s.port, err)
	}
	listener.Close()
	glog.V(3).Infof("Port %s is available", s.port)

	// Add middleware for request logging
	s.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			glog.V(2).Infof("Request started: %s %s", r.Method, r.URL.Path)

			next.ServeHTTP(w, r)

			duration := time.Since(start)
			glog.V(3).Infof("Request completed: %s %s (took %v)", r.Method, r.URL.Path, duration)
		})
	})

	glog.V(2).Infof("Server routes configured and ready to accept connections")
	err = http.ListenAndServe(addr, s.router)
	if err != nil {
		glog.Errorf("Failed to start HTTP server: %v", err)
		return err
	}
	return nil
}

// Close gracefully closes the server and its resources
func (s *Server) Close() error {
	glog.V(3).Info("Closing server resources")

	if s.historyModule != nil {
		if err := s.historyModule.Close(); err != nil {
			glog.Errorf("Error closing history module: %v", err)
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
	glog.V(3).Infof("DEBUG: sendResponse - StatusCode: %d, Success: %v, Message: %s", statusCode, success, message)
	glog.V(3).Infof("DEBUG: sendResponse - Data type: %T", data)

	// Try to marshal the response to check for JSON errors
	jsonData, err := json.Marshal(response)
	if err != nil {
		glog.Errorf("ERROR: JSON marshaling failed: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	glog.V(3).Infof("DEBUG: sendResponse - JSON data length: %d bytes, JSON data preview: %s", len(jsonData), string(jsonData[:min(len(jsonData), 200)]))

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
