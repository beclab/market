package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"market/internal/v2/db/models"
	"market/internal/v2/helper"
	"market/internal/v2/store/marketsource"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
)

// CacheManager interface for updating cache when market sources change
type CacheManager interface {
	SyncMarketSourcesToCache(sources []*MarketSource) error
	// Check if any user has non-empty state data for a specific source
	HasUserStateDataForSource(sourceID string) bool
}

// NewSettingsManager creates a new settings manager instance
func NewSettingsManager(redisClient RedisClient) *SettingsManager {
	return &SettingsManager{
		redisClient: redisClient,
	}
}

// SetCacheManager sets the cache manager for syncing market source changes
func (sm *SettingsManager) SetCacheManager(cacheManager CacheManager) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cacheManager = cacheManager
	glog.V(4).Info("Cache manager set for settings manager")
}

// GetRedisClient returns the Redis client for external modules
func (sm *SettingsManager) GetRedisClient() RedisClient {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.redisClient
}

// syncMarketSourcesToCache synchronizes market sources to cache if cache manager is available
func (sm *SettingsManager) syncMarketSourcesToCache() {
	if sm.cacheManager != nil {
		if err := sm.cacheManager.SyncMarketSourcesToCache(sm.marketSources.Sources); err != nil {
			glog.Errorf("Failed to sync market sources to cache: %v", err)
		} else {
			glog.V(4).Infof("Successfully synced %d market sources to cache", len(sm.marketSources.Sources))
		}
	}
}

// Initialize initializes the settings manager
func (sm *SettingsManager) Initialize() error {
	glog.Info("Initializing settings manager...")

	// Initialize market sources
	if err := sm.initializeMarketSources(); err != nil {
		return fmt.Errorf("failed to initialize market sources: %w", err)
	}

	// Initialize API endpoints
	if err := sm.initializeAPIEndpoints(); err != nil {
		return fmt.Errorf("failed to initialize API endpoints: %w", err)
	}

	return nil
}

// initializeMarketSources initializes market sources configuration.
//
// Storage backend is PostgreSQL (`market_sources` table). Bootstrap policy:
//
//   - PG empty (or unreachable / public env) → seed the in-memory config
//     from createDefaultMarketSources, persist it back so the syncer can
//     pick up the rows.
//   - PG already has rows → merge with the default list (so a newly added
//     hardcoded default is auto-inserted into existing deployments) and
//     persist the merged result via UpsertSources, which preserves the
//     `data` JSONB column on already-rendered rows.
func (sm *SettingsManager) initializeMarketSources() error {
	config, err := sm.loadMarketSourcesFromStore()
	if err != nil || config == nil || len(config.Sources) == 0 {
		if err != nil {
			glog.Errorf("Failed to load market sources from PG: %v", err)
		} else {
			glog.V(2).Info("market_sources table is empty; seeding defaults")
		}

		// Create default configuration from environment variables
		config = sm.createDefaultMarketSources()

		if saveErr := sm.saveMarketSourcesToStore(config); saveErr != nil {
			glog.Errorf("Failed to save default market sources to PG: %v", saveErr)
		}

		glog.V(4).Info("Loaded Official Market Sources from environment")
	} else {
		glog.V(4).Infof("Loaded market sources from PG: %d sources", len(config.Sources))

		// Merge default configuration with existing PG configuration so
		// newly added hardcoded defaults flow into existing deployments.
		config = sm.mergeWithDefaultConfig(config)

		if saveErr := sm.saveMarketSourcesToStore(config); saveErr != nil {
			glog.Errorf("Failed to save merged market sources to PG: %v", saveErr)
		}
	}

	// Set in memory
	sm.mu.Lock()
	sm.marketSources = config
	sm.mu.Unlock()

	return nil
}

// ReloadMarketSources reloads market sources from PG into memory.
// This is useful after syncing sources from chartrepo
func (sm *SettingsManager) ReloadMarketSources() error {
	glog.V(4).Info("Reloading market sources from PG...")
	config, err := sm.loadMarketSourcesFromStore()
	if err != nil {
		glog.Errorf("Failed to reload market sources from PG: %v", err)
		return err
	}
	if config == nil {
		config = &MarketSourcesConfig{}
	}

	// Merge with default config to ensure default sources are preserved
	config = sm.mergeWithDefaultConfig(config)

	// Set in memory
	sm.mu.Lock()
	sm.marketSources = config
	sm.mu.Unlock()

	glog.V(4).Infof("Successfully reloaded %d market sources from PG", len(config.Sources))
	return nil
}

// initializeAPIEndpoints initializes API endpoints configuration
func (sm *SettingsManager) initializeAPIEndpoints() error {
	// Try to load from Redis first
	config, err := sm.loadAPIEndpointsFromRedis()
	if err != nil {
		glog.Errorf("Failed to load API endpoints from Redis: %v", err)

		// Create default configuration from environment variables
		config = sm.createDefaultAPIEndpoints()

		// Save default config to Redis
		if err := sm.saveAPIEndpointsToRedis(config); err != nil {
			glog.Errorf("Failed to save default API endpoints to Redis: %v", err)
		}

		glog.V(4).Info("Loaded default API endpoints from environment")
	} else {
		glog.V(4).Info("Loaded API endpoints from Redis")
	}

	// Set in memory
	sm.mu.Lock()
	sm.apiEndpoints = config
	sm.mu.Unlock()

	return nil
}

// getMarketServiceURL returns the market service URL from environment variables
// Priority: OLARES_SYSTEM_REMOTE_SERVICE > MARKET_PROVIDER > SYNCER_REMOTE
func getMarketServiceURL() string {
	// First get from k8s CRD watcher cached value and derive {OlaresRemoteService}/market
	if remote := getCachedSystemRemoteService(); remote != "" {
		remote = strings.TrimSuffix(remote, "/")
		baseURL := remote + "/market"
		glog.Infof("Using OLARES_SYSTEM_REMOTE_SERVICE from systemenv watcher: %s -> %s", remote, baseURL)
		return baseURL
	}

	// Then check MARKET_PROVIDER
	baseURL := os.Getenv("MARKET_PROVIDER")
	if baseURL != "" {
		glog.Infof("Using MARKET_PROVIDER from environment: %s", baseURL)
		return baseURL
	}

	// Finally check SYNCER_REMOTE
	baseURL = os.Getenv("SYNCER_REMOTE")
	if baseURL != "" {
		glog.Infof("Using SYNCER_REMOTE from environment: %s", baseURL)
		return baseURL
	}

	glog.Info("No market service URL found in environment variables")
	return ""
}

// createDefaultMarketSources creates Official Market Sources from environment
func (sm *SettingsManager) createDefaultMarketSources() *MarketSourcesConfig {

	baseURL := getMarketServiceURL()

	glog.Infof("Market service base URL: %s", baseURL)

	// Add https:// prefix if baseURL doesn't start with http:// or https://
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
		glog.Infof("Added https:// prefix to baseURL: %s", baseURL)
	}

	if baseURL == "" {
		baseURL = "https://appstore-server-prod.bttcdn.com"
		glog.Infof("SYNCER_REMOTE not set, using default: %s", baseURL)
	}

	// Remove trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")
	glog.Infof("Base URL after trimming: %s", baseURL)

	defaultSource := &MarketSource{
		ID:          "market.olares",
		Name:        "market.olares",
		Type:        "remote",
		BaseURL:     baseURL,
		Priority:    100,
		IsActive:    true,
		UpdatedAt:   time.Now(),
		Description: "Official Market Sources loaded from environment",
	}

	glog.Infof("Created default market source with BaseURL: %s", defaultSource.BaseURL)

	localSource := &MarketSource{
		ID:          "upload",
		Name:        "upload",
		Type:        "local",
		BaseURL:     "file://",
		Priority:    50,
		IsActive:    true,
		UpdatedAt:   time.Now(),
		Description: "Local market source for uploaded apps",
	}

	studioSource := &MarketSource{
		ID:          "studio",
		Name:        "studio",
		Type:        "local",
		BaseURL:     "file://",
		Priority:    50,
		IsActive:    true,
		UpdatedAt:   time.Now(),
		Description: "Local market source for uploaded apps",
	}

	cliSource := &MarketSource{
		ID:          "cli",
		Name:        "cli",
		Type:        "local",
		BaseURL:     "file://",
		Priority:    50,
		IsActive:    true,
		UpdatedAt:   time.Now(),
		Description: "Local market source for uploaded apps",
	}

	return &MarketSourcesConfig{
		Sources:       []*MarketSource{defaultSource, localSource, studioSource, cliSource},
		DefaultSource: "market.olares",
		UpdatedAt:     time.Now(),
	}
}

// createDefaultAPIEndpoints creates default API endpoints from environment
func (sm *SettingsManager) createDefaultAPIEndpoints() *APIEndpointsConfig {
	hashPath := os.Getenv("API_HASH_PATH")
	if hashPath == "" {
		hashPath = "/api/v1/appstore/hash"
	}

	dataPath := os.Getenv("API_DATA_PATH")
	if dataPath == "" {
		dataPath = "/api/v1/appstore/info"
	}

	detailPath := os.Getenv("API_DETAIL_PATH")
	if detailPath == "" {
		detailPath = "/api/v1/applications/info"
	}

	return &APIEndpointsConfig{
		HashPath:   hashPath,
		DataPath:   dataPath,
		DetailPath: detailPath,
		UpdatedAt:  time.Now(),
	}
}

// GetMarketSources gets all market sources
func (sm *SettingsManager) GetMarketSources() *MarketSourcesConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.marketSources == nil {
		return nil
	}

	// Return a deep copy to prevent external modification
	sources := make([]*MarketSource, len(sm.marketSources.Sources))
	for i, src := range sm.marketSources.Sources {
		sources[i] = &MarketSource{
			ID:          src.ID,
			Name:        src.Name,
			Type:        src.Type,
			BaseURL:     src.BaseURL,
			Priority:    src.Priority,
			IsActive:    src.IsActive,
			UpdatedAt:   src.UpdatedAt,
			Description: src.Description,
		}
	}

	return &MarketSourcesConfig{
		Sources:       sources,
		DefaultSource: sm.marketSources.DefaultSource,
		UpdatedAt:     sm.marketSources.UpdatedAt,
	}
}

// GetActiveMarketSources gets all active market sources sorted by priority
func (sm *SettingsManager) GetActiveMarketSources() []*MarketSource {
	config := sm.GetMarketSources()
	if config == nil {
		return nil
	}

	var activeSources []*MarketSource
	for _, src := range config.Sources {
		if src.IsActive {
			activeSources = append(activeSources, src)
		}
	}

	// Sort by priority (descending)
	for i := 0; i < len(activeSources)-1; i++ {
		for j := i + 1; j < len(activeSources); j++ {
			if activeSources[i].Priority < activeSources[j].Priority {
				activeSources[i], activeSources[j] = activeSources[j], activeSources[i]
			}
		}
	}

	return activeSources
}

// GetDefaultMarketSource gets the Official Market Sources
func (sm *SettingsManager) GetDefaultMarketSource() *MarketSource {
	config := sm.GetMarketSources()
	if config == nil {
		return nil
	}

	// Find default source by ID
	for _, source := range config.Sources {
		if source.ID == config.DefaultSource {
			return &MarketSource{
				ID:          source.ID,
				Name:        source.Name,
				Type:        source.Type,
				BaseURL:     source.BaseURL,
				Priority:    source.Priority,
				IsActive:    source.IsActive,
				UpdatedAt:   source.UpdatedAt,
				Description: source.Description,
			}
		}
	}

	// If no default found, return first active source
	for _, source := range config.Sources {
		if source.IsActive {
			return &MarketSource{
				ID:          source.ID,
				Name:        source.Name,
				Type:        source.Type,
				BaseURL:     source.BaseURL,
				Priority:    source.Priority,
				IsActive:    source.IsActive,
				UpdatedAt:   source.UpdatedAt,
				Description: source.Description,
			}
		}
	}

	return nil
}

// GetMarketSource gets market sources from chart repository service (for API compatibility)
func (sm *SettingsManager) GetMarketSource() []*MarketSource {
	// Get chart repository service host from environment variable
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		glog.Info("CHART_REPO_SERVICE_HOST environment variable not set, falling back to default market sources")
		// Return default market sources as fallback
		defaultSource := sm.GetDefaultMarketSource()
		if defaultSource != nil {
			return []*MarketSource{defaultSource}
		}
		return nil
	}

	// Get market sources from chart repository service
	chartRepoConfig, err := getMarketSourceFromChartRepo(chartRepoHost)
	if err != nil {
		glog.Errorf("Failed to get market sources from chart repo: %v, falling back to default market sources", err)
		// Return default market sources as fallback
		defaultSource := sm.GetDefaultMarketSource()
		if defaultSource != nil {
			return []*MarketSource{defaultSource}
		}
		return nil
	}

	// Convert all ChartRepoMarketSource to MarketSource
	var marketSources []*MarketSource
	for _, chartRepoSource := range chartRepoConfig.Sources {
		marketSource := &MarketSource{
			ID:          chartRepoSource.ID,
			Name:        chartRepoSource.Name,
			Type:        chartRepoSource.Type,
			BaseURL:     chartRepoSource.BaseURL,
			Priority:    chartRepoSource.Priority,
			IsActive:    chartRepoSource.IsActive,
			UpdatedAt:   chartRepoSource.UpdatedAt,
			Description: chartRepoSource.Description,
		}
		marketSources = append(marketSources, marketSource)
	}

	glog.Infof("Retrieved %d market sources from chart repository service", len(marketSources))
	return marketSources
}

// DeleteMarketSource removes a market source from the configuration by ID
func (sm *SettingsManager) DeleteMarketSource(sourceID string) error {
	if sourceID == "" {
		return fmt.Errorf("source ID cannot be empty")
	}

	// Check if any user has non-empty state data for this source
	if sm.cacheManager != nil {
		if sm.cacheManager.HasUserStateDataForSource(sourceID) {
			return fmt.Errorf("cannot delete market source '%s': some users have non-empty state data for this source", sourceID)
		}
		glog.Infof("No user state data found for source: %s, proceeding with deletion", sourceID)
	} else {
		glog.Info("Cache manager not available, skipping user state check")
	}

	// First, delete from chart repository service
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		glog.Info("CHART_REPO_SERVICE_HOST environment variable not set, skipping chart repo sync")
	} else {
		if err := deleteMarketSourceFromChartRepo(chartRepoHost, sourceID); err != nil {
			// Only allow continuation for "source not found" error
			if strings.Contains(err.Error(), "source with ID") && strings.Contains(err.Error(), "not found") {
				glog.Infof("Warning: market source not found in chart repo: %v, continuing with local deletion", err)
			} else {
				return fmt.Errorf("failed to delete market source from chart repo: %w", err)
			}
		} else {
			glog.Infof("Successfully deleted market source from chart repo: %s", sourceID)
		}
	}

	// After successful chart repo operation, update local database
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.marketSources == nil {
		return fmt.Errorf("no market sources configured")
	}

	// Find and remove the source
	found := false
	var newSources []*MarketSource
	for _, source := range sm.marketSources.Sources {
		if source.ID != sourceID {
			newSources = append(newSources, source)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("source with ID %s not found", sourceID)
	}

	// Update the sources list
	sm.marketSources.Sources = newSources
	sm.marketSources.UpdatedAt = time.Now()

	// Merge with default config to ensure default sources are preserved
	mergedConfig := sm.mergeWithDefaultConfig(sm.marketSources)

	// Update the in-memory config with merged result
	sm.marketSources = mergedConfig

	// Drop the row in PG. mergeWithDefaultConfig may have re-inserted
	// the same id back into Sources if it is a hardcoded default — in
	// that case the subsequent UpsertSources call re-creates the row,
	// so the order Delete→Upsert matches the user-visible behaviour.
	if !helper.IsPublicEnvironment() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if delErr := marketsource.DeleteSourceByID(ctx, sourceID); delErr != nil {
			cancel()
			return fmt.Errorf("failed to delete market source %s from PG: %w", sourceID, delErr)
		}
		cancel()
	}

	// Save merged config to PG
	if err := sm.saveMarketSourcesToStore(sm.marketSources); err != nil {
		return fmt.Errorf("failed to save market sources to PG: %w", err)
	}

	// Sync to cache
	sm.syncMarketSourcesToCache()

	glog.Infof("Deleted market source: %s", sourceID)
	return nil
}

// GetAPIEndpoints gets the API endpoints configuration
func (sm *SettingsManager) GetAPIEndpoints() *APIEndpointsConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.apiEndpoints == nil {
		return nil
	}

	// Return a copy to prevent external modification
	return &APIEndpointsConfig{
		HashPath:   sm.apiEndpoints.HashPath,
		DataPath:   sm.apiEndpoints.DataPath,
		DetailPath: sm.apiEndpoints.DetailPath,
		UpdatedAt:  sm.apiEndpoints.UpdatedAt,
	}
}

// BuildAPIURL builds a complete API URL from base URL and endpoint path
func (sm *SettingsManager) BuildAPIURL(baseURL, endpointPath string) string {
	glog.V(3).Infof("Building API URL - Base URL: %s, Endpoint Path: %s", baseURL, endpointPath)

	// Add https:// prefix if baseURL doesn't start with http://, https:// or ftp://
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "ftp://") && !strings.HasPrefix(baseURL, "file://") {
		baseURL = "https://" + baseURL
		glog.Infof("Added https:// prefix to baseURL: %s", baseURL)
	}

	baseURL = strings.TrimSuffix(baseURL, "/")
	endpointPath = strings.TrimPrefix(endpointPath, "/")

	finalURL := fmt.Sprintf("%s/%s", baseURL, endpointPath)
	glog.V(3).Infof("Final API URL: %s", finalURL)

	return finalURL
}

// AddMarketSource adds a new market source
func (sm *SettingsManager) AddMarketSource(source *MarketSource) error {
	if source.ID == "" {
		return fmt.Errorf("source ID cannot be empty")
	}
	if source.BaseURL == "" {
		return fmt.Errorf("source base URL cannot be empty")
	}

	// First, add to chart repository service
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		glog.V(3).Info("CHART_REPO_SERVICE_HOST environment variable not set, skipping chart repo sync")
	} else {
		// Convert to ChartRepoMarketSource format
		chartRepoSource := &ChartRepoMarketSource{
			ID:          source.ID,
			Name:        source.Name,
			Type:        source.Type,
			BaseURL:     source.BaseURL,
			Priority:    source.Priority,
			IsActive:    source.IsActive,
			UpdatedAt:   source.UpdatedAt,
			Description: source.Description,
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, chartRepoSource); err != nil {
			return fmt.Errorf("failed to add market source to chart repo: %w", err)
		}
		glog.V(3).Infof("Successfully added market source to chart repo: %s (%s)", source.Name, source.ID)
	}

	// After successful chart repo operation, update local database
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if source with same ID already exists
	for _, existing := range sm.marketSources.Sources {
		if existing.ID == source.ID {
			return fmt.Errorf("source with ID %s already exists", source.ID)
		}
	}

	// Add new source
	source.UpdatedAt = time.Now()
	sm.marketSources.Sources = append(sm.marketSources.Sources, source)
	sm.marketSources.UpdatedAt = time.Now()

	// Merge with default config to ensure default sources are preserved
	mergedConfig := sm.mergeWithDefaultConfig(sm.marketSources)

	// Update the in-memory config with merged result
	sm.marketSources = mergedConfig

	// Save merged config to PG
	if err := sm.saveMarketSourcesToStore(sm.marketSources); err != nil {
		return fmt.Errorf("failed to save market sources to PG: %w", err)
	}

	// Sync to cache
	sm.syncMarketSourcesToCache()

	glog.V(3).Infof("Added new market source: %s (%s)", source.Name, source.ID)
	return nil
}

// UpdateAPIEndpoints updates the API endpoints configuration
func (sm *SettingsManager) UpdateAPIEndpoints(endpoints *APIEndpointsConfig) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	endpoints.UpdatedAt = time.Now()
	sm.apiEndpoints = endpoints

	// Save to Redis
	if err := sm.saveAPIEndpointsToRedis(endpoints); err != nil {
		return fmt.Errorf("failed to save API endpoints to Redis: %w", err)
	}

	glog.V(3).Infof("Updated API endpoints configuration")
	return nil
}

// loadMarketSourcesFromStore loads market sources from PostgreSQL.
//
// Public environment short-circuits to (nil, nil) so the caller falls
// through to createDefaultMarketSources (mirrors the legacy Redis-path
// behaviour where loadMarketSourcesFromRedis returned an error in
// public mode and the caller treated that as "use defaults").
//
// IsActive is not persisted in the `market_sources` schema; rows read
// from PG are surfaced with IsActive=true so the in-memory contract
// stays compatible with the rest of the settings package.
func (sm *SettingsManager) loadMarketSourcesFromStore() (*MarketSourcesConfig, error) {
	if helper.IsPublicEnvironment() {
		return nil, nil
	}

	rows, err := marketsource.GetMarketSources()
	if err != nil {
		return nil, fmt.Errorf("failed to get market sources from PG: %w", err)
	}

	sources := make([]*MarketSource, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		sources = append(sources, modelToSetting(row))
	}

	return &MarketSourcesConfig{
		Sources:   sources,
		UpdatedAt: time.Now(),
	}, nil
}

// saveMarketSourcesToStore persists market sources to PostgreSQL via an
// upsert keyed by source_id. The `data` JSONB column is intentionally
// not part of the upsert payload (see UpsertSources comment) so this
// path never clobbers a freshly-rendered catalogue snapshot.
//
// Public environment is a silent no-op (mirrors the legacy
// saveMarketSourcesToRedis behaviour).
func (sm *SettingsManager) saveMarketSourcesToStore(config *MarketSourcesConfig) error {
	if helper.IsPublicEnvironment() {
		return nil
	}
	if config == nil {
		return nil
	}

	rows := make([]*models.MarketSource, 0, len(config.Sources))
	for _, src := range config.Sources {
		if src == nil {
			continue
		}
		rows = append(rows, settingToModel(src))
	}
	if len(rows) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return marketsource.UpsertSources(ctx, rows)
}

// modelToSetting maps the persisted PG row shape onto the settings
// package's in-memory MarketSource. IsActive is not persisted in the
// schema, so it defaults to true on read (the only call sites that
// flip it to false are debug paths and tests).
func modelToSetting(m *models.MarketSource) *MarketSource {
	if m == nil {
		return nil
	}
	return &MarketSource{
		ID:          m.SourceID,
		Name:        m.SourceTitle,
		Type:        m.SourceType,
		BaseURL:     m.SourceURL,
		Priority:    m.Priority,
		IsActive:    true,
		UpdatedAt:   m.UpdatedAt,
		Description: m.Description,
	}
}

// settingToModel maps the in-memory MarketSource onto the persisted
// PG row shape. The Data JSONB column is left nil — UpsertSources
// excludes it from DoUpdates, so existing payloads are preserved on
// upsert and new rows are simply created with NULL data (the syncer
// fills it in on its next cycle).
func settingToModel(s *MarketSource) *models.MarketSource {
	if s == nil {
		return nil
	}
	return &models.MarketSource{
		SourceID:    s.ID,
		SourceTitle: s.Name,
		SourceURL:   s.BaseURL,
		SourceType:  s.Type,
		Description: s.Description,
		Priority:    s.Priority,
	}
}

// loadAPIEndpointsFromRedis loads API endpoints from Redis
func (sm *SettingsManager) loadAPIEndpointsFromRedis() (*APIEndpointsConfig, error) {

	if helper.IsPublicEnvironment() {
		return nil, fmt.Errorf("in public environment, no need to load API endpoints from Redis")
	}

	data, err := sm.redisClient.Get(RedisKeyAPIEndpoints)
	if err != nil {
		return nil, fmt.Errorf("failed to get API endpoints from Redis: %w", err)
	}

	if data == "" {
		return nil, fmt.Errorf("no API endpoints config found in Redis")
	}

	var config APIEndpointsConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal API endpoints config: %w", err)
	}

	return &config, nil
}

// saveAPIEndpointsToRedis saves API endpoints to Redis
func (sm *SettingsManager) saveAPIEndpointsToRedis(config *APIEndpointsConfig) error {

	if helper.IsPublicEnvironment() {
		return nil
	}

	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal API endpoints config: %w", err)
	}

	return sm.redisClient.Set(RedisKeyAPIEndpoints, string(data), 0)
}

// mergeWithDefaultConfig merges a default configuration with an existing configuration
func (sm *SettingsManager) mergeWithDefaultConfig(config *MarketSourcesConfig) *MarketSourcesConfig {
	defaultConfig := sm.createDefaultMarketSources()

	glog.Info("Merging default configuration with existing Redis configuration")
	glog.Infof("Existing sources: %d, Default sources: %d", len(config.Sources), len(defaultConfig.Sources))

	// Create a map of existing source IDs for quick lookup
	existingSourceIDs := make(map[string]bool)
	var filteredSources []*MarketSource

	// Filter out "Official-Market-Sources" entries and build existing source map
	for _, existingSource := range config.Sources {
		if existingSource.ID == "Official-Market-Sources" {
			glog.Infof("Removing deprecated source: %s (%s)", existingSource.Name, existingSource.ID)
			continue
		}
		existingSourceIDs[existingSource.ID] = true
		filteredSources = append(filteredSources, existingSource)
		glog.Infof("Existing source: %s (%s)", existingSource.Name, existingSource.ID)
	}

	// Update config sources with filtered list
	config.Sources = filteredSources

	// Add default sources that don't exist in the current configuration
	addedSources := 0
	for _, defaultSource := range defaultConfig.Sources {
		if !existingSourceIDs[defaultSource.ID] {
			glog.Infof("Adding new default source: %s (%s)", defaultSource.Name, defaultSource.ID)
			config.Sources = append(config.Sources, defaultSource)
			addedSources++
		} else {
			glog.Infof("Default source already exists: %s (%s)", defaultSource.Name, defaultSource.ID)
		}
	}

	if addedSources > 0 {
		glog.Infof("Added %d new sources to existing configuration", addedSources)
		config.UpdatedAt = time.Now()
	} else {
		glog.Info("No new sources to add, existing configuration is up to date")
	}

	// Ensure default source is set
	if config.DefaultSource == "" {
		glog.Infof("Setting default source to: %s", defaultConfig.DefaultSource)
		config.DefaultSource = defaultConfig.DefaultSource
		config.UpdatedAt = time.Now()
	}

	glog.Infof("Final configuration has %d sources", len(config.Sources))
	return config
}

// ClearSettingsRedis is the CLEAR_CACHE=true wipe path. Source state is
// now in PostgreSQL, so the source-list portion runs against the
// `market_sources` table; the API endpoints config still lives in Redis
// and is wiped there. The function name is kept for callsite stability.
//
// PG truncate is more destructive than the previous Redis-only clear:
// the JSONB `data` column lives on the same row, so the syncer has to
// refetch every catalogue payload after the next startup. That matches
// the spirit of CLEAR_CACHE (force a full reseed) but operators should
// be aware of the larger blast radius.
func ClearSettingsRedis(redisClient RedisClient) error {
	if !helper.IsPublicEnvironment() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := marketsource.TruncateAll(ctx); err != nil {
			cancel()
			return fmt.Errorf("failed to truncate market_sources in PG: %w", err)
		}
		cancel()
	}

	if redisClient != nil {
		if err := redisClient.Del(RedisKeyAPIEndpoints); err != nil {
			return fmt.Errorf("failed to delete API endpoints from Redis: %w", err)
		}
	}
	glog.Info("Settings store cleared successfully (market_sources truncated; API endpoints removed from Redis)")
	return nil
}

// GetMarketSettings gets market settings from Redis for a specific user
func (sm *SettingsManager) GetMarketSettings(userID string) (*MarketSettings, error) {
	return getMarketSettings(sm.redisClient, userID)
}

// UpdateMarketSettings updates market settings in Redis for a specific user
func (sm *SettingsManager) UpdateMarketSettings(userID string, settings *MarketSettings) error {
	return updateMarketSettings(sm.redisClient, userID, settings)
}
