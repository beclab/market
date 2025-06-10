package appinfo

import (
	"market/internal/v2/types"
	"time"
)

// Type aliases for backward compatibility
// 为了向后兼容而创建的类型别名
type AppDataType = types.AppDataType
type AppData = types.AppData
type SourceData = types.SourceData
type UserData = types.UserData
type CacheData = types.CacheData

// Constants for backward compatibility
// 为了向后兼容而重新导出的常量
const (
	AppInfoHistory       = types.AppInfoHistory
	AppStateLatest       = types.AppStateLatest
	AppInfoLatest        = types.AppInfoLatest
	AppInfoLatestPending = types.AppInfoLatestPending
	Other                = types.Other
)

// Constructor functions for backward compatibility
// 为了向后兼容而重新导出的构造函数
var (
	NewCacheData  = types.NewCacheData
	NewUserData   = types.NewUserData
	NewSourceData = types.NewSourceData
	NewAppData    = types.NewAppData
)

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

// AppImageAnalysis represents the image analysis result for a specific app
// AppImageAnalysis 表示特定应用的镜像分析结果
type AppImageAnalysis struct {
	AppID            string                `json:"app_id"`
	AnalyzedAt       time.Time             `json:"analyzed_at"`
	TotalImages      int                   `json:"total_images"`
	Images           map[string]*ImageInfo `json:"images"`
	Status           string                `json:"status"` // completed, failed, partial
	SourceChartURL   string                `json:"source_chart_url,omitempty"`
	RenderedChartURL string                `json:"rendered_chart_url,omitempty"`
}
