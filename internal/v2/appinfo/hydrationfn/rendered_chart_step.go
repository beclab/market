package hydrationfn

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"market/internal/v2/settings"
	"market/internal/v2/types"
	"market/internal/v2/utils"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-resty/resty/v2"
	"gopkg.in/yaml.v3"
)

// RenderedChartStep represents the step to verify and fetch rendered chart package
type RenderedChartStep struct {
	client *resty.Client
}

// NewRenderedChartStep creates a new rendered chart step
func NewRenderedChartStep() *RenderedChartStep {
	return &RenderedChartStep{
		client: resty.New(),
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

	// Step 1: Check and clean existing rendered directory if needed
	if err := s.checkAndCleanExistingRenderedDirectory(ctx, task); err != nil {
		return fmt.Errorf("failed to check and clean existing rendered directory: %w", err)
	}

	// Load and extract chart package from local file
	chartFiles, err := s.loadAndExtractChart(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to load and extract chart: %w", err)
	}

	// Store chart files in task data for later use
	task.ChartData["chart_files"] = chartFiles

	// Prepare template data for rendering
	templateData, err := s.prepareTemplateData(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to prepare template data: %w", err)
	}

	// Find and render OlaresManifest.yaml
	renderedManifest, err := s.renderOlaresManifest(chartFiles, templateData)
	if err != nil {
		return fmt.Errorf("failed to render OlaresManifest.yaml: %w", err)
	}

	// Extract entrances from rendered OlaresManifest.yaml and update templateData
	entrances, err := s.extractEntrancesFromManifest(renderedManifest)
	if err != nil {
		log.Printf("Warning: failed to extract entrances from rendered OlaresManifest.yaml: %v", err)
	} else {
		domainMap := map[string]string{}
		for name, entrance := range entrances {
			if entranceMap, ok := entrance.(map[string]interface{}); ok {
				if domain, ok := entranceMap["domain"].(string); ok {
					domainMap[name] = domain
				}
			}
		}
		templateData.Values["domain"] = domainMap
		log.Printf("Extracted %d entrances from rendered OlaresManifest.yaml", len(entrances))
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
		// Check for the specific gzip header error and delete the corrupted file
		if errors.Is(err, gzip.ErrHeader) {
			log.Printf("Invalid gzip header for file %s. Deleting it to allow re-download.", localPath)
			if removeErr := os.Remove(localPath); removeErr != nil {
				// Log failure to delete, but still return the original extraction error
				log.Printf("Warning: failed to delete corrupted chart file %s: %v", localPath, removeErr)
			}
		}
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

// fetchRenderValues fetches rendering values from the app-service without caching
func (s *RenderedChartStep) fetchRenderValues(ctx context.Context, task *HydrationTask) (map[string]interface{}, error) {
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

	log.Println("Successfully fetched render values from app-service.")
	return values, nil
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

// mergeValues performs a deep merge of two maps.
// Keys in the overlay map take precedence.
func (s *RenderedChartStep) mergeValues(base, overlay map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = make(map[string]interface{})
	}

	for key, overlayValue := range overlay {
		baseValue, ok := base[key]
		if ok {
			baseMap, baseIsMap := baseValue.(map[string]interface{})
			overlayMap, overlayIsMap := overlayValue.(map[string]interface{})
			if baseIsMap && overlayIsMap {
				base[key] = s.mergeValues(baseMap, overlayMap)
				continue
			}
		}
		base[key] = overlayValue
	}
	return base
}

// extractChartValues extracts default values from the chart's values.yaml file.
func (s *RenderedChartStep) extractChartValues(task *HydrationTask) (map[string]interface{}, error) {
	chartFilesVal, ok := task.ChartData["chart_files"]
	if !ok {
		log.Println("No chart files found in task data, cannot extract values.yaml.")
		return make(map[string]interface{}), nil
	}

	chartFiles, ok := chartFilesVal.(map[string]*ChartFile)
	if !ok {
		return nil, fmt.Errorf("chart_files in task data is not of expected type map[string]*ChartFile")
	}

	var valuesFile *ChartFile
	minDepth := -1

	// Find values.yaml at the lowest depth (root of the chart)
	for path, file := range chartFiles {
		if file.IsDir {
			continue
		}

		baseName := filepath.Base(path)
		if baseName == "values.yaml" || baseName == "values.yml" {
			depth := strings.Count(path, "/")
			if valuesFile == nil || depth < minDepth {
				valuesFile = file
				minDepth = depth
			}
		}
	}

	if valuesFile == nil {
		log.Println("values.yaml not found in chart package. Proceeding without default values.")
		return make(map[string]interface{}), nil
	}

	log.Printf("Loading default values from: %s", valuesFile.Name)
	var values map[string]interface{}
	if err := yaml.Unmarshal(valuesFile.Content, &values); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", valuesFile.Name, err)
	}

	return values, nil
}

// prepareTemplateData prepares the template data for chart rendering
func (s *RenderedChartStep) prepareTemplateData(ctx context.Context, task *HydrationTask) (*TemplateData, error) {
	templateData := &TemplateData{}

	// Load default values from chart's values.yaml
	defaultValues, err := s.extractChartValues(task)
	if err != nil {
		// Log as a warning and continue with empty defaults
		log.Printf("Warning: failed to extract chart values: %v", err)
		defaultValues = make(map[string]interface{})
	}

	// Fetch override values from app service, which act as overrides
	overrideValues, err := s.fetchRenderValues(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("could not prepare template data, failed to fetch render values: %w", err)
	}

	// Merge override values into default values
	mergedValues := s.mergeValues(defaultValues, overrideValues)
	templateData.Values = mergedValues

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

	// domain/entrances will be filled by Execute, not handled here

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
	// Find OlaresManifest.yaml file at the lowest depth
	var manifestFile *ChartFile
	var minDepth = -1

	for path, file := range chartFiles {
		if file.IsDir {
			continue
		}

		// Check if the file is an Olares manifest file.
		if strings.HasSuffix(strings.ToLower(path), "olaresmanifest.yaml") ||
			strings.HasSuffix(strings.ToLower(path), "olaresmanifest.yml") {

			// Calculate the directory depth of the file.
			depth := strings.Count(path, "/")

			// If it's the first one found, or has a shallower depth, select it.
			if manifestFile == nil || depth < minDepth {
				manifestFile = file
				minDepth = depth
			}
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
		"quote": func(str string) string {
			return fmt.Sprintf("%q", str)
		},
		"squote": func(str string) string {
			return fmt.Sprintf("'%s'", str)
		},
		"replace": func(old, new, src string) string {
			return strings.ReplaceAll(src, old, new)
		},
		"split": func(sep, s string) []string {
			return strings.Split(s, sep)
		},
		"splitList": func(sep, str string) []string {
			if str == "" {
				return []string{}
			}
			return strings.Split(str, sep)
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
		"toFloat64": func(v interface{}) float64 {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case string:
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					return f
				}
			}
			return 0.0
		},
		"float64": func(v interface{}) float64 {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case string:
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					return f
				}
			}
			return 0.0
		},
		"int": func(v interface{}) int {
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case string:
				if i, err := strconv.Atoi(val); err == nil {
					return i
				}
			}
			return 0
		},
		"add": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av + bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av + bv
				}
			}
			return 0
		},
		"sub": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av - bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av - bv
				}
			}
			return 0
		},
		"mul": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av * bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av * bv
				}
			}
			return 0
		},
		"div": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok && bv != 0 {
					return av / bv
				}
			case float64:
				if bv, ok := b.(float64); ok && bv != 0 {
					return av / bv
				}
			}
			return 0
		},
		"mod": func(a, b interface{}) interface{} {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok && bv != 0 {
					return av % bv
				}
			}
			return 0
		},
		"gt": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av > bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av > bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av > bv
				}
			}
			return false
		},
		"gte": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av >= bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av >= bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av >= bv
				}
			}
			return false
		},
		"lt": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av < bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av < bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av < bv
				}
			}
			return false
		},
		"lte": func(a, b interface{}) bool {
			switch av := a.(type) {
			case int:
				if bv, ok := b.(int); ok {
					return av <= bv
				}
			case float64:
				if bv, ok := b.(float64); ok {
					return av <= bv
				}
			case string:
				if bv, ok := b.(string); ok {
					return av <= bv
				}
			}
			return false
		},
		"cat": func(args ...interface{}) string {
			var result strings.Builder
			for _, arg := range args {
				result.WriteString(fmt.Sprintf("%v", arg))
			}
			return result.String()
		},
		"repeat": func(count int, str string) string {
			return strings.Repeat(str, count)
		},
		"has": func(needle interface{}, haystack []interface{}) bool {
			for _, item := range haystack {
				if item == needle {
					return true
				}
			}
			return false
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

		// Random functions
		"randAlphaNum": func(count int) string {
			const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},
		"randAlpha": func(count int) string {
			const charset = "abcdefghijklmnopqrstuvwxyz"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},
		"randNumeric": func(count int) string {
			const charset = "0123456789"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},
		"randAscii": func(count int) string {
			const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
			result := make([]byte, count)
			for i := range result {
				result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
				time.Sleep(1 * time.Nanosecond) // Ensure different values
			}
			return string(result)
		},

		// Utility functions
		"b64enc": func(str string) string {
			return fmt.Sprintf("%s", str) // Simplified base64 encoding
		},
		"b64dec": func(str string) string {
			return str // Simplified base64 decoding
		},
		"sha256sum": func(str string) string {
			// Simplified SHA256 hash - in production you'd use crypto/sha256
			return fmt.Sprintf("sha256-%s", str)
		},
		"genCA": func(cn string, days int) map[string]string {
			// Simplified certificate generation for templating.
			// In a real implementation, this would generate a real CA certificate.
			return map[string]string{
				"Cert": fmt.Sprintf("-----BEGIN CERTIFICATE-----\n# CA Cert for %s, valid for %d days\n-----END CERTIFICATE-----\n", cn, days),
				"Key":  fmt.Sprintf("-----BEGIN PRIVATE KEY-----\n# CA Key for %s\n-----END PRIVATE KEY-----\n", cn),
			}
		},
		"genSelfSignedCert": func(cn string, ips, sans interface{}, days int) map[string]string {
			// Simplified self-signed certificate generation for templating.
			return map[string]string{
				"Cert": fmt.Sprintf("-----BEGIN CERTIFICATE-----\n# Self-signed Cert for %s, valid for %d days\n-----END CERTIFICATE-----\n", cn, days),
				"Key":  fmt.Sprintf("-----BEGIN PRIVATE KEY-----\n# Self-signed Key for %s\n-----END PRIVATE KEY-----\n", cn),
			}
		},
		"genSignedCert": func(cn string, ips, sans interface{}, days int, ca interface{}) map[string]string {
			// Simplified signed certificate generation for templating.
			return map[string]string{
				"Cert": fmt.Sprintf("-----BEGIN CERTIFICATE-----\n# Signed Cert for %s, valid for %d days\n-----END CERTIFICATE-----\n", cn, days),
				"Key":  fmt.Sprintf("-----BEGIN PRIVATE KEY-----\n# Signed Key for %s\n-----END PRIVATE KEY-----\n", cn),
			}
		},
		"trunc": func(length int, str string) string {
			if len(str) <= length {
				return str
			}
			return str[:length]
		},
		"nospace": func(str string) string {
			return strings.ReplaceAll(str, " ", "")
		},
		"compact": func(str string) string {
			return strings.ReplaceAll(str, " ", "")
		},
		"initial": func(str string) string {
			if len(str) == 0 {
				return str
			}
			return strings.ToUpper(str[:1])
		},
		"wordwrap": func(width int, str string) string {
			// Simplified word wrapping
			if len(str) <= width {
				return str
			}
			return str[:width] + "\n" + str[width:]
		},

		// Kubernetes functions
		"lookup": func(apiVersion, kind, namespace, name string) interface{} {
			// Simplified lookup function - returns empty map for now
			// In a real implementation, this would query the Kubernetes API
			return map[string]interface{}{}
		},
		"include": func(name string, data interface{}) (string, error) {
			// Simplified include function - returns empty string for now
			// In a real implementation, this would include another template
			return "", nil
		},
		"tpl": func(template string, data interface{}) (string, error) {
			// Simplified tpl function - returns the template as-is for now
			// In a real implementation, this would render the template
			return template, nil
		},
		"fail": func(msg string) (string, error) {
			return "", fmt.Errorf(msg)
		},
		"hasKey": func(m map[string]interface{}, key string) bool {
			if m == nil {
				return false
			}
			_, ok := m[key]
			return ok
		},
		"index": s.customIndexFunc,

		// Regex functions (implementing some common Sprig functions)
		"regexMatch": func(regex string, s string) bool {
			return regexp.MustCompile(regex).MatchString(s)
		},
		"regexFindAll": func(regex string, s string, n int) []string {
			return regexp.MustCompile(regex).FindAllString(s, n)
		},
		"regexReplaceAll": func(regex, replacement, src string) string {
			return regexp.MustCompile(regex).ReplaceAllString(src, replacement)
		},
		"regexSplit": func(regex string, s string, n int) []string {
			return regexp.MustCompile(regex).Split(s, n)
		},
	}
}

// indirect is a helper function to get the value from a pointer.
func indirect(v reflect.Value) (rv reflect.Value, isNil bool) {
	for ; v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface; v = v.Elem() {
		if v.IsNil() {
			return v, true
		}
	}
	return v, false
}

// intValue is a helper function to convert a reflect.Value to an int.
func intValue(v reflect.Value) (int, error) {
	if v, isNil := indirect(v); isNil || !v.IsValid() {
		return 0, fmt.Errorf("index of nil pointer")
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(v.Uint()), nil
	case reflect.Invalid:
		return 0, fmt.Errorf("index of invalid value")
	default:
		return 0, fmt.Errorf("can't index item with %s", v.Type())
	}
}

// customIndexFunc is a custom implementation of the 'index' template function.
// It supports indexing slices/arrays with string representations of integers (e.g., "_0", "1").
func (s *RenderedChartStep) customIndexFunc(item interface{}, indices ...interface{}) (interface{}, error) {
	v := reflect.ValueOf(item)
	if !v.IsValid() {
		return nil, fmt.Errorf("index of untyped nil")
	}

	for _, i := range indices {
		index := reflect.ValueOf(i)
		if !index.IsValid() {
			return nil, fmt.Errorf("index of untyped nil")
		}

		var isNil bool
		if v, isNil = indirect(v); isNil {
			return nil, fmt.Errorf("index of nil pointer")
		}

		switch v.Kind() {
		case reflect.Array, reflect.Slice, reflect.String:
			var x int
			var err error

			if index.Kind() == reflect.String {
				strIndex := index.String()
				// Handle string indices like "_0", "1", etc. for slices
				cleanIndex := strings.TrimPrefix(strIndex, "_")
				x, err = strconv.Atoi(cleanIndex)
				if err != nil {
					// Fallback to default behavior if parsing fails
					x, err = intValue(index)
				}
			} else {
				x, err = intValue(index)
			}

			if err != nil {
				return nil, err
			}
			if x < 0 || x >= v.Len() {
				// To be more forgiving, return nil instead of error for out-of-bounds access.
				// This mimics behavior of some other template systems.
				log.Printf("Warning: index out of range [%d] with length %d, returning nil", x, v.Len())
				return nil, nil
			}
			v = v.Index(x)

		case reflect.Map:
			if !index.Type().AssignableTo(v.Type().Key()) {
				return nil, fmt.Errorf("%s is not a key type for %s", index.Type(), v.Type())
			}
			v = v.MapIndex(index)
		case reflect.Invalid:
			return nil, fmt.Errorf("index of invalid value")
		default:
			return nil, fmt.Errorf("can't index item of type %s", v.Type())
		}
	}

	if !v.IsValid() {
		// This can happen if a map lookup returned the zero value.
		// For example, indexing a map with a key that doesn't exist.
		return nil, nil
	}

	return v.Interface(), nil
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

	// If cleaned component is empty, use fallback (if provided)
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

// isTaskForPendingDataRendered checks if the current task corresponds to the pending data for rendered package
func (s *RenderedChartStep) isTaskForPendingDataRendered(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
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

// checkAndCleanExistingRenderedDirectory checks and cleans the existing rendered directory if needed
func (s *RenderedChartStep) checkAndCleanExistingRenderedDirectory(ctx context.Context, task *HydrationTask) error {
	// Get base storage path from environment variable
	basePath := os.Getenv("CHART_ROOT")
	if basePath == "" {
		return fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// Validate required path components
	if task.UserID == "" {
		return fmt.Errorf("task UserID is empty")
	}
	if task.SourceID == "" {
		return fmt.Errorf("task SourceID is empty")
	}
	if task.AppName == "" {
		return fmt.Errorf("task AppName is empty")
	}
	if task.AppVersion == "" {
		return fmt.Errorf("task AppVersion is empty")
	}

	// Clean path components to prevent invalid directory names
	userID := s.cleanPathComponent(task.UserID, "")
	sourceID := s.cleanPathComponent(task.SourceID, "")
	appName := s.cleanPathComponent(task.AppName, "")
	appVersion := s.cleanPathComponent(task.AppVersion, "")

	// Check if any component is empty after cleaning
	if userID == "" {
		return fmt.Errorf("UserID is invalid after cleaning")
	}
	if sourceID == "" {
		return fmt.Errorf("SourceID is invalid after cleaning")
	}
	if appName == "" {
		return fmt.Errorf("AppName is invalid after cleaning")
	}
	if appVersion == "" {
		return fmt.Errorf("AppVersion is invalid after cleaning")
	}

	// Build directory path: {basePath}/{username}/{source name}/{app name}-{version}/
	chartDir := filepath.Join(basePath, userID, sourceID, fmt.Sprintf("%s-%s", appName, appVersion))

	// Check if directory exists
	if _, err := os.Stat(chartDir); err == nil {
		log.Printf("Existing rendered directory found: %s", chartDir)

		// Check if the app exists in the Latest list in cache
		if s.isAppInLatestList(task) {
			log.Printf("App %s exists in Latest list, keeping existing rendered directory", task.AppID)
			return nil
		}

		log.Printf("App %s not found in Latest list, cleaning existing rendered directory", task.AppID)

		// Clean the directory
		if err := os.RemoveAll(chartDir); err != nil {
			return fmt.Errorf("failed to clean existing rendered directory: %w", err)
		}

		log.Printf("Existing rendered directory cleaned successfully")
	} else if os.IsNotExist(err) {
		log.Printf("Existing rendered directory not found, proceeding with new rendering")
	} else {
		return fmt.Errorf("failed to check existing rendered directory: %w", err)
	}

	return nil
}

// isAppInLatestList checks if the app exists in the Latest list in cache
func (s *RenderedChartStep) isAppInLatestList(task *HydrationTask) bool {
	if task.Cache == nil {
		log.Printf("Warning: Cache is nil, cannot check Latest list")
		return false
	}

	// Lock cache for thread-safe access
	task.Cache.Mutex.RLock()
	defer task.Cache.Mutex.RUnlock()

	// Check if user exists in cache
	userData, exists := task.Cache.Users[task.UserID]
	if !exists {
		log.Printf("User %s not found in cache", task.UserID)
		return false
	}

	// Check if source exists in user data
	sourceData, exists := userData.Sources[task.SourceID]
	if !exists {
		log.Printf("Source %s not found for user %s", task.SourceID, task.UserID)
		return false
	}

	// Check if app exists in AppInfoLatest list
	for _, latestApp := range sourceData.AppInfoLatest {
		if latestApp == nil {
			continue
		}

		// Compare by app name (primary identifier)
		if s.compareAppIdentifiers(latestApp, task.AppName) {
			log.Printf("Found matching app in Latest list: %s", task.AppName)
			return true
		}
	}

	log.Printf("App %s not found in Latest list", task.AppName)
	return false
}

// compareAppIdentifiers compares app identifiers between latest app data and task
func (s *RenderedChartStep) compareAppIdentifiers(latestApp *types.AppInfoLatestData, taskAppName string) bool {
	if latestApp == nil {
		return false
	}

	// Check RawData first
	if latestApp.RawData != nil {
		if latestApp.RawData.Name == taskAppName ||
			latestApp.RawData.AppID == taskAppName ||
			latestApp.RawData.ID == taskAppName {
			return true
		}
	}

	// Check AppInfo.AppEntry
	if latestApp.AppInfo != nil && latestApp.AppInfo.AppEntry != nil {
		if latestApp.AppInfo.AppEntry.Name == taskAppName ||
			latestApp.AppInfo.AppEntry.AppID == taskAppName ||
			latestApp.AppInfo.AppEntry.ID == taskAppName {
			return true
		}
	}

	// Check AppSimpleInfo
	if latestApp.AppSimpleInfo != nil {
		if latestApp.AppSimpleInfo.AppName == taskAppName ||
			latestApp.AppSimpleInfo.AppID == taskAppName {
			return true
		}
	}

	return false
}

// extractEntrancesFromChartFiles extracts entrances from OlaresManifest.yaml in chart files
func (s *RenderedChartStep) extractEntrancesFromChartFiles(files map[string]*ChartFile) (map[string]interface{}, error) {
	entries := make(map[string]interface{})

	// Iterate through files to find OlaresManifest.yaml
	for filePath, file := range files {
		if strings.HasSuffix(strings.ToLower(filePath), "olaresmanifest.yaml") ||
			strings.HasSuffix(strings.ToLower(filePath), "olaresmanifest.yml") {
			log.Printf("Found OlaresManifest file: %s", filePath)

			// Extract entrances from file content
			manifestEntries, err := s.extractEntrancesFromManifest(string(file.Content))
			if err != nil {
				return nil, fmt.Errorf("failed to extract entrances from file %s: %w", filePath, err)
			}

			entries = manifestEntries
			log.Printf("Successfully extracted %d entrances from %s", len(entries), filePath)
			break
		}
	}

	if len(entries) == 0 {
		log.Printf("No entrances found in OlaresManifest.yaml files")
	}

	return entries, nil
}

// extractEntrancesFromManifest extracts entrances from OlaresManifest.yaml
func (s *RenderedChartStep) extractEntrancesFromManifest(manifestStr string) (map[string]interface{}, error) {
	entries := make(map[string]interface{})

	// Parse the YAML content
	var manifest map[string]interface{}
	if err := yaml.Unmarshal([]byte(manifestStr), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	// Navigate to entrances section
	if entrances, ok := manifest["entrances"]; ok {
		switch entrances := entrances.(type) {
		case []interface{}:
			// Handle case where entrances is a list of maps
			for _, entrance := range entrances {
				if entranceMap, ok := entrance.(map[string]interface{}); ok {
					if name, ok := entranceMap["name"].(string); ok {
						entries[name] = entranceMap
						log.Printf("Found entrance (from list): %s", name)
					}
				}
			}
		case map[string]interface{}:
			// Handle case where entrances is a map
			for name, entrance := range entrances {
				if entranceMap, ok := entrance.(map[string]interface{}); ok {
					// Ensure the name from the key is added to the map if not present
					if _, hasName := entranceMap["name"]; !hasName {
						entranceMap["name"] = name
					}
					entries[name] = entranceMap
					log.Printf("Found entrance (from map): %s", name)
				}
			}
		}
	}

	return entries, nil
}
