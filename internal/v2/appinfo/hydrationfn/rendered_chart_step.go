package hydrationfn

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
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

// TemplateData holds data for rendering templates
type TemplateData struct {
	Values       map[string]interface{} `yaml:"Values" json:"Values"`
	Release      map[string]interface{} `yaml:"Release" json:"Release"`
	Chart        map[string]interface{} `yaml:"Chart" json:"Chart"`
	Capabilities map[string]interface{} `yaml:"Capabilities" json:"Capabilities"`
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
