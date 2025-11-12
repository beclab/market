package types

import (
	"encoding/json"
	"log"
	"reflect"
	"strings"
	"time"
)

type MarketSettings struct {
	SelectedSource string `json:"selected_source"`
}

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

// RecommendData represents data configuration for recommend
type RecommendData struct {
	Title       map[string]string `json:"title,omitempty"`
	Description map[string]string `json:"description,omitempty"`
}

// Recommend represents recommendation configuration
type Recommend struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Content     string         `json:"content"` // Comma-separated app names
	Data        *RecommendData `json:"data,omitempty"`
	Source      string         `json:"source,omitempty"` // Data source identifier
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Page represents page configuration
type Page struct {
	Category  string    `json:"category"`
	Content   string    `json:"content"` // JSON string of page content
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TopicData represents topic data for specific language
type TopicData struct {
	Group           string `json:"group"`
	Title           string `json:"title"`
	Des             string `json:"des"`
	IconImg         string `json:"iconimg"`
	DetailImg       string `json:"detailimg"`
	RichText        string `json:"richtext"`
	MobileDetailImg string `json:"mobileDetailImg"`
	MobileRichText  string `json:"mobileRichtext"`
	BackgroundColor string `json:"backgroundColor"`
	Apps            string `json:"apps"`
	IsDelete        bool   `json:"isdelete"`
}

// Topic represents topic configuration
type Topic struct {
	ID        string                `json:"_id"`
	Name      string                `json:"name"`
	Data      map[string]*TopicData `json:"data"` // i18n data by language
	Source    string                `json:"source"`
	UpdatedAt time.Time             `json:"updated_at"`
	CreatedAt time.Time             `json:"createdAt"`
}

// TopicList represents topic list configuration
type TopicList struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"` // Topic list type
	Description string            `json:"description"`
	Content     string            `json:"content"` // Comma-separated topic IDs
	Title       map[string]string `json:"title"`   // i18n title mapping
	Source      string            `json:"source"`
	UpdatedAt   time.Time         `json:"updated_at"`
	CreatedAt   time.Time         `json:"createdAt"`
}

// AppStoreTopItem represents an item in the tops list from appstore
type AppStoreTopItem struct {
	AppID string `json:"appid"`
	Rank  int    `json:"rank"`
}

// Tag represents tag configuration
type Tag struct {
	ID        string            `json:"_id"`
	Name      string            `json:"name"`
	Title     map[string]string `json:"title"` // i18n title mapping
	Icon      string            `json:"icon"`
	Sort      int               `json:"sort"` // sort order for tag
	Source    string            `json:"source"`
	UpdatedAt time.Time         `json:"updated_at"`
	CreatedAt time.Time         `json:"createdAt"`
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
	Tags       []*Tag             `json:"tags"`
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
	Type    AppDataType `json:"type"`
	Version string      `json:"version"` // Version field, cannot be empty
	Status  struct {
		Name               string `json:"name"`
		RawAppName         string `json:"rawAppName"`
		State              string `json:"state"`
		UpdateTime         string `json:"updateTime"`
		StatusTime         string `json:"statusTime"`
		LastTransitionTime string `json:"lastTransitionTime"`
		Progress           string `json:"progress"`
		EntranceStatuses   []struct {
			ID         string `json:"id"` // ID extracted from URL's first segment after splitting by "."
			Name       string `json:"name"`
			State      string `json:"state"`
			StatusTime string `json:"statusTime"`
			Reason     string `json:"reason"`
			Url        string `json:"url"`
			Invisible  bool   `json:"invisible"`
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

// SubChart represents a sub-chart configuration
type SubChart struct {
	Name   string `yaml:"name" json:"name"`
	Shared bool   `yaml:"shared,omitempty" json:"shared,omitempty"`
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
	ApiVersion  string            `json:"apiVersion,omitempty"`
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
	Permission         map[string]interface{}   `json:"permission"`     // Using interface{} for flexibility
	Entrances          []map[string]interface{} `json:"entrances"`      // Using interface{} for flexibility
	Middleware         map[string]interface{}   `json:"middleware"`     // Using interface{} for flexibility
	Options            map[string]interface{}   `json:"options"`        // Using interface{} for flexibility
	Envs               []map[string]interface{} `json:"envs,omitempty"` // Environment variable configurations

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

	// Version history information
	VersionHistory []*VersionInfo `json:"versionHistory,omitempty"`

	// Sub-charts information
	SubCharts []*SubChart `json:"subCharts,omitempty"`

	// Legacy fields for backward compatibility
	Screenshots []string               `json:"screenshots"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	UpdatedAt   string                 `json:"updated_at"`
}

// VersionInfo represents version information from gitbot
type VersionInfo struct {
	ID                 string     `json:"-" bson:"_id,omitempty"`
	AppName            string     `json:"appName" bson:"appName"`
	Version            string     `json:"version" bson:"version"`
	VersionName        string     `json:"versionName" bson:"versionName"`
	MergedAt           *time.Time `json:"mergedAt" bson:"mergedAt"`
	UpgradeDescription string     `json:"upgradeDescription" bson:"upgradeDescription"`
}

// AcceptedCurrency describes one accepted currency option
type AcceptedCurrency struct {
	Amount        string `json:"amount" yaml:"amount"`
	SystemChainID int    `json:"system_chain_id" yaml:"system_chain_id"`
	ChainID       int    `json:"chain_id" yaml:"chain_id"`
	Token         string `json:"token" yaml:"token"`
	Symbol        string `json:"symbol" yaml:"symbol"`
	Decimals      int    `json:"decimals" yaml:"decimals"`
	ProductID     string `json:"product_id" yaml:"product_id"`
}

// PriceProducts groups product types
type PriceProducts struct {
	NonConsumable struct {
		Price struct {
			AcceptedCurrencies []AcceptedCurrency `json:"accepted_currencies" yaml:"accepted_currencies"`
		} `json:"price" yaml:"price"`
	} `json:"non_consumable" yaml:"non_consumable"`
}

// PriceDeveloper holds developer identity and public key
type PriceDeveloper struct {
	DID       string `json:"did" yaml:"did"`
	RSAPublic string `json:"rsa_public" yaml:"rsa_public"`
}

// PriceConfig represents price.yaml structure
type PriceConfig struct {
	ReceiveAddresses map[string]string `json:"receive_addresses" yaml:"receive_addresses"`
	Developer        PriceDeveloper    `json:"developer" yaml:"developer"`
	Products         PriceProducts     `json:"products" yaml:"products"`
}

// AppInfo represents complete app information including analysis result
type AppInfo struct {
	AppEntry      *ApplicationInfoEntry `json:"app_entry"`
	ImageAnalysis *ImageAnalysisResult  `json:"image_analysis"`
	Price         *PriceConfig          `json:"price,omitempty"`
	PurchaseInfo  *PurchaseInfo         `json:"purchase_info,omitempty"`
}

// PurchaseInfo represents VC and purchase state stored in Redis
type PurchaseInfo struct {
	VC     string `json:"vc"`
	Status string `json:"status"` // e.g., "purchased" | "not_buy" | "not_sign" | "not_pay" | "paying"
}

// FrontendPaymentData represents payment data for frontend payment process
type FrontendPaymentData struct {
	From    string `json:"from"` // User's DID
	To      string `json:"to"`   // Developer's DID
	Product []struct {
		ProductID string `json:"product_id"`
	} `json:"product"` // Product information
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
	SupportArch    []string          `json:"support_arch,omitempty"`
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
	AppSimpleInfo   *AppSimpleInfo        `json:"app_simple_info,omitempty"`
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
	// Remove Mutex - unified lock strategy using CacheManager.mutex only
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

// AppInfoUpdate represents the data structure for app info updates
type AppInfoUpdate struct {
	AppStateLatest *AppStateLatestData `json:"app_state_latest"`
	AppInfoLatest  *AppInfoLatestData  `json:"app_info_latest"`
	Timestamp      int64               `json:"timestamp"`
	User           string              `json:"user"`
	AppName        string              `json:"app_name"`    // App name
	NotifyType     string              `json:"notify_type"` // Notify type
	Source         string              `json:"source"`      // Source
}

type MarketSystemUpdate struct {
	Timestamp     int64                  `json:"timestamp"`
	User          string                 `json:"user"`
	NotifyType    string                 `json:"notify_type"`
	Point         string                 `json:"point"`
	Extensions    map[string]string      `json:"extensions,omitempty"`     // Additional extension information
	ExtensionsObj map[string]interface{} `json:"extensions_obj,omitempty"` // Additional extension information
}

type ImageInfoUpdate struct {
	ImageInfo  *ImageInfo `json:"image_info"`
	Timestamp  int64      `json:"timestamp"`
	User       string     `json:"user"`
	NotifyType string     `json:"notify_type"`
}

// SignNotificationUpdate represents the data structure for signature notification updates
type SignNotificationUpdate struct {
	Sign  SignNotificationData `json:"sign"`
	User  string               `json:"user"`
	Vars  map[string]string    `json:"vars"`
	Topic string               `json:"topic"`
}

// SignNotificationData represents the sign data in the notification
type SignNotificationData struct {
	CallbackURL string                 `json:"callback_url"`
	SignBody    map[string]interface{} `json:"sign_body"`
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
	userData.Sources["upload"] = NewSourceDataWithType(SourceDataTypeLocal)
	userData.Sources["cli"] = NewSourceDataWithType(SourceDataTypeLocal)
	userData.Sources["studio"] = NewSourceDataWithType(SourceDataTypeLocal)

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
func NewAppStateLatestData(data map[string]interface{}, userID string, getInfoFunc func(string, string) (string, string, error)) (*AppStateLatestData, string) {
	// Extract status information from data
	var name, rawAppName, state, updateTime, statusTime, lastTransitionTime, progress string
	var entranceStatuses []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
		Url        string `json:"url"`
		Invisible  bool   `json:"invisible"`
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
		return nil, ""
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
	if progressVal, ok := data["progress"].(string); ok {
		progress = progressVal
	}
	// Extract rawAppName from data
	if rawAppNameVal, ok := data["rawAppName"].(string); ok {
		rawAppName = rawAppNameVal
	}

	// Handle entranceStatuses - support both []interface{} and []EntranceStatus
	if entranceStatusesVal, ok := data["entranceStatuses"]; ok && entranceStatusesVal != nil {
		log.Printf("DEBUG: NewAppStateLatestData - found entranceStatuses, type: %T, value: %+v", entranceStatusesVal, entranceStatusesVal)

		switch v := entranceStatusesVal.(type) {
		case []interface{}:
			log.Printf("DEBUG: NewAppStateLatestData - handling []interface{} case, length: %d", len(v))
			// Handle []interface{} case (from map[string]interface{})
			entranceStatuses = make([]struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				State      string `json:"state"`
				StatusTime string `json:"statusTime"`
				Reason     string `json:"reason"`
				Url        string `json:"url"`
				Invisible  bool   `json:"invisible"`
			}, len(v))

			for i, entrance := range v {
				log.Printf("DEBUG: NewAppStateLatestData - processing entrance[%d], type: %T, value: %+v", i, entrance, entrance)
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
					if url, ok := entranceMap["url"].(string); ok {
						entranceStatuses[i].Url = url
						// Extract ID from URL: split by "." and take the first segment
						if url != "" {
							segments := strings.Split(url, ".")
							if len(segments) > 0 {
								entranceStatuses[i].ID = segments[0]
							}
						}
					}
					if invisible, ok := entranceMap["invisible"].(bool); ok {
						entranceStatuses[i].Invisible = invisible
					}
					log.Printf("DEBUG: NewAppStateLatestData - processed entrance[%d]: ID=%s, Name=%s, State=%s, URL=%s",
						i, entranceStatuses[i].ID, entranceStatuses[i].Name, entranceStatuses[i].State, entranceStatuses[i].Url)
				} else {
					log.Printf("DEBUG: NewAppStateLatestData - entrance[%d] is not map[string]interface{}, type: %T", i, entrance)
				}
			}
		case []map[string]interface{}:
			log.Printf("DEBUG: NewAppStateLatestData - handling []map[string]interface{} case, length: %d", len(v))
			// Handle []map[string]interface{} case (direct conversion)
			entranceStatuses = make([]struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				State      string `json:"state"`
				StatusTime string `json:"statusTime"`
				Reason     string `json:"reason"`
				Url        string `json:"url"`
				Invisible  bool   `json:"invisible"`
			}, len(v))

			for i, entranceMap := range v {
				log.Printf("DEBUG: NewAppStateLatestData - processing entranceMap[%d]: %+v", i, entranceMap)
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
				if url, ok := entranceMap["url"].(string); ok {
					entranceStatuses[i].Url = url
					// Extract ID from URL: split by "." and take the first segment
					if url != "" {
						segments := strings.Split(url, ".")
						if len(segments) > 0 {
							entranceStatuses[i].ID = segments[0]
						}
					}
				}
				if invisible, ok := entranceMap["invisible"].(bool); ok {
					entranceStatuses[i].Invisible = invisible
				}
				log.Printf("DEBUG: NewAppStateLatestData - processed entrance[%d]: ID=%s, Name=%s, State=%s, URL=%s",
					i, entranceStatuses[i].ID, entranceStatuses[i].Name, entranceStatuses[i].State, entranceStatuses[i].Url)
			}
		default:
			// Try to handle other cases by converting to JSON and back
			log.Printf("DEBUG: NewAppStateLatestData - entranceStatuses type: %T, value: %+v", v, v)
		}
	} else {
		log.Printf("DEBUG: NewAppStateLatestData - no entranceStatuses found in data")
	}

	log.Printf("DEBUG: NewAppStateLatestData - final entranceStatuses count: %d", len(entranceStatuses))

	// Version assignment logic
	version := ""
	source := ""
	if versionVal, ok := data["version"].(string); ok && versionVal != "" {
		version = versionVal
		log.Printf("DEBUG: NewAppStateLatestData - using version from data: %s", version)
	}
	// If version is still empty, and getInfoFunc is available, and userID/name are not empty, try to get from record
	if version == "" && getInfoFunc != nil && userID != "" && name != "" {
		versionFromRecord, sourceFromRecord, err := getInfoFunc(userID, name)
		if err != nil {
			log.Printf("WARNING: NewAppStateLatestData - failed to get version from download record: %v", err)
		} else if versionFromRecord != "" {
			version = versionFromRecord
			source = sourceFromRecord
			log.Printf("DEBUG: NewAppStateLatestData - using version from download record: %s, source: %s", version, source)
		}
	}
	// If version is still empty, log error and return nil
	// if version == "" {
	// 	log.Printf("ERROR: NewAppStateLatestData - version is empty, cannot create AppStateLatestData")
	// 	return nil
	// }

	return &AppStateLatestData{
		Type:    AppStateLatest,
		Version: version,
		Status: struct {
			Name               string `json:"name"`
			RawAppName         string `json:"rawAppName"`
			State              string `json:"state"`
			UpdateTime         string `json:"updateTime"`
			StatusTime         string `json:"statusTime"`
			LastTransitionTime string `json:"lastTransitionTime"`
			Progress           string `json:"progress"`
			EntranceStatuses   []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				State      string `json:"state"`
				StatusTime string `json:"statusTime"`
				Reason     string `json:"reason"`
				Url        string `json:"url"`
				Invisible  bool   `json:"invisible"`
			} `json:"entranceStatuses"`
		}{
			Name:               name,
			RawAppName:         rawAppName,
			State:              state,
			UpdateTime:         updateTime,
			StatusTime:         statusTime,
			LastTransitionTime: lastTransitionTime,
			Progress:           progress,
			EntranceStatuses:   entranceStatuses,
		},
	}, source
}

// NewAppInfoLatestData creates a new app info latest data structure
func NewAppInfoLatestData(data map[string]interface{}) *AppInfoLatestData {
	log.Printf("[DEBUG] NewAppInfoLatestData: Starting with %d fields", len(data))
	if data == nil || len(data) == 0 {
		log.Printf("DEBUG: NewAppInfoLatestData called with nil or empty data, returning nil")
		return nil
	}

	// If the input is already a map version of AppInfoLatestData, restore directly
	if appInfoMap, ok := data["app_info"].(map[string]interface{}); ok {
		var appInfo AppInfo
		// Restore AppEntry
		if appEntryMap, ok := appInfoMap["app_entry"].(map[string]interface{}); ok {
			appInfo.AppEntry = NewApplicationInfoEntry(appEntryMap)
		}
		// Restore ImageAnalysis
		if imageAnalysisMap, ok := appInfoMap["image_analysis"].(map[string]interface{}); ok {
			b, err := json.Marshal(imageAnalysisMap)
			if err == nil {
				var result ImageAnalysisResult
				if err := json.Unmarshal(b, &result); err == nil {
					appInfo.ImageAnalysis = &result
				}
			}
		}

		// Restore Price
		if priceMap, ok := appInfoMap["price"].(map[string]interface{}); ok {
			b, err := json.Marshal(priceMap)
			if err == nil {
				var priceConfig PriceConfig
				if err := json.Unmarshal(b, &priceConfig); err == nil {
					appInfo.Price = &priceConfig
				}
			}
		}

		latest := &AppInfoLatestData{
			Type:            AppInfoLatest,
			Timestamp:       getCurrentTimestamp(),
			Version:         "",
			RawData:         nil,
			RawPackage:      "",
			Values:          make([]*Values, 0),
			AppInfo:         &appInfo,
			RenderedPackage: "",
		}
		if version, ok := data["version"].(string); ok {
			latest.Version = version
		}
		if rawDataMap, ok := data["raw_data"].(map[string]interface{}); ok {
			latest.RawData = NewApplicationInfoEntry(rawDataMap)
		}
		if rawPackage, ok := data["raw_package"].(string); ok {
			latest.RawPackage = rawPackage
		}
		if renderedPackage, ok := data["rendered_package"].(string); ok {
			latest.RenderedPackage = renderedPackage
		}
		// Restore Values
		if valuesArr, ok := data["values"].([]interface{}); ok {
			for _, v := range valuesArr {
				if vmap, ok := v.(map[string]interface{}); ok {
					val := &Values{}
					if fileName, ok := vmap["file_name"].(string); ok {
						val.FileName = fileName
					}
					if modifyType, ok := vmap["modify_type"].(string); ok {
						val.ModifyType = ModifyType(modifyType)
					}
					if modifyKey, ok := vmap["modify_key"].(string); ok {
						val.ModifyKey = modifyKey
					}
					if modifyValue, ok := vmap["modify_value"].(string); ok {
						val.ModifyValue = modifyValue
					}
					latest.Values = append(latest.Values, val)
				}
			}
		}

		if asi, ok := data["app_simple_info"]; ok && asi != nil {
			switch v := asi.(type) {
			case *AppSimpleInfo:
				latest.AppSimpleInfo = v
			case map[string]interface{}:
				b, err := json.Marshal(v)
				if err == nil {
					var asiObj AppSimpleInfo
					if err := json.Unmarshal(b, &asiObj); err == nil {
						latest.AppSimpleInfo = &asiObj
					}
				}
			}
		}
		return latest
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
	// rawData.Metadata["source_data"] = data

	appInfoLatest.RawData = rawData

	// Try to extract image_analysis from data
	var imageAnalysis *ImageAnalysisResult
	if ia, ok := data["image_analysis"]; ok && ia != nil {
		// Try type assertion
		switch v := ia.(type) {
		case *ImageAnalysisResult:
			imageAnalysis = v
		case map[string]interface{}:
			// Try to unmarshal
			b, err := json.Marshal(v)
			if err == nil {
				var result ImageAnalysisResult
				if err := json.Unmarshal(b, &result); err == nil {
					imageAnalysis = &result
				}
			}
		}
	}

	// Try to extract price from data
	var price *PriceConfig
	if priceData, ok := data["price"]; ok && priceData != nil {
		// Try type assertion
		switch v := priceData.(type) {
		case *PriceConfig:
			price = v
		case map[string]interface{}:
			// Try to unmarshal
			b, err := json.Marshal(v)
			if err == nil {
				var priceConfig PriceConfig
				if err := json.Unmarshal(b, &priceConfig); err == nil {
					price = &priceConfig
					log.Printf("[DEBUG] NewAppInfoLatestData: Successfully restored price config for app %s", appID)
				} else {
					log.Printf("[DEBUG] NewAppInfoLatestData: Failed to unmarshal price config: %v", err)
				}
			} else {
				log.Printf("[DEBUG] NewAppInfoLatestData: Failed to marshal price data: %v", err)
			}
		}
	} else {
		log.Printf("[DEBUG] NewAppInfoLatestData: No price data found for app %s", appID)
	}

	appInfoLatest.AppInfo = &AppInfo{
		AppEntry:      rawData,
		ImageAnalysis: imageAnalysis, // Set ImageAnalysis if present
		Price:         price,         // Set Price if present
	}

	log.Printf("[DEBUG] NewAppInfoLatestData: Completed successfully")
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
		AppInfo:         appInfo, // AppInfo already contains Price field
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
		Price:         nil, // Will be set separately if needed
	}
}

// NewAppInfoWithPrice creates a new AppInfo structure with price information
func NewAppInfoWithPrice(appEntry *ApplicationInfoEntry, imageAnalysis *ImageAnalysisResult, price *PriceConfig) *AppInfo {
	return &AppInfo{
		AppEntry:      appEntry,
		ImageAnalysis: imageAnalysis,
		Price:         price,
	}
}

// NewPriceConfig creates a new PriceConfig structure
func NewPriceConfig(receiveAddresses map[string]string, developer PriceDeveloper, products PriceProducts) *PriceConfig {
	return &PriceConfig{
		ReceiveAddresses: receiveAddresses,
		Developer:        developer,
		Products:         products,
	}
}

// NewPriceDeveloper creates a new PriceDeveloper structure
func NewPriceDeveloper(did string, rsaPublic string) PriceDeveloper {
	return PriceDeveloper{
		DID:       did,
		RSAPublic: rsaPublic,
	}
}

// NewPriceProducts creates a new PriceProducts structure
func NewPriceProducts(acceptedCurrencies []AcceptedCurrency) PriceProducts {
	return PriceProducts{
		NonConsumable: struct {
			Price struct {
				AcceptedCurrencies []AcceptedCurrency `json:"accepted_currencies" yaml:"accepted_currencies"`
			} `json:"price" yaml:"price"`
		}{
			Price: struct {
				AcceptedCurrencies []AcceptedCurrency `json:"accepted_currencies" yaml:"accepted_currencies"`
			}{
				AcceptedCurrencies: acceptedCurrencies,
			},
		},
	}
}

// NewAcceptedCurrency creates a new AcceptedCurrency structure
func NewAcceptedCurrency(amount string, systemChainID int, chainID int, token string, symbol string, decimals int) AcceptedCurrency {
	return AcceptedCurrency{
		Amount:        amount,
		SystemChainID: systemChainID,
		ChainID:       chainID,
		Token:         token,
		Symbol:        symbol,
		Decimals:      decimals,
	}
}

// getCurrentTimestamp returns current unix timestamp
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

// NewAppInfoLatestPendingDataFromLegacyData creates AppInfoLatestPendingData from a single app data
func NewAppInfoLatestPendingDataFromLegacyData(appData map[string]interface{}) *AppInfoLatestPendingData {
	// Add debug logging to inspect input data
	// log.Printf("DEBUG: NewAppInfoLatestPendingDataFromLegacyData called with appData: %+v", appData)
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
		SubCharts:  make([]*SubChart, 0),
	}

	// Use the complete field mapping function to ensure no data is lost
	mapAllApplicationInfoEntryFields(appData, rawData)

	// Debug AppLabels after mapping
	DebugAppLabelsFlow(appData, rawData, "NewAppInfoLatestPendingDataFromLegacyData")

	// Validate and fix AppLabels if needed
	ValidateAndFixAppLabels(appData, rawData)

	// Store the complete app data in metadata for later processing
	// rawData.Metadata["source_app_data"] = appData
	rawData.Metadata["validation_status"] = "validated"

	pendingData.RawData = rawData
	pendingData.AppInfo = &AppInfo{
		AppEntry:      rawData,
		ImageAnalysis: nil, // Will be filled later during hydration
		Price:         nil, // Will be filled later during hydration
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
	log.Printf("[DEBUG] mapAllApplicationInfoEntryFields: Starting with %d fields", len(sourceData))
	if sourceData == nil || entry == nil {
		log.Printf("[DEBUG] mapAllApplicationInfoEntryFields: sourceData or entry is nil")
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
	if val, ok := sourceData["apiVersion"].(string); ok && val != "" {
		entry.ApiVersion = val
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
	// Handle supportArch - check both []interface{} and []string types
	if val, ok := sourceData["supportArch"].([]interface{}); ok {
		log.Printf("DEBUG: mapAllApplicationInfoEntryFields - Found supportArch in sourceData: %+v (type: %T, length: %d)", val, val, len(val))
		entry.SupportArch = make([]string, len(val))
		for i, arch := range val {
			if archStr, ok := arch.(string); ok {
				entry.SupportArch[i] = archStr
			}
		}
		log.Printf("DEBUG: mapAllApplicationInfoEntryFields - Mapped supportArch to entry: %+v (length: %d)", entry.SupportArch, len(entry.SupportArch))
	} else if val, ok := sourceData["supportArch"].([]string); ok {
		log.Printf("DEBUG: mapAllApplicationInfoEntryFields - Found supportArch as []string in sourceData: %+v (type: %T, length: %d)", val, val, len(val))
		entry.SupportArch = append([]string{}, val...)
		log.Printf("DEBUG: mapAllApplicationInfoEntryFields - Mapped supportArch to entry: %+v (length: %d)", entry.SupportArch, len(entry.SupportArch))
	} else {
		log.Printf("DEBUG: mapAllApplicationInfoEntryFields - supportArch not found in sourceData or wrong type")
		if supportArchVal, exists := sourceData["supportArch"]; exists {
			log.Printf("DEBUG: mapAllApplicationInfoEntryFields - supportArch value exists but wrong type: %+v (type: %T)", supportArchVal, supportArchVal)
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

	// String or map fields for fullDescription
	if val, ok := sourceData["fullDescription"].(string); ok && val != "" {
		if entry.FullDescription == nil {
			entry.FullDescription = make(map[string]string)
		}
		entry.FullDescription["en-US"] = val
	} else if valMap, ok := sourceData["fullDescription"].(map[string]string); ok {
		entry.FullDescription = valMap
	} else if valMap, ok := sourceData["fullDescription"].(map[string]interface{}); ok {
		if entry.FullDescription == nil {
			entry.FullDescription = make(map[string]string)
		}
		for k, v := range valMap {
			if strVal, ok := v.(string); ok {
				entry.FullDescription[k] = strVal
			}
		}
	}
	// String or map fields for upgradeDescription
	if val, ok := sourceData["upgradeDescription"].(string); ok && val != "" {
		if entry.UpgradeDescription == nil {
			entry.UpgradeDescription = make(map[string]string)
		}
		entry.UpgradeDescription["en-US"] = val
	} else if valMap, ok := sourceData["upgradeDescription"].(map[string]string); ok {
		entry.UpgradeDescription = valMap
	} else if valMap, ok := sourceData["upgradeDescription"].(map[string]interface{}); ok {
		if entry.UpgradeDescription == nil {
			entry.UpgradeDescription = make(map[string]string)
		}
		for k, v := range valMap {
			if strVal, ok := v.(string); ok {
				entry.UpgradeDescription[k] = strVal
			}
		}
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
	if val, ok := sourceData["supportClient"]; ok {
		switch v := val.(type) {
		case map[string]interface{}:
			entry.SupportClient = v
			log.Printf("DEBUG: Successfully mapped supportClient from map[string]interface{}")
		case map[interface{}]interface{}:
			// Convert map[interface{}]interface{} to map[string]interface{}
			converted := make(map[string]interface{})
			for k, v2 := range v {
				if ks, ok := k.(string); ok {
					converted[ks] = v2
				} else {
					log.Printf("WARNING: supportClient key is not string: %T", k)
				}
			}
			entry.SupportClient = converted
			log.Printf("DEBUG: Converted supportClient from map[interface{}]interface{}")
		case map[string]string:
			// If it's map[string]string, convert to map[string]interface{}
			converted := make(map[string]interface{})
			for k, v2 := range v {
				converted[k] = v2
			}
			entry.SupportClient = converted
			log.Printf("DEBUG: Converted supportClient from map[string]string")
		default:
			log.Printf("ERROR: supportClient has unexpected type: %T, value: %v", val, val)
			// Try to create empty structure
			entry.SupportClient = make(map[string]interface{})
		}
	} else {
		log.Printf("DEBUG: supportClient field not found in source data")
	}
	if val, ok := sourceData["permission"].(map[string]interface{}); ok {
		entry.Permission = val
	}
	if val, ok := sourceData["middleware"].(map[string]interface{}); ok {
		entry.Middleware = val
	}
	if val, ok := sourceData["options"]; ok {
		switch v := val.(type) {
		case map[string]interface{}:
			entry.Options = v
		case map[interface{}]interface{}:
			converted := make(map[string]interface{})
			for k, v2 := range v {
				if ks, ok := k.(string); ok {
					converted[ks] = v2
				}
			}
			entry.Options = converted
		}
	}
	if val, ok := sourceData["i18n"].(map[string]interface{}); ok {
		entry.I18n = val
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

	// Version history information
	if val, ok := sourceData["versionHistory"].([]interface{}); ok {
		entry.VersionHistory = make([]*VersionInfo, len(val))
		for i, versionInfo := range val {
			if versionInfoMap, ok := versionInfo.(map[string]interface{}); ok {
				versionInfo := &VersionInfo{}

				if appName, ok := versionInfoMap["appName"].(string); ok {
					versionInfo.AppName = appName
				}
				if version, ok := versionInfoMap["version"].(string); ok {
					versionInfo.Version = version
				}
				if versionName, ok := versionInfoMap["versionName"].(string); ok {
					versionInfo.VersionName = versionName
				}
				if upgradeDescription, ok := versionInfoMap["upgradeDescription"].(string); ok {
					versionInfo.UpgradeDescription = upgradeDescription
				}
				if mergedAt, ok := versionInfoMap["mergedAt"].(string); ok && mergedAt != "" {
					if parsedTime, err := time.Parse(time.RFC3339, mergedAt); err == nil {
						versionInfo.MergedAt = &parsedTime
					}
				}

				entry.VersionHistory[i] = versionInfo
			}
		}
	}

	// Check sub-charts information
	if val, ok := sourceData["subCharts"].([]interface{}); ok {
		entry.SubCharts = make([]*SubChart, len(val))
		for i, subChart := range val {
			if subChartMap, ok := subChart.(map[string]interface{}); ok {
				subChart := &SubChart{}

				if name, ok := subChartMap["name"].(string); ok {
					subChart.Name = name
				}
				if shared, ok := subChartMap["shared"].(bool); ok {
					subChart.Shared = shared
				}

				entry.SubCharts[i] = subChart
			}
		}
	}

	// Validate and fix SupportClient data after mapping
	ValidateAndFixSupportClient(sourceData, entry)
	log.Printf("[DEBUG] mapAllApplicationInfoEntryFields: Completed successfully")
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
		SubCharts:          make([]*SubChart, 0),
	}

	// Map all fields from source data
	mapAllApplicationInfoEntryFields(sourceData, entry)

	// Validate and fix AppLabels if needed
	ValidateAndFixAppLabels(sourceData, entry)

	// Store a safe copy of source data in metadata to avoid circular references
	// Create a deep copy of source data without potential circular references
	safeSourceData := createSafeSourceDataCopy(sourceData)
	entry.Metadata["source_data"] = safeSourceData
	entry.Metadata["creation_method"] = "NewApplicationInfoEntry"

	return entry
}

// createSafeSourceDataCopy creates a safe copy of source data to avoid circular references
func createSafeSourceDataCopy(sourceData map[string]interface{}) map[string]interface{} {
	if sourceData == nil {
		return nil
	}

	safeCopy := make(map[string]interface{})
	visited := make(map[uintptr]bool)

	for key, value := range sourceData {
		// Skip potential circular reference keys
		if key == "source_data" || key == "raw_data" || key == "app_info" ||
			key == "parent" || key == "self" || key == "circular_ref" ||
			key == "back_ref" || key == "loop" {
			continue
		}

		safeCopy[key] = deepCopyValue(value, visited)
	}

	return safeCopy
}

// deepCopyValue performs a deep copy of a value while avoiding circular references
func deepCopyValue(value interface{}, visited map[uintptr]bool) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string, int, int64, float64, bool:
		return v
	case []string:
		return append([]string{}, v...)
	case []interface{}:
		// Only copy simple types from interface slice
		safeSlice := make([]interface{}, 0, len(v))
		for _, item := range v {
			switch item.(type) {
			case string, int, int64, float64, bool:
				safeSlice = append(safeSlice, item)
			default:
				// Skip complex slice items to avoid circular references
			}
		}
		return safeSlice
	case map[string]interface{}:
		// Check for circular references using pointer
		ptr := reflect.ValueOf(v).Pointer()
		if visited[ptr] {
			return nil // Skip circular reference
		}
		visited[ptr] = true
		defer delete(visited, ptr)

		safeMap := make(map[string]interface{})
		for k, val := range v {
			// Skip potential circular reference keys
			if k == "source_data" || k == "raw_data" || k == "app_info" ||
				k == "parent" || k == "self" || k == "circular_ref" ||
				k == "back_ref" || k == "loop" {
				continue
			}
			safeMap[k] = deepCopyValue(val, visited)
		}
		return safeMap
	case map[interface{}]interface{}:
		// 新增: 支持 map[interface{}]interface{} 转 map[string]interface{}
		safeMap := make(map[string]interface{})
		for k, val := range v {
			if ks, ok := k.(string); ok {
				safeMap[ks] = deepCopyValue(val, visited)
			}
		}
		return safeMap
	case []map[string]interface{}:
		safeSlice := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if itemCopy := deepCopyValue(item, visited); itemCopy != nil {
				if itemMap, ok := itemCopy.(map[string]interface{}); ok {
					safeSlice = append(safeSlice, itemMap)
				}
			}
		}
		return safeSlice
	default:
		// For other types, return nil to avoid potential circular references
		return nil
	}
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
	validation["apiVersion"] = entry.ApiVersion
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
	validation["metadata"] = entry.Metadata
	validation["count"] = entry.Count

	// Check array of interface{} fields
	validation["entrances"] = entry.Entrances
	validation["license"] = entry.License
	validation["legal"] = entry.Legal

	// Check version history
	validation["versionHistory"] = entry.VersionHistory

	// Check sub-charts
	validation["subCharts"] = entry.SubCharts

	return validation
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

// NewAppRenderFailedData creates a new app render failed data structure
func NewAppRenderFailedData(rawData *ApplicationInfoEntry, rawPackage string, values []*Values, appInfo *AppInfo, renderedPackage string, failureReason string, failureStep string, retryCount int) *AppRenderFailedData {
	return &AppRenderFailedData{
		Type:            AppRenderFailed, // Use correct type for render failed data
		Timestamp:       getCurrentTimestamp(),
		RawData:         rawData,
		RawPackage:      rawPackage,
		Values:          values,
		AppInfo:         appInfo, // AppInfo already contains Price field
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

type CacheConfig struct {
	Type     string `json:"type,omitempty" yaml:"type,omitempty"`
	Host     string `json:"host,omitempty" yaml:"host,omitempty"`
	Port     int    `json:"port,omitempty" yaml:"port,omitempty"`
	Database int    `json:"database,omitempty" yaml:"database,omitempty"`
}

// ValidateSupportClientData validates SupportClient data integrity
func ValidateSupportClientData(entry *ApplicationInfoEntry) {
	if entry == nil {
		log.Printf("WARNING: Cannot validate SupportClient for nil entry")
		return
	}

	if entry.SupportClient == nil {
		log.Printf("WARNING: SupportClient is nil in entry %s", entry.Name)
		return
	}

	// Validate each client type data
	expectedClients := []string{"android", "ios", "windows", "mac", "linux", "chrome", "edge"}
	validCount := 0

	for _, clientType := range expectedClients {
		if url, exists := entry.SupportClient[clientType]; exists {
			if url == nil {
				log.Printf("WARNING: SupportClient.%s is nil", clientType)
			} else if urlStr, ok := url.(string); ok {
				if urlStr == "" {
					log.Printf("WARNING: SupportClient.%s is empty string", clientType)
				} else {
					validCount++
					log.Printf("DEBUG: SupportClient.%s: %s", clientType, urlStr)
				}
			} else {
				log.Printf("WARNING: SupportClient.%s has wrong type: %T", clientType, url)
			}
		}
	}

	log.Printf("DEBUG: SupportClient validation completed for entry %s - %d valid clients found", entry.Name, validCount)
}

// ValidateAndFixSupportClient ensures SupportClient data is properly set and not lost
func ValidateAndFixSupportClient(sourceData map[string]interface{}, entry *ApplicationInfoEntry) {
	if entry == nil {
		return
	}

	// Initialize SupportClient if nil
	if entry.SupportClient == nil {
		entry.SupportClient = make(map[string]interface{})
	}

	// Check if SupportClient is missing but should be present in source data
	if len(entry.SupportClient) == 0 {
		if sourceData != nil {
			if supportClient, ok := sourceData["supportClient"]; ok && supportClient != nil {
				log.Printf("DEBUG: Attempting to fix missing SupportClient from source data")

				switch v := supportClient.(type) {
				case map[string]interface{}:
					entry.SupportClient = v
					log.Printf("DEBUG: Fixed missing SupportClient from source data: %v", entry.SupportClient)
				case map[interface{}]interface{}:
					// Convert map[interface{}]interface{} to map[string]interface{}
					converted := make(map[string]interface{})
					for k, v2 := range v {
						if ks, ok := k.(string); ok {
							converted[ks] = v2
						}
					}
					entry.SupportClient = converted
					log.Printf("DEBUG: Fixed missing SupportClient from source data (converted): %v", entry.SupportClient)
				case map[string]string:
					// Convert map[string]string to map[string]interface{}
					converted := make(map[string]interface{})
					for k, v2 := range v {
						converted[k] = v2
					}
					entry.SupportClient = converted
					log.Printf("DEBUG: Fixed missing SupportClient from source data (converted): %v", entry.SupportClient)
				default:
					log.Printf("WARNING: SupportClient in source data has unexpected type: %T", supportClient)
				}
			}
		}
	}

	// Validate the data after fixing
	ValidateSupportClientData(entry)
}

// GetAllowMultipleInstall returns the allowMultipleInstall value from Options
// Returns false if the field is not set or cannot be converted to bool
func (entry *ApplicationInfoEntry) GetAllowMultipleInstall() bool {
	if entry == nil || entry.Options == nil {
		return false
	}
	if val, ok := entry.Options["allowMultipleInstall"]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
		// Handle case where value might be stored as string (from YAML parsing)
		if strVal, ok := val.(string); ok {
			return strVal == "true" || strVal == "True" || strVal == "TRUE"
		}
	}
	return false
}
