package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// GetAppInfoFromDownloadRecord fetches app version and source from chart-repo service
func GetAppInfoFromDownloadRecord(userID, appName string) (string, string, error) {

	// Get chart repo service host from env, fallback to default
	host := os.Getenv("CHART_REPO_SERVICE_HOST")
	if host == "" {
		return "", "", fmt.Errorf("CHART_REPO_SERVICE_HOST env not set")
	}
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/app/version-for-download-history?user=%s&app_name=%s", host, userID, appName)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("failed to request chart-repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("chart-repo returned status: %d", resp.StatusCode)
	}

	var result struct {
		Success bool        `json:"success"`
		Message string      `json:"message"`
		Data    interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return "", "", fmt.Errorf("chart-repo error: %s", result.Message)
	}

	// parse data field
	dataMap, ok := result.Data.(map[string]interface{})
	if !ok {

		// if data is map[string]string, return version and source
		if dataStrMap, ok2 := result.Data.(map[string]string); ok2 {
			return dataStrMap["version"], dataStrMap["source"], nil
		}
		return "", "", fmt.Errorf("invalid data format in response")
	}

	version, _ := dataMap["version"].(string)
	source, _ := dataMap["source"].(string)
	if version == "" || source == "" {
		return "", "", fmt.Errorf("version or source not found in response")
	}

	if source == "market-local" {
		source = "local"
	}

	return version, source, nil
}
