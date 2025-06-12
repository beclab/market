package api

import (
	"encoding/json"
	"log"
	"net/http"

	"market/internal/v2/appinfo"
	"market/internal/v2/history"

	"github.com/gorilla/mux"
)

// Server represents the HTTP server
type Server struct {
	router        *mux.Router
	port          string
	cacheManager  *appinfo.CacheManager
	historyModule *history.HistoryModule
}

// NewServer creates a new server instance
func NewServer(port string, cacheManager *appinfo.CacheManager) *Server {
	// Initialize history module
	historyModule, err := history.NewHistoryModule()
	if err != nil {
		log.Printf("Warning: Failed to initialize history module: %v", err)
		// Continue without history module, but log the error
	}

	s := &Server{
		router:        mux.NewRouter(),
		port:          port,
		cacheManager:  cacheManager,
		historyModule: historyModule,
	}
	s.setupRoutes()
	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	// API version prefix
	api := s.router.PathPrefix("/api/v2").Subrouter()

	// 1. Get market debug memory information
	api.HandleFunc("/market/debug-memory", s.getMarketInfo).Methods("GET")

	// Market hash endpoint
	api.HandleFunc("/market/hash", s.getMarketHash).Methods("GET")

	// Market data endpoint
	api.HandleFunc("/market/data", s.getMarketData).Methods("GET")

	// 2. Get specific application information (supports multiple queries)
	api.HandleFunc("/apps", s.getAppsInfo).Methods("POST")

	// 3. Get rendered installation package for specific application (single app only)
	api.HandleFunc("/apps/{id}/package", s.getAppPackage).Methods("GET")

	// 4. Update specific application render configuration (single app only)
	api.HandleFunc("/apps/{id}/config", s.updateAppConfig).Methods("PUT")

	// 5. Query logs by specific conditions
	api.HandleFunc("/logs", s.queryLogs).Methods("GET")

	// 6. Install application (single)
	api.HandleFunc("/apps/{id}/install", s.installApp).Methods("POST")

	// 7. Cancel installation (single)
	api.HandleFunc("/apps/{id}/install", s.cancelInstall).Methods("DELETE")

	// 8. Uninstall application (single)
	api.HandleFunc("/apps/{id}", s.uninstallApp).Methods("DELETE")

	// 9. Upload application installation package
	api.HandleFunc("/apps/upload", s.uploadAppPackage).Methods("POST")

	// Settings endpoints
	// 设置相关接口
	// 10. Get market source configuration
	api.HandleFunc("/settings/market-source", s.getMarketSource).Methods("GET")

	// 11. Set market source configuration
	api.HandleFunc("/settings/market-source", s.setMarketSource).Methods("PUT")
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting server on port %s", s.port)
	return http.ListenAndServe(":"+s.port, s.router)
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

	json.NewEncoder(w).Encode(response)
}
