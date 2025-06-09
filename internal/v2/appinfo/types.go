package appinfo

import (
	"market/internal/v2/types"
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
