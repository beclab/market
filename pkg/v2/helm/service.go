package helm

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"

	"market/internal/v2/appinfo"
	"market/internal/v2/utils"
)

// ==================== User Context Helper Functions ====================

// extractUserContext extracts user context from HTTP headers
func extractUserContext(r *http.Request) *UserContext {
	userID := r.Header.Get("X-Market-User")
	source := r.Header.Get("X-Market-Source")

	// Log user context for debugging
	log.Printf("User Context - UserID: %s, Source: %s", userID, source)

	return &UserContext{
		UserID: userID,
		Source: source,
	}
}

// validateUserContext validates if user context is valid
func validateUserContext(userCtx *UserContext) bool {
	// Basic validation - both UserID and Source should be present
	return userCtx.UserID != "" && userCtx.Source != ""
}

// logUserAction logs user actions for audit purposes
func logUserAction(userCtx *UserContext, action, resource string) {
	log.Printf("User Action - UserID: %s, Source: %s, Action: %s, Resource: %s",
		userCtx.UserID, userCtx.Source, action, resource)
}

// ==================== Service Definition ====================

// HelmRepository represents Helm chart repository service
type HelmRepository struct {
	server interface{}
}

// NewHelmRepository creates a new helm repository service
func NewHelmRepository(server interface{}) *HelmRepository {
	return &HelmRepository{
		server: server,
	}
}

// SetupHelmRoutes configures Helm repository routes
func (hr *HelmRepository) SetupHelmRoutes(router *mux.Router) {

	// 1. Get repository index
	router.HandleFunc("/index.yaml", hr.getRepositoryIndex).Methods("GET")

	// 2. Download chart package
	router.HandleFunc("/charts/{filename:.+\\.tgz}", hr.downloadChart).Methods("GET")

	// Enhanced Management API
	api := router.PathPrefix("/api/v1").Subrouter()

	// 3. List all charts
	api.HandleFunc("/charts", hr.listCharts).Methods("GET")

	// 4. Upload chart package
	api.HandleFunc("/charts", hr.uploadChart).Methods("POST")

	// 5. Get chart information
	api.HandleFunc("/charts/{name}", hr.getChartInfo).Methods("GET")

	// 6. Delete chart version
	api.HandleFunc("/charts/{name}/{version}", hr.deleteChartVersion).Methods("DELETE")

	// 7. Get chart versions
	api.HandleFunc("/charts/{name}/versions", hr.getChartVersions).Methods("GET")

	// 8. Search charts
	api.HandleFunc("/charts/search", hr.searchCharts).Methods("GET")

	// 9. Get chart metadata
	api.HandleFunc("/charts/{name}/{version}/metadata", hr.getChartMetadata).Methods("GET")

	// System Management API

	// 10. Health check
	api.HandleFunc("/health", hr.healthCheck).Methods("GET")

	// 11. Get metrics
	api.HandleFunc("/metrics", hr.getMetrics).Methods("GET")

	// 12. Get repository configuration
	api.HandleFunc("/config", hr.getRepositoryConfig).Methods("GET")

	// 13. Update repository configuration
	api.HandleFunc("/config", hr.updateRepositoryConfig).Methods("PUT")

	// 14. Rebuild index
	api.HandleFunc("/index/rebuild", hr.rebuildIndex).Methods("POST")
}

// InitializeHelmRepository initializes the Helm repository with cache manager
func InitializeHelmRepository(cacheManager *appinfo.CacheManager) error {
	if cacheManager == nil {
		return fmt.Errorf("cache manager cannot be nil")
	}

	// Set the global cache manager
	SetCacheManager(cacheManager)

	log.Printf("Helm Repository: Initialized with cache manager")
	return nil
}

// InitializeHelmRepositoryWithRedis initializes the Helm repository with cache manager and Redis client
func InitializeHelmRepositoryWithRedis(cacheManager *appinfo.CacheManager, redisConfig *appinfo.RedisConfig) error {
	if cacheManager == nil {
		return fmt.Errorf("cache manager cannot be nil")
	}

	// Set the global cache manager
	SetCacheManager(cacheManager)

	// Create Redis client directly
	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", redisConfig.Host, redisConfig.Port),
		Password:     redisConfig.Password,
		DB:           redisConfig.DB,
		DialTimeout:  redisConfig.Timeout,
		ReadTimeout:  redisConfig.Timeout,
		WriteTimeout: redisConfig.Timeout,
	})

	// Test connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %v", err)
	}

	utils.SetRedisClient(rdb)

	log.Printf("Helm Repository: Initialized with cache manager and Redis client")
	return nil
}

// StartHelmRepositoryServer starts the Helm repository server on port 82
func StartHelmRepositoryServer() error {
	// Create a new router for Helm repository
	router := mux.NewRouter()

	// Create Helm repository service instance
	helmRepo := NewHelmRepository(nil) // Pass nil since we don't need the main server instance

	// Setup Helm routes
	helmRepo.SetupHelmRoutes(router)

	// Add CORS middleware for web browser access
	router.Use(corsMiddleware)

	// Add logging middleware
	router.Use(loggingMiddleware)

	// Start the server on port 82
	log.Printf("Starting Helm Repository server on port 82")
	return http.ListenAndServe(":82", router)
}

// StartHelmRepositoryServerWithCacheManager starts the Helm repository server with cache manager
func StartHelmRepositoryServerWithCacheManager(cacheManager *appinfo.CacheManager) error {
	// Initialize the repository with cache manager
	if err := InitializeHelmRepository(cacheManager); err != nil {
		return fmt.Errorf("failed to initialize Helm repository: %v", err)
	}

	// Start the server
	return StartHelmRepositoryServer()
}

// StartHelmRepositoryServerWithRedis starts the Helm repository server with cache manager and Redis client
func StartHelmRepositoryServerWithRedis(cacheManager *appinfo.CacheManager, redisConfig *appinfo.RedisConfig) error {
	// Initialize the repository with cache manager and Redis client
	if err := InitializeHelmRepositoryWithRedis(cacheManager, redisConfig); err != nil {
		return fmt.Errorf("failed to initialize Helm repository: %v", err)
	}

	// Start the server
	return StartHelmRepositoryServer()
}

// corsMiddleware adds CORS headers for web browser compatibility
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Market-User, X-Market-Source")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Log request details
		log.Printf("Helm Repository API: %s %s - %d - %v - %s",
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			time.Since(start),
			r.UserAgent(),
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
