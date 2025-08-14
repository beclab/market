package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
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
func SyncMarketSourceConfigWithChartRepo(redisClient RedisClient) error {
	log.Println("=== Syncing market source configuration with chart repository service ===")

	clearCache := os.Getenv("CLEAR_CACHE")
	if clearCache == "true" {
		log.Println("CLEAR_CACHE is true, clearing settings Redis keys...")
		// Only clear settings Redis keys for this module
		if err := ClearSettingsRedis(redisClient); err != nil {
			log.Printf("Failed to clear settings Redis: %v", err)
			return fmt.Errorf("failed to clear settings Redis: %w", err)
		}
	}

	// 1. Get chart repository service host from environment variable
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		log.Println("CHART_REPO_SERVICE_HOST environment variable not set, skipping sync")
		return nil
	}

	log.Printf("Chart repository service host: %s", chartRepoHost)

	// 2. Get current market source configuration from chart repository service
	log.Println("Step 1: Getting market source configuration from chart repository service")
	currentConfig, err := getMarketSourceFromChartRepo(chartRepoHost)
	if err != nil {
		log.Printf("Failed to get market source configuration: %v", err)
		return fmt.Errorf("failed to get market source configuration: %w", err)
	}

	log.Printf("Retrieved %d market sources from chart repository service", len(currentConfig.Sources))

	// 3. Check if local source (type=local, name=market-local) exists
	log.Println("Step 2: Checking for local source (type=local, name=market-local)")
	localSourceExists := false
	for _, source := range currentConfig.Sources {
		if source.Type == "local" && source.Name == "market-local" {
			localSourceExists = true
			log.Printf("Local source found: %s (ID: %s)", source.Name, source.ID)
			break
		}
	}

	// 4. Add local source if it doesn't exist
	if !localSourceExists {
		log.Println("Step 3: Adding local source (type=local, name=market-local)")
		localSource := &ChartRepoMarketSource{
			ID:          "market-local",
			Name:        "market-local",
			Type:        "local",
			BaseURL:     "file://",
			Priority:    50,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Local market source for uploaded apps",
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, localSource); err != nil {
			log.Printf("Failed to add local source: %v", err)
			return fmt.Errorf("failed to add local source: %w", err)
		}
		log.Println("Local source added successfully")
	} else {
		log.Println("Local source already exists, skipping")
	}

	// 5. Check if remote source (type=remote, name=Official-Market-Sources) exists
	log.Println("Step 4: Checking for remote source (type=remote, name=Official-Market-Sources)")
	remoteSourceExists := false
	for _, source := range currentConfig.Sources {
		if source.Type == "remote" && source.Name == "Official-Market-Sources" {
			remoteSourceExists = true
			log.Printf("Remote source found: %s (ID: %s)", source.Name, source.ID)
			break
		}
	}

	// 6. Add remote source if it doesn't exist
	if !remoteSourceExists {
		log.Println("Step 5: Adding remote source (type=remote, name=Official-Market-Sources)")
		// Get base URL from environment variables
		baseURL := os.Getenv("MARKET_PROVIDER")
		if baseURL == "" {
			baseURL = os.Getenv("SYNCER_REMOTE")
		}
		if baseURL == "" {
			baseURL = "https://appstore-server-prod.bttcdn.com"
		}

		remoteSource := &ChartRepoMarketSource{
			ID:          "Official-Market-Sources",
			Name:        "Official-Market-Sources",
			Type:        "remote",
			BaseURL:     baseURL,
			Priority:    100,
			IsActive:    true,
			UpdatedAt:   time.Now(),
			Description: "Official Market Sources",
		}

		if err := addMarketSourceToChartRepo(chartRepoHost, remoteSource); err != nil {
			log.Printf("Failed to add remote source: %v", err)
			return fmt.Errorf("failed to add remote source: %w", err)
		}
		log.Println("Remote source added successfully")
	} else {
		log.Println("Remote source already exists, skipping")
	}

	log.Println("Market source configuration sync completed successfully")
	return nil
}

// getMarketSourceFromChartRepo retrieves market source configuration from chart repository service
func getMarketSourceFromChartRepo(host string) (*ChartRepoMarketSourcesConfig, error) {
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/settings/market-source", host)

	log.Printf("Making GET request to: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("Response status: %d", resp.StatusCode)
	log.Printf("Response body: %s", string(body))

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

	log.Printf("Making POST request to: %s", url)
	log.Printf("Adding market source: %s (Type: %s, Name: %s)", source.ID, source.Type, source.Name)

	// Marshal the source to JSON
	jsonData, err := json.Marshal(source)
	if err != nil {
		return fmt.Errorf("failed to marshal market source: %w", err)
	}

	log.Printf("Request body: %s", string(jsonData))

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

	log.Printf("Response status: %d", resp.StatusCode)
	log.Printf("Response body: %s", string(body))

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

	log.Printf("Making DELETE request to: %s", url)
	log.Printf("Deleting market source with ID: %s", sourceID)

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

	log.Printf("Response status: %d", resp.StatusCode)
	log.Printf("Response body: %s", string(body))

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
