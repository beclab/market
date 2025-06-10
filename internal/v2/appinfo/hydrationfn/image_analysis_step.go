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
// ImageAnalysisStep 表示分析渲染chart中Docker镜像的步骤
type ImageAnalysisStep struct {
	imageRegex *regexp.Regexp
}

// NewImageAnalysisStep creates a new image analysis step
// NewImageAnalysisStep 创建新的镜像分析步骤
func NewImageAnalysisStep() *ImageAnalysisStep {
	// Improved regex to match Docker image references in YAML files
	// 改进的正则表达式，用于匹配YAML文件中的Docker镜像引用
	// Remove the strict end-of-line requirement and make it more flexible
	imageRegex := regexp.MustCompile(`(?i)image:\s*["\']?([a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9](?::[a-zA-Z0-9._-]+)?(?:@sha256:[a-fA-F0-9]{64})?)["\']?`)

	return &ImageAnalysisStep{
		imageRegex: imageRegex,
	}
}

// GetStepName returns the name of this step
// GetStepName 返回此步骤的名称
func (s *ImageAnalysisStep) GetStepName() string {
	return "Docker Image Analysis"
}

// CanSkip determines if this step can be skipped
// CanSkip 确定是否可以跳过此步骤
func (s *ImageAnalysisStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// Check if image analysis already exists
	// 检查镜像分析是否已存在
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
// Execute 执行Docker镜像分析
func (s *ImageAnalysisStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing image analysis step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Get rendered chart directory
	// 获取渲染chart目录
	renderedDir, exists := task.ChartData["rendered_chart_dir"]
	if !exists {
		return fmt.Errorf("rendered chart directory not found in task data")
	}

	chartDir, ok := renderedDir.(string)
	if !ok {
		return fmt.Errorf("invalid rendered chart directory type")
	}

	// Read all rendered files and extract Docker images
	// 读取所有渲染文件并提取Docker镜像
	images, err := s.extractImagesFromDirectory(chartDir)
	if err != nil {
		return fmt.Errorf("failed to extract images from rendered chart: %w", err)
	}

	if len(images) == 0 {
		log.Printf("No Docker images found in rendered chart for app: %s", task.AppID)
		// Still create an empty analysis file
		// 仍然创建一个空的分析文件
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
	// 分析每个镜像以获取详细信息
	imageInfos := make(map[string]*types.ImageInfo)
	for _, imageName := range images {
		log.Printf("Analyzing Docker image: %s", imageName)

		imageInfo, err := s.analyzeImage(ctx, imageName)
		if err != nil {
			log.Printf("Warning: failed to analyze image %s: %v", imageName, err)
			// Create basic info even if analysis fails
			// 即使分析失败也创建基本信息
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
	// 创建分析结果
	analysisResult := &types.ImageAnalysisResult{
		AppID:       task.AppID,
		UserID:      task.UserID,
		SourceID:    task.SourceID,
		AnalyzedAt:  time.Now(),
		TotalImages: len(images),
		Images:      imageInfos,
	}

	// Save analysis result to file
	// 将分析结果保存到文件
	if err := s.saveImageAnalysis(chartDir, analysisResult); err != nil {
		return fmt.Errorf("failed to save image analysis: %w", err)
	}

	// Store analysis result in task data
	// 在任务数据中存储分析结果
	task.ChartData["image_analysis"] = analysisResult

	log.Printf("Image analysis completed for app: %s, analyzed %d images", task.AppID, len(images))
	return nil
}

// extractImagesFromDirectory extracts all Docker image references from rendered chart files
// extractImagesFromDirectory 从渲染chart文件中提取所有Docker镜像引用
func (s *ImageAnalysisStep) extractImagesFromDirectory(chartDir string) ([]string, error) {
	imageSet := make(map[string]bool)

	// Walk through all files in the chart directory
	// 遍历chart目录中的所有文件
	err := filepath.Walk(chartDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// Only process YAML files
		// 只处理YAML文件
		if !s.isYAMLFile(path) {
			return nil
		}

		// Read file content
		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: failed to read file %s: %v", path, err)
			return nil
		}

		// Extract images from file content
		// 从文件内容中提取镜像
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
	// 将集合转换为切片
	images := make([]string, 0, len(imageSet))
	for image := range imageSet {
		images = append(images, image)
	}

	return images, nil
}

// isYAMLFile checks if a file is a YAML file
// isYAMLFile 检查文件是否为YAML文件
func (s *ImageAnalysisStep) isYAMLFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".yaml" || ext == ".yml"
}

// extractImagesFromContent extracts Docker image references from file content
// extractImagesFromContent 从文件内容中提取Docker镜像引用
func (s *ImageAnalysisStep) extractImagesFromContent(content string) []string {
	// Add debug logging to help diagnose image extraction issues
	// 添加调试日志以帮助诊断镜像提取问题
	log.Printf("Debug: Extracting images from content (length: %d)", len(content))

	// Show a preview of content for debugging
	// 显示内容预览用于调试
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
				// 清理镜像名称
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
// isValidImageName 验证字符串是否为有效的Docker镜像名称
func (s *ImageAnalysisStep) isValidImageName(imageName string) bool {
	// Basic validation for Docker image names
	// Docker镜像名称的基本验证
	if imageName == "" {
		return false
	}

	// Skip obvious template variables and placeholders
	// 跳过明显的模板变量和占位符
	if strings.Contains(imageName, "{{") || strings.Contains(imageName, "}}") {
		return false
	}
	if strings.Contains(imageName, "${") || strings.Contains(imageName, "}") {
		return false
	}

	// Skip common non-image strings
	// 跳过常见的非镜像字符串
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
	// 必须包含至少一个非模板标记的字符
	if strings.HasPrefix(imageName, "$") {
		return false
	}

	// Basic Docker image name format validation
	// 基本的Docker镜像名称格式验证
	// Should contain valid characters only
	validImageRegex := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]*[a-zA-Z0-9](?::[a-zA-Z0-9._-]+)?(?:@sha256:[a-fA-F0-9]{64})?$`)
	if !validImageRegex.MatchString(imageName) {
		return false
	}

	// Must have at least one valid component
	// 必须至少有一个有效组件
	parts := strings.Split(strings.Split(imageName, ":")[0], "/")
	if len(parts) == 0 {
		return false
	}

	// Check that components are not too short or invalid
	// 检查组件不能太短或无效
	for _, part := range parts {
		if len(part) < 1 || part == "." || part == ".." {
			return false
		}
	}

	return true
}

// cleanImageName cleans and normalizes an image name
// cleanImageName 清理和规范化镜像名称
func (s *ImageAnalysisStep) cleanImageName(imageName string) string {
	// Remove quotes and extra whitespace
	// 移除引号和多余空格
	cleaned := strings.Trim(imageName, `"' `)

	// Handle registry prefixes
	// 处理registry前缀
	if strings.HasPrefix(cleaned, "docker.io/") {
		// docker.io is the default registry, can be simplified
		// docker.io是默认registry，可以简化
		cleaned = strings.TrimPrefix(cleaned, "docker.io/")
	}

	// Remove any trailing slashes
	// 移除尾部斜杠
	cleaned = strings.TrimSuffix(cleaned, "/")

	return cleaned
}

// analyzeImage analyzes a single Docker image and returns detailed information
// analyzeImage 分析单个Docker镜像并返回详细信息
func (s *ImageAnalysisStep) analyzeImage(ctx context.Context, imageName string) (*types.ImageInfo, error) {
	// Clean and validate image name
	// 清理和验证镜像名称
	cleanedName := s.cleanImageName(imageName)
	if !s.isValidImageName(cleanedName) {
		return nil, fmt.Errorf("invalid image name: %s", imageName)
	}

	// Initialize image info
	// 初始化镜像信息
	imageInfo := &types.ImageInfo{
		Name:       cleanedName,
		AnalyzedAt: time.Now(),
		Status:     "not_downloaded",
	}

	// Check if this is a private image
	// 检查这是否是私人镜像
	if s.isPrivateImage(cleanedName) {
		imageInfo.Status = "private_registry"
		imageInfo.ErrorMessage = "Private registry image, analysis limited"
		log.Printf("Detected private registry image: %s", cleanedName)
		return imageInfo, nil
	}

	// Get Docker image info from registry
	// 从registry获取Docker镜像信息
	dockerImageInfo, err := utils.GetDockerImageInfo(imageName)
	if err != nil {
		imageInfo.Status = "registry_error"
		imageInfo.ErrorMessage = err.Error()
		log.Printf("Failed to get Docker image info for %s: %v", imageName, err)

		// For public images, try to get layer progress anyway
		// 对于公有镜像，仍然尝试获取层进度
		if !s.isPrivateImage(imageName) {
			s.analyzeLocalLayers(imageInfo, imageName)
		}
		return imageInfo, nil
	}

	// Fill basic image information
	// 填充基本镜像信息
	imageInfo.Tag = dockerImageInfo.Tag
	imageInfo.Architecture = dockerImageInfo.Architecture
	imageInfo.TotalSize = dockerImageInfo.TotalSize
	imageInfo.CreatedAt = dockerImageInfo.CreatedAt
	imageInfo.LayerCount = len(dockerImageInfo.Layers)

	// Analyze each layer
	// 分析每个层
	layers := make([]*types.LayerInfo, 0, len(dockerImageInfo.Layers))
	var totalDownloaded int64
	var downloadedLayers int

	for _, layer := range dockerImageInfo.Layers {
		layerInfo := &types.LayerInfo{
			Digest:    layer.Digest,
			Size:      layer.Size,
			MediaType: layer.MediaType,
		}

		// Get layer download progress
		// 获取层下载进度
		if layerProgress, err := utils.GetLayerDownloadProgress(layer.Digest); err == nil {
			layerInfo.Downloaded = layerProgress.Downloaded
			layerInfo.Progress = layerProgress.Progress
			layerInfo.LocalPath = layerProgress.LocalPath

			if layerProgress.Downloaded {
				downloadedLayers++
				totalDownloaded += layer.Size
			}
		} else {
			log.Printf("Warning: failed to get layer progress for %s: %v", layer.Digest, err)
		}

		layers = append(layers, layerInfo)
	}

	imageInfo.Layers = layers
	imageInfo.DownloadedSize = totalDownloaded
	imageInfo.DownloadedLayers = downloadedLayers

	// Calculate download progress
	// 计算下载进度
	if imageInfo.TotalSize > 0 {
		imageInfo.DownloadProgress = float64(totalDownloaded) / float64(imageInfo.TotalSize) * 100
	}

	// Determine overall status
	// 确定总体状态
	if downloadedLayers == len(layers) {
		imageInfo.Status = "fully_downloaded"
	} else if downloadedLayers > 0 {
		imageInfo.Status = "partially_downloaded"
	} else {
		imageInfo.Status = "not_downloaded"
	}

	return imageInfo, nil
}

// analyzeLocalLayers attempts to analyze local layers even when registry access fails
// analyzeLocalLayers 即使注册表访问失败也尝试分析本地层
func (s *ImageAnalysisStep) analyzeLocalLayers(imageInfo *types.ImageInfo, imageName string) {
	// Try to extract digest from image name if it contains one
	// 如果镜像名称包含摘要，尝试提取
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
				imageInfo.Layers = []*types.LayerInfo{layer}
				imageInfo.LayerCount = 1
				if layer.Downloaded {
					imageInfo.DownloadedLayers = 1
					imageInfo.DownloadedSize = layer.Size
					imageInfo.Status = "fully_downloaded"
				}
			}
		}
	}
}

// saveImageAnalysis saves the image analysis result to a JSON file
// saveImageAnalysis 将镜像分析结果保存到JSON文件
func (s *ImageAnalysisStep) saveImageAnalysis(chartDir string, analysis *types.ImageAnalysisResult) error {
	analysisPath := filepath.Join(chartDir, "image-analysis.json")

	// Convert to JSON with pretty formatting
	// 转换为格式化的JSON
	jsonData, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal image analysis to JSON: %w", err)
	}

	// Write to file
	// 写入文件
	if err := os.WriteFile(analysisPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write image analysis file: %w", err)
	}

	log.Printf("Image analysis saved to: %s", analysisPath)
	return nil
}

// isPrivateImage checks if an image is from a private registry
// isPrivateImage 检查镜像是否来自私有registry
func (s *ImageAnalysisStep) isPrivateImage(imageName string) bool {
	// Known private registry patterns
	// 已知的私有registry模式
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
	// 检查自定义registry域名（包含点但不是docker.io）
	if strings.Contains(imageName, ".") && !strings.Contains(imageName, "docker.io") {
		parts := strings.Split(imageName, "/")
		if len(parts) > 1 && strings.Contains(parts[0], ".") {
			return true
		}
	}

	return false
}
