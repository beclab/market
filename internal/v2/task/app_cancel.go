package task

import (
	"context"
	"fmt"
	"net/http"

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
func (tm *TaskModule) AppCancel(task *Task) (*AppOperationMessage, error) {
	glog.Infof("Starting app cancel: app=%s, user=%s, task_id=%s", task.AppName, task.User, task.ID)

	appName, ok := task.Metadata["app_name"].(string)
	if !ok {
		glog.Warningf("Missing app_name in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing app_name in task metadata")
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing token in task metadata")
	}

	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app"
	}

	envelope := BaseCancelEnvelope(task, cfgType) // + todo has uid?

	hdr := appservice.OpHeaders{
		Token: token,
		User:  task.User,
	}

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for cancel task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	glog.Infof("[APP] Sending HTTP request for app cancel: task=%s, app_name=%s", task.ID, appName)
	opResp, opHttpStatus, err := client.CancelApp(context.Background(), appName, hdr)
	if err != nil {
		glog.Errorf("App-service cancel failed for task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	if opResp.Code != http.StatusOK {
		envelope.SetFailed("opID not found in response data")
		envelope.SetResponse(opResp, opResp.Code, opResp.Data.String(), opHttpStatus)
		return envelope, fmt.Errorf("opID not found in response data")
	}

	task.OpID = opResp.Data.OpId
	glog.V(3).Infof("Successfully extracted opID: %s for task: %s", opResp.Data.OpId, task.ID)

	envelope.SetSuccess()
	envelope.SetOpId(opResp.Data.OpId)
	envelope.SetResponse(opResp, opResp.Code, opResp.Data.String(), opHttpStatus)

	return envelope, nil
}
