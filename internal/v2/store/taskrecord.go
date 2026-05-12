package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"market/internal/v2/db"

	"gorm.io/gorm"
)

// Task status constants mirror the iota values declared in package task
// (task.Pending=1, task.Running=2, task.Completed=3, task.Failed=4,
// task.Canceled=5). They are duplicated here as plain ints so the store
// package does not need to import internal/v2/task — that would be a
// circular dependency since task imports store. The contract is "task
// owns the enum, store accepts the int"; mismatches would surface as
// behaviour bugs in the very first caller switch and are caught early.
const (
	taskStatusPending   = 1
	taskStatusRunning   = 2
	taskStatusCompleted = 3
	taskStatusFailed    = 4
	taskStatusCanceled  = 5
)

// CreateTaskInput captures everything CreateTask needs to insert a new
// task_records row in Pending state. PendingState, when non-nil, asks
// CreateTask to also upsert the user_application_states pending row in
// the same transaction — this replaces the API handler's separate
// writePendingState call so a click on "install" lands either both
// rows or none, with no half-written state.
//
// Callers that do not want the state-table side-effect (e.g. tasks
// that have no associated user_application — clones today, future
// admin tools) leave PendingState nil; CreateTask then only writes
// task_records.
type CreateTaskInput struct {
	TaskID      string
	Type        int
	AppName     string
	UserAccount string
	Metadata    map[string]any
	CreatedAt   time.Time

	PendingState *PendingTaskState
}

// PendingTaskState locates the user_application_states row CreateTask
// should refresh. UserID/SourceID/AppID match the user_applications
// row hydration has rendered; OpType is "install" / "uninstall" /
// "upgrade" / "cancel" verbatim, mirroring the values the NATS
// pipeline writes so cross-pipeline events overlay cleanly.
type PendingTaskState struct {
	UserID, SourceID, AppID, OpType string
}

// CreateTask inserts a fresh task_records row (status=Pending) and,
// when PendingState is non-nil, upserts the user_application_states
// pending row in the same PG transaction. Both writes succeed atomically
// or neither does — callers do not need to worry about the half-state
// "task exists but state row absent" that the legacy persistTask +
// writePendingState double-call could leave behind.
//
// task_records uses INSERT (not UPSERT). A duplicate task_id surfaces
// as a UNIQUE-violation error rather than silently overwriting an
// existing row. Caller (TaskModule) generates task_id from
// timestamp+random so collisions are caller bugs, not race conditions.
func CreateTask(ctx context.Context, in CreateTaskInput) error {
	if strings.TrimSpace(in.TaskID) == "" {
		return fmt.Errorf("CreateTask: empty TaskID")
	}

	metadataJSON := "{}"
	if in.Metadata != nil {
		b, err := json.Marshal(in.Metadata)
		if err != nil {
			return fmt.Errorf("CreateTask: marshal metadata: %w", err)
		}
		metadataJSON = string(b)
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before CreateTask")
	}

	createdAt := in.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		const insertTaskQuery = `
INSERT INTO task_records (
    task_id, type, status, app_name, user_account, op_id, metadata,
    result, error_msg, created_at, updated_at
) VALUES (
    ?, ?, ?, ?, ?, '', ?, '', '', ?, NOW()
)`
		if err := tx.Exec(insertTaskQuery,
			in.TaskID, in.Type, taskStatusPending,
			in.AppName, in.UserAccount, metadataJSON, createdAt,
		).Error; err != nil {
			return fmt.Errorf("CreateTask: insert task_records: %w", err)
		}

		if in.PendingState != nil {
			err := upsertPendingStateInTx(tx, *in.PendingState)
			// ErrUserApplicationStateNotFound is a tolerable miss: the
			// task is being created against an app that has no
			// user_applications row (cloneApp's synthesized newAppName
			// is the canonical case; admin / debug paths the same).
			// Surface it via an error-free commit so the task_records
			// insert lands; the caller logs at V(3) for diagnostics.
			// Other errors (PG down, constraint violation) still
			// roll back the transaction, refusing to create a task
			// whose state plumbing is broken in unexpected ways.
			if err != nil && !errors.Is(err, ErrUserApplicationStateNotFound) {
				return err
			}
		}
		return nil
	})
}

// StartTask flips an existing task_records row from Pending to Running
// and stamps started_at. Single-table UPDATE, no transaction needed.
//
// The status guard (`AND status = Pending`) prevents a stale recovery
// path from regressing a task that has already advanced past Running.
// RowsAffected==0 is intentionally NOT treated as an error: a concurrent
// path may have already moved the task forward, which is benign.
func StartTask(ctx context.Context, taskID string, startedAt time.Time) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("StartTask: empty taskID")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before StartTask")
	}

	const query = `
UPDATE task_records
   SET status = ?, started_at = ?, updated_at = NOW()
 WHERE task_id = ? AND status = ?`

	if err := gdb.WithContext(ctx).Exec(query,
		taskStatusRunning, startedAt, taskID, taskStatusPending,
	).Error; err != nil {
		return fmt.Errorf("StartTask (task=%s): %w", taskID, err)
	}
	return nil
}

// ResetTaskToPending reverts a Running task_records row back to Pending
// and clears its transient columns. Used by TaskModule's restart
// recovery path: a task that was Running at process crash time has
// uncertain side-effects on app-service (the HTTP request may or may
// not have reached it), and Market re-queues such tasks for re-attempt.
//
// The WHERE clause restricts the update to currently-Running rows so
// that paths which already moved the task past Running (Completed /
// Failed) are not regressed.
//
// State-table cleanup is intentionally NOT performed here. The
// user_application_states row stays as last persisted; a subsequent
// re-run of the task will refresh it via CreateTask's PendingState
// path is not possible here (task already exists), so the state
// column will move forward only when the new HTTP attempt drives
// CompleteTask / FailTask, or when NATS delivers a fresh event.
func ResetTaskToPending(ctx context.Context, taskID string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("ResetTaskToPending: empty taskID")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before ResetTaskToPending")
	}

	const query = `
UPDATE task_records
   SET status = ?, started_at = NULL, completed_at = NULL,
       error_msg = '', result = '', updated_at = NOW()
 WHERE task_id = ? AND status = ?`

	if err := gdb.WithContext(ctx).Exec(query,
		taskStatusPending, taskID, taskStatusRunning,
	).Error; err != nil {
		return fmt.Errorf("ResetTaskToPending (task=%s): %w", taskID, err)
	}
	return nil
}

// CompleteTaskInput captures the data CompleteTask updates on the
// happy path (HTTP 200 + Code=200 from app-service, opID returned).
// StateUpdate, when non-nil, asks CompleteTask to also write the
// success-side columns of user_application_states in the same
// transaction.
type CompleteTaskInput struct {
	TaskID      string
	Result      string
	OpID        string
	CompletedAt time.Time

	StateUpdate *SuccessStateUpdate
}

// SuccessStateUpdate is the user_application_states write performed by
// CompleteTask on the success path. Only op_id and installed_version
// columns are touched here; state / reason / progress / entrances are
// owned by the NATS pipeline and intentionally left alone — Market
// overwriting state on success would race with NATS-delivered
// intermediate states (e.g. "downloading" / "installing") and trigger
// the monotonic-guard rejection of legitimate later events.
//
// OpID and InstalledVersion are independent optionals: empty values
// leave the corresponding column unchanged. ClearTargetVersion=true
// sets target_version=” atomically, signalling "the in-flight
// operation has completed".
type SuccessStateUpdate struct {
	UserID, SourceID, AppID string

	OpID               string // empty = leave op_id column unchanged
	InstalledVersion   string // empty = leave installed_version unchanged
	ClearTargetVersion bool
}

// CompleteTask updates an existing task_records row to Completed,
// recording the OpID returned by app-service plus the JSON result the
// caller composed. When StateUpdate is non-nil, the user_application_
// states row is updated in the same transaction.
//
// Self-contained convenience wrapper: starts and commits its own
// transaction. In-transaction callers should use CompleteTaskInTx
// instead.
func CompleteTask(ctx context.Context, in CompleteTaskInput) error {
	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before CompleteTask")
	}
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return CompleteTaskInTx(tx, in)
	})
}

// CompleteTaskInTx is the in-tx variant of CompleteTask. The caller
// supplies a *gorm.DB that wraps an open transaction; this lets a
// composite write (task_records + user_application_states +
// history_records, for example) land atomically.
func CompleteTaskInTx(tx *gorm.DB, in CompleteTaskInput) error {
	if strings.TrimSpace(in.TaskID) == "" {
		return fmt.Errorf("CompleteTask: empty TaskID")
	}
	completedAt := in.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now()
	}

	const query = `
UPDATE task_records
   SET status = ?, result = ?, op_id = ?, completed_at = ?, updated_at = NOW()
 WHERE task_id = ?`
	if err := tx.Exec(query,
		taskStatusCompleted, in.Result, in.OpID, completedAt, in.TaskID,
	).Error; err != nil {
		return fmt.Errorf("CompleteTask: update task_records (task=%s): %w", in.TaskID, err)
	}

	if in.StateUpdate != nil {
		if err := applySuccessStateUpdate(tx, *in.StateUpdate); err != nil {
			return err
		}
	}
	return nil
}

// FailTaskInput captures the data FailTask updates on the failure path
// (HTTP transport error / 5xx / Code != 200 / response missing OpID).
// StateUpdate, when non-nil, asks FailTask to also push
// user_application_states to a terminal failed state in the same
// transaction — necessary because for failures that never reached
// app-service, NATS will never deliver a state event to drive the row
// out of "pending".
type FailTaskInput struct {
	TaskID      string
	Result      string
	ErrorMsg    string
	CompletedAt time.Time

	StateUpdate *FailedStateUpdate
}

// CancelTaskInput mirrors FailTaskInput. The split is purely semantic:
// callers passing a CancelTaskInput express "the task was canceled,
// not failed", and the resulting state column writes "canceled" instead
// of "failed". The wire shape and SQL paths are otherwise identical.
type CancelTaskInput struct {
	TaskID      string
	Result      string
	ErrorMsg    string
	CompletedAt time.Time

	StateUpdate *FailedStateUpdate
}

// FailedStateUpdate is the user_application_states write performed by
// FailTask / CancelTask. Unlike SuccessStateUpdate, the state column IS
// overwritten here — for a failure / cancel that never reached
// app-service the NATS pipeline will never send a terminal event, so
// Market is the only writer that can drive the row out of "pending".
//
// The rare race where NATS does deliver a later event (e.g. a
// network-partition success that completed despite Market seeing a
// transport error) is handled by the monotonic-guard in
// UpsertStateFromNATS: a NATS event with a later EventCreateTime
// overrides Market's terminal stamp.
//
// State is required (e.g. "failed", "canceled"). Reason mirrors the
// task ErrorMsg; it is truncated to fit the user_application_states
// VARCHAR width (see stateReasonMaxLen).
type FailedStateUpdate struct {
	UserID, SourceID, AppID string

	State              string // required, e.g. "failed" / "canceled"
	Reason             string
	ClearTargetVersion bool
}

// FailTask updates an existing task_records row to Failed and records
// the error message + JSON result. When StateUpdate is non-nil, the
// user_application_states row is moved to a terminal failed state in
// the same transaction.
//
// Self-contained convenience wrapper: starts and commits its own
// transaction. Callers that already hold a *gorm.DB tx (e.g.
// task.persistTaskFailure orchestrating task_records +
// user_application_states + history_records in one atomic write)
// should call FailTaskInTx directly instead, so all three writes
// land in a single transaction.
func FailTask(ctx context.Context, in FailTaskInput) error {
	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before FailTask")
	}
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return FailTaskInTx(tx, in)
	})
}

// FailTaskInTx runs the FailTask body inside the supplied tx so the
// caller can stitch additional writes (history record, secondary
// state updates) into the same transaction. The tx parameter is a
// *gorm.DB obtained from gdb.Transaction(...) — passing a non-tx
// *gorm.DB is allowed but defeats the atomic-write purpose.
func FailTaskInTx(tx *gorm.DB, in FailTaskInput) error {
	return finishTaskAsTerminalInTx(tx, "FailTask", taskStatusFailed,
		in.TaskID, in.Result, in.ErrorMsg, in.CompletedAt, in.StateUpdate)
}

// CancelTask is the cancel-flavoured twin of FailTask. Identical SQL,
// just a different status value (Canceled) and the caller is expected
// to set StateUpdate.State = "canceled".
//
// Like FailTask, this is a self-contained convenience wrapper.
// In-transaction callers should use CancelTaskInTx instead.
func CancelTask(ctx context.Context, in CancelTaskInput) error {
	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("postgres not initialised; db.Open must run before CancelTask")
	}
	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return CancelTaskInTx(tx, in)
	})
}

// CancelTaskInTx is the in-tx variant of CancelTask. See FailTaskInTx
// for the rationale.
func CancelTaskInTx(tx *gorm.DB, in CancelTaskInput) error {
	return finishTaskAsTerminalInTx(tx, "CancelTask", taskStatusCanceled,
		in.TaskID, in.Result, in.ErrorMsg, in.CompletedAt, in.StateUpdate)
}

// finishTaskAsTerminalInTx is the shared body of FailTask/CancelTask
// (via their *InTx variants). Both share the same SQL — UPDATE
// task_records to a terminal status with result + error_msg +
// completed_at, then optionally apply a failed-state update — so the
// function is parameterised by the target status value and the
// caller's identity (used in error messages).
//
// completedAt is normalised to time.Now() when zero, preserving the
// legacy convenience behaviour. taskID empty is a caller bug, surfaced
// as an error rather than silently turning into a no-op UPDATE.
func finishTaskAsTerminalInTx(
	tx *gorm.DB,
	caller string,
	status int,
	taskID, result, errorMsg string,
	completedAt time.Time,
	stateUpdate *FailedStateUpdate,
) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("%s: empty TaskID", caller)
	}
	if completedAt.IsZero() {
		completedAt = time.Now()
	}

	const query = `
UPDATE task_records
   SET status = ?, result = ?, error_msg = ?, completed_at = ?, updated_at = NOW()
 WHERE task_id = ?`
	if err := tx.Exec(query,
		status, result, errorMsg, completedAt, taskID,
	).Error; err != nil {
		return fmt.Errorf("%s: update task_records (task=%s): %w", caller, taskID, err)
	}

	if stateUpdate != nil {
		if err := applyFailedStateUpdate(tx, *stateUpdate); err != nil {
			return err
		}
	}
	return nil
}

// upsertPendingStateInTx records that a Market-initiated operation is
// being dispatched, by upserting the user_application_states row for
// (user, source, app). It is the in-transaction counterpart of the
// public UpsertPendingState (userappstate.go); CreateTask uses it so
// the state-table write and the task_records INSERT land atomically.
//
// Column-level write contract — IDENTICAL to UpsertPendingState's:
//   - op_type, event_create_time     overwritten on UPDATE branch
//   - state                          left to schema default (”) on
//     INSERT, NEVER touched on UPDATE
//     (NATS owns this column)
//   - reason / message / progress    NOT touched (NATS owns)
//   - entrances / shared_entrances /
//     status_entrances               NOT touched (NATS owns)
//   - installed_version /
//     target_version                 NOT touched (CompleteTask /
//     FailTask own)
//   - op_id                          NOT touched (preserved on UPDATE);
//     Market's task executor calls
//     LinkOpID after the HTTP response
//
// The SQL is duplicated against UpsertPendingState rather than
// refactored: short, stable, and avoiding the cross-package signature
// change. Both helpers must evolve together when the contract
// changes — see UpsertPendingState's doc for the canonical narrative.
func upsertPendingStateInTx(tx *gorm.DB, in PendingTaskState) error {
	in.UserID = strings.TrimSpace(in.UserID)
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.AppID = strings.TrimSpace(in.AppID)
	if in.UserID == "" || in.SourceID == "" || in.AppID == "" {
		return fmt.Errorf("upsertPendingStateInTx: empty UserID/SourceID/AppID: %+v", in)
	}

	const query = `
INSERT INTO user_application_states (
    user_application_id, state, op_type, event_create_time
)
SELECT ua.id, '', ?, NOW()
FROM user_applications ua
WHERE ua.user_id = ? AND ua.source_id = ? AND ua.app_id = ?
ON CONFLICT (user_application_id) DO UPDATE SET
    op_type           = COALESCE(NULLIF(EXCLUDED.op_type, ''), user_application_states.op_type),
    event_create_time = EXCLUDED.event_create_time
`

	res := tx.Exec(query,
		truncate(in.OpType, stateOpTypeMaxLen),
		in.UserID, in.SourceID, in.AppID,
	)
	if err := res.Error; err != nil {
		return fmt.Errorf("upsertPendingStateInTx (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppID, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: (user=%s source=%s app=%s)", ErrUserApplicationStateNotFound,
			in.UserID, in.SourceID, in.AppID)
	}
	return nil
}

// applySuccessStateUpdate writes the optional success-path columns
// (op_id, installed_version, target_version=”) on the
// user_application_states row matching (user, source, app). The state
// column is intentionally NOT touched — see SuccessStateUpdate's
// comment for why.
//
// All updates run as a single UPDATE with a dynamic SET clause assembled
// from the non-empty input fields plus ClearTargetVersion. RowsAffected
// is permissive: 0 means the user_applications row is missing (caller
// is targeting an app that hydration has not rendered yet, which is a
// tolerable inconsistency for the install path) — we surface it via
// ErrUserApplicationStateNotFound so the task module can downgrade to
// V(3) logging rather than a hard error.
func applySuccessStateUpdate(tx *gorm.DB, in SuccessStateUpdate) error {
	in.UserID = strings.TrimSpace(in.UserID)
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.AppID = strings.TrimSpace(in.AppID)
	if in.UserID == "" || in.SourceID == "" || in.AppID == "" {
		return fmt.Errorf("applySuccessStateUpdate: empty UserID/SourceID/AppID: %+v", in)
	}

	updates := map[string]any{}
	if strings.TrimSpace(in.OpID) != "" {
		updates["op_id"] = truncate(in.OpID, stateOpIDMaxLen)
	}
	if strings.TrimSpace(in.InstalledVersion) != "" {
		updates["installed_version"] = in.InstalledVersion
	}
	if in.ClearTargetVersion {
		updates["target_version"] = ""
	}
	if len(updates) == 0 {
		// Nothing to update — caller asked for a SuccessStateUpdate
		// with all fields zero, which is a no-op. Treat as success.
		return nil
	}

	res := tx.Exec(`
UPDATE user_application_states uas
   SET `+buildSetClause(updates)+`
  FROM user_applications ua
 WHERE uas.user_application_id = ua.id
   AND ua.user_id   = ?
   AND ua.source_id = ?
   AND ua.app_id    = ?`,
		append(setClauseValues(updates), in.UserID, in.SourceID, in.AppID)...,
	)
	if err := res.Error; err != nil {
		return fmt.Errorf("applySuccessStateUpdate (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppID, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: (user=%s source=%s app=%s)", ErrUserApplicationStateNotFound,
			in.UserID, in.SourceID, in.AppID)
	}
	return nil
}

// applyFailedStateUpdate writes (state, reason, target_version=”) on
// the user_application_states row matching (user, source, app). Unlike
// the success path, state IS overwritten — for a failure that never
// reached app-service the NATS pipeline will never deliver a terminal
// event, and Market is the sole writer that can move the row out of
// "pending".
func applyFailedStateUpdate(tx *gorm.DB, in FailedStateUpdate) error {
	in.UserID = strings.TrimSpace(in.UserID)
	in.SourceID = strings.TrimSpace(in.SourceID)
	in.AppID = strings.TrimSpace(in.AppID)
	if in.UserID == "" || in.SourceID == "" || in.AppID == "" {
		return fmt.Errorf("applyFailedStateUpdate: empty UserID/SourceID/AppID: %+v", in)
	}
	if strings.TrimSpace(in.State) == "" {
		return fmt.Errorf("applyFailedStateUpdate: empty State; use 'failed' or 'canceled'")
	}

	updates := map[string]any{
		"state":  in.State,
		"reason": truncate(in.Reason, stateReasonMaxLen),
	}
	if in.ClearTargetVersion {
		updates["target_version"] = ""
	}

	res := tx.Exec(`
UPDATE user_application_states uas
   SET `+buildSetClause(updates)+`
  FROM user_applications ua
 WHERE uas.user_application_id = ua.id
   AND ua.user_id   = ?
   AND ua.source_id = ?
   AND ua.app_id    = ?`,
		append(setClauseValues(updates), in.UserID, in.SourceID, in.AppID)...,
	)
	if err := res.Error; err != nil {
		return fmt.Errorf("applyFailedStateUpdate (user=%s source=%s app=%s): %w",
			in.UserID, in.SourceID, in.AppID, err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: (user=%s source=%s app=%s)", ErrUserApplicationStateNotFound,
			in.UserID, in.SourceID, in.AppID)
	}
	return nil
}

// buildSetClause and setClauseValues compose the SET portion of a SQL
// UPDATE from a map of column→value pairs. Returned in deterministic
// (sorted) order so the produced SQL is stable across calls and easy to
// match in PG slow-query logs.
//
// They are intentionally tiny and not generalised: only the two
// helpers in this file consume them, and gorm's Updates(map) API
// cannot be used here because the tx.Exec form (with raw `UPDATE ...
// FROM user_applications ...` JOIN) is needed to filter by the
// user_applications join key.
func buildSetClause(updates map[string]any) string {
	cols := sortedKeys(updates)
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = c + " = ?"
	}
	return strings.Join(parts, ", ")
}

func setClauseValues(updates map[string]any) []any {
	cols := sortedKeys(updates)
	out := make([]any, len(cols))
	for i, c := range cols {
		out[i] = updates[c]
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Manual insertion sort: tiny len (≤4 here), avoids importing sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
