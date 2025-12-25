package settings

import (
	"encoding/json"
	"fmt"
	"log"
	"market/internal/v2/utils"
	"os"
	"strings"
	"time"
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
	log.Println("Cache manager set for settings manager")
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
			log.Printf("Failed to sync market sources to cache: %v", err)
		} else {
			log.Printf("Successfully synced %d market sources to cache", len(sm.marketSources.Sources))
		}
	}
}

// Initialize initializes the settings manager
func (sm *SettingsManager) Initialize() error {
	log.Println("Initializing settings manager...")

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

// initializeMarketSources initializes market sources configuration
func (sm *SettingsManager) initializeMarketSources() error {
	// Try to load from Redis first
	config, err := sm.loadMarketSourcesFromRedis()
	if err != nil {
		log.Printf("Failed to load market sources from Redis: %v", err)

		// Create default configuration from environment variables
		config = sm.createDefaultMarketSources()

		// Save default config to Redis
		if err := sm.saveMarketSourcesToRedis(config); err != nil {
			log.Printf("Failed to save Official Market Sources to Redis: %v", err)
		}

		log.Printf("Loaded Official Market Sources from environment")
	} else {
		log.Printf("Loaded market sources from Redis: %d sources", len(config.Sources))

		// Merge default configuration with existing Redis configuration
		// This ensures new default sources (like "local") are added to existing config
		config = sm.mergeWithDefaultConfig(config)

		// Save merged config back to Redis if there were changes
		if err := sm.saveMarketSourcesToRedis(config); err != nil {
			log.Printf("Failed to save merged market sources to Redis: %v", err)
		}
	}

	// Set in memory
	sm.mu.Lock()
	sm.marketSources = config
	sm.mu.Unlock()

	return nil
}

// ReloadMarketSources reloads market sources from Redis into memory
// This is useful after syncing sources from chartrepo
func (sm *SettingsManager) ReloadMarketSources() error {
	log.Println("Reloading market sources from Redis...")
	config, err := sm.loadMarketSourcesFromRedis()
	if err != nil {
		log.Printf("Failed to reload market sources from Redis: %v", err)
		return err
	}

	// Merge with default config to ensure default sources are preserved
	config = sm.mergeWithDefaultConfig(config)

	// Set in memory
	sm.mu.Lock()
	sm.marketSources = config
	sm.mu.Unlock()

	log.Printf("Successfully reloaded %d market sources from Redis", len(config.Sources))
	return nil
}

// initializeAPIEndpoints initializes API endpoints configuration
func (sm *SettingsManager) initializeAPIEndpoints() error {
	// Try to load from Redis first
	config, err := sm.loadAPIEndpointsFromRedis()
	if err != nil {
		log.Printf("Failed to load API endpoints from Redis: %v", err)

		// Create default configuration from environment variables
		config = sm.createDefaultAPIEndpoints()

		// Save default config to Redis
		if err := sm.saveAPIEndpointsToRedis(config); err != nil {
			log.Printf("Failed to save default API endpoints to Redis: %v", err)
		}

		log.Printf("Loaded default API endpoints from environment")
	} else {
		log.Printf("Loaded API endpoints from Redis")
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
		log.Printf("Using OLARES_SYSTEM_REMOTE_SERVICE from systemenv watcher: %s -> %s", remote, baseURL)
		return baseURL
	}

	// Then check MARKET_PROVIDER
	baseURL := os.Getenv("MARKET_PROVIDER")
	if baseURL != "" {
		log.Printf("Using MARKET_PROVIDER from environment: %s", baseURL)
		return baseURL
	}

	// Finally check SYNCER_REMOTE
	baseURL = os.Getenv("SYNCER_REMOTE")
	if baseURL != "" {
		log.Printf("Using SYNCER_REMOTE from environment: %s", baseURL)
		return baseURL
	}

	log.Printf("No market service URL found in environment variables")
	return ""
}

// createDefaultMarketSources creates Official Market Sources from environment
func (sm *SettingsManager) createDefaultMarketSources() *MarketSourcesConfig {

	baseURL := getMarketServiceURL()

	log.Printf("Market service base URL: %s", baseURL)

	// Add https:// prefix if baseURL doesn't start with http:// or https://
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
		log.Printf("Added https:// prefix to baseURL: %s", baseURL)
	}

	if baseURL == "" {
		baseURL = "https://appstore-server-prod.bttcdn.com"
		log.Printf("SYNCER_REMOTE not set, using default: %s", baseURL)
	}

	// Remove trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")
	log.Printf("Base URL after trimming: %s", baseURL)

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

	log.Printf("Created default market source with BaseURL: %s", defaultSource.BaseURL)

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
		log.Println("CHART_REPO_SERVICE_HOST environment variable not set, falling back to default market sources")
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
		log.Printf("Failed to get market sources from chart repo: %v, falling back to default market sources", err)
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

	log.Printf("Retrieved %d market sources from chart repository service", len(marketSources))
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
		log.Printf("No user state data found for source: %s, proceeding with deletion", sourceID)
	} else {
		log.Println("Cache manager not available, skipping user state check")
	}

	// First, delete from chart repository service
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		log.Println("CHART_REPO_SERVICE_HOST environment variable not set, skipping chart repo sync")
	} else {
		if err := deleteMarketSourceFromChartRepo(chartRepoHost, sourceID); err != nil {
			// Only allow continuation for "source not found" error
			if strings.Contains(err.Error(), "source with ID") && strings.Contains(err.Error(), "not found") {
				log.Printf("Warning: market source not found in chart repo: %v, continuing with local deletion", err)
			} else {
				return fmt.Errorf("failed to delete market source from chart repo: %w", err)
			}
		} else {
			log.Printf("Successfully deleted market source from chart repo: %s", sourceID)
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

	// Save merged config to Redis
	if err := sm.saveMarketSourcesToRedis(sm.marketSources); err != nil {
		return fmt.Errorf("failed to save market sources to Redis: %w", err)
	}

	// Sync to cache
	sm.syncMarketSourcesToCache()

	log.Printf("Deleted market source: %s", sourceID)
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
	log.Printf("Building API URL - Base URL: %s, Endpoint Path: %s", baseURL, endpointPath)

	// Add https:// prefix if baseURL doesn't start with http://, https:// or ftp://
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "ftp://") && !strings.HasPrefix(baseURL, "file://") {
		baseURL = "https://" + baseURL
		log.Printf("Added https:// prefix to baseURL: %s", baseURL)
	}

	baseURL = strings.TrimSuffix(baseURL, "/")
	endpointPath = strings.TrimPrefix(endpointPath, "/")

	finalURL := fmt.Sprintf("%s/%s", baseURL, endpointPath)
	log.Printf("Final API URL: %s", finalURL)

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
		log.Println("CHART_REPO_SERVICE_HOST environment variable not set, skipping chart repo sync")
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
		log.Printf("Successfully added market source to chart repo: %s (%s)", source.Name, source.ID)
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

	// Save merged config to Redis
	if err := sm.saveMarketSourcesToRedis(sm.marketSources); err != nil {
		return fmt.Errorf("failed to save market sources to Redis: %w", err)
	}

	// Sync to cache
	sm.syncMarketSourcesToCache()

	log.Printf("Added new market source: %s (%s)", source.Name, source.ID)
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

	log.Printf("Updated API endpoints configuration")
	return nil
}

// loadMarketSourcesFromRedis loads market sources from Redis
func (sm *SettingsManager) loadMarketSourcesFromRedis() (*MarketSourcesConfig, error) {

	if utils.IsPublicEnvironment() {
		return nil, fmt.Errorf("in public environment, no need to load market sources from Redis")
	}

	data, err := sm.redisClient.Get(RedisKeyMarketSources)
	if err != nil {
		return nil, fmt.Errorf("failed to get market sources from Redis: %w", err)
	}

	if data == "" {
		return nil, fmt.Errorf("no market sources config found in Redis")
	}

	var config MarketSourcesConfig
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	return &config, nil
}

// saveMarketSourcesToRedis saves market sources to Redis
func (sm *SettingsManager) saveMarketSourcesToRedis(config *MarketSourcesConfig) error {

	if utils.IsPublicEnvironment() {
		return nil
	}

	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal market sources config: %w", err)
	}

	return sm.redisClient.Set(RedisKeyMarketSources, string(data), 0)
}

// loadAPIEndpointsFromRedis loads API endpoints from Redis
func (sm *SettingsManager) loadAPIEndpointsFromRedis() (*APIEndpointsConfig, error) {

	if utils.IsPublicEnvironment() {
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

	if utils.IsPublicEnvironment() {
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

	log.Printf("Merging default configuration with existing Redis configuration")
	log.Printf("Existing sources: %d, Default sources: %d", len(config.Sources), len(defaultConfig.Sources))

	// Create a map of existing source IDs for quick lookup
	existingSourceIDs := make(map[string]bool)
	var filteredSources []*MarketSource

	// Filter out "Official-Market-Sources" entries and build existing source map
	for _, existingSource := range config.Sources {
		if existingSource.ID == "Official-Market-Sources" {
			log.Printf("Removing deprecated source: %s (%s)", existingSource.Name, existingSource.ID)
			continue
		}
		existingSourceIDs[existingSource.ID] = true
		filteredSources = append(filteredSources, existingSource)
		log.Printf("Existing source: %s (%s)", existingSource.Name, existingSource.ID)
	}

	// Update config sources with filtered list
	config.Sources = filteredSources

	// Add default sources that don't exist in the current configuration
	addedSources := 0
	for _, defaultSource := range defaultConfig.Sources {
		if !existingSourceIDs[defaultSource.ID] {
			log.Printf("Adding new default source: %s (%s)", defaultSource.Name, defaultSource.ID)
			config.Sources = append(config.Sources, defaultSource)
			addedSources++
		} else {
			log.Printf("Default source already exists: %s (%s)", defaultSource.Name, defaultSource.ID)
		}
	}

	if addedSources > 0 {
		log.Printf("Added %d new sources to existing configuration", addedSources)
		config.UpdatedAt = time.Now()
	} else {
		log.Printf("No new sources to add, existing configuration is up to date")
	}

	// Ensure default source is set
	if config.DefaultSource == "" {
		log.Printf("Setting default source to: %s", defaultConfig.DefaultSource)
		config.DefaultSource = defaultConfig.DefaultSource
		config.UpdatedAt = time.Now()
	}

	log.Printf("Final configuration has %d sources", len(config.Sources))
	return config
}

func ClearSettingsRedis(redisClient RedisClient) error {

	if err := redisClient.Del(RedisKeyMarketSources); err != nil {
		return fmt.Errorf("failed to delete market sources from Redis: %w", err)
	}

	if err := redisClient.Del(RedisKeyAPIEndpoints); err != nil {
		return fmt.Errorf("failed to delete API endpoints from Redis: %w", err)
	}
	log.Println("Settings Redis keys cleared successfully")
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
