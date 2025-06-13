package helm

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"market/internal/v2/appinfo"
)

// ==================== User Context Helper Functions ====================
// ==================== 用户上下文辅助函数 ====================

// extractUserContext extracts user context from HTTP headers
// extractUserContext 从 HTTP 请求头中提取用户上下文信息
func extractUserContext(r *http.Request) *UserContext {
	userID := r.Header.Get("X-Market-User")
	source := r.Header.Get("X-Market-Source")

	// Log user context for debugging
	// 记录用户上下文用于调试
	log.Printf("User Context - UserID: %s, Source: %s", userID, source)

	return &UserContext{
		UserID: userID,
		Source: source,
	}
}

// validateUserContext validates if user context is valid
// validateUserContext 验证用户上下文是否有效
func validateUserContext(userCtx *UserContext) bool {
	// Basic validation - both UserID and Source should be present
	// 基本验证 - UserID 和 Source 都应该存在
	return userCtx.UserID != "" && userCtx.Source != ""
}

// logUserAction logs user actions for audit purposes
// logUserAction 记录用户操作用于审计
func logUserAction(userCtx *UserContext, action, resource string) {
	log.Printf("User Action - UserID: %s, Source: %s, Action: %s, Resource: %s",
		userCtx.UserID, userCtx.Source, action, resource)
}

// ==================== Service Definition ====================
// ==================== 服务定义 ====================

// HelmRepository represents Helm chart repository service
// HelmRepository 表示 Helm chart 仓库服务
type HelmRepository struct {
	server interface{} // 使用 interface{} 避免循环依赖
}

// NewHelmRepository creates a new helm repository service
// NewHelmRepository 创建一个新的 helm 仓库服务
func NewHelmRepository(server interface{}) *HelmRepository {
	return &HelmRepository{
		server: server,
	}
}

// SetupHelmRoutes configures Helm repository routes
// SetupHelmRoutes 配置 Helm 仓库路由
func (hr *HelmRepository) SetupHelmRoutes(router *mux.Router) {
	// Standard Helm Repository API (Helm 客户端兼容接口)
	// 标准 Helm Repository API

	// 1. Get repository index - 获取仓库索引文件
	router.HandleFunc("/index.yaml", hr.getRepositoryIndex).Methods("GET")

	// 2. Download chart package - 下载 chart 包
	router.HandleFunc("/charts/{filename:.+\\.tgz}", hr.downloadChart).Methods("GET")

	// Enhanced Management API (增强管理 API)
	// 增强管理 API - 带版本前缀
	api := router.PathPrefix("/api/v1").Subrouter()

	// 3. List all charts - 列出所有 charts
	api.HandleFunc("/charts", hr.listCharts).Methods("GET")

	// 4. Upload chart package - 上传 chart 包
	api.HandleFunc("/charts", hr.uploadChart).Methods("POST")

	// 5. Get chart information - 获取 chart 信息
	api.HandleFunc("/charts/{name}", hr.getChartInfo).Methods("GET")

	// 6. Delete chart version - 删除 chart 版本
	api.HandleFunc("/charts/{name}/{version}", hr.deleteChartVersion).Methods("DELETE")

	// 7. Get chart versions - 获取 chart 所有版本
	api.HandleFunc("/charts/{name}/versions", hr.getChartVersions).Methods("GET")

	// 8. Search charts - 搜索 charts
	api.HandleFunc("/charts/search", hr.searchCharts).Methods("GET")

	// 9. Get chart metadata - 获取 chart 元数据
	api.HandleFunc("/charts/{name}/{version}/metadata", hr.getChartMetadata).Methods("GET")

	// System Management API (系统管理 API)
	// 系统管理 API

	// 10. Health check - 健康检查
	api.HandleFunc("/health", hr.healthCheck).Methods("GET")

	// 11. Get metrics - 获取统计指标
	api.HandleFunc("/metrics", hr.getMetrics).Methods("GET")

	// 12. Get repository configuration - 获取仓库配置
	api.HandleFunc("/config", hr.getRepositoryConfig).Methods("GET")

	// 13. Update repository configuration - 更新仓库配置
	api.HandleFunc("/config", hr.updateRepositoryConfig).Methods("PUT")

	// 14. Rebuild index - 重建索引
	api.HandleFunc("/index/rebuild", hr.rebuildIndex).Methods("POST")
}

// InitializeHelmRepository initializes the Helm repository with cache manager
// InitializeHelmRepository 使用缓存管理器初始化 Helm 仓库
func InitializeHelmRepository(cacheManager *appinfo.CacheManager) error {
	if cacheManager == nil {
		return fmt.Errorf("cache manager cannot be nil")
	}

	// Set the global cache manager
	// 设置全局缓存管理器
	SetCacheManager(cacheManager)

	log.Printf("Helm Repository: Initialized with cache manager")
	return nil
}

// StartHelmRepositoryServer starts the Helm repository server on port 82
// StartHelmRepositoryServer 在 82 端口启动 Helm 仓库服务器
func StartHelmRepositoryServer() error {
	// Create a new router for Helm repository
	// 为 Helm 仓库创建新的路由器
	router := mux.NewRouter()

	// Create Helm repository service instance
	// 创建 Helm 仓库服务实例
	helmRepo := NewHelmRepository(nil) // Pass nil since we don't need the main server instance

	// Setup Helm routes
	// 设置 Helm 路由
	helmRepo.SetupHelmRoutes(router)

	// Add CORS middleware for web browser access
	// 添加 CORS 中间件以支持浏览器访问
	router.Use(corsMiddleware)

	// Add logging middleware
	// 添加日志中间件
	router.Use(loggingMiddleware)

	// Start the server on port 82
	// 在 82 端口启动服务器
	log.Printf("Starting Helm Repository server on port 82")
	return http.ListenAndServe(":82", router)
}

// StartHelmRepositoryServerWithCacheManager starts the Helm repository server with cache manager
// StartHelmRepositoryServerWithCacheManager 使用缓存管理器启动 Helm 仓库服务器
func StartHelmRepositoryServerWithCacheManager(cacheManager *appinfo.CacheManager) error {
	// Initialize the repository with cache manager
	// 使用缓存管理器初始化仓库
	if err := InitializeHelmRepository(cacheManager); err != nil {
		return fmt.Errorf("failed to initialize Helm repository: %v", err)
	}

	// Start the server
	// 启动服务器
	return StartHelmRepositoryServer()
}

// corsMiddleware adds CORS headers for web browser compatibility
// corsMiddleware 添加 CORS 头以支持浏览器兼容性
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		// 设置 CORS 头
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Market-User, X-Market-Source")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		// 处理预检请求
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs HTTP requests
// loggingMiddleware 记录 HTTP 请求日志
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		// 创建响应写入器包装器以捕获状态码
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Log request details
		// 记录请求详情
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
// responseWriter 包装 http.ResponseWriter 以捕获状态码
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code
// WriteHeader 捕获状态码
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Note: Removed the automatic init() function to allow manual initialization with cache manager
// 注意：移除了自动的 init() 函数，允许使用缓存管理器手动初始化
