package hydrationfn

import (
	"context"
	"fmt"
	"log"
	"market/internal/v2/settings"
	"net/url"
	"strings"
)

// extractRenderConfig extracts rendering configuration from app data
func (s *RenderedChartStep) extractRenderConfig(appData map[string]interface{}) (map[string]interface{}, error) {
	renderConfig := make(map[string]interface{})

	// Extract render-specific configuration
	if render, ok := appData["render"]; ok {
		if renderMap, ok := render.(map[string]interface{}); ok {
			renderConfig = renderMap
		}
	}

	// Extract default values and configurations
	if values, ok := appData["values"]; ok {
		renderConfig["values"] = values
	}
	if config, ok := appData["config"]; ok {
		renderConfig["config"] = config
	}
	if params, ok := appData["parameters"]; ok {
		renderConfig["parameters"] = params
	}

	// Extract target architecture and platform
	if arch, ok := appData["architecture"].(string); ok {
		renderConfig["architecture"] = arch
	} else {
		renderConfig["architecture"] = "amd64" // default architecture
	}

	if platform, ok := appData["platform"].(string); ok {
		renderConfig["platform"] = platform
	} else {
		renderConfig["platform"] = "linux" // default platform
	}

	// Extract Terminus-specific configuration
	if terminus, ok := appData["terminus"]; ok {
		renderConfig["terminus"] = terminus
	}

	return renderConfig, nil
}

// buildRenderedChartURL builds the rendered chart URL from task and render config
func (s *RenderedChartStep) buildRenderedChartURL(task *HydrationTask, renderConfig map[string]interface{}) (string, error) {
	// Get market source configuration
	marketSources := task.SettingsManager.GetActiveMarketSources()
	if len(marketSources) == 0 {
		return "", fmt.Errorf("no active market sources available")
	}

	// Find the current source
	var currentSource *settings.MarketSource
	for _, source := range marketSources {
		if source.Name == task.SourceID {
			currentSource = source
			break
		}
	}

	if currentSource == nil {
		return "", fmt.Errorf("market source not found: %s", task.SourceID)
	}

	// Extract chart information from task
	sourceInfo, ok := task.ChartData["source_info"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("source chart information not available")
	}

	chartName := ""
	chartVersion := ""

	if name, ok := sourceInfo["chart_name"].(string); ok {
		chartName = name
	} else if name, ok := sourceInfo["name"].(string); ok {
		chartName = name
	}

	if version, ok := sourceInfo["chart_version"].(string); ok {
		chartVersion = version
	} else if version, ok := sourceInfo["version"].(string); ok {
		chartVersion = version
	}

	if chartName == "" || chartVersion == "" {
		return "", fmt.Errorf("missing required chart information for rendered URL: name=%s, version=%s", chartName, chartVersion)
	}

	// Get architecture and platform
	arch := "amd64"
	platform := "linux"
	if archVal, ok := renderConfig["architecture"].(string); ok {
		arch = archVal
	}
	if platformVal, ok := renderConfig["platform"].(string); ok {
		platform = platformVal
	}

	// Build rendered chart URL with architecture and platform
	baseURL := strings.TrimSuffix(currentSource.BaseURL, "/")
	return fmt.Sprintf("%s/charts/rendered/%s/%s/%s-%s.tgz", baseURL, platform, arch, chartName, chartVersion), nil
}

// verifyRenderedChartPackage verifies that the rendered chart package exists and is accessible
func (s *RenderedChartStep) verifyRenderedChartPackage(ctx context.Context, chartURL string) error {
	// Parse URL to validate format
	if _, err := url.Parse(chartURL); err != nil {
		return fmt.Errorf("invalid rendered chart URL format: %w", err)
	}

	// Perform HEAD request to check if the rendered chart package exists
	resp, err := s.client.R().
		SetContext(ctx).
		Head(chartURL)

	if err != nil {
		return fmt.Errorf("failed to verify rendered chart package accessibility: %w", err)
	}

	// Check response status
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("rendered chart package not accessible, status: %d, URL: %s", resp.StatusCode(), chartURL)
	}

	log.Printf("Rendered chart package verified successfully: %s", chartURL)
	return nil
}

// triggerChartRendering attempts to trigger chart rendering process
func (s *RenderedChartStep) triggerChartRendering(ctx context.Context, task *HydrationTask, renderConfig map[string]interface{}) error {
	// Get market source configuration
	marketSources := task.SettingsManager.GetActiveMarketSources()
	if len(marketSources) == 0 {
		return fmt.Errorf("no active market sources available")
	}

	// Find the current source
	var currentSource *settings.MarketSource
	for _, source := range marketSources {
		if source.Name == task.SourceID {
			currentSource = source
			break
		}
	}

	if currentSource == nil {
		return fmt.Errorf("market source not found: %s", task.SourceID)
	}

	// Build render trigger URL
	baseURL := strings.TrimSuffix(currentSource.BaseURL, "/")
	renderURL := fmt.Sprintf("%s/api/v1/charts/render", baseURL)

	// Prepare render request payload
	payload := map[string]interface{}{
		"source_url":    task.SourceChartURL,
		"app_id":        task.AppID,
		"user_id":       task.UserID,
		"render_config": renderConfig,
	}

	// Send render request
	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		Post(renderURL)

	if err != nil {
		return fmt.Errorf("failed to send render request: %w", err)
	}

	// Check response status
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("render request failed, status: %d, response: %s", resp.StatusCode(), resp.String())
	}

	log.Printf("Chart rendering triggered successfully for app: %s", task.AppID)
	return nil
}

// isValidChartURL performs basic validation of chart URL
func (s *RenderedChartStep) isValidChartURL(chartURL string) bool {
	if chartURL == "" {
		return false
	}

	// Parse URL
	parsedURL, err := url.Parse(chartURL)
	if err != nil {
		return false
	}

	// Check scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}

	// Check if it looks like a chart package
	return strings.HasSuffix(strings.ToLower(parsedURL.Path), ".tgz") ||
		strings.HasSuffix(strings.ToLower(parsedURL.Path), ".tar.gz")
}
