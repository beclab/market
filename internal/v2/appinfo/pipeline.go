package appinfo

import (
	"context"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/types"

	"github.com/golang/glog"
)

// Pipeline orchestrates the serial execution of all data processing phases:
//
//	Phase 1: Syncer        - fetch remote app data
//	Phase 2: Hydrator      - process pending apps (hydration + move to Latest)
//	Phase 3: DataWatcherRepo - process chart-repo state changes
//	Phase 4: StatusCorrectionChecker - correct app running statuses
//	Phase 5: Hash calculation + ForceSync
const defaultHydrationConcurrency = 5

type Pipeline struct {
	cacheManager            *CacheManager
	cache                   *types.CacheData
	syncer                  *Syncer
	hydrator                *Hydrator
	dataWatcher             *DataWatcher
	dataWatcherRepo         *DataWatcherRepo
	statusCorrectionChecker *StatusCorrectionChecker

	runGuard             atomic.Bool
	stopChan             chan struct{}
	isRunning            atomic.Bool
	interval             time.Duration
	hydrationConcurrency int
}

func NewPipeline(cacheManager *CacheManager, cache *types.CacheData, interval time.Duration) *Pipeline {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	concurrency := defaultHydrationConcurrency
	if v, err := strconv.Atoi(os.Getenv("PIPELINE_HYDRATION_CONCURRENCY")); err == nil && v > 0 {
		concurrency = v
	}

	return &Pipeline{
		cacheManager:         cacheManager,
		cache:                cache,
		stopChan:             make(chan struct{}),
		interval:             interval,
		hydrationConcurrency: concurrency,
	}
}

func (p *Pipeline) SetSyncer(s *Syncer)                     { p.syncer = s }
func (p *Pipeline) SetHydrator(h *Hydrator)                 { p.hydrator = h }
func (p *Pipeline) SetDataWatcher(dw *DataWatcher)          { p.dataWatcher = dw }
func (p *Pipeline) SetDataWatcherRepo(dwr *DataWatcherRepo) { p.dataWatcherRepo = dwr }
func (p *Pipeline) SetStatusCorrectionChecker(scc *StatusCorrectionChecker) {
	p.statusCorrectionChecker = scc
}

func (p *Pipeline) Start(ctx context.Context) error {
	if p.isRunning.Load() {
		return nil
	}
	p.isRunning.Store(true)
	go p.loop(ctx)
	glog.Infof("Pipeline started with interval %v", p.interval)
	return nil
}

func (p *Pipeline) Stop() {
	if !p.isRunning.CompareAndSwap(true, false) {
		return
	}
	close(p.stopChan)
	glog.Info("Pipeline stopped")
}

func (p *Pipeline) loop(ctx context.Context) {
	glog.Info("Pipeline loop started")
	defer glog.Info("Pipeline loop stopped")

	p.run(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.run(ctx)
		}
	}
}

func (p *Pipeline) run(ctx context.Context) {
	if !p.runGuard.CompareAndSwap(false, true) {
		glog.Warning("Pipeline: another run in progress, skipping")
		return
	}
	defer p.runGuard.Store(false)

	glog.V(2).Info("Pipeline: [LOOP] cycle start")

	startTime := time.Now()

	// add check current all users in cluster
	p.cacheManager.RemoveDeletedUser()

	// Phase 1-4: only modify data, no hash calculation or ForceSync
	p.phaseSyncer(ctx)
	hydrateUsers := p.phaseHydrateApps(ctx)
	repoUsers := p.phaseDataWatcherRepo(ctx)
	statusUsers := p.phaseStatusCorrection(ctx)

	// Phase 5: merge all affected users + dirty users, calculate hash once, sync once
	allAffected := make(map[string]bool)
	for u := range hydrateUsers {
		allAffected[u] = true
	}
	for u := range repoUsers {
		allAffected[u] = true
	}
	for u := range statusUsers {
		allAffected[u] = true
	}
	// Collect dirty users from event-driven paths (DataWatcherState)
	if p.dataWatcher != nil {
		for u := range p.dataWatcher.CollectAndClearDirtyUsers() {
			allAffected[u] = true
		}
	}

	p.phaseHashAndSync(allAffected)

	cahedData := p.cacheManager.GetCachedData()

	glog.V(2).Infof("Pipeline: [LOOP] cycle completed in %v, cached: %s", time.Since(startTime), cahedData)
}

// phaseSyncer fetches remote data
func (p *Pipeline) phaseSyncer(ctx context.Context) {
	if p.syncer == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-p.stopChan:
		return
	default:
	}
	glog.V(3).Info("Pipeline Phase 1: Syncer")
	p.syncer.SyncOnce(ctx)
}

// phaseHydrateApps processes pending apps in concurrent batches through hydration + move to Latest.
// Batch size is controlled by hydrationConcurrency (default 5, env PIPELINE_HYDRATION_CONCURRENCY).
func (p *Pipeline) phaseHydrateApps(ctx context.Context) map[string]bool {
	affectedUsers := make(map[string]bool)
	if p.hydrator == nil || p.cacheManager == nil {
		return affectedUsers
	}

	count := p.cacheManager.RestoreRetryableFailedToPending(20)
	glog.Infof("Pipeline Phase 2: restore %d Failed to Pending", count)

	items := p.cacheManager.CollectAllPendingItems()

	if len(items) == 0 {
		glog.V(2).Info("Pipeline Phase 2: no pending apps to process")
		return affectedUsers
	}

	total := len(items)
	batchSize := p.hydrationConcurrency
	if batchSize <= 0 {
		batchSize = defaultHydrationConcurrency
	}

	// Filter out items whose user or source has been deleted since collection.
	// CollectAllPendingItems returns a snapshot; async deletions (RemoveUserData,
	// SyncMarketSourcesToCache) may have removed the user/source in the meantime.
	validItems := make([]PendingItem, 0, len(items))
	for _, item := range items {
		if p.cacheManager.GetSourceData(item.UserID, item.SourceID) == nil {
			appID, appName := getAppIdentifiers(item.Pending)
			glog.V(2).Infof("Pipeline Phase 2: skipping %s %s - user %s or source %s no longer exists",
				appID, appName, item.UserID, item.SourceID)
			continue
		}
		validItems = append(validItems, item)
	}

	if len(validItems) == 0 {
		glog.V(2).Infof("Pipeline Phase 2: all %d pending apps filtered out (user/source deleted)", total)
		return affectedUsers
	}

	if len(validItems) < total {
		glog.V(2).Infof("Pipeline Phase 2: %d/%d pending apps remain after filtering deleted users/sources",
			len(validItems), total)
	}

	total = len(validItems)
	glog.V(2).Infof("Pipeline Phase 2: processing %d pending apps (concurrency=%d)", total, batchSize)

	for batchStart := 0; batchStart < total; batchStart += batchSize {
		select {
		case <-ctx.Done():
			return affectedUsers
		case <-p.stopChan:
			return affectedUsers
		default:
		}

		batchEnd := batchStart + batchSize
		if batchEnd > total {
			batchEnd = total
		}
		batch := validItems[batchStart:batchEnd]

		// Log batch items
		for i, item := range batch {
			appID, appName := getAppIdentifiers(item.Pending)
			glog.V(2).Infof("Pipeline Phase 2: [%d/%d] %s %s (user=%s, source=%s)",
				batchStart+i+1, total, appID, appName, item.UserID, item.SourceID)
		}

		// Process batch concurrently
		type hydrateResult struct {
			idx      int
			hydrated bool
		}
		results := make([]hydrateResult, len(batch))
		var wg sync.WaitGroup

		for i, item := range batch {
			wg.Add(1)
			go func(idx int, it PendingItem) {
				defer wg.Done()
				results[idx] = hydrateResult{
					idx:      idx,
					hydrated: p.hydrator.HydrateSingleApp(ctx, it.UserID, it.SourceID, it.Pending),
				}
			}(i, item)
		}
		wg.Wait()

		// Move hydrated apps to Latest (sequential — writes to the same source slice)
		for i, item := range batch {
			if results[i].hydrated && p.dataWatcher != nil {
				p.dataWatcher.ProcessSingleAppToLatest(item.UserID, item.SourceID, item.Pending)
			}
			affectedUsers[item.UserID] = true
		}
	}

	return affectedUsers
}

// phaseDataWatcherRepo processes chart-repo state changes
func (p *Pipeline) phaseDataWatcherRepo(ctx context.Context) map[string]bool {
	if p.dataWatcherRepo == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return nil
	case <-p.stopChan:
		return nil
	default:
	}
	glog.V(2).Info("Pipeline Phase 3: DataWatcherRepo")
	return p.dataWatcherRepo.ProcessOnce()
}

// phaseStatusCorrection corrects app running statuses
func (p *Pipeline) phaseStatusCorrection(ctx context.Context) map[string]bool {
	if p.statusCorrectionChecker == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return nil
	case <-p.stopChan:
		return nil
	default:
	}
	glog.V(2).Info("Pipeline Phase 4: StatusCorrectionChecker")
	return p.statusCorrectionChecker.PerformStatusCheckOnce()
}

// phaseHashAndSync calculates user hashes for all affected users and syncs to Redis.
// This is the single point where hash calculation and ForceSync happen per Pipeline cycle.
func (p *Pipeline) phaseHashAndSync(affectedUsers map[string]bool) {
	if p.dataWatcher != nil && len(affectedUsers) > 0 {
		glog.V(2).Infof("Pipeline Phase 5: calculating hash for %d affected users", len(affectedUsers))
		for userID := range affectedUsers {
			userData := p.cacheManager.GetUserData(userID)
			if userData != nil {
				p.dataWatcher.CalculateAndSetUserHashDirect(userID, userData)
			}
		}
	}
	if p.cacheManager != nil {
		if err := p.cacheManager.ForceSync(); err != nil {
			glog.Errorf("Pipeline Phase 5: ForceSync rate limited: %v", err)
		}
	}
}

func getAppIdentifiers(pd *types.AppInfoLatestPendingData) (string, string) {
	if pd == nil || pd.RawData == nil {
		return "unknown", "unknown"
	}
	appID := pd.RawData.AppID
	if appID == "" {
		appID = pd.RawData.ID
	}
	return appID, pd.RawData.Name
}

// HydrateSingleApp runs hydration steps for a single app synchronously.
// Returns true if hydration completed and data is ready for move to Latest.
func (h *Hydrator) HydrateSingleApp(ctx context.Context, userID, sourceID string, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil || pendingData.RawData == nil {
		return false
	}

	appID := pendingData.RawData.AppID
	if appID == "" {
		appID = pendingData.RawData.ID
	}
	appName := pendingData.RawData.Name
	if appID == "" {
		return false
	}

	version := ""
	if pendingData.RawData != nil {
		version = pendingData.RawData.Version
	}

	if h.isAppInRenderFailedList(userID, sourceID, appID, appName, version) {
		glog.V(2).Infof("HydrateSingleApp: skipping %s(%s) (user=%s, source=%s) - in render failed list, will retry after cleanup",
			appID, appName, userID, sourceID)
		return false
	}

	if h.isAppHydrationComplete(pendingData) {
		return true
	}

	if h.isAppInLatestQueue(userID, sourceID, appID, appName, version) {
		glog.V(2).Infof("HydrateSingleApp: skipping %s(%s) (user=%s, source=%s) - already in latest queue with version %s",
			appID, appName, userID, sourceID, version)
		return false
	}

	appDataMap := h.convertApplicationInfoEntryToMap(pendingData.RawData)
	if len(appDataMap) == 0 {
		glog.V(2).Infof("HydrateSingleApp: skipping %s(%s) (user=%s, source=%s) - convertApplicationInfoEntryToMap returned empty",
			appID, appName, userID, sourceID)
		return false
	}

	var cacheManagerIface types.CacheManagerInterface
	if h.cacheManager != nil {
		cacheManagerIface = h.cacheManager
	}
	task := hydrationfn.NewHydrationTaskWithManager(
		userID, sourceID, appID,
		appDataMap, h.cache, cacheManagerIface, h.settingsManager,
	)

	glog.V(3).Infof("HydrateSingleApp: processing %s %s (user=%s, source=%s)", appID, appName, userID, sourceID)
	taskStartTime := time.Now()

	for _, step := range h.steps {
		if step.CanSkip(ctx, task) {
			task.IncrementStep()
			continue
		}
		if err := step.Execute(ctx, task); err != nil {
			task.SetError(err)
			failureReason := err.Error()
			failureStep := step.GetStepName()
			glog.Errorf("HydrateSingleApp: step %s failed for app %s(%s): %v", failureStep, appID, appName, err)
			h.moveTaskToRenderFailed(task, failureReason, failureStep)
			duration := time.Since(taskStartTime)
			h.markTaskFailed(task, taskStartTime, duration, failureStep, failureReason)
			return false
		}
		task.IncrementStep()
	}

	if !h.isAppHydrationComplete(pendingData) {
		glog.Warningf("HydrateSingleApp: steps completed but data incomplete for app %s(%s), will retry next cycle", appID, appName)
		return false
	}

	task.SetStatus(hydrationfn.TaskStatusCompleted)
	duration := time.Since(taskStartTime)
	h.markTaskCompleted(task, taskStartTime, duration)
	glog.V(2).Infof("HydrateSingleApp: completed for app %s(%s) in %v", appID, appName, duration)
	return true
}
