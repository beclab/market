package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"market/internal/v2/settings"
	"market/internal/v2/types"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
)

// SourceChartStep represents the step to verify and fetch source chart package
type SourceChartStep struct {
	client *resty.Client
}

// NewSourceChartStep creates a new source chart step
func NewSourceChartStep() *SourceChartStep {
	return &SourceChartStep{
		client: resty.New(),
	}
}

// GetStepName returns the name of this step
func (s *SourceChartStep) GetStepName() string {
	return "Source Chart Verification"
}

// CanSkip determines if this step can be skipped
func (s *SourceChartStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// Get CHART_ROOT environment variable
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return false // Cannot skip if CHART_ROOT is not set
	}

	// Extract chart information from app data
	chartInfo, err := s.extractChartInfo(task.AppData)
	if err != nil {
		return false // Cannot skip if chart info extraction fails
	}

	// Extract chart details
	chartName := ""
	chartVersion := ""

	if name, ok := chartInfo["chart_name"].(string); ok {
		chartName = name
	} else if name, ok := chartInfo["name"].(string); ok {
		chartName = name
	}

	if version, ok := chartInfo["chart_version"].(string); ok {
		chartVersion = version
	} else if version, ok := chartInfo["version"].(string); ok {
		chartVersion = version
	}

	if chartName == "" || chartVersion == "" {
		return false // Cannot skip if required chart info is missing
	}

	// Build local file path
	chartFileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)
	localChartPath := filepath.Join(chartRoot, task.SourceID, chartFileName)

	// Check if local chart file exists
	if _, err := os.Stat(localChartPath); err == nil {
		log.Printf("Chart file exists locally, skipping download: %s", localChartPath)
		// Set the local path in task for consistency
		// Convert to absolute path before creating file:// URL
		absLocalChartPath, err := filepath.Abs(localChartPath)
		if err != nil {
			return false // Cannot skip if path conversion fails
		}
		task.SourceChartURL = "file://" + absLocalChartPath
		task.ChartData["source_info"] = chartInfo
		task.ChartData["local_path"] = localChartPath
		return true
	}

	return false // Cannot skip, file doesn't exist locally
}

// Execute performs the source chart verification and fetching
func (s *SourceChartStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing source chart step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Check if this is a local source
	if s.isLocalSource(task) {
		return s.handleLocalSource(ctx, task)
	}

	// Extract chart information from app data
	chartInfo, err := s.extractChartInfo(task.AppData)
	if err != nil {
		return fmt.Errorf("failed to extract chart info: %w", err)
	}

	// Store chart info in task data for later use in buildChartDownloadURL
	task.ChartData["source_info"] = chartInfo

	// Get CHART_ROOT environment variable
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// Create source directory path: CHART_ROOT/source_name/
	sourceDir := filepath.Join(chartRoot, task.SourceID)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		return fmt.Errorf("failed to create source directory: %w", err)
	}
	log.Printf("Created/verified source directory: %s", sourceDir)

	// Extract chart details for filename
	chartName := ""
	chartVersion := ""

	if name, ok := chartInfo["chart_name"].(string); ok {
		chartName = name
	} else if name, ok := chartInfo["name"].(string); ok {
		chartName = name
	}

	if version, ok := chartInfo["chart_version"].(string); ok {
		chartVersion = version
	} else if version, ok := chartInfo["version"].(string); ok {
		chartVersion = version
	}

	if chartName == "" || chartVersion == "" {
		return fmt.Errorf("missing required chart information: name=%s, version=%s", chartName, chartVersion)
	}

	// Build local file path: CHART_ROOT/source_name/app_name-chart_version.tgz
	chartFileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)
	localChartPath := filepath.Join(sourceDir, chartFileName)

	// Check if local chart file already exists
	if _, err := os.Stat(localChartPath); err == nil {
		log.Printf("Chart file already exists locally: %s", localChartPath)
		// Convert to absolute path before creating file:// URL
		absLocalChartPath, err := filepath.Abs(localChartPath)
		if err != nil {
			return fmt.Errorf("failed to convert chart path to absolute path: %w", err)
		}
		task.SourceChartURL = "file://" + absLocalChartPath
		task.ChartData["local_path"] = localChartPath

		// Update AppInfoLatestPendingData with source chart path
		if err := s.updatePendingDataRawPackage(task, localChartPath); err != nil {
			log.Printf("Warning: failed to update pending data raw package: %v", err)
		}

		return nil
	}

	// File doesn't exist, need to download it
	log.Printf("Chart file not found locally, downloading: %s", chartFileName)

	// Build download URL using API_CHART_PATH
	downloadURL, err := s.buildChartDownloadURL(task, chartName)
	if err != nil {
		return fmt.Errorf("failed to build chart download URL: %w", err)
	}

	// Download the chart file
	if err := s.downloadChartFile(ctx, downloadURL, localChartPath); err != nil {
		return fmt.Errorf("failed to download chart file: %w", err)
	}

	// Store the local chart path in task
	// Convert to absolute path before creating file:// URL
	absLocalChartPath, err := filepath.Abs(localChartPath)
	if err != nil {
		return fmt.Errorf("failed to convert chart path to absolute path: %w", err)
	}
	task.SourceChartURL = "file://" + absLocalChartPath
	task.ChartData["local_path"] = localChartPath
	task.ChartData["download_url"] = downloadURL

	// Update AppInfoLatestPendingData with source chart path
	if err := s.updatePendingDataRawPackage(task, localChartPath); err != nil {
		log.Printf("Warning: failed to update pending data raw package: %v", err)
	}

	log.Printf("Chart file downloaded successfully: %s", localChartPath)
	return nil
}

// extractChartInfo extracts chart information from app data
func (s *SourceChartStep) extractChartInfo(appData map[string]interface{}) (map[string]interface{}, error) {
	chartInfo := make(map[string]interface{})

	// Try to extract chart information from various possible fields
	if chart, ok := appData["chart"]; ok {
		if chartMap, ok := chart.(map[string]interface{}); ok {
			chartInfo = chartMap
		}
	}

	// Extract basic information
	if name, ok := appData["name"].(string); ok {
		chartInfo["name"] = name
	}
	if version, ok := appData["version"].(string); ok {
		chartInfo["version"] = version
	}
	if repo, ok := appData["repository"].(string); ok {
		chartInfo["repository"] = repo
	}

	// Extract chart-specific fields
	if chartName, ok := appData["chart_name"].(string); ok {
		chartInfo["chart_name"] = chartName
	}
	if chartVersion, ok := appData["chart_version"].(string); ok {
		chartInfo["chart_version"] = chartVersion
	}
	if chartRepo, ok := appData["chart_repository"].(string); ok {
		chartInfo["chart_repository"] = chartRepo
	}

	if len(chartInfo) == 0 {
		log.Printf("No chart information found in app data: %v", appData)
		return nil, fmt.Errorf("no chart information found in app data")
	}

	return chartInfo, nil
}

// buildChartDownloadURL builds the download URL for chart using API_CHART_PATH
func (s *SourceChartStep) buildChartDownloadURL(task *HydrationTask, chartName string) (string, error) {
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

	// Get API_CHART_PATH from environment variables
	apiChartPath := os.Getenv("API_CHART_PATH")
	if apiChartPath == "" {
		return "", fmt.Errorf("API_CHART_PATH environment variable is not set")
	}

	// Replace {chart_name} placeholder with actual chartName
	apiChartPath = strings.ReplaceAll(apiChartPath, "{chart_name}", chartName)

	// Build full download URL
	baseURL := strings.TrimSuffix(currentSource.BaseURL, "/")
	downloadURL := baseURL + apiChartPath

	// Extract chart version from task data (following DownloadChart version handling logic)
	chartVersion := ""
	if chartInfo, exists := task.ChartData["source_info"].(map[string]interface{}); exists {
		if version, ok := chartInfo["chart_version"].(string); ok {
			chartVersion = version
		} else if version, ok := chartInfo["version"].(string); ok {
			chartVersion = version
		}
	}

	// Apply default version logic similar to DownloadChart
	if chartVersion == "" {
		chartVersion = "1.10.9-0"
	}
	if chartVersion == "undefined" {
		chartVersion = "1.10.9-0"
	}
	if chartVersion == "latest" {
		// Use environment variable like DownloadChart does
		if latestVersion := os.Getenv("LATEST_VERSION"); latestVersion != "" {
			chartVersion = latestVersion
		} else {
			chartVersion = "1.10.9-0" // fallback
		}
	}

	// Build fileName parameter (required like in DownloadChart)
	fileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)

	// Add query parameters to URL (following DownloadChart parameter construction)
	parsedURL, err := url.Parse(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse download URL: %w", err)
	}

	queryParams := parsedURL.Query()
	queryParams.Set("version", chartVersion)
	queryParams.Set("fileName", fileName)
	parsedURL.RawQuery = queryParams.Encode()

	finalURL := parsedURL.String()
	log.Printf("Built chart download URL with parameters: %s (version=%s, fileName=%s)", finalURL, chartVersion, fileName)
	return finalURL, nil
}

// downloadChartFile downloads the chart file from the given URL to local path
func (s *SourceChartStep) downloadChartFile(ctx context.Context, downloadURL, localPath string) error {
	log.Printf("Downloading chart from: %s to: %s", downloadURL, localPath)

	// Create the file
	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close() // Ensure file is closed

	// Download the file
	resp, err := s.client.R().
		SetContext(ctx).
		SetDoNotParseResponse(true).
		Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.RawBody().Close() // Ensure response body is closed

	// Copy the response body to the file
	_, err = io.Copy(out, resp.RawBody())
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// isValidChartURL performs basic validation of chart URL
func (s *SourceChartStep) isValidChartURL(chartURL string) bool {
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

// updatePendingDataRawPackage updates the RawPackage field in AppInfoLatestPendingData
func (s *SourceChartStep) updatePendingDataRawPackage(task *HydrationTask, chartPath string) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Lock cache for thread-safe access
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Check if user exists in cache
	userData, exists := task.Cache.Users[task.UserID]
	if !exists {
		log.Printf("User %s not found in cache, skipping RawPackage update", task.UserID)
		return nil
	}

	// Find the corresponding pending data and update RawPackage
	for i, pendingData := range userData.Sources[task.SourceID].AppInfoLatestPending {
		if s.isTaskForPendingData(task, pendingData) {
			log.Printf("Updating RawPackage for pending data at index %d: %s", i, chartPath)
			userData.Sources[task.SourceID].AppInfoLatestPending[i].RawPackage = chartPath
			log.Printf("Successfully updated RawPackage for app: %s", task.AppID)

			// Log pending data after update to check for cycles
			s.logPendingDataAfterUpdate(pendingData, "after raw package update")

			return nil
		}
	}

	log.Printf("No matching pending data found for task %s, skipping RawPackage update", task.ID)
	return nil
}

// logPendingDataAfterUpdate logs pending data after update to check for cycles
func (s *SourceChartStep) logPendingDataAfterUpdate(pendingData *types.AppInfoLatestPendingData, context string) {
	log.Printf("DEBUG: Pending data structure check - %s", context)

	if pendingData == nil {
		log.Printf("DEBUG: Pending data is nil")
		return
	}

	// Try to JSON marshal the entire pending data
	if jsonData, err := json.Marshal(pendingData); err != nil {
		log.Printf("ERROR: JSON marshal failed for pending data - %s: %v", context, err)
		log.Printf("ERROR: Pending data structure: RawData=%v, AppInfo=%v, RawPackage=%s, RenderedPackage=%s",
			pendingData.RawData != nil, pendingData.AppInfo != nil, pendingData.RawPackage, pendingData.RenderedPackage)

		// Try to marshal individual components to isolate the problem
		if pendingData.RawData != nil {
			if _, err := json.Marshal(pendingData.RawData); err != nil {
				log.Printf("ERROR: JSON marshal failed for RawData - %s: %v", context, err)
			}
		}

		if pendingData.AppInfo != nil {
			if _, err := json.Marshal(pendingData.AppInfo); err != nil {
				log.Printf("ERROR: JSON marshal failed for AppInfo - %s: %v", context, err)
			}
		}
	} else {
		log.Printf("DEBUG: Pending data JSON length - %s: %d bytes", context, len(jsonData))
	}
}

// isTaskForPendingData checks if the current task corresponds to the pending data
func (s *SourceChartStep) isTaskForPendingData(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		return false
	}

	taskAppID := task.AppID

	// Check if the task AppID matches the pending data's RawData
	if pendingData.RawData != nil {
		// Standard checks for data
		// Match by ID, AppID or Name
		if pendingData.RawData.ID == taskAppID ||
			pendingData.RawData.AppID == taskAppID ||
			pendingData.RawData.Name == taskAppID {
			return true
		}
	}

	// Check AppInfo if available
	if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
		// Match by ID, AppID or Name in AppInfo
		if pendingData.AppInfo.AppEntry.ID == taskAppID ||
			pendingData.AppInfo.AppEntry.AppID == taskAppID ||
			pendingData.AppInfo.AppEntry.Name == taskAppID {
			return true
		}
	}

	return false
}

// isLocalSource checks if the task is for a local source
func (s *SourceChartStep) isLocalSource(task *HydrationTask) bool {
	// Check if the source ID indicates a local source
	// Local sources typically have specific naming patterns or are marked as local
	if task.SourceID == "local" || strings.HasPrefix(task.SourceID, "local_") {
		return true
	}

	// Check if the task has local source indicators in the data
	if task.AppData != nil {
		if sourceType, ok := task.AppData["source_type"].(string); ok && sourceType == "local" {
			return true
		}
		if isLocal, ok := task.AppData["is_local"].(bool); ok && isLocal {
			return true
		}
	}

	return false
}

// handleLocalSource handles chart verification for local sources
func (s *SourceChartStep) handleLocalSource(ctx context.Context, task *HydrationTask) error {
	log.Printf("Handling local source for app: %s (user: %s, source: %s)", task.AppID, task.UserID, task.SourceID)

	// Extract chart information from app data
	chartInfo, err := s.extractChartInfo(task.AppData)
	if err != nil {
		return fmt.Errorf("failed to extract chart info: %w", err)
	}

	// Store chart info in task data
	task.ChartData["source_info"] = chartInfo

	// Get CHART_ROOT environment variable
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// Extract chart details for filename
	chartName := ""
	chartVersion := ""

	if name, ok := chartInfo["chart_name"].(string); ok {
		chartName = name
	} else if name, ok := chartInfo["name"].(string); ok {
		chartName = name
	}

	if version, ok := chartInfo["chart_version"].(string); ok {
		chartVersion = version
	} else if version, ok := chartInfo["version"].(string); ok {
		chartVersion = version
	}

	if chartName == "" || chartVersion == "" {
		return fmt.Errorf("missing required chart information for local source: name=%s, version=%s", chartName, chartVersion)
	}

	// Build local file path: CHART_ROOT/sourceID/appName-appVersion.tgz
	chartFileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)
	localChartPath := filepath.Join(chartRoot, task.SourceID, chartFileName)

	// Check if local chart file exists
	if _, err := os.Stat(localChartPath); err == nil {
		log.Printf("Local chart file exists: %s", localChartPath)
		// Convert to absolute path before creating file:// URL
		absLocalChartPath, err := filepath.Abs(localChartPath)
		if err != nil {
			return fmt.Errorf("failed to convert chart path to absolute path: %w", err)
		}
		task.SourceChartURL = "file://" + absLocalChartPath
		task.ChartData["local_path"] = localChartPath

		// Update AppInfoLatestPendingData with source chart path
		if err := s.updatePendingDataRawPackage(task, localChartPath); err != nil {
			log.Printf("Warning: failed to update pending data raw package: %v", err)
		}

		log.Printf("Local source chart verification successful for app: %s", task.AppID)
		return nil
	}

	// Chart file doesn't exist for local source - this is a failure
	log.Printf("Local chart file not found: %s", localChartPath)
	return fmt.Errorf("local chart file not found: %s", localChartPath)
}
