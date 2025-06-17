package helm

import (
	"time"
)

// ==================== User Context Types ====================

// UserContext represents user context information from headers
type UserContext struct {
	UserID string `json:"user_id"` // From X-Market-User header
	Source string `json:"source"`  // From X-Market-Source header
}

// RequestContext represents the complete request context
type RequestContext struct {
	User      *UserContext `json:"user"`
	RequestID string       `json:"request_id"`
	Timestamp time.Time    `json:"timestamp"`
}

// ==================== Chart Management Types ====================

// ChartListResponse represents the response for listing charts
type ChartListResponse struct {
	Charts     []ChartSummary `json:"charts"`
	TotalCount int            `json:"total_count"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
}

// ChartSummary represents a summary of chart information
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
type ChartMaintainer struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ChartUploadResponse represents the response for chart upload
type ChartUploadResponse struct {
	ChartName  string    `json:"chart_name"`
	Version    string    `json:"version"`
	Filename   string    `json:"filename"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// ChartInfoResponse represents detailed chart information
type ChartInfoResponse struct {
	Name       string         `json:"name"`
	Versions   []ChartVersion `json:"versions"`
	TotalCount int            `json:"total_count"`
}

// ChartVersion represents a specific version of a chart
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
type ChartSearchResponse struct {
	Results    []ChartSummary `json:"results"`
	TotalCount int            `json:"total_count"`
	Query      string         `json:"query"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
}

// ChartMetadata represents chart metadata information
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
type ChartDependency struct {
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Repository string   `json:"repository"`
	Condition  string   `json:"condition,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Alias      string   `json:"alias,omitempty"`
}

// ==================== System Management Types ====================

// HealthStatus represents health check response
type HealthStatus struct {
	Status      string            `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Version     string            `json:"version"`
	Uptime      string            `json:"uptime"`
	Components  map[string]string `json:"components"`
	TotalCharts int               `json:"total_charts"`
}

// RepositoryMetrics represents repository metrics
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
type ChartPopularity struct {
	ChartName     string `json:"chart_name"`
	DownloadCount int64  `json:"download_count"`
	Rank          int    `json:"rank"`
}

// ActivityRecord represents recent activity
type ActivityRecord struct {
	Type      string    `json:"type"` // "upload", "download", "delete"
	ChartName string    `json:"chart_name"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	UserAgent string    `json:"user_agent,omitempty"`
}

// PerformanceStats represents performance statistics
type PerformanceStats struct {
	AvgResponseTime   float64 `json:"avg_response_time_ms"`
	RequestsPerMinute float64 `json:"requests_per_minute"`
	ErrorRate         float64 `json:"error_rate_percent"`
	CacheHitRate      float64 `json:"cache_hit_rate_percent"`
}

// RepositoryConfig represents repository configuration
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
type AuthConfiguration struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"` // "basic", "token", "oauth"
	Realm   string `json:"realm,omitempty"`
}

// FeatureFlags represents feature toggle settings
type FeatureFlags struct {
	ChartValidation bool `json:"chart_validation"`
	Provenance      bool `json:"provenance"`
	Signing         bool `json:"signing"`
	CORS            bool `json:"cors"`
	RateLimit       bool `json:"rate_limit"`
}

// IndexRebuildResponse represents index rebuild response
type IndexRebuildResponse struct {
	Status        string    `json:"status"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	Duration      string    `json:"duration"`
	ChartsScanned int       `json:"charts_scanned"`
	IndexUpdated  bool      `json:"index_updated"`
}
