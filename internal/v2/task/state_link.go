package task

import (
	"context"
	"errors"
	"strings"

	"market/internal/v2/store"

	"github.com/golang/glog"
)

// linkStateOpID is invoked by app_*.go executors immediately after
// task.OpID is populated from app-service's HTTP response. It associates
// the assigned op_id with the corresponding user_application_states row
// (keyed by user/source/app via the FK chain through user_applications),
// so any later NATS state event carrying that same opID can be cross-
// referenced for audit / debug.
//
// Best-effort and log-only: phase 1's correctness does not depend on the
// link succeeding. The legacy cache + NATS path continues to drive
// client-visible behaviour. Common skip causes:
//   - Empty op_id (caller bug; logged at V(3));
//   - Missing user / source / appID in task metadata (cancel and
//     uninstall handlers may not always know the source — V(3));
//   - No matching user_applications row (clone install paths with no
//     hydration, or apps removed from catalog mid-flight — V(3)).
//
// appIDFallback is used when task.Metadata["realAppID"] is missing; pass
// the URL-path / display name (task.AppName) at the call site so cancel
// and uninstall executors that don't carry realAppID still link cleanly.
func (tm *TaskModule) linkStateOpID(task *Task, appIDFallback, opType string) {
	if task == nil {
		return
	}
	opID := strings.TrimSpace(task.OpID)
	if opID == "" {
		glog.V(3).Infof("linkStateOpID: skipping (task=%s op=%s) — empty op_id",
			task.ID, opType)
		return
	}

	userID := strings.TrimSpace(task.User)
	if userID == "" {
		userID = metadataString(task.Metadata, "user_id")
	}
	sourceID := metadataString(task.Metadata, "source")
	appID := metadataString(task.Metadata, "realAppID")
	if appID == "" {
		appID = strings.TrimSpace(appIDFallback)
	}
	if userID == "" || sourceID == "" || appID == "" {
		glog.V(3).Infof("linkStateOpID: skipping (task=%s user=%q source=%q app=%q op=%s op_id=%s) — missing identifier",
			task.ID, userID, sourceID, appID, opType, opID)
		return
	}

	if err := store.LinkOpID(context.Background(), userID, sourceID, appID, opID); err != nil {
		if errors.Is(err, store.ErrUserApplicationStateNotFound) {
			glog.V(3).Infof("linkStateOpID: no user_application_state row for op_id link (user=%s source=%s app=%s op=%s op_id=%s)",
				userID, sourceID, appID, opType, opID)
			return
		}
		glog.Errorf("linkStateOpID: link op_id failed (task=%s user=%s source=%s app=%s op=%s op_id=%s): %v",
			task.ID, userID, sourceID, appID, opType, opID, err)
	}
}

// metadataString reads a string value from a Task.Metadata map, returning
// "" if the key is absent / not a string / blank.
func metadataString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}
