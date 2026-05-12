package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"market/internal/v2/appservice"

	"github.com/golang/glog"
)

// AppCancel cancels a running operation (typically an in-flight install)
// by dispatching a cancel request through the typed appservice client.
// The (uid) wire argument is taken from task.Metadata["app_name"] — the
// API handler stores the install task's app name there at cancel-task
// creation time so the executor can target the correct upstream
// resource regardless of how the cancel task itself was named.
//
// After app-service acknowledges the cancel, we synchronously fire
// InstallTaskCanceled to mark the cancelled install task's in-memory
// + PG status as Canceled. That side-effect is best-effort: the cancel
// HTTP succeeded, which is the user-visible success criterion, and
// failure on this side just means the NATS pipeline reconciles the
// install task's terminal state on its own timeline.
func (tm *TaskModule) AppCancel(task *Task) (string, error) {
	glog.Infof("Starting app cancel: app=%s, user=%s, task_id=%s", task.AppName, task.User, task.ID)

	appName, ok := task.Metadata["app_name"].(string)
	if !ok {
		glog.Warningf("Missing app_name in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing app_name in task metadata")
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app"
	}

	// baseEnvelope mirrors the legacy success / failure result shape so
	// API-side consumers (sync wait + frontend backend_response viewer)
	// keep the exact same field set as before the appservice.Client
	// migration. The legacy "url" field is intentionally dropped: it
	// duplicated the host/port that appservice.Client resolves itself
	// and no internal consumer reads it.
	baseEnvelope := func() map[string]interface{} {
		return map[string]interface{}{
			"operation": "cancel",
			"app_name":  task.AppName,
			"user":      task.User,
			"uid":       appName,
			"cfgType":   cfgType,
		}
	}

	hdr := appservice.OpHeaders{
		Token: token,
		User:  task.User,
	}

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for cancel task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), err
	}

	glog.Infof("[APP] Sending HTTP request for app cancel: task=%s, app_name=%s", task.ID, appName)
	opResp, err := client.CancelApp(context.Background(), appName, hdr)
	if err != nil {
		glog.Errorf("App-service cancel failed for task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		// HTTP-200-business-error / HTTP-422-structured-envelope both
		// surface as *appservice.OpError. Forward code/message + the
		// upstream `data` block (preferring the verbatim DataRaw bytes
		// when available so structured payloads survive intact) so the
		// frontend's backend_response viewer keeps working unchanged.
		var opErr *appservice.OpError
		if errors.As(err, &opErr) {
			backend := map[string]interface{}{
				"code":    opErr.Code,
				"message": opErr.Message,
			}
			switch {
			case opResp != nil && len(opResp.DataRaw) > 0:
				backend["data"] = json.RawMessage(opResp.DataRaw)
			case opErr.OpID != "":
				backend["data"] = map[string]interface{}{"opID": opErr.OpID}
			}
			envelope["backend_response"] = backend
		}
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), err
	}

	if opResp != nil && opResp.Data != nil && opResp.Data.OpID != "" {
		task.OpID = opResp.Data.OpID
		glog.Infof("Successfully extracted opID: %s for task: %s", task.OpID, task.ID)
		tm.linkStateOpID(task, appName, "cancel")
	} else {
		glog.Infof("opID not found in response data for task: %s", task.ID)
	}

	envelope := baseEnvelope()
	if opResp != nil {
		backend := map[string]interface{}{
			"code":    opResp.Code,
			"message": opResp.Message,
		}
		if opResp.Data != nil && opResp.Data.OpID != "" {
			backend["data"] = map[string]interface{}{"opID": opResp.Data.OpID}
		}
		envelope["backend_response"] = backend
	}
	envelope["opID"] = task.OpID
	envelope["status"] = "success"
	successJSON, _ := json.Marshal(envelope)
	glog.Infof("App cancel completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))

	// Best-effort install-task cleanup. See doc comment for why a
	// failure here does NOT propagate up — the cancel itself is
	// considered successful once app-service acknowledged it.
	if cancelErr := tm.InstallTaskCanceled(task.AppName, "", "", task.User); cancelErr != nil {
		glog.Errorf("[APP] InstallTaskCanceled side-effect failed for task=%s app=%s: %v",
			task.ID, task.AppName, cancelErr)
	}

	return string(successJSON), nil
}
