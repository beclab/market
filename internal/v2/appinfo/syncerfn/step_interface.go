package syncerfn

import (
	"context"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/types"

	"github.com/go-resty/resty/v2"
)

// SyncStep represents an abstract step in the synchronization process
type SyncStep interface {
	// GetStepName returns the name of the step for logging and identification
	GetStepName() string

	// Execute performs the actual step logic
	Execute(ctx context.Context, data *SyncContext) error

	// CanSkip determines if this step can be skipped based on current state
	CanSkip(ctx context.Context, data *SyncContext) bool
}

// AppStoreInfoResponse represents the structured response from /appstore/info API
type AppStoreInfoResponse struct {
	Version     string                 `json:"version"`
	Hash        string                 `json:"hash"`
	LastUpdated time.Time              `json:"last_updated"`
	Data        AppStoreDataSection    `json:"data"`
	Stats       map[string]interface{} `json:"stats"`
}

// AppStoreDataSection represents the data section of the appstore response
type AppStoreDataSection struct {
	Apps       map[string]interface{}     `json:"apps"`
	Recommends map[string]RemoteRecommend `json:"recommends"`
	Pages      map[string]RemotePage      `json:"pages"`
	Topics     map[string]RemoteTopic     `json:"topics"`
	TopicLists map[string]RemoteTopicList `json:"topic_lists"`
	Tops       []RemoteTopItem            `json:"tops"`
	Latest     []string                   `json:"latest"`
	Tags       map[string]RemoteTag       `json:"tags"`
}

// RemoteRecommendData represents the multilingual metadata nested in recommends.
type RemoteRecommendData struct {
	Title       map[string]interface{} `json:"title"`
	Description map[string]interface{} `json:"description"`
}

// RemoteRecommend represents one recommend entry from /appstore/info.
type RemoteRecommend struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Content     string               `json:"content"`
	Data        *RemoteRecommendData `json:"data"`
	Source      string               `json:"source"`
	UpdatedAt   string               `json:"updated_at"`
	CreatedAt   string               `json:"createdAt"`
}

// RemotePage represents one page entry from /appstore/info.
type RemotePage struct {
	Category  string `json:"category"`
	Content   string `json:"content"`
	Source    string `json:"source"`
	UpdatedAt string `json:"updated_at"`
	CreatedAt string `json:"createdAt"`
}

// RemoteTopicData represents language-specific topic metadata.
type RemoteTopicData struct {
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

// RemoteTopic represents one topic entry from /appstore/info.
type RemoteTopic struct {
	ID        string                     `json:"_id"`
	Name      string                     `json:"name"`
	Data      map[string]RemoteTopicData `json:"data"`
	Source    string                     `json:"source"`
	UpdatedAt string                     `json:"updated_at"`
	CreatedAt string                     `json:"createdAt"`
}

// RemoteTopicList represents one topic list entry from /appstore/info.
type RemoteTopicList struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Content     string                 `json:"content"`
	Title       map[string]interface{} `json:"title"`
	Source      string                 `json:"source"`
	UpdatedAt   string                 `json:"updated_at"`
	CreatedAt   string                 `json:"createdAt"`
}

// RemoteTopItem represents one entry in the tops section.
type RemoteTopItem struct {
	AppID       string `json:"appId"`
	LegacyAppID string `json:"appid"`
	Rank        int    `json:"rank"`
}

// RemoteTag represents one tag entry from /appstore/info.
type RemoteTag struct {
	ID        string                 `json:"_id"`
	Name      string                 `json:"name"`
	Title     map[string]interface{} `json:"title"`
	Icon      string                 `json:"icon"`
	Sort      int                    `json:"sort"`
	Source    string                 `json:"source"`
	UpdatedAt string                 `json:"updated_at"`
	CreatedAt string                 `json:"createdAt"`
}

// SyncContext holds the context data for the entire sync process
type SyncContext struct {
	Client       *resty.Client               `json:"-"`
	Cache        *types.CacheData            `json:"-"`       // Keep for backward compatibility
	CacheManager types.CacheManagerInterface `json:"-"`       // CacheManager for unified lock strategy
	MarketSource *settings.MarketSource      `json:"-"`       // Current market source being used
	Version      string                      `json:"version"` // Version to use for API requests
	RemoteHash   string                      `json:"remote_hash"`
	LocalHash    string                      `json:"local_hash"`
	HashMatches  bool                        `json:"hash_matches"` // Indicates if remote and local hashes match
	// Updated to use structured response.
	LatestData   *AppStoreInfoResponse  `json:"latest_data"`
	DetailedApps map[string]interface{} `json:"detailed_apps"`
	AppIDs       []string               `json:"app_ids"`
	Errors       []error                `json:"-"`
}

// NewSyncContextWithManager creates a new sync context with CacheManager
func NewSyncContextWithManager(cache *types.CacheData, cacheManager types.CacheManagerInterface) *SyncContext {
	return &SyncContext{
		Client:       resty.New(),
		Cache:        cache,
		CacheManager: cacheManager,
		LatestData:   nil,
		DetailedApps: make(map[string]interface{}),
		AppIDs:       make([]string, 0),
		Errors:       make([]error, 0),
	}
}

// AddError adds an error to the context in a thread-safe manner
func (sc *SyncContext) AddError(err error) {
	sc.Errors = append(sc.Errors, err)
}

// HasErrors returns true if there are any errors in the context
func (sc *SyncContext) HasErrors() bool {
	return len(sc.Errors) > 0
}

// GetErrors returns a copy of all errors
func (sc *SyncContext) GetErrors() []error {
	errors := make([]error, len(sc.Errors))
	copy(errors, sc.Errors)
	return errors
}

// SetVersion sets the version to use for API requests
func (sc *SyncContext) SetVersion(version string) {
	sc.Version = version
}

// GetVersion returns the version to use for API requests
func (sc *SyncContext) GetVersion() string {
	return sc.Version
}

// SetMarketSource sets the current market source for the sync context
func (sc *SyncContext) SetMarketSource(source *settings.MarketSource) {
	sc.MarketSource = source
}

// GetMarketSource returns the current market source
func (sc *SyncContext) GetMarketSource() *settings.MarketSource {
	return sc.MarketSource
}
