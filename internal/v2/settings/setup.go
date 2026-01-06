package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang/glog"
)

// ChartRepoMarketSource represents the market source structure used by chart repository service
type ChartRepoMarketSource struct {
	ID          string    `json:"id"`          // Unique identifier for the source
	Name        string    `json:"name"`        // Display name of the source
	Type        string    `json:"type"`        // Type of the source (local, remote)
	BaseURL     string    `json:"base_url"`    // Base URL of the market source
	Priority    int       `json:"priority"`    // Priority for selection (higher = more priority)
	IsActive    bool      `json:"is_active"`   // Whether this source is active
	UpdatedAt   time.Time `json:"updated_at"`  // Last update time
	Description string    `json:"description"` // Description of the source
}

// ChartRepoMarketSourcesConfig represents the market sources configuration from chart repository
type ChartRepoMarketSourcesConfig struct {
	Sources       []*ChartRepoMarketSource `json:"sources"`
	DefaultSource string                   `json:"default_source"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// ChartRepoResponse represents the standard response from chart repository service
type ChartRepoResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// SyncMarketSourceConfigWithChartRepo synchronizes market source configuration with chart repository service
// This function is called after AppInfo module initialization to ensure chart repo service
// has the latest market source configuration
// If settingsManager is provided, it will reload market sources from Redis after syncing
func SyncMarketSourceConfigWithChartRepo(redisClient RedisClient, settingsManager ...*SettingsManager) error {
	glog.V(2).Info("=== Syncing market source configuration with chart repository service ===")

	clearCache := os.Getenv("CLEAR_CACHE")
	if clearCache == "true" {
		glog.V(2).Info("CLEAR_CACHE is true, clearing settings Redis keys...")
		// Only clear settings Redis keys for this module
		if err := ClearSettingsRedis(redisClient); err != nil {
			glog.Errorf("Failed to clear settings Redis: %v", err)
			return fmt.Errorf("failed to clear settings Redis: %w", err)
		}
	}

	// 0. Start systemenv watcher in background (best-effort). Already started in main; keep here as no-op if already running.
	ctx := context.Background()
	StartSystemEnvWatcher(ctx)

	// 1. Get chart repository service host from environment variable
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		glog.V(3).Info("CHART_REPO_SERVICE_HOST environment variable not set, skipping sync")
		return nil
	}

	glog.V(2).Infof("Chart repository service host: %s", chartRepoHost)

	// 2. Get current market source configuration from chart repository service
	log.Println("Step 1: Getting market source configuration from chart repository service")
	currentConfig, err := getMarketSourceFromChartRepo(chartRepoHost)
	if err != nil {
		glog.Errorf("Failed to get market source configuration: %v", err)
		return fmt.Errorf("failed to get market source configuration: %w", err)
	}

	glog.V(3).Infof("Retrieved %d market sources from chart repository service", len(currentConfig.Sources))

	// 3. Check if local source (type=local, name=upload) exists
	glog.V(2).Info("Step 2: Checking for local source (type=local, name=upload)")
	localSourceExists := false
	studioSourceExists := false
	cliSourceExists := false
	for _, source := range currentConfig.Sources {
		if source.Type == "local" && source.ID == "upload" {
			localSourceExists = true
			glog.V(2).Infof("Local source found: %s (ID: %s)", source.Name, source.ID)
			break
		}
		if source.Type == "local" && source.ID == "studio" {
			studioSourceExists = true
			glog.V(2).Infof("Studio source found: %s (ID: %s)", source.Name, source.ID)
			break
		}
		if source.Type == "local" && source.ID == "cli" {
			cliSourceExists = true
			glog.V(2).Infof("CLI source found: %s (ID: %s)", source.Name, source.ID)
			break
		}
	}

	// 4. Add local sources if they don't exist
	// Track errors but don't fail the entire process for individual source failures
	var syncErrors []string

	if !localSourceExists {
		glog.V(2).Info("Step 3: Adding upload source (type=local, name=upload)")
		localSource := &ChartRepoMarketSource{
			ID:          "upload",
			Name:        "upload",
			Type:        "local",
			BaseURL:     "file://",
			Priority:    50,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Local market source for uploaded apps",
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, localSource); err != nil {
			glog.Errorf("Failed to add upload source: %v", err)
			syncErrors = append(syncErrors, fmt.Sprintf("upload source: %v", err))
		} else {
			glog.V(3).Info("upload source added successfully")
		}
	} else {
		glog.V(3).Info("upload source already exists, skipping")
	}

	if !studioSourceExists {
		glog.V(3).Info("Step 3: Adding studio source (type=local, name=studio)")
		studioSource := &ChartRepoMarketSource{
			ID:          "studio",
			Name:        "studio",
			Type:        "local",
			BaseURL:     "file://",
			Priority:    50,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Local market source for studio",
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, studioSource); err != nil {
			glog.Errorf("Failed to add studio source: %v", err)
			syncErrors = append(syncErrors, fmt.Sprintf("studio source: %v", err))
		} else {
			glog.V(2).Info("studio source added successfully")
		}
	} else {
		glog.V(3).Info("studio source already exists, skipping")
	}

	if !cliSourceExists {
		glog.V(3).Info("Step 3: Adding cli source (type=local, name=cli)")
		cliSource := &ChartRepoMarketSource{
			ID:          "cli",
			Name:        "cli",
			Type:        "local",
			BaseURL:     "file://",
			Priority:    50,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Local market source for cli",
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, cliSource); err != nil {
			glog.Errorf("Failed to add cli source: %v", err)
			syncErrors = append(syncErrors, fmt.Sprintf("cli source: %v", err))
		} else {
			glog.V(2).Info("cli source added successfully")
		}
	} else {
		glog.V(3).Info("cli source already exists, skipping")
	}

	// 5. Check if remote source (type=remote, name=market.olares) exists
	glog.V(2).Info("Step 4: Checking for remote source (type=remote, name=market.olares)")
	remoteSourceExists := false
	for _, source := range currentConfig.Sources {
		if source.Type == "remote" && source.ID == "market.olares" {
			remoteSourceExists = true
			glog.V(2).Infof("Remote source found: %s (ID: %s)", source.Name, source.ID)
			break
		}
	}

	// 6. Add remote source if it doesn't exist
	if !remoteSourceExists {
		glog.V(2).Info("Step 5: Adding remote source (type=remote, name=market.olares)")
		// Get base URL from environment variables
		baseURL := getMarketServiceURL()
		if baseURL == "" {
			glog.V(3).Info("No market service base URL found in environment variables for remote market.olares. Please check OLARES_SYSTEM_REMOTE_SERVICE, MARKET_PROVIDER, or SYNCER_REMOTE.")
			return fmt.Errorf("sync market sources: missing remote market base URL in environment")
		}

		remoteSource := &ChartRepoMarketSource{
			ID:          "market.olares",
			Name:        "market.olares",
			Type:        "remote",
			BaseURL:     baseURL,
			Priority:    100,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Official Market Sources",
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, remoteSource); err != nil {
			glog.Errorf("Failed to add remote source: %v", err)
			syncErrors = append(syncErrors, fmt.Sprintf("remote source: %v", err))
		} else {
			glog.V(2).Info("Remote source added successfully")
		}
	} else {
		glog.V(3).Info("Remote source already exists, skipping")
	}

	// 7. Get final configuration from chart repo (after all additions) and save to Redis
	glog.V(2).Info("Step 6: Saving final market source configuration to Redis")
	finalConfig, err := getMarketSourceFromChartRepo(chartRepoHost)
	if err != nil {
		glog.Errorf("Failed to get final market source configuration: %v", err)
		// Continue even if this fails - we've already synced the sources
	} else {
		// Convert ChartRepoMarketSourcesConfig to MarketSourcesConfig
		var marketSources []*MarketSource
		for _, chartRepoSource := range finalConfig.Sources {
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

		marketSourcesConfig := &MarketSourcesConfig{
			Sources:       marketSources,
			DefaultSource: finalConfig.DefaultSource,
			UpdatedAt:     time.Now(),
		}

		// Save to Redis
		configJSON, err := json.Marshal(marketSourcesConfig)
		if err != nil {
			glog.Errorf("Failed to marshal market sources config: %v", err)
		} else {
			if err := redisClient.Set(RedisKeyMarketSources, string(configJSON), 0); err != nil {
				glog.Errorf("Failed to save market sources to Redis: %v", err)
			} else {
				glog.V(2).Infof("Successfully saved %d market sources to Redis", len(marketSources))
			}
		}

		// Reload SettingsManager if provided
		if len(settingsManager) > 0 && settingsManager[0] != nil {
			glog.V(2).Info("Step 7: Reloading SettingsManager with updated market sources")
			if err := settingsManager[0].ReloadMarketSources(); err != nil {
				glog.Errorf("Failed to reload SettingsManager: %v", err)
				// Don't fail the entire sync if reload fails
			} else {
				glog.V(3).Info("Successfully reloaded SettingsManager with updated market sources")
			}
		}
	}

	// 8. Report sync results
	if len(syncErrors) > 0 {
		glog.V(3).Infof("Market source configuration sync completed with %d errors:", len(syncErrors))
		for i, err := range syncErrors {
			glog.Errorf("  Error %d: %s", i+1, err)
		}
		// Don't return error, just log warnings - individual source failures shouldn't stop the entire process
		glog.V(3).Info("Continuing with available sources...")
	} else {
		glog.V(2).Info("Market source configuration sync completed successfully")
	}

	return nil
}

// getMarketSourceFromChartRepo retrieves market source configuration from chart repository service
func getMarketSourceFromChartRepo(host string) (*ChartRepoMarketSourcesConfig, error) {
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/settings/market-source", host)

	glog.V(2).Infof("Making GET request to: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	glog.V(2).Infof("Response status: %d", resp.StatusCode)
	glog.V(2).Infof("Response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response ChartRepoResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API request failed: %s", response.Message)
	}

	// Convert response data to MarketSourcesConfig
	dataBytes, err := json.Marshal(response.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var config ChartRepoMarketSourcesConfig
	if err := json.Unmarshal(dataBytes, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal market sources config: %w", err)
	}

	return &config, nil
}

// addMarketSourceToChartRepo adds a new market source to chart repository service
func addMarketSourceToChartRepo(host string, source *ChartRepoMarketSource) error {
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/settings/market-source", host)

	glog.V(2).Infof("Making POST request to: %s", url)
	glog.V(2).Infof("Adding market source: %s (Type: %s, Name: %s)", source.ID, source.Type, source.Name)

	// Marshal the source to JSON
	jsonData, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("failed to marshal market source: %w", err)
	}

	glog.V(2).Infof("Request body: %s", string(jsonData))

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	glog.V(3).Infof("Response status: %d", resp.StatusCode)
	glog.V(3).Infof("Response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response ChartRepoResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("API request failed: %s", response.Message)
	}

	return nil
}

// deleteMarketSourceFromChartRepo deletes a market source from chart repository service
func deleteMarketSourceFromChartRepo(host string, sourceID string) error {
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/settings/market-source/%s", host, sourceID)

	glog.V(2).Infof("Making DELETE request to: %s", url)
	glog.V(2).Infof("Deleting market source with ID: %s", sourceID)

	// Create HTTP request
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	glog.V(2).Infof("Response status: %d", resp.StatusCode)
	glog.V(2).Infof("Response body: %s", string(body))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response ChartRepoResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("API request failed: %s", response.Message)
	}

	return nil
}
