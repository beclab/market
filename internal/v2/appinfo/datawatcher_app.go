package appinfo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
)

// DataWatcher monitors pending app data and moves completed hydration apps to latest
// DataWatcher 监控待处理应用数据，将完成水合的应用移动到最新
type DataWatcher struct {
	cacheManager *CacheManager
	hydrator     *Hydrator
	interval     time.Duration
	isRunning    bool
	stopChan     chan struct{}
	mutex        sync.RWMutex

	// Metrics
	// 指标
	totalAppsProcessed int64
	totalAppsMoved     int64
	lastRunTime        time.Time
	metricsMutex       sync.RWMutex
}

// NewDataWatcher creates a new DataWatcher instance
// NewDataWatcher 创建新的DataWatcher实例
func NewDataWatcher(cacheManager *CacheManager, hydrator *Hydrator) *DataWatcher {
	return &DataWatcher{
		cacheManager: cacheManager,
		hydrator:     hydrator,
		interval:     30 * time.Second, // Run every 30 seconds
		stopChan:     make(chan struct{}),
		isRunning:    false,
	}
}

// Start begins the data watching process
// Start 开始数据监控过程
func (dw *DataWatcher) Start(ctx context.Context) error {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if dw.isRunning {
		return fmt.Errorf("DataWatcher is already running")
	}

	if dw.cacheManager == nil {
		return fmt.Errorf("CacheManager is required for DataWatcher")
	}

	if dw.hydrator == nil {
		return fmt.Errorf("Hydrator is required for DataWatcher")
	}

	dw.isRunning = true
	glog.Infof("Starting DataWatcher with interval: %v", dw.interval)

	// Start the monitoring goroutine
	// 启动监控协程
	go dw.watchLoop(ctx)

	return nil
}

// Stop stops the data watching process
// Stop 停止数据监控过程
func (dw *DataWatcher) Stop() {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if !dw.isRunning {
		return
	}

	glog.Infof("Stopping DataWatcher...")
	close(dw.stopChan)
	dw.isRunning = false
}

// IsRunning returns whether the DataWatcher is currently running
// IsRunning 返回DataWatcher是否正在运行
func (dw *DataWatcher) IsRunning() bool {
	dw.mutex.RLock()
	defer dw.mutex.RUnlock()
	return dw.isRunning
}

// watchLoop is the main monitoring loop
// watchLoop 是主要的监控循环
func (dw *DataWatcher) watchLoop(ctx context.Context) {
	glog.Infof("DataWatcher monitoring loop started")
	defer glog.Infof("DataWatcher monitoring loop stopped")

	ticker := time.NewTicker(dw.interval)
	defer ticker.Stop()

	// Run once immediately
	// 立即运行一次
	dw.processCompletedApps()

	for {
		select {
		case <-ctx.Done():
			glog.Infof("DataWatcher stopped due to context cancellation")
			return
		case <-dw.stopChan:
			glog.Infof("DataWatcher stopped due to explicit stop")
			return
		case <-ticker.C:
			dw.processCompletedApps()
		}
	}
}

// processCompletedApps checks for completed hydration apps and moves them
// processCompletedApps 检查已完成水合的应用并移动它们
func (dw *DataWatcher) processCompletedApps() {
	dw.metricsMutex.Lock()
	dw.lastRunTime = time.Now()
	dw.metricsMutex.Unlock()

	glog.V(2).Infof("DataWatcher: Starting to process completed apps")

	// Get all users data from cache manager
	// 从缓存管理器获取所有用户数据
	allUsersData := dw.cacheManager.GetAllUsersData()
	if len(allUsersData) == 0 {
		glog.V(2).Infof("DataWatcher: No users data found")
		return
	}

	totalProcessed := int64(0)
	totalMoved := int64(0)

	// Process each user's data
	// 处理每个用户的数据
	for userID, userData := range allUsersData {
		processed, moved := dw.processUserData(userID, userData)
		totalProcessed += processed
		totalMoved += moved
	}

	// Update metrics
	// 更新指标
	dw.metricsMutex.Lock()
	dw.totalAppsProcessed += totalProcessed
	dw.totalAppsMoved += totalMoved
	dw.metricsMutex.Unlock()

	if totalMoved > 0 {
		glog.Infof("DataWatcher: Processed %d apps, moved %d completed apps to AppInfoLatest",
			totalProcessed, totalMoved)
	} else {
		glog.V(2).Infof("DataWatcher: Processed %d apps, no apps to move", totalProcessed)
	}
}

// processUserData processes a single user's data
// processUserData 处理单个用户的数据
func (dw *DataWatcher) processUserData(userID string, userData *UserData) (int64, int64) {
	if userData == nil {
		return 0, 0
	}

	totalProcessed := int64(0)
	totalMoved := int64(0)

	userData.Mutex.RLock()
	defer userData.Mutex.RUnlock()

	// Process each source
	// 处理每个源
	for sourceID, sourceData := range userData.Sources {
		processed, moved := dw.processSourceData(userID, sourceID, sourceData)
		totalProcessed += processed
		totalMoved += moved
	}

	return totalProcessed, totalMoved
}

// processSourceData processes a single source's data
// processSourceData 处理单个源的数据
func (dw *DataWatcher) processSourceData(userID, sourceID string, sourceData *SourceData) (int64, int64) {
	if sourceData == nil {
		return 0, 0
	}

	sourceData.Mutex.Lock()
	defer sourceData.Mutex.Unlock()

	// Check if there are any pending apps
	// 检查是否有待处理的应用
	if len(sourceData.AppInfoLatestPending) == 0 {
		return 0, 0
	}

	glog.V(2).Infof("DataWatcher: Processing %d pending apps for user=%s, source=%s",
		len(sourceData.AppInfoLatestPending), userID, sourceID)

	completedApps := make([]*AppInfoLatestPendingData, 0)
	remainingPendingApps := make([]*AppInfoLatestPendingData, 0)

	totalProcessed := int64(len(sourceData.AppInfoLatestPending))

	// Check each pending app for completion
	// 检查每个待处理应用是否完成
	for _, pendingApp := range sourceData.AppInfoLatestPending {
		if dw.isAppHydrationCompleted(pendingApp) {
			// App hydration is complete, prepare to move it
			// 应用水合已完成，准备移动它
			completedApps = append(completedApps, pendingApp)

			appID := dw.getAppID(pendingApp)
			glog.Infof("DataWatcher: App hydration completed for app=%s (user=%s, source=%s)",
				appID, userID, sourceID)
		} else {
			// App hydration is not complete, keep it in pending
			// 应用水合未完成，保持在待处理状态
			remainingPendingApps = append(remainingPendingApps, pendingApp)
		}
	}

	// Move completed apps to AppInfoLatest
	// 将完成的应用移动到AppInfoLatest
	for _, completedApp := range completedApps {
		latestApp := dw.convertPendingToLatest(completedApp)
		if latestApp != nil {
			sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, latestApp)
		}
	}

	// Update pending apps list (remove completed ones)
	// 更新待处理应用列表（移除已完成的）
	sourceData.AppInfoLatestPending = remainingPendingApps

	totalMoved := int64(len(completedApps))

	if totalMoved > 0 {
		glog.Infof("DataWatcher: Moved %d completed apps from pending to latest for user=%s, source=%s",
			totalMoved, userID, sourceID)

		// Trigger sync to Redis for this source
		// 为此源触发Redis同步
		go func() {
			if err := dw.cacheManager.SetAppData(userID, sourceID, AppInfoLatest,
				map[string]interface{}{"sync_trigger": time.Now().Unix()}); err != nil {
				glog.Errorf("DataWatcher: Failed to trigger sync after moving apps: %v", err)
			}
		}()
	}

	return totalProcessed, totalMoved
}

// isAppHydrationCompleted checks if app hydration is completed using the hydrator
// isAppHydrationCompleted 使用水合器检查应用水合是否完成
func (dw *DataWatcher) isAppHydrationCompleted(pendingApp *AppInfoLatestPendingData) bool {
	if pendingApp == nil || dw.hydrator == nil {
		return false
	}

	// Use the hydrator's method to check completion
	// 使用水合器的方法检查完成状态
	return dw.hydrator.isAppHydrationComplete(pendingApp)
}

// getAppID extracts app ID from pending app data
// getAppID 从待处理应用数据中提取应用ID
func (dw *DataWatcher) getAppID(pendingApp *AppInfoLatestPendingData) string {
	if pendingApp == nil {
		return "unknown"
	}

	if pendingApp.RawData != nil {
		if pendingApp.RawData.AppID != "" {
			return pendingApp.RawData.AppID
		}
		if pendingApp.RawData.ID != "" {
			return pendingApp.RawData.ID
		}
		if pendingApp.RawData.Name != "" {
			return pendingApp.RawData.Name
		}
	}

	if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil {
		if pendingApp.AppInfo.AppEntry.AppID != "" {
			return pendingApp.AppInfo.AppEntry.AppID
		}
		if pendingApp.AppInfo.AppEntry.ID != "" {
			return pendingApp.AppInfo.AppEntry.ID
		}
		if pendingApp.AppInfo.AppEntry.Name != "" {
			return pendingApp.AppInfo.AppEntry.Name
		}
	}

	return "unknown"
}

// convertPendingToLatest converts AppInfoLatestPendingData to AppInfoLatestData
// convertPendingToLatest 将AppInfoLatestPendingData转换为AppInfoLatestData
func (dw *DataWatcher) convertPendingToLatest(pendingApp *AppInfoLatestPendingData) *AppInfoLatestData {
	if pendingApp == nil {
		return nil
	}

	// Create the latest app data structure
	// 创建最新应用数据结构
	latestApp := &AppInfoLatestData{
		Type:      AppInfoLatest,
		Timestamp: time.Now().Unix(),
	}

	// Copy relevant data from pending to latest
	// 从待处理复制相关数据到最新
	if pendingApp.AppInfo != nil {
		latestApp.AppInfo = pendingApp.AppInfo
	}

	// Copy RawData directly (same type: *ApplicationInfoEntry)
	// 直接复制RawData（相同类型：*ApplicationInfoEntry）
	if pendingApp.RawData != nil {
		latestApp.RawData = pendingApp.RawData
	}

	// Copy package information
	// 复制包信息
	latestApp.RawPackage = pendingApp.RawPackage
	latestApp.RenderedPackage = pendingApp.RenderedPackage

	// Copy Values if present
	// 如果存在则复制Values
	if pendingApp.Values != nil {
		latestApp.Values = pendingApp.Values
	}

	// Copy version information
	// 复制版本信息
	latestApp.Version = pendingApp.Version

	return latestApp
}

// GetMetrics returns DataWatcher metrics
// GetMetrics 返回DataWatcher指标
func (dw *DataWatcher) GetMetrics() DataWatcherMetrics {
	dw.metricsMutex.RLock()
	defer dw.metricsMutex.RUnlock()

	dw.mutex.RLock()
	isRunning := dw.isRunning
	dw.mutex.RUnlock()

	return DataWatcherMetrics{
		IsRunning:          isRunning,
		TotalAppsProcessed: dw.totalAppsProcessed,
		TotalAppsMoved:     dw.totalAppsMoved,
		LastRunTime:        dw.lastRunTime,
		Interval:           dw.interval,
	}
}

// DataWatcherMetrics contains metrics for the DataWatcher
// DataWatcherMetrics 包含DataWatcher的指标
type DataWatcherMetrics struct {
	IsRunning          bool          `json:"is_running"`
	TotalAppsProcessed int64         `json:"total_apps_processed"`
	TotalAppsMoved     int64         `json:"total_apps_moved"`
	LastRunTime        time.Time     `json:"last_run_time"`
	Interval           time.Duration `json:"interval"`
}

// SetInterval sets the monitoring interval
// SetInterval 设置监控间隔
func (dw *DataWatcher) SetInterval(interval time.Duration) {
	dw.mutex.Lock()
	defer dw.mutex.Unlock()

	if interval < time.Second {
		interval = time.Second // Minimum 1 second
	}

	dw.interval = interval
	glog.Infof("DataWatcher interval set to: %v", interval)
}
