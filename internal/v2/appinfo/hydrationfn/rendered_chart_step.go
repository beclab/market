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
	"sync"
	"text/template"
	"time"

	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

// RenderValuesCache holds cached rendering values
type RenderValuesCache struct {
	mutex     sync.Mutex
	values    map[string]interface{}
	lastFetch time.Time
	cacheTTL  time.Duration
}

// RenderedChartStep represents the step to verify and fetch rendered chart package
type RenderedChartStep struct {
	client            *resty.Client
	renderValuesCache *RenderValuesCache
}

// NewRenderedChartStep creates a new rendered chart step
func NewRenderedChartStep() *RenderedChartStep {
	return &RenderedChartStep{
		client: resty.New(),
		renderValuesCache: &RenderValuesCache{
			cacheTTL: 5 * time.Minute, // Cache for 5 minutes
		},
	}
}

// GetStepName returns the name of this step
func (s *RenderedChartStep) GetStepName() string {
	return "Rendered Chart Verification"
}

// CanSkip determines if this step can be skipped
func (s *RenderedChartStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// Skip if we already have the rendered chart URL and it's valid
	if task.RenderedChartURL != "" {
		return s.isValidChartURL(task.RenderedChartURL)
	}
	return false
}

// Execute performs the rendered chart verification and processing
func (s *RenderedChartStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing rendered chart step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Check if source chart step completed successfully
	if task.SourceChartURL == "" {
		return fmt.Errorf("source chart URL is required but not available")
	}

	// Load and extract chart package from local file
	chartFiles, err := s.loadAndExtractChart(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to load and extract chart: %w", err)
	}

	// Prepare template data for rendering
	templateData, err := s.prepareTemplateData(task)
	if err != nil {
		return fmt.Errorf("failed to prepare template data: %w", err)
	}

	// Find and render OlaresManifest.yaml
	renderedManifest, err := s.renderOlaresManifest(chartFiles, templateData)
	if err != nil {
		return fmt.Errorf("failed to render OlaresManifest.yaml: %w", err)
	}

	// Render the entire chart package
	renderedChart, err := s.renderChartPackage(chartFiles, templateData)
	if err != nil {
		return fmt.Errorf("failed to render chart package: %w", err)
	}

	// Save rendered chart to directory
	if err := s.saveRenderedChart(task, renderedChart, renderedManifest); err != nil {
		return fmt.Errorf("failed to save rendered chart: %w", err)
	}

	// Store rendered content in task
	task.ChartData["rendered_manifest"] = renderedManifest
	task.ChartData["rendered_chart"] = renderedChart
	task.ChartData["template_data"] = templateData

	// Build rendered chart URL (optional, for compatibility)
	renderConfig, _ := s.extractRenderConfig(task.AppData)
	renderedChartURL, err := s.buildRenderedChartURL(task, renderConfig)
	if err != nil {
		log.Printf("Warning: failed to build rendered chart URL: %v", err)
	} else {
		task.RenderedChartURL = renderedChartURL
	}

	// Update AppInfoLatestPendingData with rendered chart directory path
	if renderedChartDir, exists := task.ChartData["rendered_chart_dir"].(string); exists {
		if err := s.updatePendingDataRenderedPackage(task, renderedChartDir); err != nil {
			log.Printf("Warning: failed to update pending data rendered package: %v", err)
		}
	}

	log.Printf("Chart rendering completed for app: %s", task.AppID)
	return nil
}

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

// loadAndExtractChart loads and extracts the chart package from local file
func (s *RenderedChartStep) loadAndExtractChart(ctx context.Context, task *HydrationTask) (map[string]*ChartFile, error) {
	// Get local chart path from task data
	var localPath string

	// Try to get local path from ChartData first
	if localPathVal, exists := task.ChartData["local_path"]; exists {
		if path, ok := localPathVal.(string); ok {
			localPath = path
		}
	}

	// If not found, try to extract from SourceChartURL
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
	if _, err := os.Stat(localPath); err != nil {
		return nil, fmt.Errorf("chart file not found: %w", err)
	}

	// Read chart file
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chart file: %w", err)
	}

	// Extract chart files from tar.gz
	chartFiles, err := s.extractTarGz(data)
	if err != nil {
		return nil, fmt.Errorf("failed to extract chart: %w", err)
	}

	log.Printf("Successfully extracted %d files from local chart", len(chartFiles))
	return chartFiles, nil
}

// getAppServiceURL builds the app service URL from environment variables
func (s *RenderedChartStep) getAppServiceURL() (string, error) {
	host := os.Getenv("APP_SERVICE_SERVICE_HOST")
	port := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if host == "" || port == "" {
		// Fallback for local development if not set
		log.Printf("APP_SERVICE_SERVICE_HOST or APP_SERVICE_SERVICE_PORT not set, using default localhost for development")
		host = "localhost"
		port = "6755"
	}

	return fmt.Sprintf("http://%s:%s/app-service/v1/apps/oamvalues", host, port), nil
}

// deepCopyMap creates a shallow copy of a map to prevent mutation of cached data
func deepCopyMap(originalMap map[string]interface{}) map[string]interface{} {
	newMap := make(map[string]interface{}, len(originalMap))
	for k, v := range originalMap {
		newMap[k] = v
	}
	return newMap
}

// fetchRenderValues fetches rendering values from the app-service, with caching.
func (s *RenderedChartStep) fetchRenderValues(ctx context.Context, task *HydrationTask) (map[string]interface{}, error) {
	s.renderValuesCache.mutex.Lock()
	defer s.renderValuesCache.mutex.Unlock()

	if s.renderValuesCache.values != nil && time.Since(s.renderValuesCache.lastFetch) < s.renderValuesCache.cacheTTL {
		log.Println("Using cached render values.")
		return deepCopyMap(s.renderValuesCache.values), nil
	}

	appServiceURL, err := s.getAppServiceURL()
	if err != nil {
		return nil, fmt.Errorf("failed to get app service URL: %w", err)
	}

	log.Printf("Fetching render values from: %s", appServiceURL)
	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		Get(appServiceURL)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch render values from app-service: %w", err)
	}

	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("app-service returned non-2xx status: %d, body: %s", resp.StatusCode(), resp.String())
	}

	var values map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &values); err != nil {
		return nil, fmt.Errorf("failed to unmarshal render values: %w", err)
	}

	s.renderValuesCache.values = values
	s.renderValuesCache.lastFetch = time.Now()
	log.Println("Successfully fetched and cached render values.")

	return deepCopyMap(values), nil
}

// extractTarGz extracts files from a tar.gz archive
func (s *RenderedChartStep) extractTarGz(data []byte) (map[string]*ChartFile, error) {
	files := make(map[string]*ChartFile)

	// Create gzip reader
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create tar reader
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
func (s *RenderedChartStep) prepareTemplateData(task *HydrationTask) (*TemplateData, error) {
	templateData := &TemplateData{}

	// Fetch base values from app service, with caching
	values, err := s.fetchRenderValues(context.Background(), task)
	if err != nil {
		return nil, fmt.Errorf("could not prepare template data, failed to fetch render values: %w", err)
	}
	templateData.Values = values

	// Get admin username using utils function
	adminUsername, err := utils.GetAdminUsername("")
	if err != nil {
		log.Printf("Warning: failed to get admin username, using default: %v", err)
		adminUsername = "admin" // fallback to default
	}

	// Set/Override task-specific values
	templateData.Values["admin"] = adminUsername
	templateData.Values["bfl"] = map[string]interface{}{
		"username": task.UserID,
	}
	if userMap, ok := templateData.Values["user"].(map[string]interface{}); ok {
		userMap["zone"] = fmt.Sprintf("user-space-%s", task.UserID)
	} else {
		templateData.Values["user"] = map[string]interface{}{
			"zone": fmt.Sprintf("user-space-%s", task.UserID),
		}
	}

	// Add Helm standard template variables
	templateData.Release = map[string]interface{}{
		"Name":      task.AppName,
		"Namespace": fmt.Sprintf("%s-%s", task.AppName, task.UserID),
		"Service":   "Helm",
	}

	// Add Chart information if available
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
	if bflMap, ok := templateData.Values["bfl"].(map[string]interface{}); ok {
		if username, exists := bflMap["username"]; exists {
			log.Printf("Debug condition - admin: '%v', bfl.username: '%v', equal: %v",
				adminUsername, username, adminUsername == username)
		}
	}

	return templateData, nil
}

// renderOlaresManifest finds and renders the OlaresManifest.yaml file
func (s *RenderedChartStep) renderOlaresManifest(chartFiles map[string]*ChartFile, templateData *TemplateData) (string, error) {
	// Find OlaresManifest.yaml file
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
	renderedContent, err := s.renderTemplate(string(manifestFile.Content), templateData)
	if err != nil {
		return "", fmt.Errorf("failed to render OlaresManifest.yaml: %w", err)
	}

	return renderedContent, nil
}

// renderChartPackage renders all template files in the chart package
func (s *RenderedChartStep) renderChartPackage(chartFiles map[string]*ChartFile, templateData *TemplateData) (map[string]string, error) {
	renderedFiles := make(map[string]string)

	// Create a cleanup function to handle errors
	cleanup := func() {
		// Clear any large data structures
		for _, file := range chartFiles {
			file.Content = nil // Clear file content
		}
	}
	defer cleanup()

	for filePath, file := range chartFiles {
		if file.IsDir {
			continue
		}

		if !s.shouldRenderFile(filePath) {
			renderedFiles[filePath] = string(file.Content)
			continue
		}

		rendered, err := s.renderTemplate(string(file.Content), templateData)
		if err != nil {
			return nil, fmt.Errorf("failed to render file %s: %w", filePath, err)
		}

		renderedFiles[filePath] = rendered
	}

	return renderedFiles, nil
}

// shouldRenderFile determines if a file should be rendered as a template
func (s *RenderedChartStep) shouldRenderFile(filePath string) bool {
	lowerPath := strings.ToLower(filePath)

	// Render YAML files, manifest files, and configuration files
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
	return strings.Contains(filePath, "templates/") ||
		strings.Contains(lowerPath, "manifest")
}

// renderTemplate renders a template string with the given data
func (s *RenderedChartStep) renderTemplate(templateContent string, data *TemplateData) (string, error) {
	// Check if content actually contains template syntax
	if !strings.Contains(templateContent, "{{") {
		return templateContent, nil
	}

	// Log template data for debugging (limit output size)
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
	// preview := templateContent
	// if len(preview) > 200 {
	// 	preview = preview[:200] + "..."
	// }
	// log.Printf("Template content preview: %s", preview)

	// Create template with custom functions (similar to Helm)
	tmpl, err := template.New("chart").
		Option("missingkey=zero"). // Use zero value for missing keys (more forgiving)
		Funcs(s.getTemplateFunctions()).
		Parse(templateContent)
	if err != nil {
		log.Printf("Template parsing failed - Error: %v", err)
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Printf("Template execution failed - Error: %v", err)
		log.Printf("Template execution failed - Available data keys: Values=%v, Release=%v, Chart=%v",
			getMapKeys(data.Values), getMapKeys(data.Release), getMapKeys(data.Chart))

		// Try with missing key as zero value to provide more helpful error info
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
func (s *RenderedChartStep) getTemplateFunctions() template.FuncMap {
	// Helper function for boolean conversion
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
type TemplateData struct {
	Values  map[string]interface{} `yaml:"Values" json:"Values"`
	Release map[string]interface{} `yaml:"Release" json:"Release"`
	Chart   map[string]interface{} `yaml:"Chart" json:"Chart"`
}

// ChartFile represents a file within the chart package
type ChartFile struct {
	Name     string
	Content  []byte
	IsDir    bool
	Mode     os.FileMode
	Modified time.Time
}

// AdminUsernameResponse represents the response structure for admin username API
type AdminUsernameResponse struct {
	Code int `json:"code"`
	Data struct {
		Username string `json:"username"`
	} `json:"data"`
}

// saveRenderedChart saves the rendered chart files to the specified directory structure
func (s *RenderedChartStep) saveRenderedChart(task *HydrationTask, renderedChart map[string]string, renderedManifest string) error {
	// Validate and clean path components to prevent invalid directory names
	userID := s.cleanPathComponent(task.UserID, "admin")
	sourceID := s.cleanPathComponent(task.SourceID, "default-source")
	appName := s.cleanPathComponent(task.AppName, "unknown-app")
	appVersion := s.cleanPathComponent(task.AppVersion, "unknown-version")

	// Log original values for debugging
	log.Printf("Original task values - UserID: %s, SourceID: %s, AppName: %s, AppVersion: %s",
		task.UserID, task.SourceID, task.AppName, task.AppVersion)
	log.Printf("Cleaned path components - UserID: %s, SourceID: %s, AppName: %s, AppVersion: %s",
		userID, sourceID, appName, appVersion)

	// Get base storage path from environment variable, with a default for development
	basePath := os.Getenv("CHART_ROOT")

	// Build directory path: {basePath}/{username}/{source name}/{app name}-{version}/
	chartDir := filepath.Join(basePath, userID, sourceID, fmt.Sprintf("%s-%s", appName, appVersion))

	// Create directory if it doesn't exist
	if err := os.MkdirAll(chartDir, 0755); err != nil {
		return fmt.Errorf("failed to create chart directory: %w", err)
	}

	log.Printf("Saving rendered chart to directory: %s", chartDir)

	// Save rendered OlaresManifest.yaml
	manifestPath := filepath.Join(chartDir, "OlaresManifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(renderedManifest), 0644); err != nil {
		return fmt.Errorf("failed to save rendered manifest: %w", err)
	}
	log.Printf("Saved rendered OlaresManifest.yaml to: %s", manifestPath)

	// Save all rendered chart files
	for filePath, content := range renderedChart {
		// Create subdirectories if needed
		fullPath := filepath.Join(chartDir, filePath)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: failed to create subdirectory %s: %v", dir, err)
			continue
		}

		// Write file content
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			log.Printf("Warning: failed to save file %s: %v", fullPath, err)
			continue
		}
	}

	// Store the rendered chart directory in task data
	task.ChartData["rendered_chart_dir"] = chartDir

	log.Printf("Successfully saved %d rendered files to: %s", len(renderedChart), chartDir)
	return nil
}

// cleanPathComponent cleans a path component by removing invalid characters
func (s *RenderedChartStep) cleanPathComponent(component, fallback string) string {
	if component == "" {
		return fallback
	}

	// Remove or replace invalid path characters
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
	cleaned = strings.Trim(cleaned, " .")

	// If cleaned component is empty, use fallback
	if cleaned == "" {
		return fallback
	}

	// Limit length to prevent excessively long directory names
	if len(cleaned) > 100 {
		cleaned = cleaned[:100]
	}

	return cleaned
}

// updatePendingDataRenderedPackage updates the RenderedPackage field in AppInfoLatestPendingData
func (s *RenderedChartStep) updatePendingDataRenderedPackage(task *HydrationTask, chartDir string) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Lock cache for thread-safe access
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Check if user exists in cache
	userData, exists := task.Cache.Users[task.UserID]
	if !exists {
		log.Printf("User %s not found in cache, skipping RenderedPackage update", task.UserID)
		return nil
	}

	// Find the corresponding pending data and update RenderedPackage
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
func (s *RenderedChartStep) isTaskForPendingDataRendered(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		return false
	}

	taskAppID := task.AppID

	// Check if the task AppID matches the pending data's RawData
	if pendingData.RawData != nil {
		// Check if this is legacy data by looking at metadata
		if pendingData.RawData.Metadata != nil {
			// Check for legacy_data in metadata - this contains multiple apps
			if legacyData, hasLegacyData := pendingData.RawData.Metadata["legacy_data"]; hasLegacyData {
				if legacyDataMap, ok := legacyData.(map[string]interface{}); ok {
					// Check if task app ID exists in the legacy data apps
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
			if legacyRawData, hasLegacyRawData := pendingData.RawData.Metadata["legacy_raw_data"]; hasLegacyRawData {
				if legacyRawDataMap, ok := legacyRawData.(map[string]interface{}); ok {
					// Similar check for legacy raw data format
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
			if repAppID, hasRepAppID := pendingData.RawData.Metadata["representative_app_id"].(string); hasRepAppID {
				if repAppID == taskAppID {
					return true
				}
			}
		}

		// Standard checks for non-legacy data
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
