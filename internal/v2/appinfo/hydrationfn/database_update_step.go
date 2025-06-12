package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"market/internal/v2/types"
)

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
func (s *DatabaseUpdateStep) prepareUpdateDataWithImages(task *HydrationTask) (map[string]interface{}, *types.AppImageAnalysis, error) {
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
	var imageAnalysis *types.AppImageAnalysis
	if analysisData, exists := task.ChartData["image_analysis"]; exists {
		if analysis, ok := analysisData.(*types.ImageAnalysisResult); ok {
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
func (s *DatabaseUpdateStep) convertToAppImageAnalysis(task *HydrationTask, result *types.ImageAnalysisResult) *types.AppImageAnalysis {
	return &types.AppImageAnalysis{
		AppID:            task.AppID,
		AnalyzedAt:       result.AnalyzedAt,
		TotalImages:      result.TotalImages,
		Images:           result.Images, // Now both use the same *types.ImageInfo type
		Status:           s.determineAnalysisStatus(result),
		SourceChartURL:   task.SourceChartURL,
		RenderedChartURL: task.RenderedChartURL,
	}
}

// determineAnalysisStatus determines the overall analysis status
// determineAnalysisStatus 确定总体分析状态
func (s *DatabaseUpdateStep) determineAnalysisStatus(result *types.ImageAnalysisResult) string {
	if result.TotalImages == 0 {
		return "no_images"
	}

	successCount := 0
	analysisAttemptedCount := 0
	for _, img := range result.Images {
		analysisAttemptedCount++
		// Consider private registry images as successfully analyzed
		// 将私有注册表镜像视为成功分析
		if img.Status == "fully_downloaded" || img.Status == "partially_downloaded" || img.Status == "private_registry" {
			successCount++
		}
	}

	// If all images were successfully analyzed (including private registry detection)
	// 如果所有镜像都成功分析（包括私有注册表检测）
	if successCount == result.TotalImages && analysisAttemptedCount == result.TotalImages {
		return "completed"
	} else if successCount > 0 {
		return "partial"
	} else {
		return "failed"
	}
}

// getImageCount safely gets image count from analysis
// getImageCount 安全地从分析中获取镜像数量
func (s *DatabaseUpdateStep) getImageCount(analysis *types.AppImageAnalysis) int {
	if analysis == nil {
		return 0
	}
	return analysis.TotalImages
}

// updateMemoryCacheWithImages updates the in-memory cache with hydrated app data including images
// updateMemoryCacheWithImages 使用包含镜像的水合应用数据更新内存缓存
func (s *DatabaseUpdateStep) updateMemoryCacheWithImages(task *HydrationTask, updateData map[string]interface{}, imageAnalysis *types.AppImageAnalysis) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Step 1: Prepare all data outside of any locks
	// 步骤1：在任何锁之外准备所有数据
	log.Printf("Preparing update data for app: %s (user: %s, source: %s)", task.AppID, task.UserID, task.SourceID)

	// Prepare metadata for hydration completion
	// 为水合完成准备元数据
	hydrationMetadata := map[string]interface{}{
		"hydration_status":    "completed",
		"hydration_timestamp": time.Now().Unix(),
		"processing_time":     time.Since(task.CreatedAt).Seconds(),
		"retry_count":         task.RetryCount,
		"hydration_task_id":   task.ID,
		"source_chart_url":    task.SourceChartURL,
		"rendered_chart_url":  task.RenderedChartURL,
		"validation_status":   "validated",
		"data_source":         "hydration_task",
	}

	// Step 2: Update RawPackage and RenderedPackage fields in pending data
	// 步骤2：更新待处理数据中的RawPackage和RenderedPackage字段
	log.Printf("Updating package information for app: %s", task.AppID)
	if err := s.updatePackageInformation(task); err != nil {
		log.Printf("Warning: Failed to update package information for app %s: %v", task.AppID, err)
		// Continue with other updates even if package update fails
		// 即使包更新失败也继续其他更新
	}

	// Step 3: Update AppInfo with image analysis
	// 步骤3：使用镜像分析更新AppInfo
	log.Printf("Updating AppInfo with image analysis for app: %s", task.AppID)
	if err := s.updateAppInfoWithImages(task, imageAnalysis, hydrationMetadata); err != nil {
		return fmt.Errorf("failed to update AppInfo with images: %w", err)
	}

	log.Printf("Cache update completed for app: %s with %d Docker images analyzed",
		task.AppID, s.getImageCount(imageAnalysis))
	return nil
}

// updatePackageInformation updates RawPackage and RenderedPackage fields
// updatePackageInformation 更新RawPackage和RenderedPackage字段
func (s *DatabaseUpdateStep) updatePackageInformation(task *HydrationTask) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Get the specific pending data entry for this task
	// 获取此任务的特定待处理数据条目
	pendingDataRef, err := s.findPendingDataForTask(task)
	if err != nil {
		return fmt.Errorf("failed to find pending data: %w", err)
	}

	if pendingDataRef == nil {
		log.Printf("No pending data found for task %s, skipping package update", task.ID)
		return nil
	}

	// Update package fields directly (these are simple string assignments)
	// 直接更新包字段（这些是简单的字符串赋值）
	if task.SourceChartURL != "" {
		pendingDataRef.RawPackage = task.SourceChartURL
	}
	if task.RenderedChartURL != "" {
		pendingDataRef.RenderedPackage = task.RenderedChartURL
	}

	log.Printf("Updated package information for app: %s (RawPackage: %s, RenderedPackage: %s)",
		task.AppID, pendingDataRef.RawPackage, pendingDataRef.RenderedPackage)
	return nil
}

// updateAppInfoWithImages updates AppInfo with image analysis results
// updateAppInfoWithImages 使用镜像分析结果更新AppInfo
func (s *DatabaseUpdateStep) updateAppInfoWithImages(task *HydrationTask, imageAnalysis *types.AppImageAnalysis, metadata map[string]interface{}) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Get the specific pending data entry for this task
	// 获取此任务的特定待处理数据条目
	pendingDataRef, err := s.findPendingDataForTask(task)
	if err != nil {
		return fmt.Errorf("failed to find pending data: %w", err)
	}

	if pendingDataRef == nil {
		log.Printf("No pending data found for task %s, skipping AppInfo update", task.ID)
		return nil
	}

	// Initialize AppInfo if it doesn't exist
	// 如果AppInfo不存在则初始化
	if pendingDataRef.AppInfo == nil {
		pendingDataRef.AppInfo = &types.AppInfo{}
	}

	// Set AppEntry to RawData if not already set
	// 如果尚未设置，将AppEntry设置为RawData
	if pendingDataRef.AppInfo.AppEntry == nil && pendingDataRef.RawData != nil {
		pendingDataRef.AppInfo.AppEntry = pendingDataRef.RawData
	}

	// Update image analysis in AppInfo
	// 在AppInfo中更新镜像分析
	if imageAnalysis != nil {
		pendingDataRef.AppInfo.ImageAnalysis = &types.ImageAnalysisResult{
			AnalyzedAt:  imageAnalysis.AnalyzedAt,
			TotalImages: imageAnalysis.TotalImages,
			Images:      imageAnalysis.Images,
		}
		log.Printf("Updated image analysis for app: %s (%d images)", task.AppID, imageAnalysis.TotalImages)
	}

	// Update metadata in RawData if available
	// 如果可用，更新RawData中的元数据
	if pendingDataRef.RawData != nil {
		if pendingDataRef.RawData.Metadata == nil {
			pendingDataRef.RawData.Metadata = make(map[string]interface{})
		}
		// Copy hydration metadata
		// 复制水合元数据
		for key, value := range metadata {
			pendingDataRef.RawData.Metadata[key] = value
		}
	}

	// Update timestamp to reflect processing completion
	// 更新时间戳以反映处理完成
	pendingDataRef.Timestamp = time.Now().Unix()

	log.Printf("Updated AppInfo for app: %s with hydration completion metadata", task.AppID)
	return nil
}

// findPendingDataForTask finds the pending data entry for a specific task
// findPendingDataForTask 查找特定任务的待处理数据条目
func (s *DatabaseUpdateStep) findPendingDataForTask(task *HydrationTask) (*types.AppInfoLatestPendingData, error) {
	if task.Cache == nil {
		return nil, fmt.Errorf("cache reference is nil")
	}

	// 使用全局锁访问缓存数据
	// Access cache data using global lock
	task.Cache.Mutex.RLock()
	defer task.Cache.Mutex.RUnlock()

	userData, userExists := task.Cache.Users[task.UserID]
	if !userExists {
		return nil, fmt.Errorf("user %s not found in cache", task.UserID)
	}

	sourceData, sourceExists := userData.Sources[task.SourceID]
	if !sourceExists {
		return nil, fmt.Errorf("source %s not found for user %s", task.SourceID, task.UserID)
	}

	// Find the matching pending data
	// 查找匹配的待处理数据
	for _, pendingData := range sourceData.AppInfoLatestPending {
		if s.isTaskForPendingData(task, pendingData) {
			return pendingData, nil
		}
	}

	return nil, nil // Not found, but not an error
}

// isTaskForPendingData checks if the current task corresponds to the pending data
// isTaskForPendingData 检查当前任务是否对应待处理数据
func (s *DatabaseUpdateStep) isTaskForPendingData(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		log.Printf("isTaskForPendingData: pendingData is nil")
		return false
	}

	taskAppID := task.AppID
	log.Printf("isTaskForPendingData: Looking for task appID: %s", taskAppID)

	// Check if the task AppID matches the pending data's RawData
	// 检查任务AppID是否匹配待处理数据的RawData
	if pendingData.RawData != nil {
		log.Printf("isTaskForPendingData: Checking RawData - ID: %s, AppID: %s, Name: %s",
			pendingData.RawData.ID, pendingData.RawData.AppID, pendingData.RawData.Name)

		// Check if this is legacy data by looking at metadata
		// 通过查看元数据检查这是否是传统数据
		if pendingData.RawData.Metadata != nil {
			// Check for legacy_data in metadata - this contains multiple apps
			// 检查元数据中的legacy_data - 这包含多个应用
			if legacyData, hasLegacyData := pendingData.RawData.Metadata["legacy_data"]; hasLegacyData {
				if legacyDataMap, ok := legacyData.(map[string]interface{}); ok {
					// Check if task app ID exists in the legacy data apps
					// 检查任务应用ID是否存在于传统数据应用中
					if dataSection, hasDataSection := legacyDataMap["data"].(map[string]interface{}); hasDataSection {
						if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
							if _, appExists := appsData[taskAppID]; appExists {
								log.Printf("isTaskForPendingData: Found task appID %s in legacy_data apps", taskAppID)
								return true
							}
						}
					}
				}
			}

			// Check for legacy_raw_data in metadata
			// 检查元数据中的legacy_raw_data
			if legacyRawData, hasLegacyRawData := pendingData.RawData.Metadata["legacy_raw_data"]; hasLegacyRawData {
				if legacyRawDataMap, ok := legacyRawData.(map[string]interface{}); ok {
					// Similar check for legacy raw data format
					// 对传统原始数据格式进行类似检查
					if dataSection, hasDataSection := legacyRawDataMap["data"].(map[string]interface{}); hasDataSection {
						if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
							if _, appExists := appsData[taskAppID]; appExists {
								log.Printf("isTaskForPendingData: Found task appID %s in legacy_raw_data apps", taskAppID)
								return true
							}
						}
					}
				}
			}

			// Check representative_app_id for legacy summary data
			// 检查传统汇总数据的representative_app_id
			if repAppID, hasRepAppID := pendingData.RawData.Metadata["representative_app_id"].(string); hasRepAppID {
				if repAppID == taskAppID {
					log.Printf("isTaskForPendingData: Found task appID %s as representative_app_id", taskAppID)
					return true
				}
			}

			// Check data_type to identify legacy data types
			// 检查data_type以识别传统数据类型
			if dataType, hasDataType := pendingData.RawData.Metadata["data_type"].(string); hasDataType {
				log.Printf("isTaskForPendingData: Pending data type: %s", dataType)

				// For legacy data types, we need to check the stored legacy data
				// 对于传统数据类型，我们需要检查存储的传统数据
				if dataType == "legacy_complete_data" || dataType == "legacy_unstructured_data" {
					// Already checked above, but log for debugging
					log.Printf("isTaskForPendingData: This is legacy data type: %s", dataType)
				}
			}
		}

		// Standard checks for non-legacy data
		// 对非传统数据进行标准检查
		// Match by ID, AppID or Name
		// 通过ID、AppID或Name匹配
		if pendingData.RawData.ID == taskAppID ||
			pendingData.RawData.AppID == taskAppID ||
			pendingData.RawData.Name == taskAppID {
			log.Printf("isTaskForPendingData: Found match in RawData")
			return true
		}

		// Try partial matches for debugging
		// 尝试部分匹配进行调试
		if pendingData.RawData.ID != "" && strings.Contains(taskAppID, pendingData.RawData.ID) {
			log.Printf("isTaskForPendingData: Partial match found - task contains RawData.ID")
		}
		if pendingData.RawData.AppID != "" && strings.Contains(taskAppID, pendingData.RawData.AppID) {
			log.Printf("isTaskForPendingData: Partial match found - task contains RawData.AppID")
		}
		if pendingData.RawData.Name != "" && strings.Contains(taskAppID, pendingData.RawData.Name) {
			log.Printf("isTaskForPendingData: Partial match found - task contains RawData.Name")
		}
	}

	// Check AppInfo if available
	// 如果可用，检查AppInfo
	if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
		log.Printf("isTaskForPendingData: Checking AppInfo.AppEntry - ID: %s, AppID: %s, Name: %s",
			pendingData.AppInfo.AppEntry.ID, pendingData.AppInfo.AppEntry.AppID, pendingData.AppInfo.AppEntry.Name)

		// Match by ID, AppID or Name in AppInfo
		// 通过AppInfo中的ID、AppID或Name匹配
		if pendingData.AppInfo.AppEntry.ID == taskAppID ||
			pendingData.AppInfo.AppEntry.AppID == taskAppID ||
			pendingData.AppInfo.AppEntry.Name == taskAppID {
			log.Printf("isTaskForPendingData: Found match in AppInfo.AppEntry")
			return true
		}
	}

	log.Printf("isTaskForPendingData: No match found for task appID: %s", taskAppID)
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
