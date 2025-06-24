package hydrationfn

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

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
