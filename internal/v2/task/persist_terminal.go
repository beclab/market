package task

import (
	"context"
	"fmt"
	"strings"

	"market/internal/v2/db"
	"market/internal/v2/history"
	"market/internal/v2/store"

	"github.com/golang/glog"
	"gorm.io/gorm"
)

// persistTaskFailure commits a task's terminal-failure footprint to
// Postgres in a single transaction. The three writes that have to
// either all land or all roll back are:
//
//  1. task_records.status = Failed + result + error_msg + completed_at
//     (via store.FailTaskInTx)
//  2. user_application_states.state = 'failed' + reason (via the
//     StateUpdate field of FailTaskInput; nil-safe when the task is
//     not associated with a user_applications row)
//  3. history_records.<row inserted> (via history.StoreRecordInTx;
//     skipped when hr is nil — caller convention: nil means "no
//     history wanted", typically because historyModule is unconfigured)
//
// The rationale for one transaction across the three tables:
//
//   - Frontend reads user_application_states.state to render UI flow,
//     and reads history_records for the audit timeline. Splitting the
//     writes lets a half-failure produce a UI that says "the app is
//     failed" but has no history entry, or vice versa — both
//     directions are user-visible inconsistencies.
//
//   - Splitting the writes also opens a window for the legacy
//     "permanently pending" bug (state stays at '' because the state
//     update succeeded for task_records but failed for state row).
//     A single tx eliminates that window structurally.
//
// PG failure handling: this function returns the error verbatim. The
// caller (handleTaskFailure / *TaskFailed family) is expected to log
// it and continue with the in-memory / NATS / callback side-effects —
// the next restart's restoreTasksFromStore will reset the task to
// Pending and re-execute, which honours the existing crash-recovery
// contract.
func (tm *TaskModule) persistTaskFailure(
	ctx context.Context,
	in store.FailTaskInput,
	hr *history.HistoryRecord,
) error {
	if strings.TrimSpace(in.TaskID) == "" {
		return fmt.Errorf("persistTaskFailure: empty TaskID")
	}

	gdb := db.Global()
	if gdb == nil {
		return fmt.Errorf("persistTaskFailure: postgres not initialised; db.Open must run first")
	}

	return gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := store.FailTaskInTx(tx, in); err != nil {
			return err
		}
		// History is only written when both:
		//   - caller supplied a HistoryRecord (hr != nil); the
		//     orchestrator skips the build step in public env or when
		//     historyModule is missing, surfacing that as a nil hr;
		//   - tm.historyModule is configured.
		//
		// Both nil-checks are kept here (defence-in-depth) so a misuse
		// from a future caller does not panic mid-transaction.
		if hr != nil && tm.historyModule != nil {
			if err := tm.historyModule.StoreRecordInTx(tx, hr); err != nil {
				return err
			}
		}
		return nil
	})
}

// resolveTaskState locates the (UserID, SourceID, AppID) tuple that a
// task's terminal write should target on user_application_states.
//
// Mirrors the resolution chain used at task-creation time in
// pendingStateFromMetadata: source comes from metadata; appID falls
// back through realAppID → app_name → task.AppName so cancel/uninstall
// paths (which carry only app_name) line up on the same row as the
// original install/upgrade did.
//
// Returns ok=false when the task is not associated with a
// user_applications row at all (e.g. admin / future reconciliation
// tasks). Callers treat that as "skip the state-table write";
// FailTaskInput.StateUpdate stays nil and only task_records gets
// updated, matching the pending-time soft-skip semantics.
func resolveTaskState(task *Task) (userID, sourceID, appID string, ok bool) {
	if task == nil {
		return "", "", "", false
	}
	userID = strings.TrimSpace(task.User)
	if userID == "" {
		return "", "", "", false
	}
	sourceID = metadataString(task.Metadata, "source")
	if sourceID == "" {
		return "", "", "", false
	}
	appID = metadataString(task.Metadata, "realAppID")
	if appID == "" {
		appID = metadataString(task.Metadata, "app_name")
	}
	if appID == "" {
		appID = strings.TrimSpace(task.AppName)
	}
	if appID == "" {
		return "", "", "", false
	}
	return userID, sourceID, appID, true
}

// buildFailedStateUpdate returns the FailedStateUpdate that a terminal-
// failure should apply to user_application_states, or nil to skip the
// state-table write entirely.
//
// Per-type policy:
//
//   - Install / Upgrade / Uninstall / Clone: write state='failed' +
//     reason on the (user, source, app) row. The state column write
//     is the ONLY thing that moves these rows out of pending when the
//     failure happens before app-service receives the request (NATS
//     will not deliver a terminal event in that path).
//
//   - CancelAppInstall: skip the state write. A failed cancel does
//     not change the underlying install's state — only the cancel
//     task's own task_records row goes to status=Failed. The install
//     it was trying to cancel may still be running on app-service,
//     and its state will be driven by NATS as usual.
//
// Version columns (installed_version / target_version) are left
// untouched: failure paths intentionally do not clean up version
// residual data; the frontend disambiguates via the state column.
func buildFailedStateUpdate(task *Task) *store.FailedStateUpdate {
	if task == nil {
		return nil
	}
	if task.Type == CancelAppInstall {
		return nil
	}
	uid, src, app, ok := resolveTaskState(task)
	if !ok {
		return nil
	}
	return &store.FailedStateUpdate{
		UserID:   uid,
		SourceID: src,
		AppID:    app,
		State:    "failed",
		Reason:   task.ErrorMsg,
	}
}

// buildFailTaskInput composes the store.FailTaskInput from a task in
// the terminal-failure shape. completedAt is taken from task.CompletedAt
// when set (the caller is expected to have stamped it before this
// runs); store.FailTaskInTx fills in time.Now() when zero, so the
// nil-pointer branch is also tolerable.
func buildFailTaskInput(task *Task) store.FailTaskInput {
	in := store.FailTaskInput{
		TaskID:      task.ID,
		Result:      task.Result,
		ErrorMsg:    task.ErrorMsg,
		StateUpdate: buildFailedStateUpdate(task),
	}
	if task.CompletedAt != nil {
		in.CompletedAt = *task.CompletedAt
	}
	return in
}

// commitTaskFailure runs the full PG side-effect of a task terminal
// failure (3-table single tx) and logs at-V(2) on success / errors at
// Error level. Returns the underlying error so callers may choose to
// short-circuit, though the contract is "log and continue" — the
// in-memory / NATS / callback steps must still run so the rest of the
// system observes the failure even when PG is degraded.
//
// This is the single entry point all four failure-side terminals
// (handleTaskFailure + the three *TaskFailed external-signal helpers)
// flow through, so future changes to the failure persistence policy
// land in one place.
func (tm *TaskModule) commitTaskFailure(ctx context.Context, task *Task, errMsg string) {
	in := buildFailTaskInput(task)
	hr := tm.buildTaskTerminalHistory(task, task.Result, fmt.Errorf("%s", errMsg))
	if err := tm.persistTaskFailure(ctx, in, hr); err != nil {
		glog.Errorf("[%s] persistTaskFailure (task=%s app=%s user=%s): %v",
			tm.instanceID, task.ID, task.AppName, task.User, err)
		return
	}
	glog.V(2).Infof("[%s] persistTaskFailure ok: task=%s app=%s user=%s",
		tm.instanceID, task.ID, task.AppName, task.User)
}
