package appinfo

import (
	"context"
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
type Pipeline struct {
	cacheManager            *CacheManager
	cache                   *types.CacheData
	syncer                  *Syncer
	hydrator                *Hydrator
	dataWatcher             *DataWatcher
	dataWatcherRepo         *DataWatcherRepo
	statusCorrectionChecker *StatusCorrectionChecker

	mutex     sync.Mutex
	stopChan  chan struct{}
	isRunning atomic.Bool
	interval  time.Duration
}

func NewPipeline(cacheManager *CacheManager, cache *types.CacheData, interval time.Duration) *Pipeline {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Pipeline{
		cacheManager: cacheManager,
		cache:        cache,
		stopChan:     make(chan struct{}),
		interval:     interval,
	}
}

func (p *Pipeline) SetSyncer(s *Syncer)                                    { p.syncer = s }
func (p *Pipeline) SetHydrator(h *Hydrator)                                { p.hydrator = h }
func (p *Pipeline) SetDataWatcher(dw *DataWatcher)                         { p.dataWatcher = dw }
func (p *Pipeline) SetDataWatcherRepo(dwr *DataWatcherRepo)                { p.dataWatcherRepo = dwr }
func (p *Pipeline) SetStatusCorrectionChecker(scc *StatusCorrectionChecker) { p.statusCorrectionChecker = scc }

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
	if !p.isRunning.Load() {
		return
	}
	close(p.stopChan)
	p.isRunning.Store(false)
	glog.Info("Pipeline stopped")
}

func (p *Pipeline) loop(ctx context.Context) {
	glog.Info("Pipeline loop started")
	defer glog.Info("Pipeline loop stopped")

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
	if !p.mutex.TryLock() {
		glog.V(3).Info("Pipeline: another run in progress, skipping")
		return
	}
	defer p.mutex.Unlock()

	startTime := time.Now()

	p.phaseSyncer(ctx)
	affectedUsers := p.phaseHydrateApps(ctx)
	p.phaseDataWatcherRepo(ctx)
	p.phaseStatusCorrection(ctx)
	p.phaseHashAndSync(affectedUsers)

	if elapsed := time.Since(startTime); elapsed > 5*time.Second {
		glog.V(2).Infof("Pipeline: cycle completed in %v", elapsed)
	}
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

// phaseHydrateApps processes pending apps one by one through hydration + move to Latest
func (p *Pipeline) phaseHydrateApps(ctx context.Context) map[string]bool {
	affectedUsers := make(map[string]bool)
	if p.hydrator == nil || p.cacheManager == nil {
		return affectedUsers
	}

	items := p.cacheManager.CollectAllPendingItems()

	if len(items) == 0 {
		return affectedUsers
	}

	total := len(items)
	glog.V(2).Infof("Pipeline Phase 2: processing %d pending apps", total)

	for idx, item := range items {
		select {
		case <-ctx.Done():
			return affectedUsers
		case <-p.stopChan:
			return affectedUsers
		default:
		}

		appID, appName := getAppIdentifiers(item.Pending)
		glog.V(2).Infof("Pipeline Phase 2: [%d/%d] %s %s (user=%s, source=%s)",
			idx+1, total, appID, appName, item.UserID, item.SourceID)

		hydrated := p.hydrator.HydrateSingleApp(ctx, item.UserID, item.SourceID, item.Pending)
		if hydrated && p.dataWatcher != nil {
			p.dataWatcher.ProcessSingleAppToLatest(item.UserID, item.SourceID, item.Pending)
		}
		affectedUsers[item.UserID] = true
	}

	return affectedUsers
}

// phaseDataWatcherRepo processes chart-repo state changes
func (p *Pipeline) phaseDataWatcherRepo(ctx context.Context) {
	if p.dataWatcherRepo == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-p.stopChan:
		return
	default:
	}
	glog.V(3).Info("Pipeline Phase 3: DataWatcherRepo")
	p.dataWatcherRepo.ProcessOnce()
}

// phaseStatusCorrection corrects app running statuses
func (p *Pipeline) phaseStatusCorrection(ctx context.Context) {
	if p.statusCorrectionChecker == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-p.stopChan:
		return
	default:
	}
	glog.V(3).Info("Pipeline Phase 4: StatusCorrectionChecker")
	p.statusCorrectionChecker.PerformStatusCheckOnce()
}

// phaseHashAndSync calculates user hashes and syncs to Redis
func (p *Pipeline) phaseHashAndSync(affectedUsers map[string]bool) {
	if p.dataWatcher != nil && len(affectedUsers) > 0 {
		for userID := range affectedUsers {
			userData := p.cacheManager.GetUserData(userID)
			if userData != nil {
				p.dataWatcher.CalculateAndSetUserHashDirect(userID, userData)
			}
		}
	}
	if p.cacheManager != nil {
		if err := p.cacheManager.ForceSync(); err != nil {
			glog.Errorf("Pipeline: ForceSync failed: %v", err)
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

	if h.isAppInRenderFailedList(userID, sourceID, appID, appName) {
		return false
	}

	if h.isAppHydrationComplete(pendingData) {
		return true
	}

	version := ""
	if pendingData.RawData != nil {
		version = pendingData.RawData.Version
	}
	if h.isAppInLatestQueue(userID, sourceID, appID, appName, version) {
		return false
	}

	appDataMap := h.convertApplicationInfoEntryToMap(pendingData.RawData)
	if len(appDataMap) == 0 {
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
			failureReason := err.Error()
			failureStep := step.GetStepName()
			glog.Errorf("HydrateSingleApp: step %s failed for app %s %s: %v", failureStep, appID, appName, err)
			h.moveTaskToRenderFailed(task, failureReason, failureStep)
			duration := time.Since(taskStartTime)
			h.markTaskFailed(task, taskStartTime, duration, failureStep, failureReason)
			return false
		}
		task.IncrementStep()
	}

	if !h.isAppHydrationComplete(pendingData) {
		glog.Warningf("HydrateSingleApp: steps completed but data incomplete for app %s %s, will retry next cycle", appID, appName)
		return false
	}

	task.SetStatus(hydrationfn.TaskStatusCompleted)
	duration := time.Since(taskStartTime)
	h.markTaskCompleted(task, taskStartTime, duration)
	glog.V(2).Infof("HydrateSingleApp: completed for app %s %s in %v", appID, appName, duration)
	return true
}
