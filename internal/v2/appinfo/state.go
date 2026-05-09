package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/nats-io/nats.go"

	"market/internal/v2/store"
)

// stateQueueCapacity bounds the in-process queue between NATS / external
// producers and the worker that persists to PG. Bounded so a slow PG
// (or a producer storm) cannot accumulate unbounded events in memory.
// At drop the producer logs a warning + increments a metric counter
// (StateNotifier.dropped); NATS-source drops are absorbed by the legacy
// cache path that subscribes to the same subject in parallel.
const stateQueueCapacity = 1024

// natsStateCreateTimeLayout matches the upstream nano-precision RFC3339
// format used by app-service in AppStateMessage.CreateTime
// (e.g. "2006-01-02T15:04:05.999999999Z"). The layout is deliberately a
// copy of datawatcher_state.go's nanoTimeLayout — kept independent here
// so state.go has no compile-time dependency on the legacy file.
var natsStateCreateTimeLayout = "2006-01-02T15:04:05.999999999Z"

// stateEvent is the unit the queue carries. It wraps an AppStateMessage
// (defined in datawatcher_state.go — same package) with provenance so
// downstream logs / metrics can distinguish which producer the event
// came from. The processing path is identical regardless of source.
type stateEvent struct {
	msg      AppStateMessage
	source   string // "nats" | future: "scc" | "manual" | ...
	enqueued time.Time
}

// StateNotifier subscribes to the app-state NATS subject in parallel to
// the legacy DataWatcherState and projects each event into the
// user_application_states PG table via store.UpsertStateFromNATS.
//
// Phase-1 contract:
//   - Cache-side state (DataWatcherState → cache.AppStateLatest) is
//     not touched. Both subscribers receive every NATS message and
//     write to their own backing stores. Frontend behaviour is driven
//     by the cache path; PG is observability-only until later phases
//     wire readers against it.
//   - StateNotifier produces no outbound NATS pushes itself; the
//     notifyStateChange method is a deliberate no-op stub left for the
//     phase-2 PR that turns off the cache path's StateMonitor and
//     centralises fan-out here.
//   - delayed-message queue / cache lookup / TaskModule reverse query
//     (the 220-line source resolution cascade in legacy
//     datawatcher_state.go) are intentionally absent — the API handler
//     pre-writes the user_application_states row, NATS msg.MarketSource
//     anchors it, and a monotonic event_create_time guard in the DAO
//     handles out-of-order delivery without a queue.
//
// External producers can feed this pipeline via EnqueueState. Phase 1
// has only one external producer (the NATS subscription itself);
// later phases plan to route StatusCorrectionChecker reconciliation
// events through the same entrypoint.
type StateNotifier struct {
	nc   *nats.Conn
	sub  *nats.Subscription
	subj string

	natsHost string
	natsPort string
	natsUser string
	natsPass string

	queue chan stateEvent

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	// dropped counts events rejected because the queue was full. Atomic
	// for lock-free producer increments; exposed via Dropped() for
	// future metric / debug surfaces.
	dropped uint64
}

// NewStateNotifier constructs a notifier with NATS connection parameters
// pulled from the same env variables as DataWatcherState, so existing
// deployments need no new configuration. The caller is responsible for
// calling Start to actually open the subscription, and Stop on shutdown.
func NewStateNotifier() *StateNotifier {
	ctx, cancel := context.WithCancel(context.Background())
	return &StateNotifier{
		subj:     getEnvOrDefault("NATS_SUBJECT_SYSTEM_APP_STATE", "os.application.*"),
		natsHost: getEnvOrDefault("NATS_HOST", "localhost"),
		natsPort: getEnvOrDefault("NATS_PORT", "4222"),
		natsUser: getEnvOrDefault("NATS_USERNAME", ""),
		natsPass: getEnvOrDefault("NATS_PASSWORD", ""),
		queue:    make(chan stateEvent, stateQueueCapacity),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start opens the NATS connection, subscribes to the configured subject,
// and starts the single processing goroutine. It is safe to call once
// per StateNotifier; subsequent calls error.
//
// Phase-1 behaviour mirrors DataWatcherState's Start: no-op in public
// environments. The legacy cache path is also disabled there, so this
// path has nothing to add by running.
func (s *StateNotifier) Start() error {
	if s.nc != nil {
		return fmt.Errorf("StateNotifier already started")
	}

	natsURL := fmt.Sprintf("nats://%s:%s", s.natsHost, s.natsPort)

	var opts []nats.Option
	if s.natsUser != "" && s.natsPass != "" {
		opts = append(opts, nats.UserInfo(s.natsUser, s.natsPass))
	}
	opts = append(opts,
		nats.DisconnectHandler(func(nc *nats.Conn) {
			glog.V(2).Infof("[state] NATS disconnected from %s", nc.ConnectedUrl())
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			glog.V(2).Infof("[state] NATS reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			glog.V(2).Infof("[state] NATS connection closed: %v", nc.LastError())
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			glog.Errorf("[state] NATS error: %v", err)
		}),
		nats.MaxReconnects(60),
		nats.ReconnectWait(2*time.Second),
	)

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return fmt.Errorf("[state] failed to connect to NATS at %s: %w", natsURL, err)
	}
	s.nc = nc

	sub, err := nc.Subscribe(s.subj, s.handleNATSMessage)
	if err != nil {
		nc.Close()
		s.nc = nil
		return fmt.Errorf("[state] failed to subscribe to %s: %w", s.subj, err)
	}
	s.sub = sub

	s.wg.Add(1)
	go s.worker()

	glog.V(2).Infof("[state] StateNotifier started, subject=%s, queue_cap=%d", s.subj, cap(s.queue))
	return nil
}

// Stop closes the NATS subscription and waits for the worker goroutine to
// drain any in-flight events before returning. Safe to call multiple times.
func (s *StateNotifier) Stop() error {
	s.cancel()

	if s.sub != nil {
		if err := s.sub.Unsubscribe(); err != nil {
			glog.Errorf("[state] unsubscribe failed: %v", err)
		}
		s.sub = nil
	}
	if s.nc != nil {
		s.nc.Close()
		s.nc = nil
	}

	s.wg.Wait()

	if dropped := atomic.LoadUint64(&s.dropped); dropped > 0 {
		glog.V(2).Infof("[state] StateNotifier stopped after dropping %d queue-full events", dropped)
	} else {
		glog.V(2).Info("[state] StateNotifier stopped cleanly")
	}
	return nil
}

// EnqueueState lets any in-process producer feed the state pipeline using
// the same shape NATS would deliver. Returns false if the queue is full
// and the event was dropped (caller may log / retry; phase 1 NATS path
// just relies on the legacy cache subscriber to absorb drops).
//
// Phase 1 only the internal NATS subscription calls this; later phases
// will route StatusCorrectionChecker reconciliation events through it
// too, sharing the PG write path and the eventual phase-2 push hook
// without a parallel pipeline.
func (s *StateNotifier) EnqueueState(msg AppStateMessage, source string) bool {
	ev := stateEvent{msg: msg, source: source, enqueued: time.Now()}
	select {
	case s.queue <- ev:
		return true
	case <-s.ctx.Done():
		return false
	default:
		atomic.AddUint64(&s.dropped, 1)
		glog.Warningf("[state] queue full (cap=%d), dropping event from %s: opID=%s app=%s state=%s",
			cap(s.queue), source, msg.OpID, msg.Name, msg.State)
		return false
	}
}

// Dropped returns the cumulative count of events the notifier has rejected
// because the queue was full. Exposed for future metric integration.
func (s *StateNotifier) Dropped() uint64 {
	return atomic.LoadUint64(&s.dropped)
}

// handleNATSMessage is the nats.MsgHandler bridging the NATS subscription
// to EnqueueState. JSON parse failures are logged and dropped silently
// (matches DataWatcherState's behaviour).
func (s *StateNotifier) handleNATSMessage(msg *nats.Msg) {
	var ev AppStateMessage
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		glog.Errorf("[state] bad NATS payload (size=%d): %v", len(msg.Data), err)
		return
	}
	s.EnqueueState(ev, "nats")
}

// worker drains the queue serially. Single worker keeps the implicit
// per-(user, source, app) ordering on monotonic guard happy without
// per-key sharding; throughput is well within PG's capability for the
// expected event volume (one event per app state transition per user).
func (s *StateNotifier) worker() {
	defer s.wg.Done()
	for {
		select {
		case ev := <-s.queue:
			s.processEvent(ev)
		case <-s.ctx.Done():
			// Context done: drain any remaining queued events so a
			// graceful shutdown doesn't lose work the producer
			// thought it had handed off.
			for {
				select {
				case ev := <-s.queue:
					s.processEvent(ev)
				default:
					return
				}
			}
		}
	}
}

// processEvent is the single per-event processing path: parse-time guard,
// PG persistence, push hook. Producers (NATS subscription, future SCC
// integration) all funnel through this exact sequence.
func (s *StateNotifier) processEvent(ev stateEvent) {
	msg := ev.msg

	// Without a (user, source, name, rawAppName) anchor the DAO has
	// nothing to write to. The DAO matches user_applications by
	// (app_name AND app_raw_name); both NATS fields are required so
	// clone vs. original disambiguates correctly. Externally-initiated
	// operations on apps not in any Market source land here too and
	// are skipped — the legacy cache path covers them via its own
	// multi-fallback resolution.
	if msg.User == "" || msg.MarketSource == "" || msg.Name == "" || msg.RawAppName == "" {
		glog.V(3).Infof("[state] skipping event without (user,source,name,rawAppName) anchor: source=%s opID=%q opType=%q name=%q rawAppName=%q user=%q marketSource=%q",
			ev.source, msg.OpID, msg.OpType, msg.Name, msg.RawAppName, msg.User, msg.MarketSource)
		return
	}

	// Parse the upstream createTime once. A missing / unparseable value
	// would defeat the monotonic guard, so we treat it as a hard failure
	// (log + drop) rather than substituting NOW() and risking a stale
	// event clobbering newer state.
	createTime, err := parseStateCreateTime(msg.CreateTime)
	if err != nil {
		glog.Errorf("[state] cannot parse CreateTime for app=%s user=%s state=%s source=%s opID=%s: %v",
			msg.Name, msg.User, msg.State, ev.source, msg.OpID, err)
		return
	}

	upd := store.StateNATSUpdate{
		UserID:          msg.User,
		SourceID:        msg.MarketSource,
		AppName:         msg.Name,
		AppRawName:      msg.RawAppName,
		State:           msg.State,
		Reason:          msg.Reason,
		Message:         msg.Message,
		Progress:        msg.Progress,
		OpID:            msg.OpID,
		OpType:          msg.OpType,
		EventCreateTime: createTime,
	}

	// Marshal entrance / shared / status payloads only when present so
	// the DAO writes SQL NULL (preserve previous JSONB) instead of an
	// empty array on no-op events.
	if len(msg.EntranceStatuses) > 0 {
		if buf, mErr := json.Marshal(msg.EntranceStatuses); mErr == nil {
			upd.StatusEntrances = buf
		} else {
			glog.Errorf("[state] marshal entranceStatuses failed (app=%s user=%s): %v", msg.Name, msg.User, mErr)
		}
	}
	if len(msg.SharedEntrances) > 0 {
		if buf, mErr := json.Marshal(msg.SharedEntrances); mErr == nil {
			upd.SharedEntrances = buf
		} else {
			glog.Errorf("[state] marshal sharedEntrances failed (app=%s user=%s): %v", msg.Name, msg.User, mErr)
		}
	}

	applied, err := store.UpsertStateFromNATS(s.ctx, upd)
	if err != nil {
		glog.Errorf("[state] persist NATS update failed (source=%s user=%s app=%s state=%s opID=%s): %v",
			ev.source, msg.User, msg.Name, msg.State, msg.OpID, err)
		return
	}
	if !applied {
		// Either the parent user_applications row is missing (app not
		// rendered for this user) or the monotonic guard rejected the
		// event as older than what we already have. Both are normal,
		// log at V(3) for diagnostic.
		glog.V(3).Infof("[state] NATS update not applied (source=%s user=%s app=%s state=%s opID=%s)",
			ev.source, msg.User, msg.Name, msg.State, msg.OpID)
		return
	}

	// Phase-2 fan-out hook. Currently a no-op; see notifyStateChange.
	s.notifyStateChange(msg)
}

// notifyStateChange is the phase-2 fan-out hook for state changes. It
// runs only after a successful UpsertStateFromNATS applied the row, so
// the push (when implemented) reflects the freshly persisted PG state.
//
// PHASE 1: intentionally no-op.
//
//	The legacy path (datawatcher_state.go → cacheManager.SetAppData →
//	utils.StateMonitor.NotifyStateChange → dataSender.SendAppInfoUpdate)
//	is still running and is the sole source of "app_info_update"
//	pushes during phase 1. Emitting from this function while the
//	legacy path is also emitting would produce duplicate client-side
//	events.
//
// PHASE 2: when the cache path's StateMonitor is retired (legacy
// datawatcher_state.go's NATS subscription unwired), fill this in with
// dataSender.SendAppInfoUpdate(...) using `msg` (or a re-read of the
// PG row, whichever the phase-2 design picks). The same PR MUST
// disable the legacy push in the same change set; do NOT enable this
// hook independently.
//
// TODO(phase2): wire dataSender push here.
func (s *StateNotifier) notifyStateChange(msg AppStateMessage) {
	// intentionally empty — see comment above
	_ = msg
}

// parseStateCreateTime parses the upstream nano-precision createTime.
// Returns an error if the input is empty so callers can drop the event
// rather than risk substituting NOW() and clobbering newer state via
// the monotonic guard.
func parseStateCreateTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("createTime is empty")
	}
	t, err := time.Parse(natsStateCreateTimeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse createTime %q: %w", s, err)
	}
	return t, nil
}
