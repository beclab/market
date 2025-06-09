package syncerfn

import (
	"context"
	"sync"
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
// 表示从/appstore/info API获取的结构化响应数据
type AppStoreInfoResponse struct {
	Version     string                 `json:"version"`
	Hash        string                 `json:"hash"`
	LastUpdated time.Time              `json:"last_updated"`
	Data        AppStoreDataSection    `json:"data"`
	Stats       map[string]interface{} `json:"stats"`
}

// AppStoreDataSection represents the data section of the appstore response
// 表示appstore响应的data部分
type AppStoreDataSection struct {
	Apps       map[string]interface{} `json:"apps"`
	Recommends map[string]interface{} `json:"recommends"`
	Pages      map[string]interface{} `json:"pages"`
	Topics     map[string]interface{} `json:"topics"`
	TopicLists map[string]interface{} `json:"topic_lists"`
}

// SyncContext holds the context data for the entire sync process
type SyncContext struct {
	Client       *resty.Client          `json:"-"`
	Cache        *types.CacheData       `json:"-"`
	MarketSource *settings.MarketSource `json:"-"`       // Current market source being used
	Version      string                 `json:"version"` // Version to use for API requests
	RemoteHash   string                 `json:"remote_hash"`
	LocalHash    string                 `json:"local_hash"`
	HashMatches  bool                   `json:"hash_matches"` // Indicates if remote and local hashes match
	// Updated to use structured response instead of generic map
	// 更新为使用结构化响应而不是通用map
	LatestData   *AppStoreInfoResponse  `json:"latest_data"`
	DetailedApps map[string]interface{} `json:"detailed_apps"`
	AppIDs       []string               `json:"app_ids"`
	Errors       []error                `json:"-"`
	mutex        sync.RWMutex           `json:"-"`
}

// NewSyncContext creates a new sync context
func NewSyncContext(cache *types.CacheData) *SyncContext {
	return &SyncContext{
		Client:       resty.New(),
		Cache:        cache,
		LatestData:   &AppStoreInfoResponse{},
		DetailedApps: make(map[string]interface{}),
		AppIDs:       make([]string, 0),
		Errors:       make([]error, 0),
	}
}

// AddError adds an error to the context in a thread-safe manner
func (sc *SyncContext) AddError(err error) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.Errors = append(sc.Errors, err)
}

// HasErrors returns true if there are any errors in the context
func (sc *SyncContext) HasErrors() bool {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return len(sc.Errors) > 0
}

// GetErrors returns a copy of all errors
func (sc *SyncContext) GetErrors() []error {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	errors := make([]error, len(sc.Errors))
	copy(errors, sc.Errors)
	return errors
}

// SetVersion sets the version to use for API requests
func (sc *SyncContext) SetVersion(version string) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.Version = version
}

// GetVersion returns the version to use for API requests
func (sc *SyncContext) GetVersion() string {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.Version
}

// SetMarketSource sets the current market source for the sync context
func (sc *SyncContext) SetMarketSource(source *settings.MarketSource) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.MarketSource = source
}

// GetMarketSource returns the current market source
func (sc *SyncContext) GetMarketSource() *settings.MarketSource {
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	return sc.MarketSource
}
