package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"
)

// ImageAnalysisStep represents the step to analyze Docker images in rendered chart
type ImageAnalysisStep struct {
	imageRegex *regexp.Regexp
}

// NewImageAnalysisStep creates a new image analysis step
func NewImageAnalysisStep() *ImageAnalysisStep {
	// Improved regex to match Docker image references in YAML files
	// Remove the strict end-of-line requirement and make it more flexible
	imageRegex := regexp.MustCompile(`(?i)image:\s*["\']?([a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9](?::[a-zA-Z0-9._-]+)?(?:@sha256:[a-fA-F0-9]{64})?)["\']?`)

	return &ImageAnalysisStep{
		imageRegex: imageRegex,
	}
}

// GetStepName returns the name of this step
func (s *ImageAnalysisStep) GetStepName() string {
	return "Docker Image Analysis"
}

// CanSkip determines if this step can be skipped
func (s *ImageAnalysisStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// Check if image analysis already exists
	if renderedDir, exists := task.ChartData["rendered_chart_dir"]; exists {
		if dir, ok := renderedDir.(string); ok {
			imageAnalysisPath := filepath.Join(dir, "image-analysis.json")
			if _, err := os.Stat(imageAnalysisPath); err == nil {
				log.Printf("Image analysis already exists for app: %s, skipping", task.AppID)
				return true
			}
		}
	}
	return false
}

// Execute performs the Docker image analysis
func (s *ImageAnalysisStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing image analysis step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Get rendered chart directory
	renderedDir, exists := task.ChartData["rendered_chart_dir"]
	if !exists {
		return fmt.Errorf("rendered chart directory not found in task data")
	}

	chartDir, ok := renderedDir.(string)
	if !ok {
		return fmt.Errorf("invalid rendered chart directory type")
	}

	// Read all rendered files and extract Docker images
	images, err := s.extractImagesFromDirectory(chartDir)
	if err != nil {
		return fmt.Errorf("failed to extract images from rendered chart: %w", err)
	}

	if len(images) == 0 {
		log.Printf("No Docker images found in rendered chart for app: %s", task.AppID)
		// Still create an empty analysis file
		emptyAnalysis := &types.ImageAnalysisResult{
			AppID:       task.AppID,
			UserID:      task.UserID,
			SourceID:    task.SourceID,
			AnalyzedAt:  time.Now(),
			TotalImages: 0,
			Images:      make(map[string]*types.ImageInfo),
		}
		return s.saveImageAnalysis(chartDir, emptyAnalysis)
	}

	log.Printf("Found %d unique Docker images in rendered chart for app: %s", len(images), task.AppID)

	// Analyze each image to get detailed information
	imageInfos := make(map[string]*types.ImageInfo)
	for _, imageName := range images {
		log.Printf("Analyzing Docker image: %s", imageName)

		imageInfo, err := s.analyzeImage(ctx, imageName)
		if err != nil {
			log.Printf("Warning: failed to analyze image %s: %v", imageName, err)
			// Create basic info even if analysis fails
			imageInfo = &types.ImageInfo{
				Name:         imageName,
				AnalyzedAt:   time.Now(),
				Status:       "analysis_failed",
				ErrorMessage: err.Error(),
			}
		}

		imageInfos[imageName] = imageInfo
	}

	// Create analysis result
	analysisResult := &types.ImageAnalysisResult{
		AppID:       task.AppID,
		UserID:      task.UserID,
		SourceID:    task.SourceID,
		AnalyzedAt:  time.Now(),
		TotalImages: len(images),
		Images:      imageInfos,
	}

	// Save analysis result to file
	if err := s.saveImageAnalysis(chartDir, analysisResult); err != nil {
		return fmt.Errorf("failed to save image analysis: %w", err)
	}

	// Store analysis result in task data
	task.ChartData["image_analysis"] = analysisResult

	log.Printf("Image analysis completed for app: %s, analyzed %d images", task.AppID, len(images))
	return nil
}

// extractImagesFromDirectory extracts all Docker image references from rendered chart files
func (s *ImageAnalysisStep) extractImagesFromDirectory(chartDir string) ([]string, error) {
	imageSet := make(map[string]bool)

	// Walk through all files in the chart directory
	err := filepath.Walk(chartDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only process YAML files
		if !s.isYAMLFile(path) {
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: failed to read file %s: %v", path, err)
			return nil
		}

		// Extract images from file content
		images := s.extractImagesFromContent(string(content))
		for _, image := range images {
			imageSet[image] = true
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk chart directory: %w", err)
	}

	// Convert set to slice
	images := make([]string, 0, len(imageSet))
	for image := range imageSet {
		images = append(images, image)
	}

	return images, nil
}

// isYAMLFile checks if a file is a YAML file
func (s *ImageAnalysisStep) isYAMLFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".yaml" || ext == ".yml"
}

// extractImagesFromContent extracts Docker image references from file content
func (s *ImageAnalysisStep) extractImagesFromContent(content string) []string {
	// Add debug logging to help diagnose image extraction issues
	log.Printf("Debug: Extracting images from content (length: %d)", len(content))

	// Show a preview of content for debugging
	// preview := content
	// if len(preview) > 500 {
	// 	preview = preview[:500] + "..."
	// }
	// log.Printf("Debug: Content preview: %s", preview)

	matches := s.imageRegex.FindAllStringSubmatch(content, -1)
	log.Printf("Debug: Found %d regex matches", len(matches))

	imageSet := make(map[string]bool)
	for i, match := range matches {
		log.Printf("Debug: Match %d: %v", i, match)
		if len(match) > 1 {
			image := strings.TrimSpace(match[1])
			log.Printf("Debug: Extracted image candidate: '%s'", image)

			if image != "" && s.isValidImageName(image) {
				// Clean up the image name
				cleanImage := s.cleanImageName(image)
				log.Printf("Debug: Cleaned image: '%s'", cleanImage)
				if cleanImage != "" {
					imageSet[cleanImage] = true
					log.Printf("Debug: Added image to set: '%s'", cleanImage)
				}
			} else {
				log.Printf("Debug: Image '%s' failed validation", image)
			}
		}
	}

	images := make([]string, 0, len(imageSet))
	for image := range imageSet {
		images = append(images, image)
	}

	log.Printf("Debug: Final extracted images: %v", images)
	return images
}

// isValidImageName validates if a string is a valid Docker image name
func (s *ImageAnalysisStep) isValidImageName(imageName string) bool {
	// Basic validation for Docker image names
	if imageName == "" {
		return false
	}

	// Skip obvious template variables and placeholders
	if strings.Contains(imageName, "{{") || strings.Contains(imageName, "}}") {
		return false
	}
	if strings.Contains(imageName, "${") || strings.Contains(imageName, "}") {
		return false
	}

	// Skip common non-image strings
	invalidNames := []string{
		"-", "https", "http", "version", "latest", "stable", "tag", "name",
		"image", "repository", "registry", "docker", "container", "pod",
	}

	lowerImage := strings.ToLower(imageName)
	for _, invalid := range invalidNames {
		if lowerImage == invalid {
			return false
		}
	}

	// Must contain at least one character that's not a template marker
	if strings.HasPrefix(imageName, "$") {
		return false
	}

	// Basic Docker image name format validation
	// Should contain valid characters only
	validImageRegex := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9](?::[a-zA-Z0-9._-]+)?(?:@sha256:[a-fA-F0-9]{64})?$`)
	if !validImageRegex.MatchString(imageName) {
		return false
	}

	// Must have at least one valid component
	parts := strings.Split(strings.Split(imageName, ":")[0], "/")
	if len(parts) == 0 {
		return false
	}

	// Check that components are not too short or invalid
	for _, part := range parts {
		if len(part) < 1 || part == "." || part == ".." {
			return false
		}
	}

	return true
}

// cleanImageName cleans and normalizes an image name
func (s *ImageAnalysisStep) cleanImageName(imageName string) string {
	// Remove quotes and extra whitespace
	cleaned := strings.Trim(imageName, `"' `)

	// Handle registry prefixes
	if strings.HasPrefix(cleaned, "docker.io/") {
		// docker.io is the default registry, can be simplified
		cleaned = strings.TrimPrefix(cleaned, "docker.io/")
	}

	// Remove any trailing slashes
	cleaned = strings.TrimSuffix(cleaned, "/")

	return cleaned
}

// analyzeImage analyzes a single Docker image and returns detailed information
func (s *ImageAnalysisStep) analyzeImage(ctx context.Context, imageName string) (*types.ImageInfo, error) {
	// Clean and validate image name
	cleanedName := s.cleanImageName(imageName)
	if !s.isValidImageName(cleanedName) {
		return nil, fmt.Errorf("invalid image name: %s", imageName)
	}

	// Initialize image info
	imageInfo := &types.ImageInfo{
		Name:       cleanedName,
		AnalyzedAt: time.Now(),
		Status:     "not_downloaded",
		Nodes:      make([]*types.NodeInfo, 0),
	}

	// Check if this is a private image
	// if s.isPrivateImage(cleanedName) {
	// 	imageInfo.Status = "private_registry"
	// 	imageInfo.ErrorMessage = "Private registry image, analysis limited"
	// 	log.Printf("Detected private registry image: %s", cleanedName)
	// 	return imageInfo, nil
	// }

	// Get Docker image info from registry
	dockerImageInfo, err := utils.GetDockerImageInfo(imageName)
	if err != nil {
		imageInfo.Status = "registry_error"
		imageInfo.ErrorMessage = err.Error()
		log.Printf("Failed to get Docker image info for %s: %v", imageName, err)

		// For public images, try to get layer progress anyway
		if !s.isPrivateImage(imageName) {
			s.analyzeLocalLayers(imageInfo, imageName)
		}
		return imageInfo, nil
	}

	// Fill basic image information
	imageInfo.Tag = dockerImageInfo.Tag
	imageInfo.Architecture = dockerImageInfo.Architecture
	imageInfo.TotalSize = dockerImageInfo.TotalSize
	imageInfo.CreatedAt = dockerImageInfo.CreatedAt

	// Calculate total layer count across all nodes
	totalLayerCount := 0
	for _, node := range dockerImageInfo.Nodes {
		totalLayerCount += node.LayerCount
	}
	imageInfo.LayerCount = totalLayerCount

	// Check if we're in production environment using environment variable
	isProduction := !utils.IsDevelopmentEnvironment()

	if isProduction {
		// Production environment: use offset-based analysis with node information
		log.Printf("Production environment detected, using offset-based analysis for %s", imageName)
		s.analyzeLocalLayersWithOffsetAndNodes(imageInfo, imageName)
	} else {
		// Development environment: use traditional local layer checking
		log.Printf("Development environment detected, using traditional local layer checking for %s", imageName)

		// Process each node from DockerImageInfo
		for _, dockerNode := range dockerImageInfo.Nodes {
			nodeInfo := &types.NodeInfo{
				NodeName:     dockerNode.NodeName,
				Architecture: dockerNode.Architecture,
				Variant:      dockerNode.Variant,
				OS:           dockerNode.OS,
				Layers:       make([]*types.LayerInfo, 0, len(dockerNode.Layers)),
				LayerCount:   len(dockerNode.Layers),
			}

			var nodeDownloaded int64
			var nodeDownloadedLayers int

			for _, layer := range dockerNode.Layers {
				layerInfo := &types.LayerInfo{
					Digest:    layer.Digest,
					Size:      layer.Size,
					MediaType: layer.MediaType,
					Offset:    layer.Offset, // Add offset from API response
				}

				// Get layer download progress
				if layerProgress, err := utils.GetLayerDownloadProgress(layer.Digest); err == nil {
					layerInfo.Downloaded = layerProgress.Downloaded
					layerInfo.Progress = layerProgress.Progress
					layerInfo.LocalPath = layerProgress.LocalPath

					if layerProgress.Downloaded {
						nodeDownloadedLayers++
						nodeDownloaded += layer.Size
					}
				} else {
					log.Printf("Warning: failed to get layer progress for %s: %v", layer.Digest, err)
				}

				nodeInfo.Layers = append(nodeInfo.Layers, layerInfo)
			}

			nodeInfo.DownloadedSize = nodeDownloaded
			nodeInfo.DownloadedLayers = nodeDownloadedLayers
			nodeInfo.TotalSize = dockerNode.TotalSize

			imageInfo.Nodes = append(imageInfo.Nodes, nodeInfo)
			imageInfo.DownloadedSize += nodeDownloaded
			imageInfo.DownloadedLayers += nodeDownloadedLayers
		}

		// Calculate download progress
		if imageInfo.TotalSize > 0 {
			imageInfo.DownloadProgress = float64(imageInfo.DownloadedSize) / float64(imageInfo.TotalSize) * 100
		}

		// Determine overall status
		if imageInfo.DownloadedLayers == imageInfo.LayerCount {
			imageInfo.Status = "fully_downloaded"
		} else if imageInfo.DownloadedLayers > 0 {
			imageInfo.Status = "partially_downloaded"
		} else {
			imageInfo.Status = "not_downloaded"
		}
	}

	return imageInfo, nil
}

// analyzeLocalLayers attempts to analyze local layers even when registry access fails
func (s *ImageAnalysisStep) analyzeLocalLayers(imageInfo *types.ImageInfo, imageName string) {
	// Try to extract digest from image name if it contains one
	if strings.Contains(imageName, "@sha256:") {
		parts := strings.Split(imageName, "@")
		if len(parts) == 2 {
			digest := parts[1]
			if layerProgress, err := utils.GetLayerDownloadProgress(digest); err == nil {
				layer := &types.LayerInfo{
					Digest:     digest,
					Downloaded: layerProgress.Downloaded,
					Progress:   layerProgress.Progress,
					LocalPath:  layerProgress.LocalPath,
					Size:       layerProgress.Size,
				}

				// Create a single node for local analysis
				nodeInfo := &types.NodeInfo{
					NodeName:         "local",
					Layers:           []*types.LayerInfo{layer},
					LayerCount:       1,
					DownloadedSize:   0,
					DownloadedLayers: 0,
				}

				if layer.Downloaded {
					nodeInfo.DownloadedLayers = 1
					nodeInfo.DownloadedSize = layer.Size
					imageInfo.Status = "fully_downloaded"
				}

				imageInfo.Nodes = append(imageInfo.Nodes, nodeInfo)
				imageInfo.LayerCount = 1
				if layer.Downloaded {
					imageInfo.DownloadedLayers = 1
					imageInfo.DownloadedSize = layer.Size
				}
			}
		}
	}
}

// analyzeLocalLayersWithOffset analyzes local layers using offset and size information from API
func (s *ImageAnalysisStep) analyzeLocalLayersWithOffset(imageInfo *types.ImageInfo, layers []utils.LayerInfo) {
	log.Printf("Analyzing local layers with offset information for image: %s", imageInfo.Name)

	// For production environment, we need to get the actual API response to get node information
	// Since we don't have node information in the current layers, we'll create a single node
	// In a real implementation, you would need to modify the API call to return node information

	nodeInfo := &types.NodeInfo{
		NodeName:   "production",
		Layers:     make([]*types.LayerInfo, 0, len(layers)),
		LayerCount: len(layers),
	}

	var totalDownloaded int64
	var downloadedLayers int

	for _, layer := range layers {
		// Use offset-based progress calculation for production environment
		if layerProgress, err := utils.GetLayerDownloadProgressByOffset(layer.Digest, layer.Offset, layer.Size); err == nil {
			typesLayer := &types.LayerInfo{
				Digest:     layer.Digest,
				Size:       layer.Size,
				MediaType:  layer.MediaType,
				Offset:     layer.Offset,
				Downloaded: layerProgress.Downloaded,
				Progress:   layerProgress.Progress,
				LocalPath:  layerProgress.LocalPath,
			}

			if layerProgress.Downloaded {
				downloadedLayers++
				totalDownloaded += layer.Size
			}

			nodeInfo.Layers = append(nodeInfo.Layers, typesLayer)
			log.Printf("Layer %s: offset=%d, size=%d, progress=%d%%, downloaded=%v",
				layer.Digest, layer.Offset, layer.Size, layerProgress.Progress, layerProgress.Downloaded)
		} else {
			log.Printf("Warning: failed to calculate layer progress for %s: %v", layer.Digest, err)
			// Create basic layer info even if progress calculation fails
			typesLayer := &types.LayerInfo{
				Digest:     layer.Digest,
				Size:       layer.Size,
				MediaType:  layer.MediaType,
				Offset:     layer.Offset,
				Downloaded: false,
				Progress:   0,
			}
			nodeInfo.Layers = append(nodeInfo.Layers, typesLayer)
		}
	}

	nodeInfo.DownloadedSize = totalDownloaded
	nodeInfo.DownloadedLayers = downloadedLayers
	nodeInfo.TotalSize = imageInfo.TotalSize

	imageInfo.Nodes = append(imageInfo.Nodes, nodeInfo)
	imageInfo.LayerCount = len(layers)
	imageInfo.DownloadedSize = totalDownloaded
	imageInfo.DownloadedLayers = downloadedLayers

	// Calculate download progress
	if imageInfo.TotalSize > 0 {
		imageInfo.DownloadProgress = float64(totalDownloaded) / float64(imageInfo.TotalSize) * 100
	}

	// Determine overall status
	if downloadedLayers == len(layers) {
		imageInfo.Status = "fully_downloaded"
	} else if downloadedLayers > 0 {
		imageInfo.Status = "partially_downloaded"
	} else {
		imageInfo.Status = "not_downloaded"
	}

	log.Printf("Image %s analysis completed: %d/%d layers downloaded, %.2f%% progress",
		imageInfo.Name, downloadedLayers, len(layers), imageInfo.DownloadProgress)
}

// analyzeLocalLayersWithOffsetAndNodes analyzes local layers using offset and size information from API with node information
func (s *ImageAnalysisStep) analyzeLocalLayersWithOffsetAndNodes(imageInfo *types.ImageInfo, imageName string) {
	log.Printf("Analyzing local layers with offset and node information for image: %s", imageName)

	// Get the complete API response with node information
	apiResponse, err := utils.GetImageInfoAPIResponse(imageName)
	if err != nil {
		log.Printf("Warning: failed to get API response with node information: %v", err)
		// Fallback to single node analysis
		s.analyzeLocalLayersWithOffset(imageInfo, []utils.LayerInfo{})
		return
	}

	var totalDownloaded int64
	var totalDownloadedLayers int
	var totalLayers int

	// Process each node from the API response
	for _, nodeImage := range apiResponse.Images {
		nodeInfo := &types.NodeInfo{
			NodeName:     nodeImage.Node,
			Architecture: nodeImage.Architecture,
			Variant:      nodeImage.Variant,
			OS:           nodeImage.OS,
			Layers:       make([]*types.LayerInfo, 0, len(nodeImage.LayersData)),
			LayerCount:   len(nodeImage.LayersData),
		}

		var nodeDownloaded int64
		var nodeDownloadedLayers int

		for _, layer := range nodeImage.LayersData {
			// Use offset-based progress calculation for production environment
			if layerProgress, err := utils.GetLayerDownloadProgressByOffset(layer.Digest, layer.Offset, layer.Size); err == nil {
				typesLayer := &types.LayerInfo{
					Digest:     layer.Digest,
					Size:       layer.Size,
					MediaType:  layer.MediaType,
					Offset:     layer.Offset,
					Downloaded: layerProgress.Downloaded,
					Progress:   layerProgress.Progress,
					LocalPath:  layerProgress.LocalPath,
				}

				if layerProgress.Downloaded {
					nodeDownloadedLayers++
					nodeDownloaded += layer.Size
				}

				nodeInfo.Layers = append(nodeInfo.Layers, typesLayer)
				log.Printf("Node %s, Layer %s: offset=%d, size=%d, progress=%d%%, downloaded=%v",
					nodeImage.Node, layer.Digest, layer.Offset, layer.Size, layerProgress.Progress, layerProgress.Downloaded)
			} else {
				log.Printf("Warning: failed to calculate layer progress for %s: %v", layer.Digest, err)
				// Create basic layer info even if progress calculation fails
				typesLayer := &types.LayerInfo{
					Digest:     layer.Digest,
					Size:       layer.Size,
					MediaType:  layer.MediaType,
					Offset:     layer.Offset,
					Downloaded: false,
					Progress:   0,
				}
				nodeInfo.Layers = append(nodeInfo.Layers, typesLayer)
			}
		}

		nodeInfo.DownloadedSize = nodeDownloaded
		nodeInfo.DownloadedLayers = nodeDownloadedLayers
		nodeInfo.TotalSize = nodeInfo.DownloadedSize // This will be updated with actual total size

		// Calculate node total size
		var nodeTotalSize int64
		for _, layer := range nodeImage.LayersData {
			nodeTotalSize += layer.Size
		}
		nodeInfo.TotalSize = nodeTotalSize

		imageInfo.Nodes = append(imageInfo.Nodes, nodeInfo)
		totalDownloaded += nodeDownloaded
		totalDownloadedLayers += nodeDownloadedLayers
		totalLayers += len(nodeImage.LayersData)
	}

	imageInfo.LayerCount = totalLayers
	imageInfo.DownloadedSize = totalDownloaded
	imageInfo.DownloadedLayers = totalDownloadedLayers

	// Calculate download progress across all nodes
	if imageInfo.TotalSize > 0 {
		imageInfo.DownloadProgress = float64(totalDownloaded) / float64(imageInfo.TotalSize) * 100
	}

	// Determine overall status
	if totalDownloadedLayers == totalLayers {
		imageInfo.Status = "fully_downloaded"
	} else if totalDownloadedLayers > 0 {
		imageInfo.Status = "partially_downloaded"
	} else {
		imageInfo.Status = "not_downloaded"
	}

	log.Printf("Image %s analysis completed: %d/%d layers downloaded across %d nodes, %.2f%% progress",
		imageInfo.Name, totalDownloadedLayers, totalLayers, len(imageInfo.Nodes), imageInfo.DownloadProgress)
}

// saveImageAnalysis saves the image analysis result to a JSON file
func (s *ImageAnalysisStep) saveImageAnalysis(chartDir string, analysis *types.ImageAnalysisResult) error {
	analysisPath := filepath.Join(chartDir, "image-analysis.json")

	// Convert to JSON with pretty formatting
	jsonData, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal image analysis to JSON: %w", err)
	}

	// Write to file
	if err := os.WriteFile(analysisPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write image analysis file: %w", err)
	}

	log.Printf("Image analysis saved to: %s", analysisPath)
	return nil
}

// isPrivateImage checks if an image is from a private registry
func (s *ImageAnalysisStep) isPrivateImage(imageName string) bool {
	// Known private registry patterns
	privatePatterns := []string{
		"aboveos/",          // aboveos private registry
		"private.registry.", // common private registry pattern
		"registry.local/",   // local registry
		"localhost:",        // localhost registry
		"127.0.0.1:",        // localhost IP
		"harbor.",           // Harbor registry
		"nexus.",            // Nexus registry
	}

	for _, pattern := range privatePatterns {
		if strings.Contains(imageName, pattern) {
			return true
		}
	}

	// Check for custom registry domains (contains dots but not docker.io)
	if strings.Contains(imageName, ".") && !strings.Contains(imageName, "docker.io") {
		parts := strings.Split(imageName, "/")
		if len(parts) > 1 && strings.Contains(parts[0], ".") {
			return true
		}
	}

	return false
}
