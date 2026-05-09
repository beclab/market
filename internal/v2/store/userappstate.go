package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/db"
)

// stateReasonMaxLen / stateProgressMaxLen mirror the user_application_states
// VARCHAR widths in migration 00004. The DAO truncates over-length values
// so PG never rejects a write because the upstream NATS payload was
// noisier than expected.
const (
	stateReasonMaxLen   = 200
	stateProgressMaxLen = 10
	stateOpIDMaxLen     = 64
	stateOpTypeMaxLen   = 32
)

// ErrUserApplicationStateNotFound is returned by readers / linkers when no
// user_applications row exists for the requested (user, source, app)
// tuple. user_application_states rows always FK to user_applications, so
// the absence of the parent is the proximate cause.
var ErrUserApplicationStateNotFound = errors.New("user_applications row not found for user_application_states write")

// PendingStateInput captures everything UpsertPendingState needs to insert
// (or refresh) the state row when a Market-initiated operation kicks off.
// It is invoked from API handlers before TaskModule.AddTask so the per-app
// row is observable from the moment the user clicks; the matching task
// row is written separately by the task pipeline.
//
// OpType is the operation kind being initiated (install / uninstall /
// upgrade / clone / cancel / stop / resume). State is typically "pending"
// at this point but is exposed as an input so future call sites (e.g. an
// admin debug endpoint) can override it.
type PendingStateInput struct {
	UserID   string
	SourceID string
	AppID    string

	State  string // "pending" by default for the M3 path
	OpType string
}

// StateNATSUpdate captures the projection of an AppStateMessage that the
// DAO needs to upsert. The DAO does not import internal/v2/appinfo, so the
// caller (state.go) flattens the wire struct into this DTO.
//
// AppName is the value carried in NATS msg.Name; the DAO matches it
// against user_applications.app_name (and falls back to app_raw_name to
// cover clones, where the synthesized clone alias lives in app_name and
// the original app name lives in app_raw_name). NATS does NOT carry an
// app_id field — manifest ids are not part of the wire contract — so
// matching by app_id is not possible on this path.
//
// EventCreateTime is the parsed msg.CreateTime; it MUST be non-zero so the
// monotonic guard in UpsertStateFromNATS can reject out-of-order events.
//
// Entrances / SharedEntrances / StatusEntrances are pre-marshalled
// json.RawMessage payloads; the DAO writes them verbatim into the JSONB
// columns. nil means "this NATS event did not carry the block" — the DAO
// preserves the previously stored value (does not overwrite with NULL).
type StateNATSUpdate struct {
	UserID          string
	SourceID        string
	AppName         string
	State           string
	Reason          string
	Message         string
	Progress        string
	OpID            string
	OpType          string
	EventCreateTime time.Time
	Entrances       []byte // raw JSON or nil
	SharedEntrances []byte
	StatusEntrances []byte
}

// UpsertPendingState records that a Market-initiated operation is being
// dispatched for the (user, source, app) tuple. It UPSERTs by
// user_application_id so a second click on an in-flight app refreshes
// the same row rather than failing on the unique constraint.
//
// Column-level write contract:
//   - op_type             — overwritten to the new in.OpType so the
//     frontend can immediately tell which kind of
//     operation Market is dispatching ("running
//     (uninstalling)" UI badges etc.). When the
//     caller passes an empty OpType the previous
//     value is preserved.
//   - event_create_time   — set to NOW() so subsequent NATS events
//     whose createTime is later than this moment
//     pass the monotonic guard in UpsertStateFromNATS.
//   - state column        — NOT touched on the UPDATE branch. The state
//     column reflects what the app is actually
//     doing (per NATS events); Market overwriting
//     it on every click would briefly clobber a
//     legitimate "running" / "downloading" with a
//     Market-internal placeholder, then race the
//     NATS pipeline to recover. INSERT path uses
//     empty string as initial value (schema
//     default), letting NATS fill it on first
//     event.
//   - reason / message / progress / entrances / shared_entrances /
//     status_entrances    — NOT touched. Owned by NATS.
//   - installed_version / target_version
//     — NOT touched. Owned by CompleteTask /
//     FailTask in the task lifecycle.
//   - op_id               — NOT touched (preserved on UPDATE). At
//     pending-row creation Market has not yet
//     called app-service so the opID is unknown;
//     overwriting would defeat the executor's
//     subsequent LinkOpID call.
//
// op_id is intentionally not accepted as an input here: at this point
// Market has not yet called app-service so no opID is known. The task
// executor calls LinkOpID after the HTTP response to fill it in.
//
// in.State is accepted for forward compatibility but is currently
// ignored — the helper always uses the schema default (”) on INSERT
// and never touches state on UPDATE.
//
// Returns ErrUserApplicationStateNotFound if no user_applications row
// exists for the given (user, source, app) — typically a caller bug
// (hydration must have rendered the app for this user before the user
// can act on it).
func UpsertPendingState(ctx context.Context, in PendingStateInput) error {
	in.UserID = strings.TrimSpace(in.UserID)
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.AppID = strings.TrimSpace(in.AppID)
	if in.UserID == "" || in.SourceID == "" || in.AppID == "" {
		return fmt.Errorf("UpsertPendingState: empty UserID/SourceID/AppID: %+v", in)
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application state store usage")
	}

	const query = `
INSERT INTO user_application_states (
    user_application_id, state, op_type, event_create_time
)
SELECT ua.id, '', $1, NOW()
FROM user_applications ua
WHERE ua.user_id = $2 AND ua.source_id = $3 AND ua.app_id = $4
ON CONFLICT (user_application_id) DO UPDATE SET
    op_type           = COALESCE(NULLIF(EXCLUDED.op_type, ''), user_application_states.op_type),
    event_create_time = EXCLUDED.event_create_time
    -- state / reason / message / progress / entrances /
    -- shared_entrances / status_entrances are owned by the NATS
    -- pipeline and intentionally not touched here.
    --
    -- installed_version / target_version are owned by CompleteTask /
    -- FailTask and intentionally not touched here.
    --
    -- op_id is not known at pending-row write time; preserve the
    -- existing value so the executor's LinkOpID call can fill it in
    -- without racing this UPSERT.
`

	res := gdb.WithContext(ctx).Exec(query,
		truncate(in.OpType, stateOpTypeMaxLen),
		in.UserID, in.SourceID, in.AppID,
	)
	if err := res.Error; err != nil {
		return fmt.Errorf("upsert pending user_application_state (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppID, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: (user=%s source=%s app=%s)", ErrUserApplicationStateNotFound,
			in.UserID, in.SourceID, in.AppID)
	}
	return nil
}

// LinkOpID associates an app-service-issued op_id with the
// user_application_states row for (user, source, app). It is called by
// task executors after the app-service HTTP response carries the opID
// back, so subsequent NATS state events (which also carry the same opID)
// can be cross-referenced via task_records.op_id for audit / debug.
//
// LinkOpID does NOT update state / progress / event_create_time — those
// are owned by the NATS path. It only fills in op_id; if op_id is empty
// (caller failed to parse) the call is a silent no-op.
//
// The match is by (user, source, app) → user_applications.id →
// user_application_states.user_application_id. Returns
// ErrUserApplicationStateNotFound if no state row exists yet (which
// would mean UpsertPendingState was not called first — caller bug).
func LinkOpID(ctx context.Context, userID, sourceID, appID, opID string) error {
	opID = strings.TrimSpace(opID)
	if opID == "" {
		return nil
	}
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	appID = strings.TrimSpace(appID)
	if userID == "" || sourceID == "" || appID == "" {
		return fmt.Errorf("LinkOpID: empty userID/sourceID/appID")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before user application state store usage")
	}

	const query = `
UPDATE user_application_states
   SET op_id = $1
  FROM user_applications ua
 WHERE user_application_states.user_application_id = ua.id
   AND ua.user_id   = $2
   AND ua.source_id = $3
   AND ua.app_id    = $4
`
	res := gdb.WithContext(ctx).Exec(query, truncate(opID, stateOpIDMaxLen), userID, sourceID, appID)
	if err := res.Error; err != nil {
		return fmt.Errorf("link op_id to user_application_state (user=%s source=%s app=%s op_id=%s): %w",
			userID, sourceID, appID, opID, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: (user=%s source=%s app=%s)", ErrUserApplicationStateNotFound,
			userID, sourceID, appID)
	}
	return nil
}

// UpsertStateFromNATS persists a single NATS app-state event into
// user_application_states. It is the main writer fed by state.go's
// StateNotifier. Two paths converge here:
//
//   - Market-initiated operations: an UpsertPendingState row already
//     exists; this call updates state / progress / etc. and may overwrite
//     op_id / op_type if the NATS event carries non-empty values.
//
//   - Externally-initiated operations: no Market task / no API handler
//     write. This call is the FIRST writer for the row; the parent
//     user_applications row must already exist (hydration has rendered
//     the app for this user even though Market did not initiate the
//     operation).
//
// Monotonic guard: an event whose EventCreateTime is older than the
// stored event_create_time is silently dropped (returned bool=false).
// op_id / op_type are preserved across writes that arrive empty — a NATS
// event without an op_id does not erase a previously linked op_id.
//
// Returns (applied, error). applied=false with err=nil means the event
// was rejected by the monotonic guard (older than stored) or the parent
// user_applications row is absent (silently skipped, matching the
// "out-of-Market app" case where state.go's MarketSource guard normally
// catches it; we double-skip here for safety).
func UpsertStateFromNATS(ctx context.Context, in StateNATSUpdate) (bool, error) {
	in.UserID = strings.TrimSpace(in.UserID)
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.AppName = strings.TrimSpace(in.AppName)
	if in.UserID == "" || in.SourceID == "" || in.AppName == "" {
		return false, fmt.Errorf("UpsertStateFromNATS: empty UserID/SourceID/AppName")
	}
	if in.EventCreateTime.IsZero() {
		return false, fmt.Errorf("UpsertStateFromNATS: zero EventCreateTime; caller must parse msg.CreateTime")
	}

	gdb := db.Global()
	if gdb == nil {
		return false, fmt.Errorf("postgres not initialised; db.Open must run before user application state store usage")
	}

	// Match the user_applications row by app_name OR app_raw_name. NATS
	// only ever carries msg.Name (no manifest id), and for clones the
	// synthesized alias lives in app_name while the original app name
	// lives in app_raw_name; matching either column finds the right row
	// in both clone and non-clone cases (mirrors LookupAppLocator's
	// pattern). $13 is bound twice — once per side of the OR.
	const query = `
INSERT INTO user_application_states (
    user_application_id,
    state, reason, message, progress,
    op_id, op_type, event_create_time,
    entrances, shared_entrances, status_entrances
)
SELECT ua.id, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
FROM user_applications ua
WHERE ua.user_id = $11 AND ua.source_id = $12
  AND (ua.app_name = $13 OR ua.app_raw_name = $13)
ON CONFLICT (user_application_id) DO UPDATE SET
    state             = EXCLUDED.state,
    reason            = EXCLUDED.reason,
    message           = EXCLUDED.message,
    progress          = EXCLUDED.progress,
    -- Preserve previously linked op_id / op_type when the incoming event
    -- carries empty values (typical of externally-initiated operations).
    op_id             = COALESCE(NULLIF(EXCLUDED.op_id, ''), user_application_states.op_id),
    op_type           = COALESCE(NULLIF(EXCLUDED.op_type, ''), user_application_states.op_type),
    event_create_time = EXCLUDED.event_create_time,
    -- nil JSONB columns leave the existing payload alone — the NATS event
    -- did not carry that block, so we should not erase it.
    entrances         = COALESCE(EXCLUDED.entrances, user_application_states.entrances),
    shared_entrances  = COALESCE(EXCLUDED.shared_entrances, user_application_states.shared_entrances),
    status_entrances  = COALESCE(EXCLUDED.status_entrances, user_application_states.status_entrances)
WHERE user_application_states.event_create_time < EXCLUDED.event_create_time
`

	res := gdb.WithContext(ctx).Exec(query,
		in.State,
		truncate(in.Reason, stateReasonMaxLen),
		in.Message,
		truncate(in.Progress, stateProgressMaxLen),
		truncate(in.OpID, stateOpIDMaxLen),
		truncate(in.OpType, stateOpTypeMaxLen),
		in.EventCreateTime,
		nullableJSON(in.Entrances),
		nullableJSON(in.SharedEntrances),
		nullableJSON(in.StatusEntrances),
		in.UserID, in.SourceID, in.AppName,
	)
	if err := res.Error; err != nil {
		return false, fmt.Errorf("upsert user_application_state from NATS (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppName, err)
	}
	// res.RowsAffected == 0 means EITHER:
	//   - parent user_applications row is missing → INSERT's SELECT yielded
	//     0 rows → no row inserted (silent skip is correct: state.go's
	//     MarketSource guard plus this safety net both reject "app not in
	//     Market catalog" cases without surfacing an error);
	//   - the monotonic guard rejected the update (event older than stored).
	// Both outcomes are non-errors; the caller logs at V(2) on its side.
	return res.RowsAffected > 0, nil
}

// nullableJSON returns nil for nil/empty input so the SQL parameter is
// SQL NULL (preserved by the COALESCE clause), rather than a zero-length
// JSONB which would overwrite the existing payload with '{}' / '[]'.
func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// GetInstalledAppVersion returns the user_application_states.installed_version
// for the named app in the given source, used by cloneApp to discover which
// version of the original app is currently installed (the clone is meant to
// share the original chart's version, but the request body does not carry
// it so the helper bridges the gap).
//
// The match is by ua.app_name (not app_raw_name) because cloneApp's caller
// supplies the original app's name, not a clone alias; clones live behind
// a different name in user_applications and would not be the desired
// resolution target here.
//
// render_status is intentionally NOT filtered to 'success': the presence
// of a user_application_states row already implies app-service has
// reported a state for this row, which implies a successful render
// happened at some point. Filtering would also reject installed apps whose
// catalog entry was later revoked but whose runtime state is still valid.
//
// Returns ("", nil) when no row matches — same as the cache-side
// GetAppVersionFromState's (version, found=false) tuple, just collapsed
// into the (string, error) idiom store helpers favour.
func GetInstalledAppVersion(ctx context.Context, userID, sourceID, appName string) (string, error) {
	userID = strings.TrimSpace(userID)
	sourceID = strings.TrimSpace(sourceID)
	appName = strings.TrimSpace(appName)
	if userID == "" || sourceID == "" || appName == "" {
		return "", fmt.Errorf("GetInstalledAppVersion: empty userID/sourceID/appName")
	}

	gdb := db.Global()
	if gdb == nil {
		return "", fmt.Errorf("postgres not initialised; db.Open must run before user application state store usage")
	}

	const query = `
SELECT uas.installed_version
FROM user_application_states uas
JOIN user_applications ua ON uas.user_application_id = ua.id
WHERE ua.user_id   = ?
  AND ua.source_id = ?
  AND ua.app_name  = ?
LIMIT 1
`

	var versions []string
	if err := gdb.WithContext(ctx).Raw(query, userID, sourceID, appName).Scan(&versions).Error; err != nil {
		return "", fmt.Errorf("get installed app version (user=%s source=%s app=%s): %w",
			userID, sourceID, appName, err)
	}
	if len(versions) == 0 {
		return "", nil
	}
	return versions[0], nil
}
