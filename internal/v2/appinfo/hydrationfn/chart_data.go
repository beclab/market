package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"market/internal/v2/utils"
	"os"
)

// prepareTemplateData prepares the template data for chart rendering
func (s *RenderedChartStep) prepareTemplateData(ctx context.Context, task *HydrationTask) (*TemplateData, error) {
	templateData := &TemplateData{
		Values:       make(map[string]interface{}),
		Release:      make(map[string]interface{}),
		Chart:        make(map[string]interface{}),
		Capabilities: make(map[string]interface{}),
	}

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
			"Name":    fmt.Sprintf("%v", sourceInfo["chart_name"]),
			"Version": fmt.Sprintf("%v", sourceInfo["chart_version"]),
		}
	} else {
		templateData.Chart = map[string]interface{}{
			"Name":    fmt.Sprintf("%v", task.AppName),
			"Version": fmt.Sprintf("%v", task.AppVersion),
		}
	}

	// Add Capabilities information for Helm template compatibility
	templateData.Capabilities = map[string]interface{}{
		"KubeVersion": map[string]interface{}{
			"Major": "1",
			"Minor": "28",
		},
		"APIVersions": map[string]interface{}{
			"Has": func(apiVersion string) bool {
				// Simplified version check - in production this would check actual API versions
				return true
			},
		},
		"Supports": map[string]interface{}{
			"CRD": func(apiVersion string) bool {
				// Simplified CRD support check
				return true
			},
		},
	}

	log.Printf("Template data prepared - Admin: %s, User: %s, Release.Namespace: %s",
		adminUsername, task.UserID, templateData.Release["Namespace"])

	// Log Capabilities information for debugging
	if kubeVersion, ok := templateData.Capabilities["KubeVersion"].(map[string]interface{}); ok {
		log.Printf("Template data - KubeVersion: %s.%s", kubeVersion["Major"], kubeVersion["Minor"])
	}

	// Debug the condition used in OlaresManifest.yaml
	if bflMap, ok := templateData.Values["bfl"].(map[string]interface{}); ok {
		if username, exists := bflMap["username"]; exists {
			log.Printf("Debug condition - admin: '%v', bfl.username: '%v', equal: %v",
				adminUsername, username, adminUsername == username)
		}
	}

	// Log summary of available template data
	log.Printf("Template data summary - Values keys: %v, Release keys: %v, Chart keys: %v, Capabilities keys: %v",
		getMapKeys(templateData.Values), getMapKeys(templateData.Release), getMapKeys(templateData.Chart), getMapKeys(templateData.Capabilities))

	if v, ok := mergedValues["nameOverride"]; ok {
		mergedValues["nameOverride"] = fmt.Sprintf("%v", v)
	}

	return templateData, nil
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
