package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"market/internal/v2/appservice"

	"github.com/golang/glog"
)

// AppUninstall uninstalls an application using the typed appservice
// client. The (all, deleteData) flags come from the API handler's
// request body and ride through task metadata; both default to false
// when missing, matching app-service's "no cascade / no delete" wire
// semantics.
func (tm *TaskModule) AppUninstall(task *Task) (string, error) {
	appName := task.AppName

	glog.Infof("Starting app uninstallation: app=%s, user=%s, task_id=%s", appName, task.User, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Errorf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app"
	}

	all, ok := task.Metadata["all"].(bool)
	if !ok {
		glog.Warningf("Missing all parameter in task metadata for task: %s, using default false", task.ID)
		all = false
	}

	deleteData, ok := task.Metadata["deleteData"].(bool)
	if !ok {
		glog.Warningf("Missing deleteData parameter in task metadata for task: %s, using default false", task.ID)
		deleteData = false
	}

	// baseEnvelope mirrors the legacy success / failure result shape so
	// API-side consumers (sync wait + frontend backend_response viewer)
	// keep the exact same field set as before the appservice.Client
	// migration. The legacy "url" field is intentionally dropped: it
	// duplicated the host/port the typed client resolves itself.
	baseEnvelope := func() map[string]interface{} {
		return map[string]interface{}{
			"operation":  "uninstall",
			"app_name":   appName,
			"user":       task.User,
			"cfgType":    cfgType,
			"all":        all,
			"deleteData": deleteData,
		}
	}

	hdr := appservice.OpHeaders{
		Token: token,
		User:  task.User,
	}
	opts := appservice.UninstallOptions{
		All:        all,
		DeleteData: deleteData,
	}

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for uninstall task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), err
	}

	glog.Infof("[APP] Sending HTTP request for app uninstallation: task=%s, all=%v deleteData=%v", task.ID, all, deleteData)
	opResp, err := client.UninstallApp(context.Background(), appName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service uninstall failed for task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		// HTTP-200-business-error / HTTP-422-structured-envelope both
		// surface as *appservice.OpError; forward code/message + the
		// upstream `data` block so the frontend's backend_response
		// viewer keeps working unchanged.
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
		tm.linkStateOpID(task, appName, "uninstall")
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
	glog.Infof("App uninstallation completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
