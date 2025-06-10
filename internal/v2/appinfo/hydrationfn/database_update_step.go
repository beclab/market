package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"market/internal/v2/types"
)

// AppImageInfo represents detailed information about a Docker image for app cache
// AppImageInfo 表示应用缓存的Docker镜像详细信息
type AppImageInfo struct {
	Name             string          `json:"name"`
	Tag              string          `json:"tag,omitempty"`
	Architecture     string          `json:"architecture,omitempty"`
	TotalSize        int64           `json:"total_size"`
	DownloadedSize   int64           `json:"downloaded_size"`
	DownloadProgress float64         `json:"download_progress"`
	LayerCount       int             `json:"layer_count"`
	DownloadedLayers int             `json:"downloaded_layers"`
	CreatedAt        time.Time       `json:"created_at,omitempty"`
	AnalyzedAt       time.Time       `json:"analyzed_at"`
	Status           string          `json:"status"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Layers           []*AppLayerInfo `json:"layers,omitempty"`
}

// AppLayerInfo represents information about a Docker image layer for app cache
// AppLayerInfo 表示应用缓存的Docker镜像层信息
type AppLayerInfo struct {
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	MediaType  string `json:"media_type,omitempty"`
	Downloaded bool   `json:"downloaded"`
	Progress   int    `json:"progress"` // 0-100
	LocalPath  string `json:"local_path,omitempty"`
}

// AppImageAnalysis represents the image analysis result for a specific app in cache
// AppImageAnalysis 表示缓存中特定应用的镜像分析结果
type AppImageAnalysis struct {
	AppID            string                   `json:"app_id"`
	AnalyzedAt       time.Time                `json:"analyzed_at"`
	TotalImages      int                      `json:"total_images"`
	Images           map[string]*AppImageInfo `json:"images"`
	Status           string                   `json:"status"` // completed, failed, partial, no_images
	SourceChartURL   string                   `json:"source_chart_url,omitempty"`
	RenderedChartURL string                   `json:"rendered_chart_url,omitempty"`
}

// DatabaseUpdateStep represents the step to update memory cache and database
// DatabaseUpdateStep 表示更新内存缓存和数据库的步骤
type DatabaseUpdateStep struct {
}

// NewDatabaseUpdateStep creates a new database update step
// NewDatabaseUpdateStep 创建新的数据库更新步骤
func NewDatabaseUpdateStep() *DatabaseUpdateStep {
	return &DatabaseUpdateStep{}
}

// GetStepName returns the name of this step
// GetStepName 返回此步骤的名称
func (s *DatabaseUpdateStep) GetStepName() string {
	return "Database and Cache Update"
}

// CanSkip determines if this step can be skipped
// CanSkip 确定是否可以跳过此步骤
func (s *DatabaseUpdateStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// This step should rarely be skipped as it's the final step
	// 此步骤很少应该被跳过，因为它是最后一步
	return false
}

// Execute performs the database and cache update
// Execute 执行数据库和缓存更新
func (s *DatabaseUpdateStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing database update step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	// Validate that previous steps completed successfully
	// 验证前面的步骤成功完成
	if err := s.validatePreviousSteps(task); err != nil {
		return fmt.Errorf("previous steps validation failed: %w", err)
	}

	// Prepare update data with image analysis integration
	// 准备更新数据并集成镜像分析
	updateData, imageAnalysis, err := s.prepareUpdateDataWithImages(task)
	if err != nil {
		return fmt.Errorf("failed to prepare update data: %w", err)
	}

	// Update memory cache with image analysis
	// 使用镜像分析更新内存缓存
	if err := s.updateMemoryCacheWithImages(task, updateData, imageAnalysis); err != nil {
		return fmt.Errorf("failed to update memory cache: %w", err)
	}

	// Store update information in task
	// 在任务中存储更新信息
	task.DatabaseUpdateData = updateData

	log.Printf("Database and cache update completed for app: %s with %d Docker images analyzed",
		task.AppID, s.getImageCount(imageAnalysis))
	return nil
}

// validatePreviousSteps validates that all previous steps completed successfully
// validatePreviousSteps 验证所有前面的步骤都成功完成
func (s *DatabaseUpdateStep) validatePreviousSteps(task *HydrationTask) error {
	// Check if source chart URL is available
	// 检查源chart URL是否可用
	if task.SourceChartURL == "" {
		return fmt.Errorf("source chart URL is missing")
	}

	// Check if rendered chart URL is available
	// 检查渲染chart URL是否可用
	if task.RenderedChartURL == "" {
		return fmt.Errorf("rendered chart URL is missing")
	}

	// Check if chart data is available
	// 检查chart数据是否可用
	if len(task.ChartData) == 0 {
		return fmt.Errorf("chart data is missing")
	}

	return nil
}

// prepareUpdateDataWithImages prepares the data to be updated including image analysis
// prepareUpdateDataWithImages 准备要更新的数据，包括镜像分析
func (s *DatabaseUpdateStep) prepareUpdateDataWithImages(task *HydrationTask) (map[string]interface{}, *AppImageAnalysis, error) {
	updateData := make(map[string]interface{})

	// Basic app information
	// 基本应用信息
	updateData["app_id"] = task.AppID
	updateData["user_id"] = task.UserID
	updateData["source_id"] = task.SourceID
	updateData["app_name"] = task.AppName
	updateData["app_version"] = task.AppVersion

	// Chart URLs
	// Chart URLs
	updateData["source_chart_url"] = task.SourceChartURL
	updateData["rendered_chart_url"] = task.RenderedChartURL

	// Chart data
	// Chart数据
	updateData["chart_data"] = task.ChartData

	// Extract and integrate image analysis data
	// 提取并集成镜像分析数据
	var imageAnalysis *AppImageAnalysis
	if analysisData, exists := task.ChartData["image_analysis"]; exists {
		if analysis, ok := analysisData.(*ImageAnalysisResult); ok {
			// Convert ImageAnalysisResult to AppImageAnalysis
			// 将ImageAnalysisResult转换为AppImageAnalysis
			imageAnalysis = s.convertToAppImageAnalysis(task, analysis)

			// Add image analysis to update data
			// 将镜像分析添加到更新数据
			updateData["docker_images"] = imageAnalysis.Images
			updateData["total_docker_images"] = imageAnalysis.TotalImages
			updateData["image_analysis_status"] = imageAnalysis.Status
			updateData["image_analysis_timestamp"] = imageAnalysis.AnalyzedAt.Unix()

			log.Printf("Integrated %d Docker images for app: %s", imageAnalysis.TotalImages, task.AppID)
		}
	}

	// Original app data (selective)
	// 原始应用数据（选择性的）
	if task.AppData != nil {
		// Copy important fields from original app data
		// 从原始应用数据复制重要字段
		importantFields := []string{
			"description", "icon", "keywords", "maintainers",
			"home", "sources", "dependencies", "tags",
			"category", "subcategory", "license", "screenshots",
		}

		for _, field := range importantFields {
			if value, exists := task.AppData[field]; exists {
				updateData[field] = value
			}
		}
	}

	// Hydration metadata
	// 水合元数据
	updateData["hydration_status"] = "completed"
	updateData["hydration_timestamp"] = time.Now().Unix()
	updateData["hydration_task_id"] = task.ID

	// Processing metrics
	// 处理指标
	updateData["processing_time"] = time.Since(task.CreatedAt).Seconds()
	updateData["retry_count"] = task.RetryCount

	return updateData, imageAnalysis, nil
}

// convertToAppImageAnalysis converts ImageAnalysisResult to AppImageAnalysis
// convertToAppImageAnalysis 将ImageAnalysisResult转换为AppImageAnalysis
func (s *DatabaseUpdateStep) convertToAppImageAnalysis(task *HydrationTask, result *ImageAnalysisResult) *AppImageAnalysis {
	return &AppImageAnalysis{
		AppID:            task.AppID,
		AnalyzedAt:       result.AnalyzedAt,
		TotalImages:      result.TotalImages,
		Images:           s.convertImageInfoMap(result.Images),
		Status:           s.determineAnalysisStatus(result),
		SourceChartURL:   task.SourceChartURL,
		RenderedChartURL: task.RenderedChartURL,
	}
}

// convertImageInfoMap converts hydrationfn.ImageInfo to appinfo.ImageInfo
// convertImageInfoMap 将hydrationfn.ImageInfo转换为appinfo.ImageInfo
func (s *DatabaseUpdateStep) convertImageInfoMap(hydrationImages map[string]*ImageInfo) map[string]*AppImageInfo {
	result := make(map[string]*AppImageInfo)

	for name, hydrationImg := range hydrationImages {
		appImg := &AppImageInfo{
			Name:             hydrationImg.Name,
			Tag:              hydrationImg.Tag,
			Architecture:     hydrationImg.Architecture,
			TotalSize:        hydrationImg.TotalSize,
			DownloadedSize:   hydrationImg.DownloadedSize,
			DownloadProgress: hydrationImg.DownloadProgress,
			LayerCount:       hydrationImg.LayerCount,
			DownloadedLayers: hydrationImg.DownloadedLayers,
			CreatedAt:        hydrationImg.CreatedAt,
			AnalyzedAt:       hydrationImg.AnalyzedAt,
			Status:           hydrationImg.Status,
			ErrorMessage:     hydrationImg.ErrorMessage,
			Layers:           s.convertLayerInfoSlice(hydrationImg.Layers),
		}
		result[name] = appImg
	}

	return result
}

// convertLayerInfoSlice converts layer info slice
// convertLayerInfoSlice 转换层信息切片
func (s *DatabaseUpdateStep) convertLayerInfoSlice(hydrationLayers []*LayerInfo) []*AppLayerInfo {
	result := make([]*AppLayerInfo, len(hydrationLayers))

	for i, layer := range hydrationLayers {
		result[i] = &AppLayerInfo{
			Digest:     layer.Digest,
			Size:       layer.Size,
			MediaType:  layer.MediaType,
			Downloaded: layer.Downloaded,
			Progress:   layer.Progress,
			LocalPath:  layer.LocalPath,
		}
	}

	return result
}

// determineAnalysisStatus determines the overall analysis status
// determineAnalysisStatus 确定总体分析状态
func (s *DatabaseUpdateStep) determineAnalysisStatus(result *ImageAnalysisResult) string {
	if result.TotalImages == 0 {
		return "no_images"
	}

	successCount := 0
	for _, img := range result.Images {
		if img.Status == "fully_downloaded" || img.Status == "partially_downloaded" {
			successCount++
		}
	}

	if successCount == result.TotalImages {
		return "completed"
	} else if successCount > 0 {
		return "partial"
	} else {
		return "failed"
	}
}

// getImageCount safely gets image count from analysis
// getImageCount 安全地从分析中获取镜像数量
func (s *DatabaseUpdateStep) getImageCount(analysis *AppImageAnalysis) int {
	if analysis == nil {
		return 0
	}
	return analysis.TotalImages
}

// updateMemoryCacheWithImages updates the in-memory cache with hydrated app data including images
// updateMemoryCacheWithImages 使用包含镜像的水合应用数据更新内存缓存
func (s *DatabaseUpdateStep) updateMemoryCacheWithImages(task *HydrationTask, updateData map[string]interface{}, imageAnalysis *AppImageAnalysis) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Lock cache for thread-safe access
	// 锁定缓存以进行线程安全访问
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Ensure user exists in cache
	// 确保用户在缓存中存在
	if _, exists := task.Cache.Users[task.UserID]; !exists {
		task.Cache.Users[task.UserID] = types.NewUserData()
		log.Printf("Created user data in cache for user: %s", task.UserID)
	}

	userData := task.Cache.Users[task.UserID]
	userData.Mutex.Lock()
	defer userData.Mutex.Unlock()

	// Ensure source exists for user
	// 确保用户的源存在
	if _, exists := userData.Sources[task.SourceID]; !exists {
		userData.Sources[task.SourceID] = types.NewSourceData()
		log.Printf("Created source data in cache for user: %s, source: %s", task.UserID, task.SourceID)
	}

	sourceData := userData.Sources[task.SourceID]
	sourceData.Mutex.Lock()
	defer sourceData.Mutex.Unlock()

	// Create hydrated app data with image analysis
	// 创建包含镜像分析的水合应用数据
	hydratedAppData := types.NewAppData(types.AppInfoLatest, updateData)
	hydratedAppData.Version = task.AppVersion
	hydratedAppData.Timestamp = time.Now().Unix()

	// Add image analysis to app data if available
	// 如果可用，将镜像分析添加到应用数据
	if imageAnalysis != nil {
		if hydratedAppData.Data == nil {
			hydratedAppData.Data = make(map[string]interface{})
		}
		hydratedAppData.Data["image_analysis"] = imageAnalysis
	}

	// Update cache with hydrated data
	// 使用水合数据更新缓存
	sourceData.AppInfoLatest = hydratedAppData

	// Also update AppInfoHistory with hydrated version
	// 同时使用水合版本更新AppInfoHistory
	historyEntry := types.NewAppData(types.AppInfoHistory, updateData)
	historyEntry.Version = task.AppVersion
	historyEntry.Timestamp = time.Now().Unix()
	if imageAnalysis != nil {
		if historyEntry.Data == nil {
			historyEntry.Data = make(map[string]interface{})
		}
		historyEntry.Data["image_analysis"] = imageAnalysis
	}
	sourceData.AppInfoHistory = append(sourceData.AppInfoHistory, historyEntry)

	// Clear the pending data since hydration is complete
	// 清除待处理数据，因为水合已完成
	if sourceData.AppInfoLatestPending != nil {
		// Check if this task corresponds to the pending data
		// 检查此任务是否对应待处理数据
		if s.isTaskForPendingData(task, sourceData.AppInfoLatestPending) {
			sourceData.AppInfoLatestPending = nil
			log.Printf("Cleared pending data for app: %s (user: %s, source: %s)",
				task.AppID, task.UserID, task.SourceID)
		}
	}

	log.Printf("Memory cache updated successfully for app: %s (user: %s, source: %s) with image analysis",
		task.AppID, task.UserID, task.SourceID)

	return nil
}

// isTaskForPendingData checks if the current task corresponds to the pending data
// isTaskForPendingData 检查当前任务是否对应待处理数据
func (s *DatabaseUpdateStep) isTaskForPendingData(task *HydrationTask, pendingData *types.AppData) bool {
	if pendingData == nil || pendingData.Data == nil {
		return false
	}

	// Try to find the app in pending data
	// 尝试在待处理数据中找到应用
	if appsData, ok := pendingData.Data["data"]; ok {
		if appsMap, ok := appsData.(map[string]interface{}); ok {
			if apps, ok := appsMap["apps"]; ok {
				if appsDict, ok := apps.(map[string]interface{}); ok {
					if appInfo, exists := appsDict[task.AppID]; exists {
						// Found the app in pending data, this task is for it
						// 在待处理数据中找到应用，此任务是针对它的
						_ = appInfo // appInfo can be used for additional verification if needed
						return true
					}
				}
			}
		}
	}

	return false
}

// updateDatabase would update the persistent database (not implemented in this example)
// updateDatabase 会更新持久数据库（在此示例中未实现）
func (s *DatabaseUpdateStep) updateDatabase(task *HydrationTask, updateData map[string]interface{}) error {
	// This would typically involve:
	// 这通常涉及：
	// 1. Connect to database (PostgreSQL, MySQL, etc.)
	// 1. 连接到数据库（PostgreSQL、MySQL等）
	// 2. Prepare SQL statements or use ORM
	// 2. 准备SQL语句或使用ORM
	// 3. Insert or update records
	// 3. 插入或更新记录
	// 4. Handle transactions
	// 4. 处理事务

	// For now, we'll just log the operation
	// 现在，我们只记录操作
	dataJSON, _ := json.MarshalIndent(updateData, "", "  ")
	log.Printf("Database update (simulated) for app: %s, data: %s", task.AppID, string(dataJSON))

	return nil
}
