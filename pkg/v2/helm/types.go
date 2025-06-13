package helm

import (
	"time"
)

// ==================== User Context Types ====================
// ==================== 用户上下文类型 ====================

// UserContext represents user context information from headers
// UserContext 表示从请求头中获取的用户上下文信息
type UserContext struct {
	UserID string `json:"user_id"` // From X-Market-User header
	Source string `json:"source"`  // From X-Market-Source header
}

// RequestContext represents the complete request context
// RequestContext 表示完整的请求上下文
type RequestContext struct {
	User      *UserContext `json:"user"`
	RequestID string       `json:"request_id"`
	Timestamp time.Time    `json:"timestamp"`
}

// ==================== Chart Management Types ====================
// ==================== Chart 管理类型 ====================

// ChartListResponse represents the response for listing charts
// ChartListResponse 表示列出 charts 的响应结构
type ChartListResponse struct {
	Charts     []ChartSummary `json:"charts"`
	TotalCount int            `json:"total_count"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
}

// ChartSummary represents a summary of chart information
// ChartSummary 表示 chart 的摘要信息
type ChartSummary struct {
	Name          string            `json:"name"`
	LatestVersion string            `json:"latest_version"`
	AppVersion    string            `json:"app_version"`
	Description   string            `json:"description"`
	Home          string            `json:"home"`
	Icon          string            `json:"icon"`
	Keywords      []string          `json:"keywords"`
	Maintainers   []ChartMaintainer `json:"maintainers"`
	TotalVersions int               `json:"total_versions"`
	Created       time.Time         `json:"created"`
	Updated       time.Time         `json:"updated"`
	Deprecated    bool              `json:"deprecated"`
}

// ChartMaintainer represents chart maintainer information
// ChartMaintainer 表示 chart 维护者信息
type ChartMaintainer struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ChartUploadResponse represents the response for chart upload
// ChartUploadResponse 表示 chart 上传的响应结构
type ChartUploadResponse struct {
	ChartName  string    `json:"chart_name"`
	Version    string    `json:"version"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// ChartInfoResponse represents detailed chart information
// ChartInfoResponse 表示详细的 chart 信息
type ChartInfoResponse struct {
	Name       string         `json:"name"`
	Versions   []ChartVersion `json:"versions"`
	TotalCount int            `json:"total_count"`
}

// ChartVersion represents a specific version of a chart
// ChartVersion 表示 chart 的特定版本
type ChartVersion struct {
	Version     string            `json:"version"`
	AppVersion  string            `json:"app_version"`
	Description string            `json:"description"`
	Created     time.Time         `json:"created"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	URLs        []string          `json:"urls"`
	Maintainers []ChartMaintainer `json:"maintainers"`
	Keywords    []string          `json:"keywords"`
	Home        string            `json:"home"`
	Sources     []string          `json:"sources"`
	Icon        string            `json:"icon"`
	Deprecated  bool              `json:"deprecated"`
}

// ChartSearchResponse represents search results
// ChartSearchResponse 表示搜索结果
type ChartSearchResponse struct {
	Results    []ChartSummary `json:"results"`
	TotalCount int            `json:"total_count"`
	Query      string         `json:"query"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
}

// ChartMetadata represents chart metadata information
// ChartMetadata 表示 chart 元数据信息
type ChartMetadata struct {
	APIVersion   string            `json:"apiVersion"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	KubeVersion  string            `json:"kubeVersion"`
	Description  string            `json:"description"`
	Type         string            `json:"type"`
	Keywords     []string          `json:"keywords"`
	Home         string            `json:"home"`
	Sources      []string          `json:"sources"`
	Dependencies []ChartDependency `json:"dependencies"`
	Maintainers  []ChartMaintainer `json:"maintainers"`
	Icon         string            `json:"icon"`
	AppVersion   string            `json:"appVersion"`
	Deprecated   bool              `json:"deprecated"`
	Annotations  map[string]string `json:"annotations"`
}

// ChartDependency represents a chart dependency
// ChartDependency 表示 chart 依赖
type ChartDependency struct {
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Repository string   `json:"repository"`
	Condition  string   `json:"condition,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Alias      string   `json:"alias,omitempty"`
}

// ==================== System Management Types ====================
// ==================== 系统管理类型 ====================

// HealthStatus represents health check response
// HealthStatus 表示健康检查响应
type HealthStatus struct {
	Status      string            `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Version     string            `json:"version"`
	Uptime      string            `json:"uptime"`
	Components  map[string]string `json:"components"`
	TotalCharts int               `json:"total_charts"`
}

// RepositoryMetrics represents repository metrics
// RepositoryMetrics 表示仓库指标
type RepositoryMetrics struct {
	TotalCharts        int               `json:"total_charts"`
	TotalVersions      int               `json:"total_versions"`
	TotalDownloads     int64             `json:"total_downloads"`
	TotalUploads       int64             `json:"total_uploads"`
	StorageUsed        int64             `json:"storage_used"`
	StorageLimit       int64             `json:"storage_limit"`
	LastUpdate         time.Time         `json:"last_update"`
	PopularCharts      []ChartPopularity `json:"popular_charts"`
	RecentActivity     []ActivityRecord  `json:"recent_activity"`
	PerformanceMetrics PerformanceStats  `json:"performance"`
}

// ChartPopularity represents chart popularity statistics
// ChartPopularity 表示 chart 流行度统计
type ChartPopularity struct {
	ChartName     string `json:"chart_name"`
	DownloadCount int64  `json:"download_count"`
	Rank          int    `json:"rank"`
}

// ActivityRecord represents recent activity
// ActivityRecord 表示最近活动记录
type ActivityRecord struct {
	Type      string    `json:"type"` // "upload", "download", "delete"
	ChartName string    `json:"chart_name"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	UserAgent string    `json:"user_agent,omitempty"`
}

// PerformanceStats represents performance statistics
// PerformanceStats 表示性能统计
type PerformanceStats struct {
	AvgResponseTime   float64 `json:"avg_response_time_ms"`
	RequestsPerMinute float64 `json:"requests_per_minute"`
	ErrorRate         float64 `json:"error_rate_percent"`
	CacheHitRate      float64 `json:"cache_hit_rate_percent"`
}

// RepositoryConfig represents repository configuration
// RepositoryConfig 表示仓库配置
type RepositoryConfig struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	BaseURL        string            `json:"base_url"`
	StorageType    string            `json:"storage_type"`
	StoragePath    string            `json:"storage_path"`
	MaxFileSize    int64             `json:"max_file_size"`
	IndexCacheTTL  int               `json:"index_cache_ttl"`
	EnableMetrics  bool              `json:"enable_metrics"`
	EnableLogging  bool              `json:"enable_logging"`
	Authentication AuthConfiguration `json:"authentication"`
	Features       FeatureFlags      `json:"features"`
}

// AuthConfiguration represents authentication settings
// AuthConfiguration 表示认证设置
type AuthConfiguration struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"` // "basic", "token", "oauth"
	Realm   string `json:"realm,omitempty"`
}

// FeatureFlags represents feature toggle settings
// FeatureFlags 表示功能开关设置
type FeatureFlags struct {
	ChartValidation bool `json:"chart_validation"`
	Provenance      bool `json:"provenance"`
	Signing         bool `json:"signing"`
	CORS            bool `json:"cors"`
	RateLimit       bool `json:"rate_limit"`
}

// IndexRebuildResponse represents index rebuild response
// IndexRebuildResponse 表示索引重建响应
type IndexRebuildResponse struct {
	Status        string    `json:"status"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	Duration      string    `json:"duration"`
	ChartsScanned int       `json:"charts_scanned"`
	IndexUpdated  bool      `json:"index_updated"`
}
