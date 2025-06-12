package hydrationfn

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"market/internal/v2/settings"
	"market/internal/v2/types"
	"market/internal/v2/utils"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
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

	// Load and extract chart package from local file
	// 从本地文件加载并解压chart包
	chartFiles, err := s.loadAndExtractChart(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to load and extract chart: %w", err)
	}

	// Prepare template data for rendering
	// 准备用于渲染的模板数据
	templateData, err := s.prepareTemplateData(task)
	if err != nil {
		return fmt.Errorf("failed to prepare template data: %w", err)
	}

	// Find and render OlaresManifest.yaml
	// 查找并渲染 OlaresManifest.yaml
	renderedManifest, err := s.renderOlaresManifest(chartFiles, templateData)
	if err != nil {
		return fmt.Errorf("failed to render OlaresManifest.yaml: %w", err)
	}

	// Render the entire chart package
	// 渲染整个chart包
	renderedChart, err := s.renderChartPackage(chartFiles, templateData)
	if err != nil {
		return fmt.Errorf("failed to render chart package: %w", err)
	}

	// Save rendered chart to directory
	// 将渲染后的chart保存到目录
	if err := s.saveRenderedChart(task, renderedChart, renderedManifest); err != nil {
		return fmt.Errorf("failed to save rendered chart: %w", err)
	}

	// Store rendered content in task
	// 在任务中存储渲染内容
	task.ChartData["rendered_manifest"] = renderedManifest
	task.ChartData["rendered_chart"] = renderedChart
	task.ChartData["template_data"] = templateData

	// Build rendered chart URL (optional, for compatibility)
	// 构建渲染chart URL（可选，用于兼容性）
	renderConfig, _ := s.extractRenderConfig(task.AppData)
	renderedChartURL, err := s.buildRenderedChartURL(task, renderConfig)
	if err != nil {
		log.Printf("Warning: failed to build rendered chart URL: %v", err)
	} else {
		task.RenderedChartURL = renderedChartURL
	}

	// Update AppInfoLatestPendingData with rendered chart directory path
	// 使用渲染chart目录路径更新AppInfoLatestPendingData
	if renderedChartDir, exists := task.ChartData["rendered_chart_dir"].(string); exists {
		if err := s.updatePendingDataRenderedPackage(task, renderedChartDir); err != nil {
			log.Printf("Warning: failed to update pending data rendered package: %v", err)
		}
	}

	log.Printf("Chart rendering completed for app: %s", task.AppID)
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

// loadAndExtractChart loads and extracts the chart package from local file
// loadAndExtractChart 从本地文件加载并解压chart包
func (s *RenderedChartStep) loadAndExtractChart(ctx context.Context, task *HydrationTask) (map[string]*ChartFile, error) {
	// Get local chart path from task data
	// 从任务数据中获取本地chart路径
	var localPath string

	// Try to get local path from ChartData first
	// 首先尝试从ChartData获取本地路径
	if localPathVal, exists := task.ChartData["local_path"]; exists {
		if path, ok := localPathVal.(string); ok {
			localPath = path
		}
	}

	// If not found, try to extract from SourceChartURL
	// 如果未找到，尝试从SourceChartURL提取
	if localPath == "" && task.SourceChartURL != "" {
		if strings.HasPrefix(task.SourceChartURL, "file://") {
			localPath = strings.TrimPrefix(task.SourceChartURL, "file://")
		}
	}

	if localPath == "" {
		return nil, fmt.Errorf("local chart path not found in task data")
	}

	log.Printf("Loading chart from local file: %s", localPath)

	// Check if file exists
	// 检查文件是否存在
	if _, err := os.Stat(localPath); err != nil {
		return nil, fmt.Errorf("chart file not found: %w", err)
	}

	// Read chart file
	// 读取chart文件
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chart file: %w", err)
	}

	// Extract chart files from tar.gz
	// 从tar.gz中提取chart文件
	chartFiles, err := s.extractTarGz(data)
	if err != nil {
		return nil, fmt.Errorf("failed to extract chart: %w", err)
	}

	log.Printf("Successfully extracted %d files from local chart", len(chartFiles))
	return chartFiles, nil
}

// extractTarGz extracts files from a tar.gz archive
// extractTarGz 从tar.gz归档中提取文件
func (s *RenderedChartStep) extractTarGz(data []byte) (map[string]*ChartFile, error) {
	files := make(map[string]*ChartFile)

	// Create gzip reader
	// 创建gzip读取器
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create tar reader
	// 创建tar读取器
	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading tar: %w", err)
		}

		// Skip directories
		// 跳过目录
		if header.Typeflag == tar.TypeDir {
			files[header.Name] = &ChartFile{
				Name:     header.Name,
				IsDir:    true,
				Mode:     header.FileInfo().Mode(),
				Modified: header.ModTime,
			}
			continue
		}

		// Read file content
		// 读取文件内容
		content, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", header.Name, err)
		}

		files[header.Name] = &ChartFile{
			Name:     header.Name,
			Content:  content,
			IsDir:    false,
			Mode:     header.FileInfo().Mode(),
			Modified: header.ModTime,
		}
	}

	return files, nil
}

// prepareTemplateData prepares the template data for chart rendering
// prepareTemplateData 为chart渲染准备模板数据
func (s *RenderedChartStep) prepareTemplateData(task *HydrationTask) (*TemplateData, error) {
	templateData := &TemplateData{}

	// Initialize Values map
	// 初始化Values map
	templateData.Values = make(map[string]interface{})

	// Get admin username using utils function
	// 使用 utils 函数获取管理员用户名
	adminUsername, err := utils.GetAdminUsername("")
	if err != nil {
		log.Printf("Warning: failed to get admin username, using default: %v", err)
		adminUsername = "admin" // fallback to default
	}

	// Set admin username and user info
	// 设置管理员用户名和用户信息
	templateData.Values["admin"] = adminUsername
	templateData.Values["bfl"] = map[string]interface{}{
		"username": task.UserID,
	}

	// Add userspace values (commonly used in templates)
	// 添加用户空间值（模板中常用）
	templateData.Values["userspace"] = map[string]interface{}{
		"appCache": "",
		"userData": "",
		"appData":  "",
		"data":     "",
	}

	// Add user zone and scheduling info
	// 添加用户区域和调度信息
	templateData.Values["user"] = map[string]interface{}{
		"zone": fmt.Sprintf("user-space-%s", task.UserID),
	}

	templateData.Values["schedule"] = map[string]interface{}{
		"nodeName": "node",
	}

	// Add OS and application keys (for some apps that need them)
	// 添加操作系统和应用密钥（某些应用需要）
	templateData.Values["os"] = map[string]interface{}{
		"appKey":    "appKey",
		"appSecret": "appSecret",
	}

	// Add domain entries (empty for now, can be populated from app config)
	// 添加域名条目（暂时为空，可以从应用配置中填充）
	templateData.Values["domain"] = make(map[string]interface{})

	// Add middleware configurations
	// 添加中间件配置
	templateData.Values["postgres"] = map[string]interface{}{
		"databases": make(map[string]interface{}),
	}
	templateData.Values["redis"] = make(map[string]interface{})
	templateData.Values["mongodb"] = map[string]interface{}{
		"databases": make(map[string]interface{}),
	}
	templateData.Values["zinc"] = map[string]interface{}{
		"indexes": make(map[string]interface{}),
	}

	// Add service and cluster configurations
	// 添加服务和集群配置
	templateData.Values["svcs"] = make(map[string]interface{})
	templateData.Values["cluster"] = make(map[string]interface{})
	templateData.Values["dep"] = make(map[string]interface{})

	// Add OIDC and NATS configurations (for apps that use them)
	// 添加 OIDC 和 NATS 配置（用于使用它们的应用）
	templateData.Values["oidc"] = map[string]interface{}{
		"client": make(map[string]interface{}),
		"issuer": "issuer",
	}
	templateData.Values["nats"] = map[string]interface{}{
		"subjects": make(map[string]interface{}),
		"refs":     make(map[string]interface{}),
	}

	// Add GPU configuration
	// 添加 GPU 配置
	templateData.Values["GPU"] = make(map[string]interface{})

	// Add Helm standard template variables
	// 添加 Helm 标准模板变量
	templateData.Release = map[string]interface{}{
		"Name":      task.AppName,
		"Namespace": fmt.Sprintf("%s-%s", task.AppName, task.UserID),
		"Service":   "Helm",
	}

	// Add Chart information if available
	// 如果可用，添加 Chart 信息
	if sourceInfo, ok := task.ChartData["source_info"].(map[string]interface{}); ok {
		templateData.Chart = map[string]interface{}{
			"Name":    sourceInfo["chart_name"],
			"Version": sourceInfo["chart_version"],
		}
	} else {
		templateData.Chart = map[string]interface{}{
			"Name":    task.AppName,
			"Version": task.AppVersion,
		}
	}

	log.Printf("Template data prepared - Admin: %s, User: %s, Release.Namespace: %s",
		adminUsername, task.UserID, templateData.Release["Namespace"])

	// Debug the condition used in OlaresManifest.yaml
	// 调试 OlaresManifest.yaml 中使用的条件
	if bflMap, ok := templateData.Values["bfl"].(map[string]interface{}); ok {
		if username, exists := bflMap["username"]; exists {
			log.Printf("Debug condition - admin: '%v', bfl.username: '%v', equal: %v",
				adminUsername, username, adminUsername == username)
		}
	}

	return templateData, nil
}

// renderOlaresManifest finds and renders the OlaresManifest.yaml file
// renderOlaresManifest 查找并渲染 OlaresManifest.yaml 文件
func (s *RenderedChartStep) renderOlaresManifest(chartFiles map[string]*ChartFile, templateData *TemplateData) (string, error) {
	// Find OlaresManifest.yaml file
	// 查找 OlaresManifest.yaml 文件
	var manifestFile *ChartFile
	for path, file := range chartFiles {
		if strings.HasSuffix(strings.ToLower(path), "olaresmanifest.yaml") ||
			strings.HasSuffix(strings.ToLower(path), "olaresmanifest.yml") {
			manifestFile = file
			break
		}
	}

	if manifestFile == nil {
		return "", fmt.Errorf("OlaresManifest.yaml not found in chart package")
	}

	log.Printf("Found OlaresManifest at: %s", manifestFile.Name)

	// Render the manifest template
	// 渲染manifest模板
	renderedContent, err := s.renderTemplate(string(manifestFile.Content), templateData)
	if err != nil {
		return "", fmt.Errorf("failed to render OlaresManifest.yaml: %w", err)
	}

	return renderedContent, nil
}

// renderChartPackage renders all template files in the chart package
// renderChartPackage 渲染chart包中的所有模板文件
func (s *RenderedChartStep) renderChartPackage(chartFiles map[string]*ChartFile, templateData *TemplateData) (map[string]string, error) {
	renderedFiles := make(map[string]string)

	for path, file := range chartFiles {
		if file.IsDir {
			continue
		}

		// Check if file should be rendered (YAML files and certain others)
		// 检查文件是否应该被渲染（YAML文件和某些其他文件）
		if s.shouldRenderFile(path) {
			log.Printf("Rendering file: %s", path)
			renderedContent, err := s.renderTemplate(string(file.Content), templateData)
			if err != nil {
				log.Printf("Warning: failed to render %s: %v", path, err)
				// Store original content if rendering fails
				// 如果渲染失败则存储原始内容
				renderedFiles[path] = string(file.Content)
			} else {
				renderedFiles[path] = renderedContent
			}
		} else {
			// Store non-template files as-is
			// 按原样存储非模板文件
			renderedFiles[path] = string(file.Content)
		}
	}

	log.Printf("Rendered %d files in chart package", len(renderedFiles))
	return renderedFiles, nil
}

// shouldRenderFile determines if a file should be rendered as a template
// shouldRenderFile 确定文件是否应该作为模板渲染
func (s *RenderedChartStep) shouldRenderFile(filePath string) bool {
	lowerPath := strings.ToLower(filePath)

	// Render YAML files, manifest files, and configuration files
	// 渲染YAML文件、manifest文件和配置文件
	renderExtensions := []string{
		".yaml", ".yml", ".json", ".toml",
		".conf", ".config", ".properties",
	}

	for _, ext := range renderExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}

	// Skip binary files and certain file types
	// 跳过二进制文件和某些文件类型
	skipExtensions := []string{
		".tar", ".gz", ".zip", ".tgz",
		".png", ".jpg", ".jpeg", ".gif", ".svg",
		".exe", ".bin", ".so", ".dll",
	}

	for _, ext := range skipExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return false
		}
	}

	// Check for template markers in file content (basic heuristic)
	// 检查文件内容中的模板标记（基本启发式）
	return strings.Contains(filePath, "templates/") ||
		strings.Contains(lowerPath, "manifest")
}

// renderTemplate renders a template string with the given data
// renderTemplate 使用给定数据渲染模板字符串
func (s *RenderedChartStep) renderTemplate(templateContent string, data *TemplateData) (string, error) {
	// Check if content actually contains template syntax
	// 检查内容是否确实包含模板语法
	if !strings.Contains(templateContent, "{{") {
		return templateContent, nil
	}

	// Log template data for debugging (limit output size)
	// 记录模板数据用于调试（限制输出大小）
	log.Printf("Template rendering - Starting template execution")
	if adminVal, exists := data.Values["admin"]; exists {
		log.Printf("Template rendering - admin value: %v", adminVal)
	}
	if bflVal, exists := data.Values["bfl"]; exists {
		log.Printf("Template rendering - bfl value: %+v", bflVal)
	}
	if data.Release != nil {
		log.Printf("Template rendering - Release: %+v", data.Release)
	}
	if data.Chart != nil {
		log.Printf("Template rendering - Chart: %+v", data.Chart)
	}

	// Show a preview of template content for debugging
	// 显示模板内容的预览用于调试
	// preview := templateContent
	// if len(preview) > 200 {
	// 	preview = preview[:200] + "..."
	// }
	// log.Printf("Template content preview: %s", preview)

	// Create template with custom functions (similar to Helm)
	// 创建带有自定义函数的模板（类似于Helm）
	tmpl, err := template.New("chart").
		Option("missingkey=zero"). // Use zero value for missing keys (more forgiving)
		Funcs(s.getTemplateFunctions()).
		Parse(templateContent)
	if err != nil {
		log.Printf("Template parsing failed - Error: %v", err)
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute template
	// 执行模板
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("Template execution failed - Error: %v", err)
		log.Printf("Template execution failed - Available data keys: Values=%v, Release=%v, Chart=%v",
			getMapKeys(data.Values), getMapKeys(data.Release), getMapKeys(data.Chart))

		// Try with missing key as zero value to provide more helpful error info
		// 尝试使用零值处理缺失键以提供更有用的错误信息
		tmplZero, _ := template.New("chart-zero").
			Option("missingkey=zero").
			Funcs(s.getTemplateFunctions()).
			Parse(templateContent)

		var bufZero bytes.Buffer
		if errZero := tmplZero.Execute(&bufZero, data); errZero == nil {
			log.Printf("Template would succeed with missing keys as zero values")
		}

		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	renderedContent := buf.String()
	log.Printf("Template rendered successfully - Original length: %d, Rendered length: %d",
		len(templateContent), len(renderedContent))

	return renderedContent, nil
}

// getTemplateFunctions returns template functions for rendering
// getTemplateFunctions 返回用于渲染的模板函数
func (s *RenderedChartStep) getTemplateFunctions() template.FuncMap {
	// Helper function for boolean conversion
	// 布尔值转换的辅助函数
	toBoolHelper := func(v interface{}) bool {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val != ""
		case int:
			return val != 0
		case float64:
			return val != 0
		case nil:
			return false
		default:
			return true
		}
	}

	return template.FuncMap{
		// Basic functions
		// 基础函数
		"default": func(defaultValue interface{}, value interface{}) interface{} {
			if value == nil || value == "" {
				return defaultValue
			}
			return value
		},
		"empty": func(value interface{}) bool {
			return value == nil || value == "" || value == 0
		},
		"required": func(warn string, val interface{}) (interface{}, error) {
			if val == nil || val == "" {
				return val, fmt.Errorf(warn)
			}
			return val, nil
		},

		// String functions
		// 字符串函数
		"quote": func(str string) string {
			return fmt.Sprintf(`"%s"`, str)
		},
		"squote": func(str string) string {
			return fmt.Sprintf(`'%s'`, str)
		},
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"title": strings.Title,
		"untitle": func(str string) string {
			if len(str) == 0 {
				return str
			}
			return strings.ToLower(str[:1]) + str[1:]
		},
		"trim": strings.TrimSpace,
		"trimAll": func(cutset, s string) string {
			return strings.Trim(s, cutset)
		},
		"trimPrefix": func(prefix, s string) string {
			return strings.TrimPrefix(s, prefix)
		},
		"trimSuffix": func(suffix, s string) string {
			return strings.TrimSuffix(s, suffix)
		},
		"replace": func(old, new, src string) string {
			return strings.ReplaceAll(src, old, new)
		},
		"split": func(sep, s string) []string {
			return strings.Split(s, sep)
		},
		"join": func(sep string, elems []interface{}) string {
			strs := make([]string, len(elems))
			for i, elem := range elems {
				strs[i] = fmt.Sprintf("%v", elem)
			}
			return strings.Join(strs, sep)
		},
		"contains": func(substr, str string) bool {
			return strings.Contains(str, substr)
		},
		"hasPrefix": func(prefix, s string) bool {
			return strings.HasPrefix(s, prefix)
		},
		"hasSuffix": func(suffix, s string) bool {
			return strings.HasSuffix(s, suffix)
		},

		// Formatting functions
		// 格式化函数
		"indent": func(spaces int, text string) string {
			pad := strings.Repeat(" ", spaces)
			return pad + strings.Replace(text, "\n", "\n"+pad, -1)
		},
		"nindent": func(spaces int, text string) string {
			pad := strings.Repeat(" ", spaces)
			return "\n" + pad + strings.Replace(text, "\n", "\n"+pad, -1)
		},
		"toYaml": func(v interface{}) string {
			data, err := yaml.Marshal(v)
			if err != nil {
				return ""
			}
			return strings.TrimSuffix(string(data), "\n")
		},
		"toJson": func(v interface{}) string {
			data, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			return string(data)
		},
		"toPrettyJson": func(v interface{}) string {
			data, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return ""
			}
			return string(data)
		},

		// Type conversion functions
		// 类型转换函数
		"toString": func(v interface{}) string {
			return fmt.Sprintf("%v", v)
		},
		"toInt": func(v interface{}) int {
			if i, ok := v.(int); ok {
				return i
			}
			return 0
		},
		"toBool": toBoolHelper,

		// Conditional functions
		// 条件函数
		"eq": func(a, b interface{}) bool { return a == b },
		"ne": func(a, b interface{}) bool { return a != b },
		"and": func(args ...interface{}) bool {
			for _, arg := range args {
				if !toBoolHelper(arg) {
					return false
				}
			}
			return true
		},
		"or": func(args ...interface{}) bool {
			for _, arg := range args {
				if toBoolHelper(arg) {
					return true
				}
			}
			return false
		},
		"not": func(a interface{}) bool { return !toBoolHelper(a) },

		// List functions
		// 列表函数
		"list": func(items ...interface{}) []interface{} {
			return items
		},
		"first": func(list []interface{}) interface{} {
			if len(list) == 0 {
				return nil
			}
			return list[0]
		},
		"last": func(list []interface{}) interface{} {
			if len(list) == 0 {
				return nil
			}
			return list[len(list)-1]
		},
		"has": func(needle interface{}, haystack []interface{}) bool {
			for _, item := range haystack {
				if item == needle {
					return true
				}
			}
			return false
		},
	}
}

// getMapKeys returns the keys of a map for debugging purposes
// getMapKeys 返回map的键用于调试目的
func getMapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TemplateData holds data for rendering templates
// TemplateData 保存用于渲染模板的数据
type TemplateData struct {
	Values  map[string]interface{} `yaml:"Values" json:"Values"`
	Release map[string]interface{} `yaml:"Release" json:"Release"`
	Chart   map[string]interface{} `yaml:"Chart" json:"Chart"`
}

// ChartFile represents a file within the chart package
// ChartFile 表示chart包中的文件
type ChartFile struct {
	Name     string
	Content  []byte
	IsDir    bool
	Mode     os.FileMode
	Modified time.Time
}

// AdminUsernameResponse represents the response structure for admin username API
// AdminUsernameResponse 管理员用户名 API 响应结构
type AdminUsernameResponse struct {
	Code int `json:"code"`
	Data struct {
		Username string `json:"username"`
	} `json:"data"`
}

// saveRenderedChart saves the rendered chart files to the specified directory structure
// saveRenderedChart 将渲染后的chart文件保存到指定的目录结构
func (s *RenderedChartStep) saveRenderedChart(task *HydrationTask, renderedChart map[string]string, renderedManifest string) error {
	// Validate and clean path components to prevent invalid directory names
	// 验证并清理路径组件以防止无效的目录名
	userID := s.cleanPathComponent(task.UserID, "admin")
	sourceID := s.cleanPathComponent(task.SourceID, "default-source")
	appName := s.cleanPathComponent(task.AppName, "unknown-app")
	appVersion := s.cleanPathComponent(task.AppVersion, "unknown-version")

	// Log original values for debugging
	// 记录原始值用于调试
	log.Printf("Original task values - UserID: %s, SourceID: %s, AppName: %s, AppVersion: %s",
		task.UserID, task.SourceID, task.AppName, task.AppVersion)
	log.Printf("Cleaned path components - UserID: %s, SourceID: %s, AppName: %s, AppVersion: %s",
		userID, sourceID, appName, appVersion)

	// Build directory path: charts/{username}/{source name}/{app name}-{version}/
	// 构建目录路径：charts/{username}/{source name}/{app name}-{version}/
	chartDir := filepath.Join("charts", userID, sourceID, fmt.Sprintf("%s-%s", appName, appVersion))

	// Create directory if it doesn't exist
	// 如果目录不存在则创建
	if err := os.MkdirAll(chartDir, 0755); err != nil {
		return fmt.Errorf("failed to create chart directory: %w", err)
	}

	log.Printf("Saving rendered chart to directory: %s", chartDir)

	// Save rendered OlaresManifest.yaml
	// 保存渲染后的 OlaresManifest.yaml
	manifestPath := filepath.Join(chartDir, "OlaresManifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(renderedManifest), 0644); err != nil {
		return fmt.Errorf("failed to save rendered manifest: %w", err)
	}
	log.Printf("Saved rendered OlaresManifest.yaml to: %s", manifestPath)

	// Save all rendered chart files
	// 保存所有渲染后的chart文件
	for filePath, content := range renderedChart {
		// Create subdirectories if needed
		// 如果需要则创建子目录
		fullPath := filepath.Join(chartDir, filePath)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: failed to create subdirectory %s: %v", dir, err)
			continue
		}

		// Write file content
		// 写入文件内容
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			log.Printf("Warning: failed to save file %s: %v", fullPath, err)
			continue
		}
	}

	// Store the rendered chart directory in task data
	// 在任务数据中存储渲染chart目录
	task.ChartData["rendered_chart_dir"] = chartDir

	log.Printf("Successfully saved %d rendered files to: %s", len(renderedChart), chartDir)
	return nil
}

// cleanPathComponent cleans a path component by removing invalid characters
// cleanPathComponent 通过删除无效字符来清理路径组件
func (s *RenderedChartStep) cleanPathComponent(component, fallback string) string {
	if component == "" {
		return fallback
	}

	// Remove or replace invalid path characters
	// 删除或替换无效路径字符
	cleaned := strings.ReplaceAll(component, ":", "-")
	cleaned = strings.ReplaceAll(cleaned, "/", "-")
	cleaned = strings.ReplaceAll(cleaned, "\\", "-")
	cleaned = strings.ReplaceAll(cleaned, "*", "-")
	cleaned = strings.ReplaceAll(cleaned, "?", "-")
	cleaned = strings.ReplaceAll(cleaned, "<", "-")
	cleaned = strings.ReplaceAll(cleaned, ">", "-")
	cleaned = strings.ReplaceAll(cleaned, "|", "-")
	cleaned = strings.ReplaceAll(cleaned, "\"", "-")

	// Trim spaces and dots from ends
	// 修剪两端的空格和点
	cleaned = strings.Trim(cleaned, " .")

	// If cleaned component is empty, use fallback
	// 如果清理后的组件为空，使用回退值
	if cleaned == "" {
		return fallback
	}

	// Limit length to prevent excessively long directory names
	// 限制长度以防止过长的目录名
	if len(cleaned) > 100 {
		cleaned = cleaned[:100]
	}

	return cleaned
}

// updatePendingDataRenderedPackage updates the RenderedPackage field in AppInfoLatestPendingData
// updatePendingDataRenderedPackage 更新AppInfoLatestPendingData中的RenderedPackage字段
func (s *RenderedChartStep) updatePendingDataRenderedPackage(task *HydrationTask, chartDir string) error {
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
		log.Printf("User %s not found in cache, skipping RenderedPackage update", task.UserID)
		return nil
	}

	// Find the corresponding pending data and update RenderedPackage
	// 查找对应的待处理数据并更新RenderedPackage
	for i, pendingData := range userData.Sources[task.SourceID].AppInfoLatestPending {
		if s.isTaskForPendingDataRendered(task, pendingData) {
			log.Printf("Updating RenderedPackage for pending data at index %d: %s", i, chartDir)
			userData.Sources[task.SourceID].AppInfoLatestPending[i].RenderedPackage = chartDir
			log.Printf("Successfully updated RenderedPackage for app: %s", task.AppID)
			return nil
		}
	}

	log.Printf("No matching pending data found for task %s, skipping RenderedPackage update", task.ID)
	return nil
}

// isTaskForPendingDataRendered checks if the current task corresponds to the pending data
// isTaskForPendingDataRendered 检查当前任务是否对应待处理数据
func (s *RenderedChartStep) isTaskForPendingDataRendered(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
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
