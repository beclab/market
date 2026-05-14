package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"market/internal/v2/helper"

	"github.com/golang/glog"
)

// VersionInfo represents version information response
type VersionInfo struct {
	Version string `json:"version"`
}

// GetTerminusVersionValue returns the parsed version string
func GetTerminusVersionValue() (string, error) {
	// Check public environment first
	if helper.IsPublicEnvironment() {
		version := os.Getenv("PUBLIC_VERSION")
		if version != "" {
			return version, nil
		}
	}

	// Get version from service
	versionResponse, err := getTerminusVersionFromService()
	if err != nil {
		glog.Errorf("Failed to get version from service: %v", err)
		return "", fmt.Errorf("failed to get version from service: %w", err)
	}

	glog.Infof("Version response: %s", versionResponse)

	var versionInfo VersionInfo
	err = json.Unmarshal([]byte(versionResponse), &versionInfo)
	if err != nil {
		glog.Errorf("Failed to parse version JSON: %v", err)
		return "", fmt.Errorf("failed to parse version response: %w", err)
	}

	return versionInfo.Version, nil
}

// getTerminusVersionFromService retrieves version from the app service
func getTerminusVersionFromService() (string, error) {
	// Get app service host and port from environment
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if appServiceHost == "" || appServicePort == "" {
		return "", fmt.Errorf("app service host or port not configured")
	}

	// Build version endpoint URL
	url := fmt.Sprintf("http://%s:%s/app-service/v1/terminus/version", appServiceHost, appServicePort)

	// Create HTTP client with timeout
	client := &http.Client{Timeout: 30 * time.Second}

	// Create HTTP request
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create version request: %w", err)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch version from service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version service returned status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read version response: %w", err)
	}

	return string(body), nil
}
