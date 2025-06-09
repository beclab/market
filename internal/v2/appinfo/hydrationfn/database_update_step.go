package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	// Prepare update data
	// 准备更新数据
	updateData, err := s.prepareUpdateData(task)
	if err != nil {
		return fmt.Errorf("failed to prepare update data: %w", err)
	}

	// Update memory cache
	// 更新内存缓存
	if err := s.updateMemoryCache(task, updateData); err != nil {
		return fmt.Errorf("failed to update memory cache: %w", err)
	}

	// Store update information in task
	// 在任务中存储更新信息
	task.DatabaseUpdateData = updateData

	log.Printf("Database and cache update completed for app: %s", task.AppID)
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

// prepareUpdateData prepares the data to be updated in cache and database
// prepareUpdateData 准备要在缓存和数据库中更新的数据
func (s *DatabaseUpdateStep) prepareUpdateData(task *HydrationTask) (map[string]interface{}, error) {
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

	return updateData, nil
}

// updateMemoryCache updates the in-memory cache with hydrated app data
// updateMemoryCache 使用水合应用数据更新内存缓存
func (s *DatabaseUpdateStep) updateMemoryCache(task *HydrationTask, updateData map[string]interface{}) error {
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

	// Create hydrated app data
	// 创建水合应用数据
	hydratedAppData := types.NewAppData(types.AppInfoLatest, updateData)
	hydratedAppData.Version = task.AppVersion
	hydratedAppData.Timestamp = time.Now().Unix()

	// Update cache with hydrated data
	// 使用水合数据更新缓存
	sourceData.AppInfoLatest = hydratedAppData

	// Also update AppInfoHistory with hydrated version
	// 同时使用水合版本更新AppInfoHistory
	historyEntry := types.NewAppData(types.AppInfoHistory, updateData)
	historyEntry.Version = task.AppVersion
	historyEntry.Timestamp = time.Now().Unix()
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

	log.Printf("Memory cache updated successfully for app: %s (user: %s, source: %s)",
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
