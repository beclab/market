package task

import (
	"context"
	"fmt"
	"net/http"

	"market/internal/v2/appservice"

	"github.com/golang/glog"
)

// AppUninstall uninstalls an application using the typed appservice
// client. The (all, deleteData) flags come from the API handler's
// request body and ride through task metadata; both default to false
// when missing, matching app-service's "no cascade / no delete" wire
// semantics.
func (tm *TaskModule) AppUninstall(task *Task) (*AppOperationMessage, error) {
	appName := task.AppName

	glog.Infof("Starting app uninstallation: app=%s, user=%s, task_id=%s", appName, task.User, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Errorf("Missing token in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing token in task metadata")
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

	envelope := BaseUninstallEnvelope(task, cfgType, all, deleteData)

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
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	glog.Infof("[APP] Sending HTTP request for app uninstallation: task=%s, all=%v deleteData=%v", task.ID, all, deleteData)
	opResp, opHttpStatus, err := client.UninstallApp(context.Background(), appName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service uninstall failed for task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	if opResp.Code != http.StatusOK {
		envelope.SetFailed("opID not found in response data")
		envelope.SetResponse(opResp, opResp.Code, opResp.Data.String(), opHttpStatus)
		return envelope, fmt.Errorf("opID not found in response data")
	}

	task.OpID = opResp.Data.OpId

	envelope.SetSuccess()
	envelope.SetOpId(opResp.Data.OpId)
	envelope.SetResponse(opResp, opResp.Code, opResp.Data.String(), opHttpStatus)

	glog.Infof("App uninstallation completed successfully: task=%s", task.ID)

	return envelope, nil
}
