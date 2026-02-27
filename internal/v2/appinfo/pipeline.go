package appinfo

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
)

// SyncPipeline orchestrates Syncer, Hydrator, and DataWatcher in strict serial
// order within a single goroutine loop:
//
//	Stage 1 — Syncer.executeSyncCycle()              (fetch remote -> AppInfoLatestPending)
//	          ↓  blocks until complete
//	Stage 2 — Hydrator.ProcessAllPendingSync()       (hydrate all pending items, wait for every task)
//	          ↓  blocks until complete
//	Stage 3 — DataWatcher.ProcessCompletedAppsSync() (move completed items -> AppInfoLatest)
//	          ↓  blocks until complete
//	          sleep(interval) → next cycle
//
// Key guarantees compared to the concurrent mode:
//   - No async hydrationNotifier: CacheManager does NOT call
//     hydrationNotifier.NotifyPendingDataUpdate() in pipeline mode, so the
//     Syncer will never cause the Hydrator to run concurrently.
//   - Hydrator's pendingDataMonitor goroutine is NOT started; the pipeline
//     explicitly calls ProcessAllPendingSync().
//   - DataWatcher's 30-second watchLoop goroutine is NOT started; the pipeline
//     explicitly calls ProcessCompletedAppsSync().
type SyncPipeline struct {
	syncer      *Syncer
	hydrator    *Hydrator
	dataWatcher *DataWatcher

	interval  time.Duration
	stopChan  chan struct{}
	isRunning atomic.Bool
	mutex     sync.Mutex

	cycleTimeout time.Duration

	// Metrics
	totalCycles   atomic.Int64
	successCycles atomic.Int64
	failureCycles atomic.Int64
	lastCycleTime atomic.Value // time.Time
	lastCycleDur  atomic.Value // time.Duration
	lastCycleErr  atomic.Value // string
}

// PipelineConfig holds configuration for the SyncPipeline.
type PipelineConfig struct {
	Interval     time.Duration `json:"interval"`
	CycleTimeout time.Duration `json:"cycle_timeout"`
}

// DefaultPipelineConfig returns sensible defaults.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		Interval:     5 * time.Minute,
		CycleTimeout: 30 * time.Minute,
	}
}

// NewSyncPipeline creates a pipeline that drives the three components serially.
func NewSyncPipeline(syncer *Syncer, hydrator *Hydrator, dataWatcher *DataWatcher, cfg PipelineConfig) *SyncPipeline {
	p := &SyncPipeline{
		syncer:       syncer,
		hydrator:     hydrator,
		dataWatcher:  dataWatcher,
		interval:     cfg.Interval,
		cycleTimeout: cfg.CycleTimeout,
		stopChan:     make(chan struct{}),
	}
	p.lastCycleTime.Store(time.Time{})
	p.lastCycleDur.Store(time.Duration(0))
	p.lastCycleErr.Store("")
	return p
}

// Start launches the pipeline loop in a background goroutine.
func (p *SyncPipeline) Start(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.isRunning.Load() {
		return fmt.Errorf("pipeline is already running")
	}
	p.isRunning.Store(true)

	glog.Infof("Starting SyncPipeline (interval=%v, cycleTimeout=%v)", p.interval, p.cycleTimeout)
	go p.loop(ctx)
	return nil
}

// Stop signals the pipeline to stop.
func (p *SyncPipeline) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.isRunning.Load() {
		return
	}
	glog.Info("Stopping SyncPipeline...")
	close(p.stopChan)
	p.isRunning.Store(false)
}

// IsRunning returns whether the pipeline is active.
func (p *SyncPipeline) IsRunning() bool {
	return p.isRunning.Load()
}

func (p *SyncPipeline) loop(ctx context.Context) {
	defer func() {
		p.isRunning.Store(false)
		glog.Info("SyncPipeline loop stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopChan:
			return
		default:
		}

		p.runCycle(ctx)

		select {
		case <-ctx.Done():
			return
		case <-p.stopChan:
			return
		case <-time.After(p.interval):
		}
	}
}

// runCycle executes one complete Syncer -> Hydrator -> DataWatcher cycle.
//
// The three stages are strictly sequential: each stage blocks until it finishes
// before the next one starts. This is the core serial guarantee.
func (p *SyncPipeline) runCycle(parentCtx context.Context) {
	cycleStart := time.Now()
	p.totalCycles.Add(1)
	p.lastCycleTime.Store(cycleStart)

	ctx, cancel := context.WithTimeout(parentCtx, p.cycleTimeout)
	defer cancel()

	glog.Infof("========== PIPELINE CYCLE START ==========")

	// ── Stage 1: Syncer ─────────────────────────────────────────────────
	// Fetches remote data and writes to AppInfoLatestPending.
	// Blocks until the full sync cycle completes (or fails).
	glog.Infof("Pipeline Stage 1/3: Syncer — fetching remote data")
	stage1Start := time.Now()

	syncErr := p.syncer.executeSyncCycle(ctx)
	if syncErr != nil {
		glog.Errorf("Pipeline Stage 1 (Syncer) failed after %v: %v", time.Since(stage1Start), syncErr)
		// Even if sync fails, there may be previously pending data that still
		// needs hydration, so we continue to Stage 2.
	} else {
		glog.Infof("Pipeline Stage 1 (Syncer) completed in %v", time.Since(stage1Start))
	}

	// ── Stage 2: Hydrator ───────────────────────────────────────────────
	// Collects all pending items under a short RLock, then processes each
	// one serially in THIS goroutine (no queue, no workers). Each hydration
	// step can freely acquire/release cacheManager locks without competing
	// with any other goroutine.
	glog.Infof("Pipeline Stage 2/3: Hydrator — processing all pending items serially")
	stage2Start := time.Now()

	if err := p.hydrator.ProcessAllPendingSerial(ctx); err != nil {
		glog.Errorf("Pipeline Stage 2 (Hydrator) failed after %v: %v", time.Since(stage2Start), err)
		p.recordCycleResult(cycleStart, fmt.Errorf("hydrator: %w", err))
		glog.Errorf("========== PIPELINE CYCLE ABORTED ==========")
		return
	}
	glog.Infof("Pipeline Stage 2 (Hydrator) completed in %v", time.Since(stage2Start))

	// ── Stage 3: DataWatcher ────────────────────────────────────────────
	// Checks all pending items for completed hydration status, moves them
	// from AppInfoLatestPending to AppInfoLatest, and recalculates user hashes.
	glog.Infof("Pipeline Stage 3/3: DataWatcher — moving completed apps to latest")
	stage3Start := time.Now()

	p.dataWatcher.ProcessCompletedAppsSync()

	glog.Infof("Pipeline Stage 3 (DataWatcher) completed in %v", time.Since(stage3Start))

	// ── Cycle done ──────────────────────────────────────────────────────
	dur := time.Since(cycleStart)
	p.recordCycleResult(cycleStart, nil)
	glog.Infof("========== PIPELINE CYCLE END (total=%v, sync=%v, hydrate=%v, watch=%v) ==========",
		dur, stage1Start.Sub(cycleStart)+time.Since(stage1Start),
		time.Since(stage2Start), time.Since(stage3Start))
}

func (p *SyncPipeline) recordCycleResult(start time.Time, err error) {
	dur := time.Since(start)
	p.lastCycleDur.Store(dur)

	if err != nil {
		p.failureCycles.Add(1)
		p.lastCycleErr.Store(err.Error())
	} else {
		p.successCycles.Add(1)
		p.lastCycleErr.Store("")
	}
}

// PipelineMetrics contains runtime metrics.
type PipelineMetrics struct {
	IsRunning     bool          `json:"is_running"`
	Interval      time.Duration `json:"interval"`
	TotalCycles   int64         `json:"total_cycles"`
	SuccessCycles int64         `json:"success_cycles"`
	FailureCycles int64         `json:"failure_cycles"`
	LastCycleTime time.Time     `json:"last_cycle_time"`
	LastCycleDur  time.Duration `json:"last_cycle_duration"`
	LastCycleErr  string        `json:"last_cycle_error,omitempty"`
}

// GetMetrics returns pipeline metrics.
func (p *SyncPipeline) GetMetrics() PipelineMetrics {
	var lastTime time.Time
	if t, ok := p.lastCycleTime.Load().(time.Time); ok {
		lastTime = t
	}
	var lastDur time.Duration
	if d, ok := p.lastCycleDur.Load().(time.Duration); ok {
		lastDur = d
	}
	var lastErr string
	if s, ok := p.lastCycleErr.Load().(string); ok {
		lastErr = s
	}

	return PipelineMetrics{
		IsRunning:     p.isRunning.Load(),
		Interval:      p.interval,
		TotalCycles:   p.totalCycles.Load(),
		SuccessCycles: p.successCycles.Load(),
		FailureCycles: p.failureCycles.Load(),
		LastCycleTime: lastTime,
		LastCycleDur:  lastDur,
		LastCycleErr:  lastErr,
	}
}
