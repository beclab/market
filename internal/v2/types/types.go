package types

import (
	"log"
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

// Others contains additional data for AppInfoLatestPendingData
type Others struct {
	Hash       string       `json:"hash"`
	Version    string       `json:"version"`
	Topics     []*Topic     `json:"topics"`
	TopicLists []*TopicList `json:"topic_lists"`
	Recommends []*Recommend `json:"recommends"`
	Pages      []*Page      `json:"pages"`
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
	Type      AppDataType            `json:"type"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	Version   string                 `json:"version,omitempty"`
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
	Others               *Others                     `json:"others,omitempty"` // Additional data like hash, topics, etc.
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

// ImageInfo represents detailed information about a Docker image
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
type LayerInfo struct {
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	MediaType  string `json:"media_type,omitempty"`
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

	// Store the original data in metadata for later processing
	rawData.Metadata["source_data"] = data
	rawData.Metadata["data_type"] = "legacy_app_latest_data"

	// Extract other basic fields if available
	if desc, ok := data["description"].(string); ok {
		rawData.Description = map[string]string{"en-US": desc} // Initialize with default language
	}
	if icon, ok := data["icon"].(string); ok {
		rawData.Icon = icon
	}
	if version, ok := data["version"].(string); ok {
		rawData.Version = version
	}
	if chartName, ok := data["chartName"].(string); ok {
		rawData.ChartName = chartName
	}
	if cfgType, ok := data["cfgType"].(string); ok {
		rawData.CfgType = cfgType
	}

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

	// Extract other optional fields
	if desc, ok := appData["description"].(string); ok {
		rawData.Description = map[string]string{"en-US": desc} // Initialize with default language
	}
	if icon, ok := appData["icon"].(string); ok {
		rawData.Icon = icon
	}
	if version, ok := appData["version"].(string); ok {
		rawData.Version = version
	}
	if chartName, ok := appData["chartName"].(string); ok {
		rawData.ChartName = chartName
	}
	if cfgType, ok := appData["cfgType"].(string); ok {
		rawData.CfgType = cfgType
	}
	if categories, ok := appData["categories"].([]interface{}); ok {
		rawData.Categories = make([]string, len(categories))
		for i, cat := range categories {
			if catStr, ok := cat.(string); ok {
				rawData.Categories[i] = catStr
			}
		}
	}

	// Store the complete app data in metadata for later processing
	rawData.Metadata["source_app_data"] = appData
	rawData.Metadata["data_type"] = "single_app_data"
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
