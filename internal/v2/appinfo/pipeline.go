package appinfo

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
)

// SyncPipeline orchestrates Syncer, Hydrator, and DataWatcher in serial mode.
//
// In pipeline mode the three stages execute sequentially in a single loop:
//
//	Syncer.executeSyncCycle()       – fetch remote data into AppInfoLatestPending
//	Hydrator.ProcessAllPendingSync() – hydrate all pending items and wait for completion
//	DataWatcher.processCompletedApps() – move completed items to AppInfoLatest
//
// This eliminates lock contention between the three components and guarantees
// that each stage sees a consistent snapshot produced by the previous stage.
type SyncPipeline struct {
	syncer      *Syncer
	hydrator    *Hydrator
	dataWatcher *DataWatcher

	interval  time.Duration
	stopChan  chan struct{}
	isRunning atomic.Bool
	mutex     sync.Mutex

	// Per-cycle timeout; protects against a single stage hanging forever.
	cycleTimeout time.Duration

	// Metrics
	totalCycles     atomic.Int64
	successCycles   atomic.Int64
	failureCycles   atomic.Int64
	lastCycleTime   atomic.Value // time.Time
	lastCycleDur    atomic.Value // time.Duration
	lastCycleErr    atomic.Value // string
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
func (p *SyncPipeline) runCycle(parentCtx context.Context) {
	cycleStart := time.Now()
	p.totalCycles.Add(1)
	p.lastCycleTime.Store(cycleStart)

	ctx, cancel := context.WithTimeout(parentCtx, p.cycleTimeout)
	defer cancel()

	glog.V(2).Info("========== PIPELINE CYCLE START ==========")

	// Stage 1: Syncer
	glog.V(2).Info("Pipeline Stage 1/3: Syncer")
	syncErr := p.syncer.executeSyncCycle(ctx)
	if syncErr != nil {
		glog.Errorf("Pipeline Stage 1 (Syncer) failed: %v", syncErr)
	}

	// Stage 2: Hydrator – process all newly pending items synchronously
	glog.V(2).Info("Pipeline Stage 2/3: Hydrator")
	if err := p.hydrator.ProcessAllPendingSync(ctx); err != nil {
		glog.Errorf("Pipeline Stage 2 (Hydrator) failed: %v", err)
		p.recordCycleResult(cycleStart, fmt.Errorf("hydrator: %w", err))
		return
	}

	// Stage 3: DataWatcher
	glog.V(2).Info("Pipeline Stage 3/3: DataWatcher")
	p.dataWatcher.processCompletedApps()

	dur := time.Since(cycleStart)
	p.recordCycleResult(cycleStart, nil)
	glog.V(2).Infof("========== PIPELINE CYCLE END (duration=%v) ==========", dur)
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
