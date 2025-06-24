package types

import (
	"log"
	"strings"
	"sync"
	"time"
)

// ModifyType represents different types of value modifications
type ModifyType string

const (
	ModifyTypeEnv      ModifyType = "env"             // Environment variable modification
	ModifyTypeUserPerm ModifyType = "user_permission" // User permission modification
	ModifyTypeResource ModifyType = "resource_limit"  // Resource limit modification
)

// AppDataType represents different types of app data
type AppDataType string

const (
	AppInfoHistory       AppDataType = "app-info-history"
	AppStateLatest       AppDataType = "app-state-latest"
	AppInfoLatest        AppDataType = "app-info-latest"
	AppInfoLatestPending AppDataType = "app-info-latest-pending"
	AppRenderFailed      AppDataType = "app-render-failed"
	Other                AppDataType = "other"
)

// SourceDataType represents the type of source data
type SourceDataType string

const (
	SourceDataTypeLocal  SourceDataType = "local"  // Local source data
	SourceDataTypeRemote SourceDataType = "remote" // Remote source data
)

// Recommend represents recommendation configuration
type Recommend struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"` // Comma-separated app names
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Page represents page configuration
type Page struct {
	Category  string    `json:"category"`
	Content   string    `json:"content"` // JSON string of page content
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Topic represents topic configuration
type Topic struct {
	ID            string            `json:"_id"`
	Name          string            `json:"name"`
	Name2         map[string]string `json:"name2"`
	Introduction  string            `json:"introduction"`
	Introduction2 map[string]string `json:"introduction2"`
	Des           string            `json:"des"` // Description
	Des2          map[string]string `json:"des2"`
	IconImg       string            `json:"iconimg"`   // Icon image URL
	DetailImg     string            `json:"detailimg"` // Detail image URL
	RichText      string            `json:"richtext"`  // Rich text content
	RichText2     map[string]string `json:"richtext2"`
	Apps          string            `json:"apps"` // Comma-separated app names
	IsDelete      bool              `json:"isdelete"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// TopicList represents topic list configuration
type TopicList struct {
	Name        string    `json:"name"`
	Type        string    `json:"type"` // Topic list type
	Description string    `json:"description"`
	Content     string    `json:"content"` // Comma-separated topic IDs
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AppStoreTopItem represents an item in the tops list from appstore
type AppStoreTopItem struct {
	AppID string `json:"appid"`
	Rank  int    `json:"rank"`
}

// Others contains additional data for AppInfoLatestPendingData
type Others struct {
	Hash       string             `json:"hash"`
	Version    string             `json:"version"`
	Topics     []*Topic           `json:"topics"`
	TopicLists []*TopicList       `json:"topic_lists"`
	Recommends []*Recommend       `json:"recommends"`
	Pages      []*Page            `json:"pages"`
	Tops       []*AppStoreTopItem `json:"tops"`
	Latest     []string           `json:"latest"`
}

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
	Type   AppDataType `json:"type"`
	Status struct {
		Name               string `json:"name"`
		State              string `json:"state"`
		UpdateTime         string `json:"updateTime"`
		StatusTime         string `json:"statusTime"`
		LastTransitionTime string `json:"lastTransitionTime"`
		EntranceStatuses   []struct {
			Name       string `json:"name"`
			State      string `json:"state"`
			StatusTime string `json:"statusTime"`
			Reason     string `json:"reason"`
		} `json:"entranceStatuses"`
	} `json:"status"`
}

// AppInfoLatestData contains latest app info data
type AppInfoLatestData struct {
	Type            AppDataType           `json:"type"`
	Timestamp       int64                 `json:"timestamp"`
	Version         string                `json:"version,omitempty"`
	RawData         *ApplicationInfoEntry `json:"raw_data"`
	RawPackage      string                `json:"raw_package"`
	Values          []*Values             `json:"values"` // Changed to array
	AppInfo         *AppInfo              `json:"app_info"`
	RenderedPackage string                `json:"rendered_package"`
	AppSimpleInfo   *AppSimpleInfo        `json:"app_simple_info"`
}

// Values represents custom rendering parameters
type Values struct {
	FileName    string     `json:"file_name"`    // File name
	ModifyType  ModifyType `json:"modify_type"`  // Type of modification
	ModifyKey   string     `json:"modify_key"`   // Key of the modified value
	ModifyValue string     `json:"modify_value"` // Modified value
}

// ApplicationInfoEntry represents the structure returned by the applications/info API
type ApplicationInfoEntry struct {
	ID string `json:"id"`

	Name        string            `json:"name"`
	CfgType     string            `json:"cfgType"`
	ChartName   string            `json:"chartName"`
	Icon        string            `json:"icon"`
	Description map[string]string `json:"description"` // Changed to support multi-language
	AppID       string            `json:"appID"`
	Title       map[string]string `json:"title"` // Changed to support multi-language
	Version     string            `json:"version"`
	Categories  []string          `json:"categories"`
	VersionName string            `json:"versionName"`

	FullDescription    map[string]string        `json:"fullDescription"`    // Changed to support multi-language
	UpgradeDescription map[string]string        `json:"upgradeDescription"` // Changed to support multi-language
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

	// Legacy fields for backward compatibility
	Screenshots []string               `json:"screenshots"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	UpdatedAt   string                 `json:"updated_at"`
}

// AppInfo represents complete app information including analysis result
type AppInfo struct {
	AppEntry      *ApplicationInfoEntry `json:"app_entry"`
	ImageAnalysis *ImageAnalysisResult  `json:"image_analysis"`
}

// AppsInfoRequest represents the request body for applications/info API
type AppsInfoRequest struct {
	AppIds  []string `json:"app_ids"`
	Version string   `json:"version"`
}

// AppsInfoResponse represents the response from applications/info API
type AppsInfoResponse struct {
	Apps     map[string]*ApplicationInfoEntry `json:"apps"`
	Version  string                           `json:"version"`
	NotFound []string                         `json:"not_found,omitempty"`
	Message  string                           `json:"message,omitempty"` // For 202 Accepted responses
}

type AppSimpleInfo struct {
	AppID          string            `json:"app_id"`
	AppName        string            `json:"app_name"`
	AppIcon        string            `json:"app_icon"`
	AppDescription map[string]string `json:"app_description"`
	AppVersion     string            `json:"app_version"`
	AppTitle       map[string]string `json:"app_title"`
	Categories     []string          `json:"categories"`
}

// AppInfoLatestPendingData contains pending app info data with extended structure
type AppInfoLatestPendingData struct {
	Type            AppDataType           `json:"type"`
	Timestamp       int64                 `json:"timestamp"`
	Version         string                `json:"version,omitempty"`
	RawData         *ApplicationInfoEntry `json:"raw_data"`
	RawPackage      string                `json:"raw_package"`
	Values          []*Values             `json:"values"` // Changed to array
	AppInfo         *AppInfo              `json:"app_info"`
	RenderedPackage string                `json:"rendered_package"`
}

// AppRenderFailedData contains app data that failed to render with failure reason
type AppRenderFailedData struct {
	Type            AppDataType           `json:"type"`
	Timestamp       int64                 `json:"timestamp"`
	Version         string                `json:"version,omitempty"`
	RawData         *ApplicationInfoEntry `json:"raw_data"`
	RawPackage      string                `json:"raw_package"`
	Values          []*Values             `json:"values"`
	AppInfo         *AppInfo              `json:"app_info"`
	RenderedPackage string                `json:"rendered_package"`
	FailureReason   string                `json:"failure_reason"` // Reason for render failure
	FailureStep     string                `json:"failure_step"`   // Which step failed
	FailureTime     time.Time             `json:"failure_time"`   // When the failure occurred
	RetryCount      int                   `json:"retry_count"`    // Number of retry attempts
}

// AppOtherData contains other app data
type AppOtherData struct {
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
}

// SourceData represents data from a specific source
type SourceData struct {
	Type                 SourceDataType              `json:"type"` // Source type: local or remote
	AppInfoHistory       []*AppInfoHistoryData       `json:"app_info_history"`
	AppStateLatest       []*AppStateLatestData       `json:"app_state_latest"`
	AppInfoLatest        []*AppInfoLatestData        `json:"app_info_latest"`
	AppInfoLatestPending []*AppInfoLatestPendingData `json:"app_info_latest_pending"`
	AppRenderFailed      []*AppRenderFailedData      `json:"app_render_failed"` // Apps that failed to render
	Others               *Others                     `json:"others,omitempty"`  // Additional data like hash, topics, etc.
	// Remove Mutex, all lock operations will be managed by CacheData
}

// UserData represents data for a specific user
type UserData struct {
	Sources map[string]*SourceData `json:"sources"`
	Hash    string                 `json:"hash"`
	// Remove Mutex, all lock operations will be managed by CacheData
}

// CacheData represents the entire cache with a single global lock
type CacheData struct {
	Users map[string]*UserData `json:"users"`
	// Single global lock to manage all data access
	Mutex sync.RWMutex `json:"-"`
}

// NodeInfo represents information about a specific node and its layers
type NodeInfo struct {
	NodeName         string       `json:"node_name"`
	Architecture     string       `json:"architecture,omitempty"`
	Variant          string       `json:"variant,omitempty"`
	OS               string       `json:"os,omitempty"`
	Layers           []*LayerInfo `json:"layers,omitempty"`
	DownloadedSize   int64        `json:"downloaded_size"`
	DownloadedLayers int          `json:"downloaded_layers"`
	TotalSize        int64        `json:"total_size"`
	LayerCount       int          `json:"layer_count"`
}

// ImageInfo represents detailed information about a Docker image
type ImageInfo struct {
	Name             string      `json:"name"`
	Tag              string      `json:"tag,omitempty"`
	Architecture     string      `json:"architecture,omitempty"`
	TotalSize        int64       `json:"total_size"`
	DownloadedSize   int64       `json:"downloaded_size"`
	DownloadProgress float64     `json:"download_progress"`
	LayerCount       int         `json:"layer_count"`
	DownloadedLayers int         `json:"downloaded_layers"`
	CreatedAt        time.Time   `json:"created_at,omitempty"`
	AnalyzedAt       time.Time   `json:"analyzed_at"`
	Status           string      `json:"status"` // fully_downloaded, partially_downloaded, not_downloaded, registry_error, analysis_failed, private_registry
	ErrorMessage     string      `json:"error_message,omitempty"`
	Nodes            []*NodeInfo `json:"nodes,omitempty"`
}

// LayerInfo represents information about a Docker image layer
type LayerInfo struct {
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	MediaType  string `json:"media_type,omitempty"`
	Offset     int64  `json:"offset,omitempty"` // Add offset field for production API
	Downloaded bool   `json:"downloaded"`
	Progress   int    `json:"progress"` // 0-100
	LocalPath  string `json:"local_path,omitempty"`
}

// ImageAnalysisResult represents the complete image analysis result
type ImageAnalysisResult struct {
	AppID       string                `json:"app_id"`
	UserID      string                `json:"user_id"`
	SourceID    string                `json:"source_id"`
	AnalyzedAt  time.Time             `json:"analyzed_at"`
	TotalImages int                   `json:"total_images"`
	Images      map[string]*ImageInfo `json:"images"`
}

// AppImageAnalysis represents the image analysis result for a specific app
type AppImageAnalysis struct {
	AppID            string                `json:"app_id"`
	AnalyzedAt       time.Time             `json:"analyzed_at"`
	TotalImages      int                   `json:"total_images"`
	Images           map[string]*ImageInfo `json:"images"`
	Status           string                `json:"status"` // completed, failed, partial, no_images
	SourceChartURL   string                `json:"source_chart_url,omitempty"`
	RenderedChartURL string                `json:"rendered_chart_url,omitempty"`
}

// InstallOptions represents the options for app installation
type InstallOptions struct {
	App       string `json:"app"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	RepoUrl   string `json:"repo_url"`
	ChartPath string `json:"chart_path"`
}

// NewCacheData creates a new cache data structure
func NewCacheData() *CacheData {
	return &CacheData{
		Users: make(map[string]*UserData),
	}
}

// NewUserData creates a new user data structure
func NewUserData() *UserData {
	userData := &UserData{
		Sources: make(map[string]*SourceData),
	}

	// Create a default local source for the user
	userData.Sources["local"] = NewSourceDataWithType(SourceDataTypeLocal)

	return userData
}

// NewSourceData creates a new source data structure
func NewSourceData() *SourceData {
	return &SourceData{
		Type:                 SourceDataTypeRemote, // Default to remote type
		AppInfoHistory:       make([]*AppInfoHistoryData, 0),
		AppStateLatest:       make([]*AppStateLatestData, 0),
		AppInfoLatest:        make([]*AppInfoLatestData, 0),
		AppInfoLatestPending: make([]*AppInfoLatestPendingData, 0),
		AppRenderFailed:      make([]*AppRenderFailedData, 0),
	}
}

// NewSourceDataWithType creates a new source data structure with specified type
func NewSourceDataWithType(sourceType SourceDataType) *SourceData {
	return &SourceData{
		Type:                 sourceType,
		AppInfoHistory:       make([]*AppInfoHistoryData, 0),
		AppStateLatest:       make([]*AppStateLatestData, 0),
		AppInfoLatest:        make([]*AppInfoLatestData, 0),
		AppInfoLatestPending: make([]*AppInfoLatestPendingData, 0),
		AppRenderFailed:      make([]*AppRenderFailedData, 0),
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
	// Extract status information from data
	var name, state, updateTime, statusTime, lastTransitionTime string
	var entranceStatuses []struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
	}

	// Extract name from various possible fields
	if nameVal, ok := data["name"].(string); ok && nameVal != "" {
		name = nameVal
	} else if appNameVal, ok := data["appName"].(string); ok && appNameVal != "" {
		name = appNameVal
	} else if appIDVal, ok := data["appID"].(string); ok && appIDVal != "" {
		name = appIDVal
	} else if idVal, ok := data["id"].(string); ok && idVal != "" {
		name = idVal
	}

	// If no valid name found, return nil
	if name == "" {
		log.Printf("ERROR: NewAppStateLatestData failed to extract name from data - missing required name field")
		log.Printf("ERROR: Available fields in data: %v", getMapKeys(data))
		return nil
	}

	if stateVal, ok := data["state"].(string); ok {
		state = stateVal
	}
	if updateTimeVal, ok := data["updateTime"].(string); ok {
		updateTime = updateTimeVal
	}
	if statusTimeVal, ok := data["statusTime"].(string); ok {
		statusTime = statusTimeVal
	}
	if lastTransitionTimeVal, ok := data["lastTransitionTime"].(string); ok {
		lastTransitionTime = lastTransitionTimeVal
	}

	if entranceStatusesVal, ok := data["entranceStatuses"].([]interface{}); ok {
		entranceStatuses = make([]struct {
			Name       string `json:"name"`
			State      string `json:"state"`
			StatusTime string `json:"statusTime"`
			Reason     string `json:"reason"`
		}, len(entranceStatusesVal))

		for i, entrance := range entranceStatusesVal {
			if entranceMap, ok := entrance.(map[string]interface{}); ok {
				if name, ok := entranceMap["name"].(string); ok {
					entranceStatuses[i].Name = name
				}
				if entranceState, ok := entranceMap["state"].(string); ok {
					entranceStatuses[i].State = entranceState
				}
				if entranceStatusTime, ok := entranceMap["statusTime"].(string); ok {
					entranceStatuses[i].StatusTime = entranceStatusTime
				}
				if reason, ok := entranceMap["reason"].(string); ok {
					entranceStatuses[i].Reason = reason
				}
			}
		}
	}

	return &AppStateLatestData{
		Type: AppStateLatest,
		Status: struct {
			Name               string `json:"name"`
			State              string `json:"state"`
			UpdateTime         string `json:"updateTime"`
			StatusTime         string `json:"statusTime"`
			LastTransitionTime string `json:"lastTransitionTime"`
			EntranceStatuses   []struct {
				Name       string `json:"name"`
				State      string `json:"state"`
				StatusTime string `json:"statusTime"`
				Reason     string `json:"reason"`
			} `json:"entranceStatuses"`
		}{
			Name:               name,
			State:              state,
			UpdateTime:         updateTime,
			StatusTime:         statusTime,
			LastTransitionTime: lastTransitionTime,
			EntranceStatuses:   entranceStatuses,
		},
	}
}

// NewAppInfoLatestData creates a new app info latest data structure
func NewAppInfoLatestData(data map[string]interface{}) *AppInfoLatestData {
	// Validate input data - ensure we have meaningful data to work with
	if data == nil || len(data) == 0 {
		log.Printf("DEBUG: NewAppInfoLatestData called with nil or empty data, returning nil")
		return nil
	}

	// Check if we have essential app identifiers or meaningful content
	var appID, appName string
	hasEssentialData := false

	if id, ok := data["id"].(string); ok && id != "" {
		appID = id
		hasEssentialData = true
	}
	if name, ok := data["name"].(string); ok && name != "" {
		appName = name
		hasEssentialData = true
	}
	if appID == "" {
		if aid, ok := data["appID"].(string); ok && aid != "" {
			appID = aid
			hasEssentialData = true
		} else if aid, ok := data["app_id"].(string); ok && aid != "" {
			appID = aid
			hasEssentialData = true
		}
	}
	if appName == "" {
		if title, ok := data["title"].(string); ok && title != "" {
			appName = title
			hasEssentialData = true
		}
	}

	// Check for other indicators of valid app data
	if !hasEssentialData {
		// Check for chart name, icon, or other app-specific fields
		if chartName, ok := data["chartName"].(string); ok && chartName != "" {
			hasEssentialData = true
		} else if icon, ok := data["icon"].(string); ok && icon != "" {
			hasEssentialData = true
		} else if desc, ok := data["description"].(string); ok && desc != "" {
			hasEssentialData = true
		} else if version, ok := data["version"].(string); ok && version != "" {
			hasEssentialData = true
		}
	}

	// If no essential data found, return nil to prevent empty data creation
	if !hasEssentialData {
		log.Printf("DEBUG: NewAppInfoLatestData found no essential app data in input, returning nil")
		log.Printf("DEBUG: Input data keys: %v", getMapKeys(data))
		return nil
	}

	// For backward compatibility, we'll try to create a basic AppInfoLatestData structure
	appInfoLatest := &AppInfoLatestData{
		Type:            AppInfoLatest,
		Timestamp:       getCurrentTimestamp(),
		Version:         "",
		RawData:         nil,
		RawPackage:      "",
		Values:          make([]*Values, 0),
		AppInfo:         &AppInfo{},
		RenderedPackage: "",
	}

	// Extract version if available in the data
	if version, ok := data["version"].(string); ok && version != "" {
		appInfoLatest.Version = version
	}

	// Create ApplicationInfoEntry with the validated data
	if appID == "" && appName != "" {
		appID = appName
	}
	if appName == "" && appID != "" {
		appName = appID
	}

	rawData := &ApplicationInfoEntry{
		ID:         appID,
		AppID:      appID,
		Name:       appName,
		Title:      map[string]string{"en-US": appName}, // Initialize with default language
		CreateTime: getCurrentTimestamp(),
		UpdateTime: getCurrentTimestamp(),
		Metadata:   make(map[string]interface{}),
	}

	// Use the complete field mapping function to ensure no data is lost
	mapAllApplicationInfoEntryFields(data, rawData)

	// Store the original data in metadata for later processing
	rawData.Metadata["source_data"] = data

	appInfoLatest.RawData = rawData
	appInfoLatest.AppInfo = &AppInfo{
		AppEntry:      rawData,
		ImageAnalysis: nil, // Will be filled later if needed
	}

	return appInfoLatest
}

// Helper function to get map keys for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// NewAppInfoLatestPendingData creates a new app info latest pending data structure
func NewAppInfoLatestPendingData(rawData *ApplicationInfoEntry, rawPackage string) *AppInfoLatestPendingData {
	return &AppInfoLatestPendingData{
		Type:            AppInfoLatestPending,
		Timestamp:       getCurrentTimestamp(),
		RawData:         rawData,
		RawPackage:      rawPackage,
		Values:          make([]*Values, 0),
		AppInfo:         &AppInfo{},
		RenderedPackage: "",
	}
}

// NewAppInfoLatestPendingDataComplete creates a complete app info latest pending data structure
func NewAppInfoLatestPendingDataComplete(rawData *ApplicationInfoEntry, rawPackage string, values []*Values, appInfo *AppInfo, renderedPackage string) *AppInfoLatestPendingData {
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

// NewAppInfoLatestPendingDataWithOthers creates a complete app info latest pending data structure with Others
// Deprecated: Others is now stored in SourceData, use NewAppInfoLatestPendingDataComplete instead
func NewAppInfoLatestPendingDataWithOthers(rawData *ApplicationInfoEntry, rawPackage string, values []*Values, appInfo *AppInfo, renderedPackage string, others *Others) *AppInfoLatestPendingData {
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
func NewValues(fileName string, modifyType ModifyType, modifyKey string, modifyValue string) *Values {
	return &Values{
		FileName:    fileName,
		ModifyType:  modifyType,
		ModifyKey:   modifyKey,
		ModifyValue: modifyValue,
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

// NewAppInfoLatestPendingDataFromLegacyData creates AppInfoLatestPendingData from a single app data
func NewAppInfoLatestPendingDataFromLegacyData(appData map[string]interface{}) *AppInfoLatestPendingData {
	// Add debug logging to inspect input data
	log.Printf("DEBUG: NewAppInfoLatestPendingDataFromLegacyData called with appData: %+v", appData)
	// if appData != nil {
	// 	log.Printf("DEBUG: appData length: %d", len(appData))
	// 	for key, value := range appData {
	// 		log.Printf("DEBUG: appData[%s] = %v (type: %T)", key, value, value)
	// 	}
	// }

	// Validate input data - ensure we have at least basic app identifier
	if appData == nil || len(appData) == 0 {
		log.Printf("DEBUG: appData is nil or empty, returning nil")
		return nil
	}

	// Filter out apps with Suspend or Remove labels
	if appLabels, ok := appData["appLabels"].([]interface{}); ok {
		for _, labelInterface := range appLabels {
			if label, ok := labelInterface.(string); ok {
				if strings.EqualFold(label, "suspend") || strings.EqualFold(label, "remove") {
					log.Printf("DEBUG: Skipping app with label: %s", label)
					return nil
				}
			}
		}
	}

	// Check if we have essential app identifiers (id, name, or appID)
	var primaryID, primaryName string

	if id, ok := appData["id"].(string); ok && id != "" {
		primaryID = id
		log.Printf("DEBUG: Found primaryID from 'id': %s", primaryID)
	}
	if name, ok := appData["name"].(string); ok && name != "" {
		primaryName = name
		log.Printf("DEBUG: Found primaryName from 'name': %s", primaryName)
	}
	if appID, ok := appData["appID"].(string); ok && appID != "" && appID != "0" {
		if primaryID == "" {
			primaryID = appID
			log.Printf("DEBUG: Found primaryID from 'appID': %s", primaryID)
		}
	}
	if primaryID == "" {
		if appID, ok := appData["app_id"].(string); ok && appID != "" && appID != "0" {
			primaryID = appID
			log.Printf("DEBUG: Found primaryID from 'app_id': %s", primaryID)
		}
	}
	if primaryName == "" {
		if title, ok := appData["title"].(string); ok && title != "" {
			primaryName = title
			log.Printf("DEBUG: Found primaryName from 'title': %s", primaryName)
		}
	}

	log.Printf("DEBUG: Final primaryID: '%s', primaryName: '%s'", primaryID, primaryName)

	// If this doesn't look like single app data, return nil
	if primaryID == "" && primaryName == "" {
		log.Printf("DEBUG: Both primaryID and primaryName are empty, returning nil")
		return nil
	}

	// Ensure we have both ID and name
	if primaryID == "" {
		primaryID = primaryName
	}
	if primaryName == "" {
		primaryName = primaryID
	}

	pendingData := &AppInfoLatestPendingData{
		Type:            AppInfoLatestPending,
		Timestamp:       getCurrentTimestamp(),
		RawData:         nil,
		RawPackage:      "",
		Values:          make([]*Values, 0),
		AppInfo:         &AppInfo{},
		RenderedPackage: "",
	}

	// Extract version from app data if available
	if version, ok := appData["version"].(string); ok && version != "" {
		pendingData.Version = version
	}

	// Create ApplicationInfoEntry from app data
	rawData := &ApplicationInfoEntry{
		ID:         primaryID,
		AppID:      primaryID,
		Name:       primaryName,
		Title:      map[string]string{"en-US": primaryName}, // Initialize with default language
		CreateTime: getCurrentTimestamp(),
		UpdateTime: getCurrentTimestamp(),
		Metadata:   make(map[string]interface{}),
	}

	// Use the complete field mapping function to ensure no data is lost
	mapAllApplicationInfoEntryFields(appData, rawData)

	// Debug AppLabels after mapping
	DebugAppLabelsFlow(appData, rawData, "NewAppInfoLatestPendingDataFromLegacyData")

	// Validate and fix AppLabels if needed
	ValidateAndFixAppLabels(appData, rawData)

	// Store the complete app data in metadata for later processing
	rawData.Metadata["source_app_data"] = appData
	rawData.Metadata["validation_status"] = "validated"

	pendingData.RawData = rawData
	pendingData.AppInfo = &AppInfo{
		AppEntry:      rawData,
		ImageAnalysis: nil, // Will be filled later during hydration
	}

	return pendingData
}

// NewAppInfoLatestPendingDataFromLegacyCompleteData creates AppInfoLatestPendingData from complete legacy data with single app
func NewAppInfoLatestPendingDataFromLegacyCompleteData(appData map[string]interface{}, others *Others) *AppInfoLatestPendingData {
	log.Printf("DEBUG: CALL POINT 4 - ")
	pendingData := NewAppInfoLatestPendingDataFromLegacyData(appData)
	// Check if base pending data creation was successful
	if pendingData == nil {
		// Return nil if base data validation failed
		return nil
	}
	// Note: Others is now stored in SourceData, not in AppInfoLatestPendingData
	return pendingData
}

// mapAllApplicationInfoEntryFields maps all fields from source data to ApplicationInfoEntry
// This function ensures no data is lost during conversion
func mapAllApplicationInfoEntryFields(sourceData map[string]interface{}, entry *ApplicationInfoEntry) {
	if sourceData == nil || entry == nil {
		return
	}

	// Basic fields
	if val, ok := sourceData["id"].(string); ok && val != "" {
		entry.ID = val
	}
	if val, ok := sourceData["name"].(string); ok && val != "" {
		entry.Name = val
	}
	if val, ok := sourceData["cfgType"].(string); ok && val != "" {
		entry.CfgType = val
	}
	if val, ok := sourceData["chartName"].(string); ok && val != "" {
		entry.ChartName = val
	}
	if val, ok := sourceData["icon"].(string); ok && val != "" {
		entry.Icon = val
	}
	if val, ok := sourceData["appID"].(string); ok && val != "" {
		entry.AppID = val
	}
	if val, ok := sourceData["version"].(string); ok && val != "" {
		entry.Version = val
	}
	if val, ok := sourceData["versionName"].(string); ok && val != "" {
		entry.VersionName = val
	}

	// Multi-language fields
	if val, ok := sourceData["description"].(string); ok && val != "" {
		if entry.Description == nil {
			entry.Description = make(map[string]string)
		}
		entry.Description["en-US"] = val
	}
	if val, ok := sourceData["title"].(string); ok && val != "" {
		if entry.Title == nil {
			entry.Title = make(map[string]string)
		}
		entry.Title["en-US"] = val
	}

	// Array fields
	if val, ok := sourceData["categories"].([]interface{}); ok {
		entry.Categories = make([]string, len(val))
		for i, cat := range val {
			if catStr, ok := cat.(string); ok {
				entry.Categories[i] = catStr
			}
		}
		// Debug AppLabels mapping
		DebugAppLabelsFlow(sourceData, entry, "mapAllApplicationInfoEntryFields")
	}
	if val, ok := sourceData["appLabels"].([]interface{}); ok {
		entry.AppLabels = make([]string, len(val))
		for i, label := range val {
			if labelStr, ok := label.(string); ok {
				entry.AppLabels[i] = labelStr
			}
		}
		// Debug AppLabels mapping
		DebugAppLabelsFlow(sourceData, entry, "mapAllApplicationInfoEntryFields")
	}
	if val, ok := sourceData["locale"].([]interface{}); ok {
		entry.Locale = make([]string, len(val))
		for i, loc := range val {
			if locStr, ok := loc.(string); ok {
				entry.Locale[i] = locStr
			}
		}
	}
	if val, ok := sourceData["supportArch"].([]interface{}); ok {
		entry.SupportArch = make([]string, len(val))
		for i, arch := range val {
			if archStr, ok := arch.(string); ok {
				entry.SupportArch[i] = archStr
			}
		}
	}
	if val, ok := sourceData["promoteImage"].([]interface{}); ok {
		entry.PromoteImage = make([]string, len(val))
		for i, img := range val {
			if imgStr, ok := img.(string); ok {
				entry.PromoteImage[i] = imgStr
			}
		}
	}
	if val, ok := sourceData["screenshots"].([]interface{}); ok {
		entry.Screenshots = make([]string, len(val))
		for i, screenshot := range val {
			if screenshotStr, ok := screenshot.(string); ok {
				entry.Screenshots[i] = screenshotStr
			}
		}
	}
	if val, ok := sourceData["tags"].([]interface{}); ok {
		entry.Tags = make([]string, len(val))
		for i, tag := range val {
			if tagStr, ok := tag.(string); ok {
				entry.Tags[i] = tagStr
			}
		}
	}

	// String fields
	if val, ok := sourceData["fullDescription"].(string); ok && val != "" {
		if entry.FullDescription == nil {
			entry.FullDescription = make(map[string]string)
		}
		entry.FullDescription["en-US"] = val
	}
	if val, ok := sourceData["upgradeDescription"].(string); ok && val != "" {
		if entry.UpgradeDescription == nil {
			entry.UpgradeDescription = make(map[string]string)
		}
		entry.UpgradeDescription["en-US"] = val
	}
	if val, ok := sourceData["promoteVideo"].(string); ok && val != "" {
		entry.PromoteVideo = val
	}
	if val, ok := sourceData["subCategory"].(string); ok && val != "" {
		entry.SubCategory = val
	}
	if val, ok := sourceData["developer"].(string); ok && val != "" {
		entry.Developer = val
	}
	if val, ok := sourceData["requiredMemory"].(string); ok && val != "" {
		entry.RequiredMemory = val
	}
	if val, ok := sourceData["requiredDisk"].(string); ok && val != "" {
		entry.RequiredDisk = val
	}
	if val, ok := sourceData["requiredGPU"].(string); ok && val != "" {
		entry.RequiredGPU = val
	}
	if val, ok := sourceData["requiredCPU"].(string); ok && val != "" {
		entry.RequiredCPU = val
	}
	if val, ok := sourceData["target"].(string); ok && val != "" {
		entry.Target = val
	}
	if val, ok := sourceData["submitter"].(string); ok && val != "" {
		entry.Submitter = val
	}
	if val, ok := sourceData["doc"].(string); ok && val != "" {
		entry.Doc = val
	}
	if val, ok := sourceData["website"].(string); ok && val != "" {
		entry.Website = val
	}
	if val, ok := sourceData["featuredImage"].(string); ok && val != "" {
		entry.FeaturedImage = val
	}
	if val, ok := sourceData["sourceCode"].(string); ok && val != "" {
		entry.SourceCode = val
	}
	if val, ok := sourceData["modelSize"].(string); ok && val != "" {
		entry.ModelSize = val
	}
	if val, ok := sourceData["namespace"].(string); ok && val != "" {
		entry.Namespace = val
	}
	if val, ok := sourceData["lastCommitHash"].(string); ok && val != "" {
		entry.LastCommitHash = val
	}
	if val, ok := sourceData["updated_at"].(string); ok && val != "" {
		entry.UpdatedAt = val
	}

	// Numeric fields
	if val, ok := sourceData["rating"].(float64); ok {
		entry.Rating = float32(val)
	}
	if val, ok := sourceData["createTime"].(int64); ok {
		entry.CreateTime = val
	}
	if val, ok := sourceData["updateTime"].(int64); ok {
		entry.UpdateTime = val
	}

	// Boolean fields
	if val, ok := sourceData["onlyAdmin"].(bool); ok {
		entry.OnlyAdmin = val
	}

	// Interface{} fields (complex objects)
	if val, ok := sourceData["supportClient"].(map[string]interface{}); ok {
		entry.SupportClient = val
	}
	if val, ok := sourceData["permission"].(map[string]interface{}); ok {
		entry.Permission = val
	}
	if val, ok := sourceData["middleware"].(map[string]interface{}); ok {
		entry.Middleware = val
	}
	if val, ok := sourceData["options"].(map[string]interface{}); ok {
		entry.Options = val
	}
	if val, ok := sourceData["i18n"].(map[string]interface{}); ok {
		entry.I18n = val
	}
	if val, ok := sourceData["variants"].(map[string]interface{}); ok {
		entry.Variants = val
	}
	if val, ok := sourceData["metadata"].(map[string]interface{}); ok {
		entry.Metadata = val
	}
	if val, ok := sourceData["count"].(interface{}); ok {
		entry.Count = val
	}

	// Array of interface{} fields
	if val, ok := sourceData["entrances"].([]interface{}); ok {
		entry.Entrances = make([]map[string]interface{}, len(val))
		for i, entrance := range val {
			if entranceMap, ok := entrance.(map[string]interface{}); ok {
				entry.Entrances[i] = entranceMap
			}
		}
	}
	if val, ok := sourceData["license"].([]interface{}); ok {
		entry.License = make([]map[string]interface{}, len(val))
		for i, license := range val {
			if licenseMap, ok := license.(map[string]interface{}); ok {
				entry.License[i] = licenseMap
			}
		}
	}
	if val, ok := sourceData["legal"].([]interface{}); ok {
		entry.Legal = make([]map[string]interface{}, len(val))
		for i, legal := range val {
			if legalMap, ok := legal.(map[string]interface{}); ok {
				entry.Legal[i] = legalMap
			}
		}
	}

	// Handle legacy field names for backward compatibility
	if val, ok := sourceData["app_id"].(string); ok && val != "" && entry.AppID == "" {
		entry.AppID = val
	}
	if val, ok := sourceData["app_name"].(string); ok && val != "" && entry.Name == "" {
		entry.Name = val
	}
	if val, ok := sourceData["app_version"].(string); ok && val != "" && entry.Version == "" {
		entry.Version = val
	}
}

// NewApplicationInfoEntry creates a complete ApplicationInfoEntry from source data
// This function ensures all fields are properly mapped to avoid data loss
func NewApplicationInfoEntry(sourceData map[string]interface{}) *ApplicationInfoEntry {
	if sourceData == nil || len(sourceData) == 0 {
		return nil
	}

	// Extract essential identifiers
	var appID, appName string

	if id, ok := sourceData["id"].(string); ok && id != "" {
		appID = id
	}
	if name, ok := sourceData["name"].(string); ok && name != "" {
		appName = name
	}
	if appID == "" {
		if aid, ok := sourceData["appID"].(string); ok && aid != "" {
			appID = aid
		} else if aid, ok := sourceData["app_id"].(string); ok && aid != "" {
			appID = aid
		}
	}
	if appName == "" {
		if title, ok := sourceData["title"].(string); ok && title != "" {
			appName = title
		}
	}

	// Ensure we have both ID and name
	if appID == "" && appName != "" {
		appID = appName
	}
	if appName == "" && appID != "" {
		appName = appID
	}

	// If no essential data found, return nil
	if appID == "" && appName == "" {
		return nil
	}

	// Create the entry with basic initialization
	entry := &ApplicationInfoEntry{
		ID:                 appID,
		AppID:              appID,
		Name:               appName,
		Title:              map[string]string{"en-US": appName}, // Initialize with default language
		Description:        make(map[string]string),
		FullDescription:    make(map[string]string),
		UpgradeDescription: make(map[string]string),
		CreateTime:         getCurrentTimestamp(),
		UpdateTime:         getCurrentTimestamp(),
		Metadata:           make(map[string]interface{}),
	}

	// Map all fields from source data
	mapAllApplicationInfoEntryFields(sourceData, entry)

	// Validate and fix AppLabels if needed
	ValidateAndFixAppLabels(sourceData, entry)

	// Store source data in metadata for reference
	entry.Metadata["source_data"] = sourceData
	entry.Metadata["creation_method"] = "NewApplicationInfoEntry"

	return entry
}

// ValidateApplicationInfoEntryFields validates that all fields are properly mapped
// This function helps identify any missing field mappings
func ValidateApplicationInfoEntryFields(entry *ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		return map[string]interface{}{"error": "entry is nil"}
	}

	validation := make(map[string]interface{})

	// Check basic fields
	validation["id"] = entry.ID
	validation["name"] = entry.Name
	validation["cfgType"] = entry.CfgType
	validation["chartName"] = entry.ChartName
	validation["icon"] = entry.Icon
	validation["appID"] = entry.AppID
	validation["version"] = entry.Version
	validation["versionName"] = entry.VersionName

	// Check multi-language fields
	validation["description"] = entry.Description
	validation["title"] = entry.Title
	validation["fullDescription"] = entry.FullDescription
	validation["upgradeDescription"] = entry.UpgradeDescription

	// Check array fields
	validation["categories"] = entry.Categories
	validation["appLabels"] = entry.AppLabels
	validation["locale"] = entry.Locale
	validation["supportArch"] = entry.SupportArch
	validation["promoteImage"] = entry.PromoteImage
	validation["screenshots"] = entry.Screenshots
	validation["tags"] = entry.Tags

	// Check string fields
	validation["promoteVideo"] = entry.PromoteVideo
	validation["subCategory"] = entry.SubCategory
	validation["developer"] = entry.Developer
	validation["requiredMemory"] = entry.RequiredMemory
	validation["requiredDisk"] = entry.RequiredDisk
	validation["requiredGPU"] = entry.RequiredGPU
	validation["requiredCPU"] = entry.RequiredCPU
	validation["target"] = entry.Target
	validation["submitter"] = entry.Submitter
	validation["doc"] = entry.Doc
	validation["website"] = entry.Website
	validation["featuredImage"] = entry.FeaturedImage
	validation["sourceCode"] = entry.SourceCode
	validation["modelSize"] = entry.ModelSize
	validation["namespace"] = entry.Namespace
	validation["lastCommitHash"] = entry.LastCommitHash
	validation["updated_at"] = entry.UpdatedAt

	// Check numeric fields
	validation["rating"] = entry.Rating
	validation["createTime"] = entry.CreateTime
	validation["updateTime"] = entry.UpdateTime

	// Check boolean fields
	validation["onlyAdmin"] = entry.OnlyAdmin

	// Check interface{} fields
	validation["supportClient"] = entry.SupportClient
	validation["permission"] = entry.Permission
	validation["middleware"] = entry.Middleware
	validation["options"] = entry.Options
	validation["i18n"] = entry.I18n
	validation["variants"] = entry.Variants
	validation["metadata"] = entry.Metadata
	validation["count"] = entry.Count

	// Check array of interface{} fields
	validation["entrances"] = entry.Entrances
	validation["license"] = entry.License
	validation["legal"] = entry.Legal

	return validation
}

// ExampleCompleteFieldMapping demonstrates how to use the complete field mapping
// This function shows the proper way to create ApplicationInfoEntry without data loss
func ExampleCompleteFieldMapping() {
	// Sample source data with all possible fields
	sampleData := map[string]interface{}{
		"id":                 "example-app-123",
		"name":               "Example Application",
		"cfgType":            "app",
		"chartName":          "example-chart",
		"icon":               "https://example.com/icon.png",
		"appID":              "example-app-123",
		"version":            "1.0.0",
		"versionName":        "First Release",
		"description":        "This is an example application",
		"title":              "Example App",
		"fullDescription":    "This is a complete description of the example application",
		"upgradeDescription": "This update includes new features",
		"promoteImage":       []interface{}{"https://example.com/promo1.png", "https://example.com/promo2.png"},
		"promoteVideo":       "https://example.com/video.mp4",
		"subCategory":        "Development",
		"locale":             []interface{}{"en-US", "zh-CN"},
		"developer":          "Example Developer",
		"requiredMemory":     "512Mi",
		"requiredDisk":       "1Gi",
		"supportClient":      map[string]interface{}{"chrome": ">=90", "edge": ">=90"},
		"supportArch":        []interface{}{"amd64", "arm64"},
		"requiredGPU":        "1Gi",
		"requiredCPU":        "1",
		"rating":             4.5,
		"target":             "production",
		"permission":         map[string]interface{}{"appData": true, "userData": []string{"read", "write"}},
		"entrances":          []interface{}{map[string]interface{}{"name": "main", "host": "localhost", "port": 8080}},
		"middleware":         map[string]interface{}{"postgres": map[string]interface{}{"enabled": true}},
		"options":            map[string]interface{}{"policies": []interface{}{}},
		"submitter":          "example@example.com",
		"doc":                "https://example.com/docs",
		"website":            "https://example.com",
		"featuredImage":      "https://example.com/featured.png",
		"sourceCode":         "https://github.com/example/app",
		"license":            []interface{}{map[string]interface{}{"text": "MIT", "url": "https://opensource.org/licenses/MIT"}},
		"legal":              []interface{}{map[string]interface{}{"text": "Terms of Service", "url": "https://example.com/terms"}},
		"i18n":               map[string]interface{}{"en-US": map[string]interface{}{"metadata": map[string]interface{}{"title": "Example App"}}},
		"modelSize":          "2GB",
		"namespace":          "default",
		"onlyAdmin":          false,
		"lastCommitHash":     "abc123def456",
		"createTime":         int64(1640995200),
		"updateTime":         int64(1640995200),
		"appLabels":          []interface{}{"featured", "recommended"},
		"count":              42,
		"variants":           map[string]interface{}{"user": map[string]interface{}{"name": "User Variant"}},
		"screenshots":        []interface{}{"https://example.com/screenshot1.png"},
		"tags":               []interface{}{"example", "demo"},
		"categories":         []interface{}{"Development", "Tools"},
		"metadata":           map[string]interface{}{"custom_field": "custom_value"},
		"updated_at":         "2024-01-01T00:00:00Z",
	}

	// Create ApplicationInfoEntry using the complete field mapping
	entry := NewApplicationInfoEntry(sampleData)
	if entry == nil {
		log.Printf("ERROR: Failed to create ApplicationInfoEntry")
		return
	}

	// Validate that all fields are properly mapped
	validation := ValidateApplicationInfoEntryFields(entry)

	log.Printf("SUCCESS: ApplicationInfoEntry created with %d fields", len(validation))
	log.Printf("Sample field values:")
	log.Printf("  ID: %s", entry.ID)
	log.Printf("  Name: %s", entry.Name)
	log.Printf("  Version: %s", entry.Version)
	log.Printf("  Rating: %f", entry.Rating)
	log.Printf("  Categories: %v", entry.Categories)
	log.Printf("  RequiredMemory: %s", entry.RequiredMemory)
	log.Printf("  SupportClient: %v", entry.SupportClient)
}

// DebugAppLabelsFlow helps debug AppLabels data flow through the system
func DebugAppLabelsFlow(sourceData map[string]interface{}, entry *ApplicationInfoEntry, stage string) {
	log.Printf("DEBUG: AppLabels Flow - Stage: %s", stage)

	// Check source data
	if sourceData != nil {
		if appLabels, ok := sourceData["appLabels"].([]interface{}); ok {
			log.Printf("DEBUG: Source data AppLabels: %v (type: %T, length: %d)", appLabels, appLabels, len(appLabels))
		} else {
			log.Printf("DEBUG: Source data AppLabels: not found or wrong type")
		}
	} else {
		log.Printf("DEBUG: Source data is nil")
	}

	// Check entry
	if entry != nil {
		log.Printf("DEBUG: Entry AppLabels: %v (type: %T, length: %d)", entry.AppLabels, entry.AppLabels, len(entry.AppLabels))
		if len(entry.AppLabels) > 0 {
			for i, label := range entry.AppLabels {
				log.Printf("DEBUG: Entry AppLabels[%d]: %s", i, label)
			}
		}
	} else {
		log.Printf("DEBUG: Entry is nil")
	}

	log.Printf("DEBUG: AppLabels Flow - Stage: %s - END", stage)
}

// ValidateAndFixAppLabels ensures AppLabels are properly set and not lost
func ValidateAndFixAppLabels(sourceData map[string]interface{}, entry *ApplicationInfoEntry) {
	if entry == nil {
		return
	}

	// Initialize AppLabels if nil
	if entry.AppLabels == nil {
		entry.AppLabels = make([]string, 0)
	}

	// Check if AppLabels are missing but should be present in source data
	if len(entry.AppLabels) == 0 {
		if sourceData != nil {
			if appLabels, ok := sourceData["appLabels"].([]interface{}); ok && len(appLabels) > 0 {
				// Re-map AppLabels from source data
				entry.AppLabels = make([]string, len(appLabels))
				for i, label := range appLabels {
					if labelStr, ok := label.(string); ok {
						entry.AppLabels[i] = labelStr
					}
				}
				log.Printf("DEBUG: Fixed missing AppLabels from source data: %v", entry.AppLabels)
			}
		}
	}

	// Check metadata for stored AppLabels
	if len(entry.AppLabels) == 0 && entry.Metadata != nil {
		if sourceAppData, ok := entry.Metadata["source_app_data"].(map[string]interface{}); ok {
			if appLabels, ok := sourceAppData["appLabels"].([]interface{}); ok && len(appLabels) > 0 {
				entry.AppLabels = make([]string, len(appLabels))
				for i, label := range appLabels {
					if labelStr, ok := label.(string); ok {
						entry.AppLabels[i] = labelStr
					}
				}
				log.Printf("DEBUG: Fixed missing AppLabels from metadata: %v", entry.AppLabels)
			}
		}
	}
}

// TestAppLabelsFix demonstrates the AppLabels fix functionality
func TestAppLabelsFix() {
	// Test case 1: AppLabels in source data
	testData1 := map[string]interface{}{
		"id":        "test-app-1",
		"name":      "Test App 1",
		"appLabels": []interface{}{"featured", "recommended", "suspend"},
	}

	entry1 := NewApplicationInfoEntry(testData1)
	log.Printf("TEST 1: AppLabels from source data: %v", entry1.AppLabels)

	// Test case 2: AppLabels in metadata
	testData2 := map[string]interface{}{
		"id":        "test-app-2",
		"name":      "Test App 2",
		"appLabels": []interface{}{"remove", "nsfw"},
	}

	entry2 := &ApplicationInfoEntry{
		ID:        "test-app-2",
		Name:      "Test App 2",
		AppLabels: []string{}, // Empty AppLabels
		Metadata: map[string]interface{}{
			"source_app_data": testData2,
		},
	}

	ValidateAndFixAppLabels(nil, entry2)
	log.Printf("TEST 2: AppLabels from metadata: %v", entry2.AppLabels)

	// Test case 3: No AppLabels
	testData3 := map[string]interface{}{
		"id":   "test-app-3",
		"name": "Test App 3",
	}

	entry3 := NewApplicationInfoEntry(testData3)
	log.Printf("TEST 3: No AppLabels: %v", entry3.AppLabels)
}

// NewAppRenderFailedData creates a new app render failed data structure
func NewAppRenderFailedData(rawData *ApplicationInfoEntry, rawPackage string, values []*Values, appInfo *AppInfo, renderedPackage string, failureReason string, failureStep string, retryCount int) *AppRenderFailedData {
	return &AppRenderFailedData{
		Type:            AppRenderFailed, // Use correct type for render failed data
		Timestamp:       getCurrentTimestamp(),
		RawData:         rawData,
		RawPackage:      rawPackage,
		Values:          values,
		AppInfo:         appInfo,
		RenderedPackage: renderedPackage,
		FailureReason:   failureReason,
		FailureStep:     failureStep,
		FailureTime:     time.Now(),
		RetryCount:      retryCount,
	}
}

// NewAppRenderFailedDataFromPending creates AppRenderFailedData from AppInfoLatestPendingData
func NewAppRenderFailedDataFromPending(pendingData *AppInfoLatestPendingData, failureReason string, failureStep string, retryCount int) *AppRenderFailedData {
	return &AppRenderFailedData{
		Type:            pendingData.Type,
		Timestamp:       pendingData.Timestamp,
		Version:         pendingData.Version,
		RawData:         pendingData.RawData,
		RawPackage:      pendingData.RawPackage,
		Values:          pendingData.Values,
		AppInfo:         pendingData.AppInfo,
		RenderedPackage: pendingData.RenderedPackage,
		FailureReason:   failureReason,
		FailureStep:     failureStep,
		FailureTime:     time.Now(),
		RetryCount:      retryCount,
	}
}
