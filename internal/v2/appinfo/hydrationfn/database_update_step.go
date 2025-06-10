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

	// Find and update the corresponding pending data
	// 查找并更新对应的待处理数据
	var foundPendingData *types.AppInfoLatestPendingData

	log.Printf("Looking for pending data for task %s (appID: %s) among %d pending entries",
		task.ID, task.AppID, len(sourceData.AppInfoLatestPending))

	for i, pendingData := range sourceData.AppInfoLatestPending {
		log.Printf("Checking pending data %d: RawData exists: %t", i, pendingData.RawData != nil)
		if pendingData.RawData != nil {
			log.Printf("  RawData - ID: %s, AppID: %s", pendingData.RawData.ID, pendingData.RawData.AppID)
		}
		if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
			log.Printf("  AppInfo.AppEntry - ID: %s, AppID: %s",
				pendingData.AppInfo.AppEntry.ID, pendingData.AppInfo.AppEntry.AppID)
		}

		if s.isTaskForPendingData(task, pendingData) {
			foundPendingData = sourceData.AppInfoLatestPending[i]
			log.Printf("Found matching pending data at index %d for task %s", i, task.ID)
			break
		}
	}

	if foundPendingData == nil {
		// Create new pending data entry from task information
		// 从任务信息创建新的待处理数据条目
		log.Printf("No matching pending data found for task %s, creating new pending entry", task.ID)

		// Create ApplicationInfoEntry from task data
		// 从任务数据创建ApplicationInfoEntry
		rawData := &types.ApplicationInfoEntry{
			ID:          task.AppID,
			AppID:       task.AppID,
			Name:        task.AppName,
			Version:     task.AppVersion,
			Title:       task.AppName,
			Description: "",
		}

		// Extract additional fields from task.AppData if available
		// 如果可用，从task.AppData中提取其他字段
		if task.AppData != nil {
			if name, ok := task.AppData["name"].(string); ok {
				rawData.Name = name
			}
			if title, ok := task.AppData["title"].(string); ok {
				rawData.Title = title
			}
			if desc, ok := task.AppData["description"].(string); ok {
				rawData.Description = desc
			}
			if icon, ok := task.AppData["icon"].(string); ok {
				rawData.Icon = icon
			}
			if developer, ok := task.AppData["developer"].(string); ok {
				rawData.Developer = developer
			}
		}

		// Add processing metadata
		// 添加处理元数据
		rawData.Metadata = make(map[string]interface{})
		rawData.Metadata["hydration_status"] = "completed"
		rawData.Metadata["hydration_timestamp"] = time.Now().Unix()
		rawData.Metadata["processing_time"] = time.Since(task.CreatedAt).Seconds()
		rawData.Metadata["retry_count"] = task.RetryCount
		rawData.Metadata["hydration_task_id"] = task.ID
		rawData.Metadata["source_chart_url"] = task.SourceChartURL
		rawData.Metadata["rendered_chart_url"] = task.RenderedChartURL

		// Create new pending data entry
		// 创建新的待处理数据条目
		newPendingData := &types.AppInfoLatestPendingData{
			Type:      types.AppInfoLatestPending,
			Timestamp: time.Now().Unix(),
			Version:   task.AppVersion,
			RawData:   rawData,
			AppInfo: &types.AppInfo{
				AppEntry: rawData,
			},
		}

		// Add image analysis to AppInfo if available
		// 如果可用，将镜像分析添加到AppInfo
		if imageAnalysis != nil {
			newPendingData.AppInfo.ImageAnalysis = &types.ImageAnalysisResult{
				AnalyzedAt:  imageAnalysis.AnalyzedAt,
				TotalImages: imageAnalysis.TotalImages,
				Images:      imageAnalysis.Images,
			}
		}

		// Add to pending data
		// 添加到待处理数据
		sourceData.AppInfoLatestPending = append(sourceData.AppInfoLatestPending, newPendingData)

		log.Printf("Created new pending data entry for app: %s (user: %s, source: %s) with hydration results",
			task.AppID, task.UserID, task.SourceID)
		return nil
	}

	// Update the pending data with hydration results
	// 使用水合结果更新待处理数据
	if foundPendingData.RawData != nil {
		// Update other fields from chart data
		// 从chart数据更新其他字段
		if task.ChartData != nil {
			// Copy relevant chart data to RawData
			// 将相关chart数据复制到RawData
			for key, value := range task.ChartData {
				switch key {
				case "description":
					if desc, ok := value.(string); ok {
						foundPendingData.RawData.Description = desc
					}
				case "developer":
					if developer, ok := value.(string); ok {
						foundPendingData.RawData.Developer = developer
					}
				case "icon":
					if icon, ok := value.(string); ok {
						foundPendingData.RawData.Icon = icon
					}
				case "title":
					if title, ok := value.(string); ok {
						foundPendingData.RawData.Title = title
					}
				}
			}
		}
	}

	// Update AppInfo section with processed data
	// 使用处理后的数据更新AppInfo部分
	if foundPendingData.AppInfo == nil {
		foundPendingData.AppInfo = &types.AppInfo{}
	}

	// Update the AppEntry with chart URLs and processed data
	// 使用chart URLs和处理数据更新AppEntry
	if foundPendingData.AppInfo.AppEntry == nil && foundPendingData.RawData != nil {
		foundPendingData.AppInfo.AppEntry = foundPendingData.RawData
	}

	// Add or update image analysis in AppInfo
	// 在AppInfo中添加或更新镜像分析
	if imageAnalysis != nil {
		foundPendingData.AppInfo.ImageAnalysis = &types.ImageAnalysisResult{
			AnalyzedAt:  imageAnalysis.AnalyzedAt,
			TotalImages: imageAnalysis.TotalImages,
			Images:      imageAnalysis.Images,
		}
	}

	// Add processing metadata to pending data
	// 向待处理数据添加处理元数据
	if foundPendingData.RawData != nil {
		// Use existing Metadata field if available
		// 如果可用，使用现有的Metadata字段
		if foundPendingData.RawData.Metadata == nil {
			foundPendingData.RawData.Metadata = make(map[string]interface{})
		}
		foundPendingData.RawData.Metadata["hydration_status"] = "completed"
		foundPendingData.RawData.Metadata["hydration_timestamp"] = time.Now().Unix()
		foundPendingData.RawData.Metadata["processing_time"] = time.Since(task.CreatedAt).Seconds()
		foundPendingData.RawData.Metadata["retry_count"] = task.RetryCount
		foundPendingData.RawData.Metadata["hydration_task_id"] = task.ID
		foundPendingData.RawData.Metadata["source_chart_url"] = task.SourceChartURL
		foundPendingData.RawData.Metadata["rendered_chart_url"] = task.RenderedChartURL
	}

	// Update timestamp to reflect processing completion
	// 更新时间戳以反映处理完成
	foundPendingData.Timestamp = time.Now().Unix()

	log.Printf("Updated pending data for app: %s (user: %s, source: %s) with hydration results including %d Docker images",
		task.AppID, task.UserID, task.SourceID, s.getImageCount(imageAnalysis))

	return nil
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
