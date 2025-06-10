package appinfo

import (
	"market/internal/v2/types"
)

// Type aliases for backward compatibility
// 为了向后兼容而创建的类型别名
type AppDataType = types.AppDataType
type AppData = types.AppData
type AppInfoHistoryData = types.AppInfoHistoryData
type AppStateLatestData = types.AppStateLatestData
type AppInfoLatestData = types.AppInfoLatestData
type AppInfoLatestPendingData = types.AppInfoLatestPendingData
type AppOtherData = types.AppOtherData
type SourceData = types.SourceData
type UserData = types.UserData
type CacheData = types.CacheData

// Image-related type aliases for unified access
// 镜像相关类型别名，用于统一访问
type ImageInfo = types.ImageInfo
type LayerInfo = types.LayerInfo
type ImageAnalysisResult = types.ImageAnalysisResult
type AppImageAnalysis = types.AppImageAnalysis

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
	NewCacheData                              = types.NewCacheData
	NewUserData                               = types.NewUserData
	NewSourceData                             = types.NewSourceData
	NewAppData                                = types.NewAppData
	NewAppInfoHistoryData                     = types.NewAppInfoHistoryData
	NewAppStateLatestData                     = types.NewAppStateLatestData
	NewAppInfoLatestData                      = types.NewAppInfoLatestData
	NewAppInfoLatestPendingData               = types.NewAppInfoLatestPendingData
	NewAppInfoLatestPendingDataFromLegacyData = types.NewAppInfoLatestPendingDataFromLegacyData
	NewAppOtherData                           = types.NewAppOtherData
)
