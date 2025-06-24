package hydrationfn

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

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
