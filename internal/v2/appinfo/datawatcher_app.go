package appinfo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"

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

	// Processing mutex to ensure only one cycle runs at a time
	// 处理互斥锁确保一次只运行一个周期
	processingMutex sync.Mutex

	// Active hash calculations tracking
	// 活跃hash计算跟踪
	activeHashCalculations map[string]bool
	hashMutex              sync.Mutex

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
		cacheManager:           cacheManager,
		hydrator:               hydrator,
		interval:               30 * time.Second, // Run every 30 seconds
		stopChan:               make(chan struct{}),
		isRunning:              false,
		activeHashCalculations: make(map[string]bool),
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
	// Ensure only one processing cycle runs at a time
	// 确保一次只运行一个处理周期
	if !dw.processingMutex.TryLock() {
		glog.Warningf("DataWatcher: Previous processing cycle still running, skipping this cycle")
		return
	}
	defer dw.processingMutex.Unlock()

	processingStart := time.Now()
	dw.metricsMutex.Lock()
	dw.lastRunTime = time.Now()
	dw.metricsMutex.Unlock()

	glog.Infof("DataWatcher: Starting to process completed apps")

	// Create timeout context for entire processing cycle
	// 为整个处理周期创建超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Get all users data from cache manager with timeout
	// 带超时从缓存管理器获取所有用户数据
	var allUsersData map[string]*types.UserData

	userDataChan := make(chan map[string]*types.UserData, 1)
	go func() {
		data := dw.cacheManager.GetAllUsersData()
		userDataChan <- data
	}()

	select {
	case allUsersData = <-userDataChan:
		// Successfully got user data
	case <-ctx.Done():
		glog.Errorf("DataWatcher: Timeout getting all users data")
		return
	}

	if len(allUsersData) == 0 {
		glog.Infof("DataWatcher: No users data found, processing cycle completed in %v", time.Since(processingStart))
		return
	}

	glog.Infof("DataWatcher: Found %d users to process", len(allUsersData))
	totalProcessed := int64(0)
	totalMoved := int64(0)

	// Process users in batches to avoid holding locks too long
	// 批量处理用户以避免长时间持有锁
	const batchSize = 5
	userCount := 0
	userBatch := make([]string, 0, batchSize)
	userDataBatch := make(map[string]*types.UserData)

	for userID, userData := range allUsersData {
		userBatch = append(userBatch, userID)
		userDataBatch[userID] = userData
		userCount++

		// Process batch when it's full or we've reached the end
		// 当批次满或到达末尾时处理批次
		if len(userBatch) >= batchSize || userCount == len(allUsersData) {
			batchProcessed, batchMoved := dw.processUserBatch(ctx, userBatch, userDataBatch)
			totalProcessed += batchProcessed
			totalMoved += batchMoved

			// Clear batch for next iteration
			// 清除批次以进行下一次迭代
			userBatch = userBatch[:0]
			userDataBatch = make(map[string]*types.UserData)

			// Check timeout between batches
			// 在批次之间检查超时
			select {
			case <-ctx.Done():
				glog.Errorf("DataWatcher: Timeout during batch processing after processing %d users", userCount)
				return
			default:
			}
		}
	}

	// Update metrics
	// 更新指标
	dw.metricsMutex.Lock()
	dw.totalAppsProcessed += totalProcessed
	dw.totalAppsMoved += totalMoved
	dw.metricsMutex.Unlock()

	processingDuration := time.Since(processingStart)
	if totalMoved > 0 {
		glog.Infof("DataWatcher: Processing cycle completed in %v - %d apps processed, %d moved to AppInfoLatest",
			processingDuration, totalProcessed, totalMoved)
	} else {
		glog.Infof("DataWatcher: Processing cycle completed in %v - %d apps processed, no moves needed",
			processingDuration, totalProcessed)
	}
}

// processUserBatch processes a batch of users
// processUserBatch 处理一批用户
func (dw *DataWatcher) processUserBatch(ctx context.Context, userIDs []string, userDataMap map[string]*types.UserData) (int64, int64) {
	totalProcessed := int64(0)
	totalMoved := int64(0)

	for i, userID := range userIDs {
		// Check timeout during batch processing
		// 在批处理过程中检查超时
		select {
		case <-ctx.Done():
			glog.Errorf("DataWatcher: Timeout during user batch processing (user %d/%d)", i+1, len(userIDs))
			return totalProcessed, totalMoved
		default:
		}

		userData := userDataMap[userID]
		if userData == nil {
			continue
		}

		glog.Infof("DataWatcher: Processing user %d/%d in batch: %s", i+1, len(userIDs), userID)
		processed, moved := dw.processUserData(userID, userData)
		totalProcessed += processed
		totalMoved += moved
		glog.V(2).Infof("DataWatcher: User %s completed: %d processed, %d moved", userID, processed, moved)
	}

	return totalProcessed, totalMoved
}

// processUserData processes a single user's data
// processUserData 处理单个用户的数据
func (dw *DataWatcher) processUserData(userID string, userData *UserData) (int64, int64) {
	if userData == nil {
		return 0, 0
	}

	// Step 1: Collect source data references under minimal lock
	// 步骤1：在最小锁保护下收集源数据引用
	userData.Mutex.RLock()
	sourceRefs := make(map[string]*SourceData)
	for sourceID, sourceData := range userData.Sources {
		sourceRefs[sourceID] = sourceData
	}
	userData.Mutex.RUnlock()

	// Step 2: Process each source without holding user lock
	// 步骤2：在不持有用户锁的情况下处理每个源
	totalProcessed := int64(0)
	totalMoved := int64(0)

	for sourceID, sourceData := range sourceRefs {
		processed, moved := dw.processSourceData(userID, sourceID, sourceData)
		totalProcessed += processed
		totalMoved += moved
	}

	// Step 3: If any pending apps were moved, calculate and update user data hash asynchronously
	// 步骤3：如果有任何待处理应用被移动，异步计算并更新用户数据hash
	if totalMoved > 0 {
		glog.Infof("DataWatcher: %d apps moved for user %s, scheduling delayed hash calculation", totalMoved, userID)

		// Check if hash calculation is already in progress for this user
		// 检查此用户是否已有hash计算正在进行
		dw.hashMutex.Lock()
		if dw.activeHashCalculations[userID] {
			dw.hashMutex.Unlock()
			glog.Warningf("DataWatcher: Hash calculation already in progress for user %s, skipping", userID)
			return totalProcessed, totalMoved
		}
		dw.activeHashCalculations[userID] = true
		dw.hashMutex.Unlock()

		// Schedule hash calculation with a small delay to ensure all locks are released
		// 安排带小延迟的hash计算以确保所有锁都已释放
		go func() {
			defer func() {
				// Clean up tracking when done
				// 完成时清理跟踪
				dw.hashMutex.Lock()
				delete(dw.activeHashCalculations, userID)
				dw.hashMutex.Unlock()
			}()

			// Wait a short time to ensure all source processing locks are released
			// 等待短时间以确保所有源处理锁都已释放
			time.Sleep(100 * time.Millisecond)
			glog.Infof("DataWatcher: Starting delayed hash calculation for user %s", userID)
			dw.calculateAndSetUserHashAsync(userID, userData)
		}()
	} else {
		glog.V(2).Infof("DataWatcher: No apps moved for user %s, skipping hash calculation", userID)
	}

	return totalProcessed, totalMoved
}

// calculateAndSetUserHash calculates and sets hash for user data
// calculateAndSetUserHash 计算并设置用户数据的hash
func (dw *DataWatcher) calculateAndSetUserHash(userID string, userData *UserData) {
	startTime := time.Now()
	glog.Infof("DataWatcher: Starting hash calculation for user %s", userID)

	// Get the original user data from cache manager instead of using the copied one
	// 从缓存管理器获取原始用户数据，而不是使用拷贝的数据
	originalUserData := dw.cacheManager.GetUserData(userID)
	if originalUserData == nil {
		glog.Errorf("DataWatcher: Original user data not found in cache for user %s", userID)
		return
	}

	// Try to acquire user data lock with timeout
	// 尝试带超时的用户数据锁获取
	userLockAcquired := make(chan bool, 1)
	go func() {
		originalUserData.Mutex.RLock()
		userLockAcquired <- true
	}()

	select {
	case <-userLockAcquired:
		// Lock acquired, proceed with hash calculation
		defer originalUserData.Mutex.RUnlock()
		glog.Infof("DataWatcher: User lock acquired for user %s", userID)
	case <-time.After(5 * time.Second):
		glog.Errorf("DataWatcher: Timeout acquiring user lock for hash calculation, user %s", userID)
		return
	}

	// Quick hash calculation using direct access (already have user lock)
	// 使用直接访问进行快速hash计算（已有用户锁）
	glog.Infof("DataWatcher: Performing direct hash calculation for user %s", userID)

	// Simple hash based on basic user data without nested locks
	// 基于基本用户数据的简单hash，无需嵌套锁
	hashInput := fmt.Sprintf("user:%s|sources:%d|timestamp:%d",
		userID, len(originalUserData.Sources), time.Now().Unix())

	newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))

	duration := time.Since(startTime)
	glog.Infof("DataWatcher: Direct hash calculation completed in %v for user %s, hash=%s", duration, userID, newHash)

	if newHash == "" {
		glog.Infof("DataWatcher: No hash calculated for user %s (empty data)", userID)
		return
	}

	// Update hash on the original user data (we already have the lock)
	// 在原始用户数据上更新hash（我们已有锁）
	oldHash := originalUserData.Hash
	if oldHash == newHash {
		glog.Infof("DataWatcher: Hash unchanged for user %s", userID)
		return
	}

	// Release read lock and acquire write lock for hash update
	// 释放读锁并获取写锁以更新hash
	originalUserData.Mutex.RUnlock()
	originalUserData.Mutex.Lock()
	originalUserData.Hash = newHash
	originalUserData.Mutex.Unlock()

	// Re-acquire read lock for the defer statement
	// 为defer语句重新获取读锁
	originalUserData.Mutex.RLock()

	glog.Infof("DataWatcher: Hash updated for user %s", userID)

	// Immediately verify the hash was set correctly
	// 立即验证hash是否正确设置
	glog.Infof("DataWatcher: Verification - originalUserData.Hash = '%s' for user %s", originalUserData.Hash, userID)

	// Also verify through cache manager
	// 同时通过缓存管理器验证
	if dw.cacheManager != nil {
		verifyUserData := dw.cacheManager.GetUserData(userID)
		if verifyUserData != nil {
			verifyUserData.Mutex.RLock()
			verifyHash := verifyUserData.Hash
			verifyUserData.Mutex.RUnlock()
			glog.Infof("DataWatcher: Verification via CacheManager - hash = '%s' for user %s", verifyHash, userID)
		} else {
			glog.Errorf("DataWatcher: Verification failed - CacheManager.GetUserData returned nil for user %s", userID)
		}

		// Also verify through GetAllUsersData
		// 同时通过GetAllUsersData验证
		allUsers := dw.cacheManager.GetAllUsersData()
		if adminUser, exists := allUsers[userID]; exists {
			glog.Infof("DataWatcher: Verification via GetAllUsersData - hash = '%s' for user %s", adminUser.Hash, userID)
		} else {
			glog.Errorf("DataWatcher: Verification failed - GetAllUsersData does not contain user %s", userID)
		}
	}

	glog.Infof("DataWatcher: Updated user data hash for user=%s (old=%s, new=%s) in %v",
		userID, oldHash, newHash, duration)

	// Trigger sync to Redis for user data update
	// 为用户数据更新触发Redis同步
	if dw.cacheManager != nil {
		glog.Infof("DataWatcher: Triggering sync to Redis for user %s", userID)
		go func(userID string, cacheManager *CacheManager) {
			// Add nil checks before calling sync
			// 在调用同步之前添加nil检查
			if cacheManager == nil {
				glog.Errorf("DataWatcher: CacheManager is nil, cannot trigger user sync after hash update")
				return
			}
			if userID == "" {
				glog.Errorf("DataWatcher: Invalid userID, cannot trigger user sync")
				return
			}

			// Trigger user data sync directly instead of using SetAppData
			// 直接触发用户数据同步而不是使用SetAppData
			cacheManager.requestSync(SyncRequest{
				UserID: userID,
				Type:   SyncUser,
			})
			glog.Infof("DataWatcher: Successfully triggered sync to Redis for user %s", userID)
		}(userID, dw.cacheManager)
	}

	glog.Infof("DataWatcher: Hash calculation method completed for user %s", userID)
}

// calculateAndSetUserHashAsync calculates and sets hash for user data asynchronously
// calculateAndSetUserHashAsync 异步计算并设置用户数据的hash
func (dw *DataWatcher) calculateAndSetUserHashAsync(userID string, userData *UserData) {
	glog.Infof("DataWatcher: Starting async hash calculation for user %s", userID)

	// Add timeout to prevent hanging
	// 添加超时以防止挂起
	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("DataWatcher: Panic during hash calculation for user %s: %v", userID, r)
			}
		}()
		glog.Infof("DataWatcher: Hash calculation goroutine started for user %s", userID)
		dw.calculateAndSetUserHash(userID, userData)
		glog.Infof("DataWatcher: Hash calculation goroutine completed for user %s", userID)
		done <- true
	}()

	select {
	case <-done:
		// Hash calculation completed successfully
		// hash计算成功完成
		glog.Infof("DataWatcher: Hash calculation finished successfully for user %s", userID)
	case <-time.After(10 * time.Second):
		glog.Errorf("DataWatcher: Hash calculation timeout for user %s after 10 seconds", userID)
	}
}

// createUserDataSnapshot creates a snapshot of user data for hash calculation
// createUserDataSnapshot 为hash计算创建用户数据快照
func (dw *DataWatcher) createUserDataSnapshot(userID string, userData *UserData) (*UserDataSnapshot, error) {
	// This method is now unused - using direct hash calculation instead
	// 此方法现在未使用 - 改为使用直接hash计算
	return nil, fmt.Errorf("snapshot method deprecated")
}

// Snapshot data structures for lock-free hash calculation
// 用于无锁hash计算的快照数据结构

type UserDataSnapshot struct {
	Hash    string
	Sources map[string]*SourceDataSnapshot
}

func (s *UserDataSnapshot) GetSources() map[string]utils.SourceDataInterface {
	result := make(map[string]utils.SourceDataInterface)
	for sourceID, sourceData := range s.Sources {
		result[sourceID] = sourceData
	}
	return result
}

func (s *UserDataSnapshot) GetHash() string {
	return s.Hash
}

func (s *UserDataSnapshot) SetHash(hash string) {
	s.Hash = hash
}

type SourceDataSnapshot struct {
	AppStateLatest []interface{}
	AppInfoLatest  []interface{}
	Others         *OthersSnapshot
}

func (s *SourceDataSnapshot) GetAppStateLatest() []interface{} {
	return s.AppStateLatest
}

func (s *SourceDataSnapshot) GetAppInfoLatest() []interface{} {
	return s.AppInfoLatest
}

func (s *SourceDataSnapshot) GetOthers() utils.OthersInterface {
	if s.Others == nil {
		return nil
	}
	return s.Others
}

type OthersSnapshot struct {
	Topics     []interface{}
	TopicLists []interface{}
	Recommends []interface{}
	Pages      []interface{}
}

func (s *OthersSnapshot) GetTopics() []interface{} {
	return s.Topics
}

func (s *OthersSnapshot) GetTopicLists() []interface{} {
	return s.TopicLists
}

func (s *OthersSnapshot) GetRecommends() []interface{} {
	return s.Recommends
}

func (s *OthersSnapshot) GetPages() []interface{} {
	return s.Pages
}

// processSourceData processes a single source's data
// processSourceData 处理单个源的数据
func (dw *DataWatcher) processSourceData(userID, sourceID string, sourceData *SourceData) (int64, int64) {
	if sourceData == nil {
		return 0, 0
	}

	// Create timeout context for this source processing
	// 为此源处理创建超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var pendingApps []*AppInfoLatestPendingData
	var completedApps []*AppInfoLatestPendingData
	// Initialize as empty slice instead of nil to ensure it's never null
	// 初始化为空slice而不是nil，确保它永远不会是null
	remainingPendingApps := make([]*AppInfoLatestPendingData, 0)

	glog.V(2).Infof("DataWatcher: Acquiring read lock to check pending apps for user=%s, source=%s", userID, sourceID)

	// Step 1: Quick check and data copy with minimal lock time
	// 步骤1：最小锁时间的快速检查和数据复制
	func() {
		sourceData.Mutex.RLock()
		defer sourceData.Mutex.RUnlock()

		// Quick check - if no pending apps, exit early
		// 快速检查 - 如果没有待处理应用，提早退出
		if len(sourceData.AppInfoLatestPending) == 0 {
			glog.V(2).Infof("DataWatcher: No pending apps for user=%s, source=%s", userID, sourceID)
			return
		}

		// Copy pending apps for processing outside the lock
		// 复制待处理应用以在锁外处理
		pendingApps = make([]*AppInfoLatestPendingData, len(sourceData.AppInfoLatestPending))
		copy(pendingApps, sourceData.AppInfoLatestPending)
	}()

	// Early exit if no pending apps found
	// 如果没有找到待处理应用则提早退出
	if len(pendingApps) == 0 {
		return 0, 0
	}

	glog.Infof("DataWatcher: Found %d pending apps to check for user=%s, source=%s", len(pendingApps), userID, sourceID)

	totalProcessed := int64(len(pendingApps))

	// Step 2: Check completion status without any locks
	// 步骤2：在没有任何锁的情况下检查完成状态
	for i, pendingApp := range pendingApps {
		// Check context timeout during processing
		// 在处理过程中检查上下文超时
		select {
		case <-ctx.Done():
			glog.Errorf("DataWatcher: Timeout during pending apps processing for user=%s, source=%s (processed %d/%d)",
				userID, sourceID, i, len(pendingApps))
			return totalProcessed, 0
		default:
		}

		// Use timeout-protected hydration check
		// 使用超时保护的水合检查
		if dw.isAppHydrationCompletedWithTimeout(ctx, pendingApp) {
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

	// Step 3: Only acquire write lock if we have changes to make
	// 步骤3：只有在需要进行更改时才获取写锁
	if len(completedApps) == 0 {
		glog.V(2).Infof("DataWatcher: No completed apps found for user=%s, source=%s", userID, sourceID)
		return totalProcessed, 0
	}

	glog.Infof("DataWatcher: Found %d completed apps, acquiring write lock for user=%s, source=%s",
		len(completedApps), userID, sourceID)

	// Step 4: Attempt write lock with shorter timeout to avoid long waits
	// 步骤4：尝试获取写锁，使用更短的超时以避免长时间等待
	lockCtx, lockCancel := context.WithTimeout(ctx, 5*time.Second)
	defer lockCancel()

	lockStartTime := time.Now()
	lockAcquired := false

	// Try to acquire write lock with timeout
	// 尝试获取带超时的写锁
	writeLockChan := make(chan bool, 1)
	go func() {
		sourceData.Mutex.Lock()
		writeLockChan <- true
	}()

	select {
	case <-writeLockChan:
		lockAcquired = true
		lockAcquireTime := time.Since(lockStartTime)
		glog.Infof("DataWatcher: Write lock acquired in %v for user=%s, source=%s", lockAcquireTime, userID, sourceID)

		defer func() {
			sourceData.Mutex.Unlock()
			totalLockTime := time.Since(lockStartTime)
			glog.Infof("DataWatcher: Write lock released after %v for user=%s, source=%s", totalLockTime, userID, sourceID)
		}()
	case <-lockCtx.Done():
		glog.Warningf("DataWatcher: Timeout acquiring write lock for user=%s, source=%s, will retry next cycle", userID, sourceID)
		return totalProcessed, 0
	}

	if !lockAcquired {
		return totalProcessed, 0
	}

	// Step 5: Re-verify data hasn't changed during our processing
	// 步骤5：重新验证数据在我们处理期间没有发生变化
	currentPendingCount := len(sourceData.AppInfoLatestPending)
	if currentPendingCount != len(pendingApps) {
		glog.Warningf("DataWatcher: Pending apps changed during processing for user=%s, source=%s (was %d, now %d), will retry next cycle",
			userID, sourceID, len(pendingApps), currentPendingCount)
		return totalProcessed, 0
	}

	// Step 6: Move completed apps to AppInfoLatest (under write lock)
	// 步骤6：将完成的应用移动到AppInfoLatest（在写锁下）
	actualMovedCount := int64(0)
	for _, completedApp := range completedApps {
		latestApp := dw.convertPendingToLatest(completedApp)
		if latestApp != nil {
			sourceData.AppInfoLatest = append(sourceData.AppInfoLatest, latestApp)
			actualMovedCount++
		} else {
			// Log when an app fails conversion
			// 记录应用转换失败的情况
			appID := dw.getAppID(completedApp)
			glog.Warningf("DataWatcher: Failed to convert completed app %s to latest format (user=%s, source=%s)",
				appID, userID, sourceID)
		}
	}

	// Update pending apps list (remove completed ones)
	// 更新待处理应用列表（移除已完成的）
	sourceData.AppInfoLatestPending = remainingPendingApps

	totalMoved := actualMovedCount

	if totalMoved > 0 {
		glog.Infof("DataWatcher: Moved %d completed apps from pending to latest for user=%s, source=%s",
			totalMoved, userID, sourceID)

		// Trigger sync to Redis for this source (async to avoid holding lock)
		// 为此源触发Redis同步（异步以避免持有锁）
		go func(userID, sourceID string, cacheManager *CacheManager) {
			// Add nil checks and validation before calling SetAppData
			// 在调用SetAppData之前添加nil检查和验证
			if cacheManager == nil {
				glog.Errorf("DataWatcher: CacheManager is nil, cannot trigger sync after moving apps")
				return
			}
			if userID == "" || sourceID == "" {
				glog.Errorf("DataWatcher: Invalid userID or sourceID, cannot trigger sync (userID=%s, sourceID=%s)", userID, sourceID)
				return
			}

			if err := cacheManager.SetAppData(userID, sourceID, AppInfoLatest,
				map[string]interface{}{"sync_trigger": time.Now().Unix()}); err != nil {
				glog.Errorf("DataWatcher: Failed to trigger sync after moving apps: %v", err)
			}
		}(userID, sourceID, dw.cacheManager)
	} else if len(completedApps) > 0 {
		// Log when completed apps exist but none were moved
		// 当存在已完成应用但未移动任何应用时记录
		glog.Warningf("DataWatcher: Found %d completed apps but none were moved to latest (user=%s, source=%s)",
			len(completedApps), userID, sourceID)
	}

	return totalProcessed, totalMoved
}

// isAppHydrationCompletedWithTimeout checks if app hydration is completed with timeout protection
// isAppHydrationCompletedWithTimeout 带超时保护检查应用水合是否完成
func (dw *DataWatcher) isAppHydrationCompletedWithTimeout(ctx context.Context, pendingApp *AppInfoLatestPendingData) bool {
	if pendingApp == nil {
		glog.V(2).Infof("DataWatcher: isAppHydrationCompletedWithTimeout called with nil pendingApp")
		return false
	}
	if dw.hydrator == nil {
		glog.Errorf("DataWatcher: Hydrator is nil, cannot check hydration completion")
		return false
	}

	// Create a channel to receive the result
	// 创建通道接收结果
	resultChan := make(chan bool, 1)

	// Run hydration check in a goroutine with timeout
	// 在协程中运行带超时的水合检查
	go func() {
		defer func() {
			if r := recover(); r != nil {
				glog.Errorf("DataWatcher: Panic in hydration check: %v", r)
				resultChan <- false
			}
		}()

		result := dw.hydrator.isAppHydrationComplete(pendingApp)
		resultChan <- result
	}()

	// Wait for result or timeout
	// 等待结果或超时
	select {
	case result := <-resultChan:
		return result
	case <-ctx.Done():
		appID := dw.getAppID(pendingApp)
		glog.Warningf("DataWatcher: Timeout checking hydration completion for app=%s", appID)
		return false
	}
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
		glog.Warningf("DataWatcher: convertPendingToLatest called with nil pendingApp")
		return nil
	}

	// Validate that the pending app has essential data
	// 验证待处理应用是否包含基本数据
	hasRawData := pendingApp.RawData != nil
	hasAppInfo := pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil
	hasPackageInfo := pendingApp.RawPackage != "" || pendingApp.RenderedPackage != ""

	// Return nil if no essential data is present
	// 如果没有基本数据则返回nil
	if !hasRawData && !hasAppInfo && !hasPackageInfo {
		appID := dw.getAppID(pendingApp)
		glog.Warningf("DataWatcher: Skipping conversion of pending app %s - no essential data found", appID)
		return nil
	}

	// Additional validation for data integrity
	// 额外的数据完整性验证
	if hasRawData && (pendingApp.RawData.AppID == "" && pendingApp.RawData.ID == "" && pendingApp.RawData.Name == "") {
		glog.Warningf("DataWatcher: Skipping conversion - RawData exists but lacks identifying information")
		return nil
	}

	if hasAppInfo && (pendingApp.AppInfo.AppEntry.AppID == "" && pendingApp.AppInfo.AppEntry.ID == "" && pendingApp.AppInfo.AppEntry.Name == "") {
		glog.Warningf("DataWatcher: Skipping conversion - AppInfo exists but lacks identifying information")
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

	// Create AppSimpleInfo from available data
	// 从可用数据创建AppSimpleInfo
	latestApp.AppSimpleInfo = dw.createAppSimpleInfo(pendingApp)

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

// createAppSimpleInfo creates an AppSimpleInfo from pending app data
// createAppSimpleInfo 从待处理应用数据创建AppSimpleInfo
func (dw *DataWatcher) createAppSimpleInfo(pendingApp *AppInfoLatestPendingData) *AppSimpleInfo {
	if pendingApp == nil {
		return nil
	}

	appSimpleInfo := &AppSimpleInfo{}

	// Extract information from RawData if available
	// 如果可用，从RawData中提取信息
	if pendingApp.RawData != nil {
		// Use AppID as the primary identifier
		// 使用AppID作为主要标识符
		if pendingApp.RawData.AppID != "" {
			appSimpleInfo.AppID = pendingApp.RawData.AppID
		} else if pendingApp.RawData.ID != "" {
			appSimpleInfo.AppID = pendingApp.RawData.ID
		}

		// Use Name or Title for AppName
		// 使用Name或Title作为AppName
		if pendingApp.RawData.Name != "" {
			appSimpleInfo.AppName = pendingApp.RawData.Name
		} else if pendingApp.RawData.Title != "" {
			appSimpleInfo.AppName = pendingApp.RawData.Title
		}

		// Use Icon for AppIcon
		// 使用Icon作为AppIcon
		appSimpleInfo.AppIcon = pendingApp.RawData.Icon

		// Use Description for AppDescription
		// 使用Description作为AppDescription
		appSimpleInfo.AppDescription = pendingApp.RawData.Description

		// Use Version for AppVersion
		// 使用Version作为AppVersion
		appSimpleInfo.AppVersion = pendingApp.RawData.Version
	}

	// Fallback to AppInfo data if RawData is insufficient
	// 如果RawData不足，则回退到AppInfo数据
	if pendingApp.AppInfo != nil && pendingApp.AppInfo.AppEntry != nil {
		entry := pendingApp.AppInfo.AppEntry

		// Fill missing AppID
		// 填充缺失的AppID
		if appSimpleInfo.AppID == "" {
			if entry.AppID != "" {
				appSimpleInfo.AppID = entry.AppID
			} else if entry.ID != "" {
				appSimpleInfo.AppID = entry.ID
			}
		}

		// Fill missing AppName
		// 填充缺失的AppName
		if appSimpleInfo.AppName == "" {
			if entry.Name != "" {
				appSimpleInfo.AppName = entry.Name
			} else if entry.Title != "" {
				appSimpleInfo.AppName = entry.Title
			}
		}

		// Fill missing AppIcon
		// 填充缺失的AppIcon
		if appSimpleInfo.AppIcon == "" {
			appSimpleInfo.AppIcon = entry.Icon
		}

		// Fill missing AppDescription
		// 填充缺失的AppDescription
		if appSimpleInfo.AppDescription == "" {
			appSimpleInfo.AppDescription = entry.Description
		}

		// Fill missing AppVersion
		// 填充缺失的AppVersion
		if appSimpleInfo.AppVersion == "" {
			appSimpleInfo.AppVersion = entry.Version
		}
	}

	// Use pendingApp version if still empty
	// 如果仍为空，使用pendingApp的版本
	if appSimpleInfo.AppVersion == "" && pendingApp.Version != "" {
		appSimpleInfo.AppVersion = pendingApp.Version
	}

	// Return nil if no essential information is available
	// 如果没有基本信息可用，返回nil
	if appSimpleInfo.AppID == "" && appSimpleInfo.AppName == "" {
		glog.Warningf("DataWatcher: createAppSimpleInfo - no essential app information available")
		return nil
	}

	return appSimpleInfo
}
