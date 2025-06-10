package types

import (
	"sync"
	"time"
)

// AppDataType represents different types of app data
type AppDataType string

const (
	AppInfoHistory       AppDataType = "app-info-history"
	AppStateLatest       AppDataType = "app-state-latest"
	AppInfoLatest        AppDataType = "app-info-latest"
	AppInfoLatestPending AppDataType = "app-info-latest-pending"
	Other                AppDataType = "other"
)

// AppData contains the actual data and metadata
type AppData struct {
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// AppInfoHistoryData contains app info history data
type AppInfoHistoryData struct {
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// AppStateLatestData contains latest app state data
type AppStateLatestData struct {
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// AppInfoLatestData contains latest app info data
type AppInfoLatestData struct {
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// Values represents custom rendering parameters
// Values 表示自定义渲染参数
type Values struct {
	CustomParams map[string]interface{} `json:"custom_params,omitempty"`
	Environment  string                 `json:"environment,omitempty"`
	Namespace    string                 `json:"namespace,omitempty"`
	Resources    map[string]interface{} `json:"resources,omitempty"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

// ApplicationInfoEntry represents the structure returned by the applications/info API
// ApplicationInfoEntry 表示应用信息API返回的结构
type ApplicationInfoEntry struct {
	ID string `json:"id"`

	Name        string   `json:"name"`
	CfgType     string   `json:"cfgType"`
	ChartName   string   `json:"chartName"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	AppID       string   `json:"appID"`
	Title       string   `json:"title"`
	Version     string   `json:"version"`
	Categories  []string `json:"categories"`
	VersionName string   `json:"versionName"`

	FullDescription    string                   `json:"fullDescription"`
	UpgradeDescription string                   `json:"upgradeDescription"`
	PromoteImage       []string                 `json:"promoteImage"`
	PromoteVideo       string                   `json:"promoteVideo"`
	SubCategory        string                   `json:"subCategory"`
	Locale             []string                 `json:"locale"`
	Developer          string                   `json:"developer"`
	RequiredMemory     string                   `json:"requiredMemory"`
	RequiredDisk       string                   `json:"requiredDisk"`
	SupportClient      map[string]interface{}   `json:"supportClient"` // Using interface{} for flexibility
	SupportArch        []string                 `json:"supportArch"`
	RequiredGPU        string                   `json:"requiredGPU,omitempty"`
	RequiredCPU        string                   `json:"requiredCPU"`
	Rating             float32                  `json:"rating"`
	Target             string                   `json:"target"`
	Permission         map[string]interface{}   `json:"permission"` // Using interface{} for flexibility
	Entrances          []map[string]interface{} `json:"entrances"`  // Using interface{} for flexibility
	Middleware         map[string]interface{}   `json:"middleware"` // Using interface{} for flexibility
	Options            map[string]interface{}   `json:"options"`    // Using interface{} for flexibility

	Submitter     string                   `json:"submitter"`
	Doc           string                   `json:"doc"`
	Website       string                   `json:"website"`
	FeaturedImage string                   `json:"featuredImage"`
	SourceCode    string                   `json:"sourceCode"`
	License       []map[string]interface{} `json:"license"` // Using interface{} for flexibility
	Legal         []map[string]interface{} `json:"legal"`   // Using interface{} for flexibility
	I18n          map[string]interface{}   `json:"i18n"`    // Using interface{} for flexibility

	ModelSize string `json:"modelSize,omitempty"`

	Namespace string `json:"namespace"`
	OnlyAdmin bool   `json:"onlyAdmin"`

	LastCommitHash string      `json:"lastCommitHash"`
	CreateTime     int64       `json:"createTime"`
	UpdateTime     int64       `json:"updateTime"`
	AppLabels      []string    `json:"appLabels,omitempty"`
	Count          interface{} `json:"count"`

	Variants map[string]interface{} `json:"variants,omitempty"` // Using interface{} for flexibility

	// Image analysis information
	// 镜像分析信息
	ImageAnalysis *AppImageAnalysis `json:"image_analysis,omitempty"`

	// Legacy fields for backward compatibility
	Screenshots []string               `json:"screenshots"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	UpdatedAt   string                 `json:"updated_at"`
}

// AppInfo represents complete app information including analysis result
// AppInfo 表示完整的应用信息，包括分析结果
type AppInfo struct {
	AppEntry      *ApplicationInfoEntry `json:"app_entry"`
	ImageAnalysis *ImageAnalysisResult  `json:"image_analysis"`
}

// AppsInfoRequest represents the request body for applications/info API
// AppsInfoRequest 表示应用信息API的请求体
type AppsInfoRequest struct {
	AppIds  []string `json:"app_ids"`
	Version string   `json:"version"`
}

// AppsInfoResponse represents the response from applications/info API
// AppsInfoResponse 表示应用信息API的响应
type AppsInfoResponse struct {
	Apps     map[string]*ApplicationInfoEntry `json:"apps"`
	Version  string                           `json:"version"`
	NotFound []string                         `json:"not_found,omitempty"`
	Message  string                           `json:"message,omitempty"` // For 202 Accepted responses
}

// AppInfoLatestPendingData contains pending app info data with extended structure
// AppInfoLatestPendingData 包含扩展结构的待处理应用信息数据
type AppInfoLatestPendingData struct {
	Type            AppDataType           `json:"type"`
	Timestamp       int64                 `json:"timestamp"`
	Version         string                `json:"version,omitempty"`
	RawData         *ApplicationInfoEntry `json:"raw_data"`
	RawPackage      string                `json:"raw_package"`
	Values          *Values               `json:"values"`
	AppInfo         *AppInfo              `json:"app_info"`
	RenderedPackage string                `json:"rendered_package"`
}

// AppOtherData contains other app data
type AppOtherData struct {
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// SourceData contains all app data for a specific source
type SourceData struct {
	AppInfoHistory       []*AppInfoHistoryData       `json:"app_info_history"`
	AppStateLatest       []*AppStateLatestData       `json:"app_state_latest"`
	AppInfoLatest        []*AppInfoLatestData        `json:"app_info_latest"`
	AppInfoLatestPending []*AppInfoLatestPendingData `json:"app_info_latest_pending"`
	Other                map[string]*AppOtherData    `json:"other"`
	Mutex                sync.RWMutex                `json:"-"`
}

// UserData contains all sources for a specific user
type UserData struct {
	Sources map[string]*SourceData `json:"sources"`
	Mutex   sync.RWMutex           `json:"-"`
}

// CacheData represents the entire cache structure
type CacheData struct {
	Users map[string]*UserData `json:"users"`
	Mutex sync.RWMutex         `json:"-"`
}

// ImageInfo represents detailed information about a Docker image
// ImageInfo 表示Docker镜像的详细信息
type ImageInfo struct {
	Name             string       `json:"name"`
	Tag              string       `json:"tag,omitempty"`
	Architecture     string       `json:"architecture,omitempty"`
	TotalSize        int64        `json:"total_size"`
	DownloadedSize   int64        `json:"downloaded_size"`
	DownloadProgress float64      `json:"download_progress"`
	LayerCount       int          `json:"layer_count"`
	DownloadedLayers int          `json:"downloaded_layers"`
	CreatedAt        time.Time    `json:"created_at,omitempty"`
	AnalyzedAt       time.Time    `json:"analyzed_at"`
	Status           string       `json:"status"` // fully_downloaded, partially_downloaded, not_downloaded, registry_error, analysis_failed, private_registry
	ErrorMessage     string       `json:"error_message,omitempty"`
	Layers           []*LayerInfo `json:"layers,omitempty"`
}

// LayerInfo represents information about a Docker image layer
// LayerInfo 表示Docker镜像层的信息
type LayerInfo struct {
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	MediaType  string `json:"media_type,omitempty"`
	Downloaded bool   `json:"downloaded"`
	Progress   int    `json:"progress"` // 0-100
	LocalPath  string `json:"local_path,omitempty"`
}

// ImageAnalysisResult represents the complete image analysis result
// ImageAnalysisResult 表示完整的镜像分析结果
type ImageAnalysisResult struct {
	AppID       string                `json:"app_id"`
	UserID      string                `json:"user_id"`
	SourceID    string                `json:"source_id"`
	AnalyzedAt  time.Time             `json:"analyzed_at"`
	TotalImages int                   `json:"total_images"`
	Images      map[string]*ImageInfo `json:"images"`
}

// AppImageAnalysis represents the image analysis result for a specific app
// AppImageAnalysis 表示特定应用的镜像分析结果
type AppImageAnalysis struct {
	AppID            string                `json:"app_id"`
	AnalyzedAt       time.Time             `json:"analyzed_at"`
	TotalImages      int                   `json:"total_images"`
	Images           map[string]*ImageInfo `json:"images"`
	Status           string                `json:"status"` // completed, failed, partial, no_images
	SourceChartURL   string                `json:"source_chart_url,omitempty"`
	RenderedChartURL string                `json:"rendered_chart_url,omitempty"`
}

// NewCacheData creates a new cache data structure
func NewCacheData() *CacheData {
	return &CacheData{
		Users: make(map[string]*UserData),
	}
}

// NewUserData creates a new user data structure
func NewUserData() *UserData {
	return &UserData{
		Sources: make(map[string]*SourceData),
	}
}

// NewSourceData creates a new source data structure
func NewSourceData() *SourceData {
	return &SourceData{
		AppInfoHistory:       make([]*AppInfoHistoryData, 0),
		AppStateLatest:       make([]*AppStateLatestData, 0),
		AppInfoLatest:        make([]*AppInfoLatestData, 0),
		AppInfoLatestPending: make([]*AppInfoLatestPendingData, 0),
		Other:                make(map[string]*AppOtherData),
	}
}

// NewAppData creates a new app data structure
func NewAppData(dataType AppDataType, data map[string]interface{}) *AppData {
	return &AppData{
		Type:      dataType,
		Data:      data,
		Timestamp: getCurrentTimestamp(),
	}
}

// NewAppInfoHistoryData creates a new app info history data structure
func NewAppInfoHistoryData(data map[string]interface{}) *AppInfoHistoryData {
	return &AppInfoHistoryData{
		Type:      AppInfoHistory,
		Data:      data,
		Timestamp: getCurrentTimestamp(),
	}
}

// NewAppStateLatestData creates a new app state latest data structure
func NewAppStateLatestData(data map[string]interface{}) *AppStateLatestData {
	return &AppStateLatestData{
		Type:      AppStateLatest,
		Data:      data,
		Timestamp: getCurrentTimestamp(),
	}
}

// NewAppInfoLatestData creates a new app info latest data structure
func NewAppInfoLatestData(data map[string]interface{}) *AppInfoLatestData {
	return &AppInfoLatestData{
		Type:      AppInfoLatest,
		Data:      data,
		Timestamp: getCurrentTimestamp(),
	}
}

// NewAppInfoLatestPendingData creates a new app info latest pending data structure
func NewAppInfoLatestPendingData(rawData *ApplicationInfoEntry, rawPackage string) *AppInfoLatestPendingData {
	return &AppInfoLatestPendingData{
		Type:            AppInfoLatestPending,
		Timestamp:       getCurrentTimestamp(),
		RawData:         rawData,
		RawPackage:      rawPackage,
		Values:          &Values{},
		AppInfo:         &AppInfo{},
		RenderedPackage: "",
	}
}

// NewAppInfoLatestPendingDataComplete creates a complete app info latest pending data structure
func NewAppInfoLatestPendingDataComplete(rawData *ApplicationInfoEntry, rawPackage string, values *Values, appInfo *AppInfo, renderedPackage string) *AppInfoLatestPendingData {
	return &AppInfoLatestPendingData{
		Type:            AppInfoLatestPending,
		Timestamp:       getCurrentTimestamp(),
		RawData:         rawData,
		RawPackage:      rawPackage,
		Values:          values,
		AppInfo:         appInfo,
		RenderedPackage: renderedPackage,
	}
}

// NewAppOtherData creates a new app other data structure
func NewAppOtherData(data map[string]interface{}) *AppOtherData {
	return &AppOtherData{
		Type:      Other,
		Data:      data,
		Timestamp: getCurrentTimestamp(),
	}
}

// NewValues creates a new Values structure
func NewValues() *Values {
	return &Values{
		CustomParams: make(map[string]interface{}),
		Resources:    make(map[string]interface{}),
		Config:       make(map[string]interface{}),
	}
}

// NewAppInfo creates a new AppInfo structure
func NewAppInfo(appEntry *ApplicationInfoEntry, imageAnalysis *ImageAnalysisResult) *AppInfo {
	return &AppInfo{
		AppEntry:      appEntry,
		ImageAnalysis: imageAnalysis,
	}
}

// getCurrentTimestamp returns current unix timestamp
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

// NewAppInfoLatestPendingDataFromLegacyData creates AppInfoLatestPendingData from legacy map data
// NewAppInfoLatestPendingDataFromLegacyData 从传统的map数据创建AppInfoLatestPendingData
func NewAppInfoLatestPendingDataFromLegacyData(data map[string]interface{}) *AppInfoLatestPendingData {
	return &AppInfoLatestPendingData{
		Type:            AppInfoLatestPending,
		Timestamp:       getCurrentTimestamp(),
		RawData:         nil, // Legacy data doesn't have structured RawData
		RawPackage:      "",  // No package path in legacy data
		Values:          &Values{},
		AppInfo:         &AppInfo{},
		RenderedPackage: "",
	}
}
