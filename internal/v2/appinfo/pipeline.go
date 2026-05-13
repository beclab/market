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
	"market/internal/v2/watchers"

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
	// dataSender is held independently of dataWatcher so the
	// post-hydration NATS notification keeps working when datawatcher_app
	// is eventually retired. nil means "no NATS available", in which case
	// notifyRenderSuccess only logs.
	dataSender *DataSender

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
func (p *Pipeline) SetDataSender(ds *DataSender)            { p.dataSender = ds }
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

	// Phase 1-4: only modify data, no hash calculation or ForceSync.
	//
	// Phase ordering note: phaseDataWatcherRepo runs BEFORE phaseHydrateApps
	// so that "app_upload_completed" events land in the applications table
	// in time for the same cycle's hydration step to pick them up as a
	// fresh / version-drift candidate via store.ListRenderCandidates.
	// "image_info_updated" events patch user_applications.image_analysis
	// directly; the subsequent hydration step does NOT overwrite those
	// rows because ListRenderCandidates only selects rows in
	// render_status != 'success' or with metadata->>'version' drift.
	p.phaseSyncer(ctx)
	hydrateUsers := p.phaseHydrateApps(ctx)
	repoUsers := p.phaseDataWatcherRepo(ctx)
	// statusUsers := p.phaseStatusCorrection(ctx) // + todo need to remove

	// Phase 5: merge all affected users + dirty users, calculate hash once, sync once
	allAffected := make(map[string]bool)
	for u := range hydrateUsers {
		allAffected[u] = true
	}
	for u := range repoUsers {
		allAffected[u] = true
	}
	// for u := range statusUsers {
	// 	allAffected[u] = true
	// }
	// Collect dirty users from event-driven paths (DataWatcherState)
	// if p.dataWatcher != nil {
	// 	for u := range p.dataWatcher.CollectAndClearDirtyUsers() {
	// 		allAffected[u] = true
	// 	}
	// }

	// cahedData := p.cacheManager.GetCachedData()

	// glog.V(2).Infof("Pipeline: [LOOP] cycle completed in %v, cached: %s", time.Since(startTime), cahedData)
	glog.V(2).Infof("Pipeline: [LOOP] cycle completed in %v", time.Since(startTime))
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

	// GetUsers (instead of GetUserIDs) so the per-user UserDataInfo —
	// crucially Zone — is available at iteration time and can be
	// injected into each RenderCandidate without a second sync.Map
	// lookup later. Downstream EnrichEntranceURLs (run inside
	// TaskForApiStep) needs the zone to compute entrance URLs the
	// same way app-service does at install time.
	users := watchers.GetUsers()
	if len(users) == 0 {
		glog.V(2).Info("Pipeline Phase 2: no users available, skipping hydration")
		return affectedUsers
	}

	glog.V(2).Infof("Pipeline Phase 2: users list (count=%d)", len(users))

	candidates := make([]store.RenderCandidate, 0)
	for _, u := range users {
		select {
		case <-ctx.Done():
			return affectedUsers
		case <-p.stopChan:
			return affectedUsers
		default:
		}
		userCandidates, err := store.ListRenderCandidates(ctx, u.Name, hydrationCandidateLimitPerUser)
		if err != nil {
			glog.Errorf("Pipeline Phase 2: list render candidates for user %s failed: %v", u.Name, err)
			continue
		}
		// Inject Zone so it travels with the candidate down to the
		// HydrationTask and ultimately to TaskForApiStep. Empty zone
		// is OK — EnrichEntranceURLs no-ops in that case.
		for i := range userCandidates {
			userCandidates[i].UserZone = u.Zone
		}
		if len(userCandidates) > 0 {
			glog.V(3).Infof("Pipeline Phase 2: user=%s zone=%q, %d candidate(s)", u.Name, u.Zone, len(userCandidates))
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
				task, ok := p.hydrator.HydrateSingleApp(ctx, c)
				if !ok {
					return
				}
				p.notifyRenderSuccess(c, task)

			}(c)
		}
		wg.Wait()

		for _, c := range batch {
			affectedUsers[c.UserID] = true
		}
	}

	return affectedUsers
}

// notifyRenderSuccess emits the unified "new_app_ready" market system
// update after a successful hydration, matching the cache-era semantics.
// The first-install vs version-upgrade split exists only in the log line;
// the NATS payload is identical so the frontend keeps its single-event
// handler.
//
// The pushed app_info is composed from task.RenderedManifest (= the JSONB
// payload that was just persisted to user_applications) via
// types.ComposeApplicationInfoEntry, so subscribers see the per-user
// rendered manifest rather than the source-side applications.app_entry
// catalog snapshot. When the manifest is unexpectedly nil (would mean the
// hydration step success-path skipped manifest assembly), we fall back to
// the candidate's catalog entry so the notification still carries useful
// scalar info; this branch should be unreachable as long as
// TaskForApiStep enforces "success ⇒ raw_data" on chart-repo's response.
//
// Notifications are best-effort: a missing dataSender is silent, and a
// SendMarketSystemUpdate failure is logged but does not fail hydration.
func (p *Pipeline) notifyRenderSuccess(c store.RenderCandidate, task *hydrationfn.HydrationTask) {
	isUpgrade := c.ExistingRenderStatus == "success" &&
		c.ExistingAppVersion != c.AppVersion
	if isUpgrade {
		glog.V(2).Infof("hydration: version upgrade user=%s app=%s %s -> %s",
			c.UserID, c.AppID, c.ExistingAppVersion, c.AppVersion)
	} else {
		glog.V(2).Infof("hydration: first install user=%s app=%s version=%s",
			c.UserID, c.AppID, task.AppVersion)
	}

	if p.dataSender == nil {
		return
	}

	var appInfo interface{}
	if task != nil && task.RenderedManifest != nil {
		// Re-inject the identity / header scalars on Compose so the
		// pushed app_info carries non-empty id / appID / version /
		// cfgType / apiVersion. These five keys are not carried in the
		// JSONB manifest blocks (id/appID are not present on
		// *oac.AppConfiguration at all; version arrives nested under
		// metadata; cfgType arrives as olaresManifest.type), so without
		// re-injection ComposeApplicationInfoEntry would emit an entry
		// with empty strings for all five fields. See
		// types.EntryScalars for the per-field rationale.
		scalars := types.EntryScalars{
			ID:      task.AppID,
			AppID:   task.AppID,
			Version: task.AppVersion,
			CfgType: task.AppType,
		}
		if task.AppEntry != nil {
			scalars.ApiVersion = task.AppEntry.ApiVersion
		}
		entry, err := types.ComposeApplicationInfoEntry(task.RenderedManifest, scalars)
		if err != nil {
			glog.Errorf("hydration: compose app_info from rendered manifest failed for app %s (user=%s): %v; falling back to catalog entry",
				c.AppID, c.UserID, err)
			appInfo = c.AppEntry
		} else {
			appInfo = entry
		}
	} else {
		glog.Warningf("hydration: rendered manifest unavailable for app %s (user=%s); falling back to catalog entry in notification",
			c.AppID, c.UserID)
		appInfo = c.AppEntry
	}

	update := types.MarketSystemUpdate{
		Timestamp:  time.Now().Unix(),
		User:       c.UserID,
		NotifyType: "market_system_point",
		Point:      "new_app_ready",
		Extensions: map[string]string{
			"app_name":    c.AppName,
			"app_version": c.AppVersion,
			"source":      c.SourceID,
		},
		ExtensionsObj: map[string]interface{}{
			"app_info": appInfo,
		},
	}
	if err := p.dataSender.SendMarketSystemUpdate(update); err != nil {
		glog.Errorf("hydration: notify render success failed for app %s (user=%s): %v",
			c.AppID, c.UserID, err)
	}
}

// phaseDataWatcherRepo processes chart-repo state changes
func (p *Pipeline) phaseDataWatcherRepo(ctx context.Context) map[string]bool {
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

// HydrateSingleApp runs hydration steps for a single PG-sourced candidate.
// On success it returns the completed task (with task.RenderedManifest set
// by TaskForApiStep, so the caller can compose the per-user app_info for
// notifications) plus true. On failure it returns nil, false; the (user,
// source, app) row in user_applications has already been upserted to
// render_status='failed' via store.MarkRenderFailed by then.
func (h *Hydrator) HydrateSingleApp(ctx context.Context, c store.RenderCandidate) (*hydrationfn.HydrationTask, bool) {
	if c.AppEntry == nil || strings.TrimSpace(c.AppID) == "" {
		return nil, false
	}

	pending := &types.AppInfoLatestPendingData{
		Type:    types.AppInfoLatestPending,
		Version: c.AppVersion,
		RawData: c.AppEntry,
	}

	// SourceType is sourced from market_sources via store.ListRenderCandidates'
	// INNER JOIN, so c.SourceType is guaranteed non-empty here. No fallback to
	// the settings manager is needed; doing one would mask an invariant
	// violation (applications.source_id without a matching market_sources row)
	// rather than letting it surface as a missing-candidate error.
	task := hydrationfn.NewHydrationTaskFromInput(hydrationfn.HydrationTaskInput{
		UserID:          c.UserID,
		UserZone:        c.UserZone,
		SourceID:        c.SourceID,
		SourceType:      c.SourceType,
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
			return nil, false
		}
		task.IncrementStep()
	}

	task.SetStatus(hydrationfn.TaskStatusCompleted)
	duration := time.Since(taskStartTime)
	h.markTaskCompleted(task, taskStartTime, duration)
	glog.V(2).Infof("HydrateSingleApp: completed for app %s(%s) in %v", c.AppID, c.AppName, duration)
	return task, true
}
