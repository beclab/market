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

// SourceData contains all app data for a specific source
type SourceData struct {
	AppInfoHistory       []*AppData          `json:"app_info_history"`
	AppStateLatest       *AppData            `json:"app_state_latest"`
	AppInfoLatest        *AppData            `json:"app_info_latest"`
	AppInfoLatestPending *AppData            `json:"app_info_latest_pending"`
	Other                map[string]*AppData `json:"other"`
	Mutex                sync.RWMutex        `json:"-"`
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
		AppInfoHistory: make([]*AppData, 0),
		Other:          make(map[string]*AppData),
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

// getCurrentTimestamp returns current unix timestamp
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}
