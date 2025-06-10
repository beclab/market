package hydrationfn

import (
	"context"
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
// SourceChartStep 表示验证和获取源chart包的步骤
type SourceChartStep struct {
	client *resty.Client
}

// NewSourceChartStep creates a new source chart step
// NewSourceChartStep 创建新的源chart步骤
func NewSourceChartStep() *SourceChartStep {
	return &SourceChartStep{
		client: resty.New(),
	}
}

// GetStepName returns the name of this step
// GetStepName 返回此步骤的名称
func (s *SourceChartStep) GetStepName() string {
	return "Source Chart Verification"
}

// CanSkip determines if this step can be skipped
// CanSkip 确定是否可以跳过此步骤
func (s *SourceChartStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// Get CHART_ROOT environment variable
	// 获取CHART_ROOT环境变量
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return false // Cannot skip if CHART_ROOT is not set
	}

	// Extract chart information from app data
	// 从应用数据中提取chart信息
	chartInfo, err := s.extractChartInfo(task.AppData)
	if err != nil {
		return false // Cannot skip if chart info extraction fails
	}

	// Extract chart details
	// 提取chart详情
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
	// 构建本地文件路径
	chartFileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)
	localChartPath := filepath.Join(chartRoot, task.SourceID, chartFileName)

	// Check if local chart file exists
	// 检查本地chart文件是否存在
	if _, err := os.Stat(localChartPath); err == nil {
		log.Printf("Chart file exists locally, skipping download: %s", localChartPath)
		// Set the local path in task for consistency
		// 为了一致性，在任务中设置本地路径
		task.SourceChartURL = "file://" + localChartPath
		task.ChartData["source_info"] = chartInfo
		task.ChartData["local_path"] = localChartPath
		return true
	}

	return false // Cannot skip, file doesn't exist locally
}

// Execute performs the source chart verification and fetching
// Execute 执行源chart验证和获取
func (s *SourceChartStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing source chart step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Extract chart information from app data
	// 从应用数据中提取chart信息
	chartInfo, err := s.extractChartInfo(task.AppData)
	if err != nil {
		return fmt.Errorf("failed to extract chart info: %w", err)
	}

	// Store chart info in task data for later use in buildChartDownloadURL
	// 将chart信息存储在任务数据中，供buildChartDownloadURL后续使用
	task.ChartData["source_info"] = chartInfo

	// Get CHART_ROOT environment variable
	// 获取CHART_ROOT环境变量
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// Create source directory path: CHART_ROOT/source_name/
	// 创建源目录路径：CHART_ROOT/源名称/
	sourceDir := filepath.Join(chartRoot, task.SourceID)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		return fmt.Errorf("failed to create source directory: %w", err)
	}
	log.Printf("Created/verified source directory: %s", sourceDir)

	// Extract chart details for filename
	// 提取chart详情用于文件名
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
	// 构建本地文件路径：CHART_ROOT/源名称/应用名-chart版本.tgz
	chartFileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)
	localChartPath := filepath.Join(sourceDir, chartFileName)

	// Check if local chart file already exists
	// 检查本地chart文件是否已存在
	if _, err := os.Stat(localChartPath); err == nil {
		log.Printf("Chart file already exists locally: %s", localChartPath)
		task.SourceChartURL = "file://" + localChartPath
		task.ChartData["local_path"] = localChartPath

		// Update AppInfoLatestPendingData with source chart path
		// 使用源chart路径更新AppInfoLatestPendingData
		if err := s.updatePendingDataRawPackage(task, localChartPath); err != nil {
			log.Printf("Warning: failed to update pending data raw package: %v", err)
		}

		return nil
	}

	// File doesn't exist, need to download it
	// 文件不存在，需要下载
	log.Printf("Chart file not found locally, downloading: %s", chartFileName)

	// Build download URL using API_CHART_PATH
	// 使用API_CHART_PATH构建下载URL
	downloadURL, err := s.buildChartDownloadURL(task, chartName)
	if err != nil {
		return fmt.Errorf("failed to build chart download URL: %w", err)
	}

	// Download the chart file
	// 下载chart文件
	if err := s.downloadChartFile(ctx, downloadURL, localChartPath); err != nil {
		return fmt.Errorf("failed to download chart file: %w", err)
	}

	// Store the local chart path in task
	// 在任务中存储本地chart路径
	task.SourceChartURL = "file://" + localChartPath
	task.ChartData["local_path"] = localChartPath
	task.ChartData["download_url"] = downloadURL

	// Update AppInfoLatestPendingData with source chart path
	// 使用源chart路径更新AppInfoLatestPendingData
	if err := s.updatePendingDataRawPackage(task, localChartPath); err != nil {
		log.Printf("Warning: failed to update pending data raw package: %v", err)
	}

	log.Printf("Chart file downloaded successfully: %s", localChartPath)
	return nil
}

// extractChartInfo extracts chart information from app data
// extractChartInfo 从应用数据中提取chart信息
func (s *SourceChartStep) extractChartInfo(appData map[string]interface{}) (map[string]interface{}, error) {
	chartInfo := make(map[string]interface{})

	// Try to extract chart information from various possible fields
	// 尝试从各种可能的字段中提取chart信息
	if chart, ok := appData["chart"]; ok {
		if chartMap, ok := chart.(map[string]interface{}); ok {
			chartInfo = chartMap
		}
	}

	// Extract basic information
	// 提取基本信息
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
	// 提取chart特定字段
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
		return nil, fmt.Errorf("no chart information found in app data")
	}

	return chartInfo, nil
}

// buildChartDownloadURL builds the download URL for chart using API_CHART_PATH
// buildChartDownloadURL 使用API_CHART_PATH构建chart下载URL
func (s *SourceChartStep) buildChartDownloadURL(task *HydrationTask, chartName string) (string, error) {
	// Get market source configuration
	// 获取市场源配置
	marketSources := task.SettingsManager.GetActiveMarketSources()
	if len(marketSources) == 0 {
		return "", fmt.Errorf("no active market sources available")
	}

	// Find the current source
	// 查找当前源
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
	// 从环境变量获取API_CHART_PATH
	apiChartPath := os.Getenv("API_CHART_PATH")
	if apiChartPath == "" {
		return "", fmt.Errorf("API_CHART_PATH environment variable is not set")
	}

	// Replace {chart_name} placeholder with actual chartName
	// 将{chart_name}占位符替换为实际的chartName
	apiChartPath = strings.ReplaceAll(apiChartPath, "{chart_name}", chartName)

	// Build full download URL
	// 构建完整下载URL
	baseURL := strings.TrimSuffix(currentSource.BaseURL, "/")
	downloadURL := baseURL + apiChartPath

	// Extract chart version from task data (参考 DownloadChart 的版本处理逻辑)
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
	// 应用与DownloadChart类似的默认版本逻辑
	if chartVersion == "" {
		chartVersion = "1.10.9-0"
	}
	if chartVersion == "undefined" {
		chartVersion = "1.10.9-0"
	}
	if chartVersion == "latest" {
		// Use environment variable like DownloadChart does
		// 像DownloadChart一样使用环境变量
		if latestVersion := os.Getenv("LATEST_VERSION"); latestVersion != "" {
			chartVersion = latestVersion
		} else {
			chartVersion = "1.10.9-0" // fallback
		}
	}

	// Build fileName parameter (required like in DownloadChart)
	// 构建fileName参数（像DownloadChart中一样是必需的）
	fileName := fmt.Sprintf("%s-%s.tgz", chartName, chartVersion)

	// Add query parameters to URL (参考 DownloadChart 的参数构建)
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
// downloadChartFile 从给定URL下载chart文件到本地路径
func (s *SourceChartStep) downloadChartFile(ctx context.Context, downloadURL, localPath string) error {
	log.Printf("Downloading chart from: %s to: %s", downloadURL, localPath)

	// Create HTTP request to download the file
	// 创建HTTP请求下载文件
	resp, err := s.client.R().
		SetContext(ctx).
		Get(downloadURL)

	if err != nil {
		return fmt.Errorf("failed to download chart file: %w", err)
	}

	// Check response status
	// 检查响应状态
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("failed to download chart file, status: %d, URL: %s", resp.StatusCode(), downloadURL)
	}

	// Create the local file
	// 创建本地文件
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local chart file: %w", err)
	}
	defer file.Close()

	// Write response body to file
	// 将响应体写入文件
	_, err = io.Copy(file, strings.NewReader(string(resp.Body())))
	if err != nil {
		return fmt.Errorf("failed to write chart file: %w", err)
	}

	log.Printf("Chart file downloaded successfully: %s (%d bytes)", localPath, len(resp.Body()))
	return nil
}

// isValidChartURL performs basic validation of chart URL
// isValidChartURL 对chart URL执行基本验证
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
// updatePendingDataRawPackage 更新AppInfoLatestPendingData中的RawPackage字段
func (s *SourceChartStep) updatePendingDataRawPackage(task *HydrationTask, chartPath string) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Lock cache for thread-safe access
	// 锁定缓存以进行线程安全访问
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Check if user exists in cache
	// 检查用户是否在缓存中存在
	userData, exists := task.Cache.Users[task.UserID]
	if !exists {
		log.Printf("User %s not found in cache, skipping RawPackage update", task.UserID)
		return nil
	}

	userData.Mutex.Lock()
	defer userData.Mutex.Unlock()

	// Check if source exists for user
	// 检查用户的源是否存在
	sourceData, exists := userData.Sources[task.SourceID]
	if !exists {
		log.Printf("Source %s not found for user %s, skipping RawPackage update", task.SourceID, task.UserID)
		return nil
	}

	sourceData.Mutex.Lock()
	defer sourceData.Mutex.Unlock()

	// Find the corresponding pending data and update RawPackage
	// 查找对应的待处理数据并更新RawPackage
	for i, pendingData := range sourceData.AppInfoLatestPending {
		if s.isTaskForPendingData(task, pendingData) {
			log.Printf("Updating RawPackage for pending data at index %d: %s", i, chartPath)
			sourceData.AppInfoLatestPending[i].RawPackage = chartPath
			log.Printf("Successfully updated RawPackage for app: %s", task.AppID)
			return nil
		}
	}

	log.Printf("No matching pending data found for task %s, skipping RawPackage update", task.ID)
	return nil
}

// isTaskForPendingData checks if the current task corresponds to the pending data
// isTaskForPendingData 检查当前任务是否对应待处理数据
func (s *SourceChartStep) isTaskForPendingData(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		return false
	}

	taskAppID := task.AppID

	// Check if the task AppID matches the pending data's RawData
	// 检查任务AppID是否匹配待处理数据的RawData
	if pendingData.RawData != nil {
		// Check if this is legacy data by looking at metadata
		// 通过查看元数据检查这是否是传统数据
		if pendingData.RawData.Metadata != nil {
			// Check for legacy_data in metadata - this contains multiple apps
			// 检查元数据中的legacy_data - 这包含多个应用
			if legacyData, hasLegacyData := pendingData.RawData.Metadata["legacy_data"]; hasLegacyData {
				if legacyDataMap, ok := legacyData.(map[string]interface{}); ok {
					// Check if task app ID exists in the legacy data apps
					// 检查任务应用ID是否存在于传统数据应用中
					if dataSection, hasDataSection := legacyDataMap["data"].(map[string]interface{}); hasDataSection {
						if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
							if _, appExists := appsData[taskAppID]; appExists {
								return true
							}
						}
					}
				}
			}

			// Check for legacy_raw_data in metadata
			// 检查元数据中的legacy_raw_data
			if legacyRawData, hasLegacyRawData := pendingData.RawData.Metadata["legacy_raw_data"]; hasLegacyRawData {
				if legacyRawDataMap, ok := legacyRawData.(map[string]interface{}); ok {
					// Similar check for legacy raw data format
					// 对传统原始数据格式进行类似检查
					if dataSection, hasDataSection := legacyRawDataMap["data"].(map[string]interface{}); hasDataSection {
						if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
							if _, appExists := appsData[taskAppID]; appExists {
								return true
							}
						}
					}
				}
			}

			// Check representative_app_id for legacy summary data
			// 检查传统汇总数据的representative_app_id
			if repAppID, hasRepAppID := pendingData.RawData.Metadata["representative_app_id"].(string); hasRepAppID {
				if repAppID == taskAppID {
					return true
				}
			}
		}

		// Standard checks for non-legacy data
		// 对非传统数据进行标准检查
		// Match by ID, AppID or Name
		// 通过ID、AppID或Name匹配
		if pendingData.RawData.ID == taskAppID ||
			pendingData.RawData.AppID == taskAppID ||
			pendingData.RawData.Name == taskAppID {
			return true
		}
	}

	// Check AppInfo if available
	// 如果可用，检查AppInfo
	if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
		// Match by ID, AppID or Name in AppInfo
		// 通过AppInfo中的ID、AppID或Name匹配
		if pendingData.AppInfo.AppEntry.ID == taskAppID ||
			pendingData.AppInfo.AppEntry.AppID == taskAppID ||
			pendingData.AppInfo.AppEntry.Name == taskAppID {
			return true
		}
	}

	return false
}
