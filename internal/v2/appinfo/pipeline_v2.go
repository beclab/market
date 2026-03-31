package appinfo

import (
	"context"
	"fmt"
	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/appinfo/syncerfn"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
)

type SyncerRunFunc func(ctx context.Context) error
type HydratorRunFunc func(ctx context.Context) error

type PipelineV2 struct {
	syncerRun   SyncerRunFunc
	hydratorRun HydratorRunFunc

	syncEvery    time.Duration // 5m
	hydrateEvery time.Duration // 30s

	stopCh  chan struct{}
	started atomic.Bool
	running atomic.Bool
}

func NewPipelineV2() *PipelineV2 {
	var syncerRun = syncerfn.Syncer{}
	var hydrationRun = hydrationfn.Hydration{}

	return &PipelineV2{
		syncerRun:    syncerRun.Run,
		hydratorRun:  hydrationRun.Run,
		syncEvery:    5 * time.Minute,
		hydrateEvery: 30 * time.Second,
		stopCh:       make(chan struct{}),
	}
}

func (p *PipelineV2) Start(ctx context.Context) error {
	if p.syncerRun == nil {
		return fmt.Errorf("syncerRun is nil")
	}
	if p.hydratorRun == nil {
		return fmt.Errorf("hydratorRun is nil")
	}

	if !p.started.CompareAndSwap(false, true) {
		return nil
	}

	go p.loop(ctx)

	glog.Infof("Pipeline started (sync=%v, hydrator=%v)", p.syncEvery, p.hydrateEvery)

	return nil
}

func (p *PipelineV2) Stop() {
	if !p.started.CompareAndSwap(true, false) {
		return
	}
	close(p.stopCh)

	glog.Info("Pipeline stopped")
}

func (p *PipelineV2) loop(ctx context.Context) {
	nextSync := time.Now()
	nextHydrator := time.Now()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			now := time.Now()

			if !now.Before(nextSync) {
				p.runSyncCycle(ctx)
				nextSync = time.Now().Add(p.syncEvery)

				nextHydrator = time.Now().Add(p.hydrateEvery)
				continue
			}

			if !now.Before(nextHydrator) {
				p.runHydrator(ctx)
				nextHydrator = time.Now().Add(p.hydrateEvery)
			}
		}
	}
}

func (p *PipelineV2) runSyncCycle(ctx context.Context) {
	start := time.Now()
	glog.Info("Pipeline: sync cycle start")

	if err := p.syncerRun(ctx); err != nil {
		glog.Errorf("Pipeline: syncer failed: %v", err)
		return
	}

	if err := p.hydratorRun(ctx); err != nil {
		glog.Errorf("Pipeline: hydrator(after sync) failed: %v", err)
		return
	}

	glog.Infof("Pipeline: sync cycle done, cost=%v", time.Since(start))
}

func (p *PipelineV2) runHydrator(ctx context.Context) {
	if err := p.hydratorRun(ctx); err != nil {
		glog.Errorf("Pipeline: hydrator(30s) failed: %v", err)
	}
}
