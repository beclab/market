package hydrationfn

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CustomParamsUpdateStep handles updating custom parameters
type CustomParamsUpdateStep struct {
	stepName string
}

// NewCustomParamsUpdateStep creates a new custom parameters update step
func NewCustomParamsUpdateStep() *CustomParamsUpdateStep {
	return &CustomParamsUpdateStep{
		stepName: "CustomParamsUpdate",
	}
}

// GetStepName returns the name of the step
func (s *CustomParamsUpdateStep) GetStepName() string {
	return s.stepName
}

// CanSkip determines if this step can be skipped
func (s *CustomParamsUpdateStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// if task.AppData == nil {
	// 	return true
	// }

	// values, ok := task.AppData["values"]
	// if !ok || values == nil {
	// 	return true
	// }

	return false
}

// Execute performs the custom parameters update
func (s *CustomParamsUpdateStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Starting custom parameters update for app: %s", task.AppID)

	// 1. Get the original chart package path
	rawPackage := task.SourceChartURL
	if rawPackage == "" {
		return fmt.Errorf("source chart path is empty for app: %s", task.AppID)
	}

	// Handle file:// protocol URL path
	if strings.HasPrefix(rawPackage, "file://") {
		rawPackage = strings.TrimPrefix(rawPackage, "file://")
	}

	log.Printf("Debug: Raw package path: %s", rawPackage)

	// Check if file exists
	fileInfo, err := os.Stat(rawPackage)
	if err != nil {
		return fmt.Errorf("chart file does not exist or is not accessible: %v", err)
	}

	// Check file size
	if fileInfo.Size() == 0 {
		return fmt.Errorf("chart file is empty")
	}

	// Check file permissions
	if fileInfo.Mode()&0400 == 0 {
		return fmt.Errorf("chart file is not readable")
	}

	// Check file type
	if !strings.HasSuffix(rawPackage, ".tgz") && !strings.HasSuffix(rawPackage, ".tar.gz") {
		log.Printf("Warning: Chart file may not be in correct format: %s", rawPackage)
	}

	// 2. Create temporary directory for extraction and modification
	tempDir, err := ioutil.TempDir("", "chart-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 3. Extract chart package to temporary directory
	extractDir := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create extract directory: %v", err)
	}

	if err := s.extractChart(rawPackage, extractDir); err != nil {
		return fmt.Errorf("failed to extract chart: %v", err)
	}

	// 4. Get custom parameters and update if available
	if values, ok := task.AppData["values"]; ok && values != nil {
		log.Printf("Found custom values, updating chart parameters")
		if err := s.updateChartValues(extractDir, values); err != nil {
			return fmt.Errorf("failed to update chart values: %v", err)
		}
	} else {
		log.Printf("No custom values found, skipping parameter update")
	}

	// 5. Repackage chart
	version := task.AppVersion
	if version == "" {
		version = "latest"
	}
	newChartName := fmt.Sprintf("%s-%s", task.AppName, version)

	// Get CHART_ROOT environment variable
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// Build new chart path
	newChartPath := filepath.Join(chartRoot, task.UserID, task.SourceID, newChartName, fmt.Sprintf("%s.tgz", newChartName))
	log.Printf("Debug: New chart will be saved to: %s", newChartPath)

	// Ensure target directory exists
	if err := os.MkdirAll(filepath.Dir(newChartPath), 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %v", err)
	}

	if err := s.packageChart(extractDir, newChartPath); err != nil {
		return fmt.Errorf("failed to package chart: %v", err)
	}

	// 6. Update package path in task
	task.ChartData["local_path"] = newChartPath

	log.Printf("Custom parameters update completed for app: %s, new chart path: %s", task.AppID, newChartPath)
	return nil
}

// extractChart
func (s *CustomParamsUpdateStep) extractChart(chartPath, targetDir string) error {

	log.Printf("Debug: Extracting chart from %s to %s", chartPath, targetDir)
	cmd := exec.Command("tar", "-xzf", chartPath, "-C", targetDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract chart: %v, output: %s", err, string(output))
	}
	log.Printf("Debug: Chart extracted successfully")
	return nil
}

// updateChartValues
func (s *CustomParamsUpdateStep) updateChartValues(chartDir string, values interface{}) error {
	// Walk through all yaml files in the chart directory
	err := filepath.Walk(chartDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process yaml files
		if !info.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			// Read file content
			content, err := ioutil.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %v", path, err)
			}

			// Update file content
			updatedContent, err := s.updateYamlContent(string(content), values)
			if err != nil {
				return fmt.Errorf("failed to update content in file %s: %v", path, err)
			}

			// Write back to file
			if err := ioutil.WriteFile(path, []byte(updatedContent), info.Mode()); err != nil {
				return fmt.Errorf("failed to write updated content to file %s: %v", path, err)
			}
		}
		return nil
	})

	return err
}

// updateYamlContent
func (s *CustomParamsUpdateStep) updateYamlContent(content string, values interface{}) (string, error) {
	// TODO: Implement YAML content update logic
	// 1. Parse YAML content
	// 2. Update values according to the provided values
	// 3. Regenerate YAML content
	return content, nil
}

// packageChart
func (s *CustomParamsUpdateStep) packageChart(sourceDir, targetPath string) error {

	cmd := exec.Command("tar", "-czf", targetPath, "-C", sourceDir, ".")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to package chart: %v", err)
	}
	return nil
}
