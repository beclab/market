package appinfo

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/store"
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

// hydrationCandidateLimitPerUser caps how many candidates ListRenderCandidates
// returns per user per cycle. The PG candidate query is cheap, but we still
// bound it to avoid letting a single user hog an entire cycle when the
// applications table is freshly populated.
const hydrationCandidateLimitPerUser = 200

// phaseHydrateApps drives hydration off PG: for every known user it pulls
// (source, app) candidates from store.ListRenderCandidates (rows that are
// new, in pending/failed status, or whose manifest_version drifted away
// from applications.app_version) and sends them through the hydration steps
// concurrently. Per-batch concurrency is controlled by hydrationConcurrency
// (default 5, env PIPELINE_HYDRATION_CONCURRENCY).
func (p *Pipeline) phaseHydrateApps(ctx context.Context) map[string]bool {
	affectedUsers := make(map[string]bool)
	if p.hydrator == nil || p.cacheManager == nil {
		return affectedUsers
	}

	users := p.cacheManager.GetOrCreateUserIDs("system")
	if len(users) == 0 {
		glog.V(2).Info("Pipeline Phase 2: no users available, skipping hydration")
		return affectedUsers
	}

	candidates := make([]store.RenderCandidate, 0)
	for _, userID := range users {
		select {
		case <-ctx.Done():
			return affectedUsers
		case <-p.stopChan:
			return affectedUsers
		default:
		}
		userCandidates, err := store.ListRenderCandidates(ctx, userID, hydrationCandidateLimitPerUser)
		if err != nil {
			glog.Errorf("Pipeline Phase 2: list render candidates for user %s failed: %v", userID, err)
			continue
		}
		if len(userCandidates) > 0 {
			glog.V(3).Infof("Pipeline Phase 2: user=%s, %d candidate(s)", userID, len(userCandidates))
		}
		candidates = append(candidates, userCandidates...)
	}

	total := len(candidates)
	if total == 0 {
		glog.V(2).Info("Pipeline Phase 2: no render candidates to process")
		return affectedUsers
	}

	batchSize := p.hydrationConcurrency
	if batchSize <= 0 {
		batchSize = defaultHydrationConcurrency
	}
	glog.V(2).Infof("Pipeline Phase 2: processing %d render candidates (concurrency=%d)", total, batchSize)

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
		batch := candidates[batchStart:batchEnd]

		for i, c := range batch {
			glog.V(2).Infof("Pipeline Phase 2: [%d/%d] %s %s (user=%s, source=%s)",
				batchStart+i+1, total, c.AppID, c.AppName, c.UserID, c.SourceID)
		}

		var wg sync.WaitGroup
		for _, c := range batch {
			wg.Add(1)
			go func(c store.RenderCandidate) {
				defer wg.Done()
				p.hydrator.HydrateSingleApp(ctx, c)
			}(c)
		}
		wg.Wait()

		for _, c := range batch {
			affectedUsers[c.UserID] = true
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

// HydrateSingleApp runs hydration steps for a single PG-sourced candidate.
// Returns true when chart-repo accepted the render (= store.UpsertRenderSuccess
// happened in TaskForApiStep) and false otherwise. On failure, the (user,
// source, app) row in user_applications is upserted to render_status='failed'
// via store.MarkRenderFailed.
func (h *Hydrator) HydrateSingleApp(ctx context.Context, c store.RenderCandidate) bool {
	if c.AppEntry == nil || strings.TrimSpace(c.AppID) == "" {
		return false
	}

	pending := &types.AppInfoLatestPendingData{
		Type:    types.AppInfoLatestPending,
		Version: c.AppVersion,
		RawData: c.AppEntry,
	}

	sourceType := ""
	if h.settingsManager != nil {
		for _, s := range h.settingsManager.GetActiveMarketSources() {
			if s.ID == c.SourceID {
				sourceType = string(s.Type)
				break
			}
		}
	}

	task := hydrationfn.NewHydrationTaskFromInput(hydrationfn.HydrationTaskInput{
		UserID:          c.UserID,
		SourceID:        c.SourceID,
		SourceType:      sourceType,
		AppID:           c.AppID,
		AppName:         c.AppName,
		AppVersion:      c.AppVersion,
		AppType:         c.AppType,
		AppEntry:        c.AppEntry,
		PendingPayload:  pending,
		SettingsManager: h.settingsManager,
	})

	glog.V(3).Infof("HydrateSingleApp: processing %s %s (user=%s, source=%s)", c.AppID, c.AppName, c.UserID, c.SourceID)
	taskStartTime := time.Now()

	for _, step := range h.steps {
		if step.CanSkip(ctx, task) {
			task.IncrementStep()
			continue
		}
		if err := step.Execute(ctx, task); err != nil {
			task.SetError(err)
			failureStep := step.GetStepName()
			failureReason := err.Error()
			glog.Errorf("HydrateSingleApp: step %s failed for app %s(%s): %v", failureStep, c.AppID, c.AppName, err)

			if perr := store.MarkRenderFailed(ctx, store.MarkRenderFailedInput{
				UserID:      c.UserID,
				SourceID:    c.SourceID,
				AppID:       c.AppID,
				AppName:     c.AppName,
				AppRawID:    c.AppID,
				AppRawName:  c.AppName,
				RenderError: failureReason,
			}); perr != nil {
				glog.Errorf("HydrateSingleApp: persist failure for app %s(%s) failed: %v", c.AppID, c.AppName, perr)
			}

			duration := time.Since(taskStartTime)
			h.markTaskFailed(task, taskStartTime, duration, failureStep, failureReason)
			return false
		}
		task.IncrementStep()
	}

	task.SetStatus(hydrationfn.TaskStatusCompleted)
	duration := time.Since(taskStartTime)
	h.markTaskCompleted(task, taskStartTime, duration)
	glog.V(2).Infof("HydrateSingleApp: completed for app %s(%s) in %v", c.AppID, c.AppName, duration)
	return true
}
