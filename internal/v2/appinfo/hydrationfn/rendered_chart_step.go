package hydrationfn

import (
	"context"
	"fmt"
	"log"
	"market/internal/v2/settings"
	"net/url"
	"strings"

	"github.com/go-resty/resty/v2"
)

// RenderedChartStep represents the step to verify and fetch rendered chart package
// RenderedChartStep 表示验证和获取渲染chart包的步骤
type RenderedChartStep struct {
	client *resty.Client
}

// NewRenderedChartStep creates a new rendered chart step
// NewRenderedChartStep 创建新的渲染chart步骤
func NewRenderedChartStep() *RenderedChartStep {
	return &RenderedChartStep{
		client: resty.New(),
	}
}

// GetStepName returns the name of this step
// GetStepName 返回此步骤的名称
func (s *RenderedChartStep) GetStepName() string {
	return "Rendered Chart Verification"
}

// CanSkip determines if this step can be skipped
// CanSkip 确定是否可以跳过此步骤
func (s *RenderedChartStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// Skip if we already have the rendered chart URL and it's valid
	// 如果我们已经有渲染chart URL且有效则跳过
	if task.RenderedChartURL != "" {
		return s.isValidChartURL(task.RenderedChartURL)
	}
	return false
}

// Execute performs the rendered chart verification and processing
// Execute 执行渲染chart验证和处理
func (s *RenderedChartStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing rendered chart step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Check if source chart step completed successfully
	// 检查源chart步骤是否成功完成
	if task.SourceChartURL == "" {
		return fmt.Errorf("source chart URL is required but not available")
	}

	// Extract rendering configuration from app data
	// 从应用数据中提取渲染配置
	renderConfig, err := s.extractRenderConfig(task.AppData)
	if err != nil {
		return fmt.Errorf("failed to extract render config: %w", err)
	}

	// Build rendered chart URL
	// 构建渲染chart URL
	renderedChartURL, err := s.buildRenderedChartURL(task, renderConfig)
	if err != nil {
		return fmt.Errorf("failed to build rendered chart URL: %w", err)
	}

	// Verify the rendered chart package exists and is accessible
	// 验证渲染chart包存在且可访问
	if err := s.verifyRenderedChartPackage(ctx, renderedChartURL); err != nil {
		// If rendered chart doesn't exist, try to trigger chart rendering
		// 如果渲染chart不存在，尝试触发chart渲染
		log.Printf("Rendered chart not found, attempting to trigger rendering: %s", renderedChartURL)

		if err := s.triggerChartRendering(ctx, task, renderConfig); err != nil {
			return fmt.Errorf("failed to trigger chart rendering: %w", err)
		}

		// Wait a moment and try verification again
		// 稍等一下再次尝试验证
		if err := s.verifyRenderedChartPackage(ctx, renderedChartURL); err != nil {
			return fmt.Errorf("rendered chart still not available after triggering: %w", err)
		}
	}

	// Store the rendered chart URL in task
	// 在任务中存储渲染chart URL
	task.RenderedChartURL = renderedChartURL

	// Store additional render data
	// 存储额外的渲染数据
	task.ChartData["render_config"] = renderConfig
	task.ChartData["rendered_url"] = renderedChartURL

	log.Printf("Rendered chart verification completed for app: %s, URL: %s",
		task.AppID, renderedChartURL)

	return nil
}

// extractRenderConfig extracts rendering configuration from app data
// extractRenderConfig 从应用数据中提取渲染配置
func (s *RenderedChartStep) extractRenderConfig(appData map[string]interface{}) (map[string]interface{}, error) {
	renderConfig := make(map[string]interface{})

	// Extract render-specific configuration
	// 提取渲染特定配置
	if render, ok := appData["render"]; ok {
		if renderMap, ok := render.(map[string]interface{}); ok {
			renderConfig = renderMap
		}
	}

	// Extract default values and configurations
	// 提取默认值和配置
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
	// 提取目标架构和平台
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
	// 提取Terminus特定配置
	if terminus, ok := appData["terminus"]; ok {
		renderConfig["terminus"] = terminus
	}

	return renderConfig, nil
}

// buildRenderedChartURL builds the rendered chart URL from task and render config
// buildRenderedChartURL 从任务和渲染配置构建渲染chart URL
func (s *RenderedChartStep) buildRenderedChartURL(task *HydrationTask, renderConfig map[string]interface{}) (string, error) {
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

	// Extract chart information from task
	// 从任务中提取chart信息
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
	// 获取架构和平台
	arch := "amd64"
	platform := "linux"
	if archVal, ok := renderConfig["architecture"].(string); ok {
		arch = archVal
	}
	if platformVal, ok := renderConfig["platform"].(string); ok {
		platform = platformVal
	}

	// Build rendered chart URL with architecture and platform
	// 使用架构和平台构建渲染chart URL
	baseURL := strings.TrimSuffix(currentSource.BaseURL, "/")
	return fmt.Sprintf("%s/charts/rendered/%s/%s/%s-%s.tgz", baseURL, platform, arch, chartName, chartVersion), nil
}

// verifyRenderedChartPackage verifies that the rendered chart package exists and is accessible
// verifyRenderedChartPackage 验证渲染chart包存在且可访问
func (s *RenderedChartStep) verifyRenderedChartPackage(ctx context.Context, chartURL string) error {
	// Parse URL to validate format
	// 解析URL以验证格式
	if _, err := url.Parse(chartURL); err != nil {
		return fmt.Errorf("invalid rendered chart URL format: %w", err)
	}

	// Perform HEAD request to check if the rendered chart package exists
	// 执行HEAD请求检查渲染chart包是否存在
	resp, err := s.client.R().
		SetContext(ctx).
		Head(chartURL)

	if err != nil {
		return fmt.Errorf("failed to verify rendered chart package accessibility: %w", err)
	}

	// Check response status
	// 检查响应状态
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("rendered chart package not accessible, status: %d, URL: %s", resp.StatusCode(), chartURL)
	}

	log.Printf("Rendered chart package verified successfully: %s", chartURL)
	return nil
}

// triggerChartRendering attempts to trigger chart rendering process
// triggerChartRendering 尝试触发chart渲染过程
func (s *RenderedChartStep) triggerChartRendering(ctx context.Context, task *HydrationTask, renderConfig map[string]interface{}) error {
	// Get market source configuration
	// 获取市场源配置
	marketSources := task.SettingsManager.GetActiveMarketSources()
	if len(marketSources) == 0 {
		return fmt.Errorf("no active market sources available")
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
		return fmt.Errorf("market source not found: %s", task.SourceID)
	}

	// Build render trigger URL
	// 构建渲染触发URL
	baseURL := strings.TrimSuffix(currentSource.BaseURL, "/")
	renderURL := fmt.Sprintf("%s/api/v1/charts/render", baseURL)

	// Prepare render request payload
	// 准备渲染请求载荷
	payload := map[string]interface{}{
		"source_url":    task.SourceChartURL,
		"app_id":        task.AppID,
		"user_id":       task.UserID,
		"render_config": renderConfig,
	}

	// Send render request
	// 发送渲染请求
	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		Post(renderURL)

	if err != nil {
		return fmt.Errorf("failed to send render request: %w", err)
	}

	// Check response status
	// 检查响应状态
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return fmt.Errorf("render request failed, status: %d, response: %s", resp.StatusCode(), resp.String())
	}

	log.Printf("Chart rendering triggered successfully for app: %s", task.AppID)
	return nil
}

// isValidChartURL performs basic validation of chart URL
// isValidChartURL 对chart URL执行基本验证
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
