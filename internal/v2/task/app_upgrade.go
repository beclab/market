package task

import (
	"context"
	"fmt"
	"net/http"

	"market/internal/v2/appservice"

	"github.com/golang/glog"
)

// AppUpgrade upgrades an application using the typed appservice client.
// task.AppName is the wire-level app name — the clone alias for clone-app
// upgrades, the original app name otherwise. The chart that gets upgraded
// is always the original (clones share the chart), but the URL path uses
// the wire name so app-service can route to the right install instance.
//
// Behaviour change vs the legacy hand-rolled HTTP path: when app-service
// returns HTTP 200 + Code != 200, the typed client surfaces the response
// as *appservice.OpError and AppUpgrade returns it as a hard failure.
// The legacy code logged the bad code at info level and still returned
// success, which silently masked upstream rejections — the new strict
// behaviour matches AppInstall and lets the task pipeline mark the row
// failed correctly.
func (tm *TaskModule) AppUpgrade(task *Task) (*AppOperationMessage, error) {
	appName := task.AppName
	user := task.User

	rawAppName, isCloneApp := task.Metadata["rawAppName"].(string)
	if isCloneApp && rawAppName != "" {
		glog.Infof("Starting clone app upgrade: cloneAppName=%s, rawAppName=%s, user=%s, task_id=%s", appName, rawAppName, user, task.ID)
	} else {
		glog.Infof("Starting app upgrade: app=%s, user=%s, task_id=%s", appName, user, task.ID)
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing token in task metadata")
	}

	source, _ := task.Metadata["source"].(string)
	if source == "" {
		glog.Warningf("undefine source for task: %s", task.ID)
	}

	version, ok := task.Metadata["version"].(string)
	if !ok {
		glog.Warningf("Missing version in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing version in task metadata for upgrade")
	}

	var apiSource string
	if source == "upload" {
		apiSource = "custom"
	} else {
		apiSource = "market"
	}
	glog.Infof("App source: %s, API source: %s for task: %s", source, apiSource, task.ID)

	var envs []AppEnvVar
	if envsData, ok := task.Metadata["envs"]; ok && envsData != nil {
		envs, _ = envsData.([]AppEnvVar)
		glog.Infof("Retrieved %d environment variables for task: %s", len(envs), task.ID)
	}

	// Tolerant images extraction. The legacy code used a bare type
	// assertion (task.Metadata["images"].([]Image)) that would panic
	// when the field was absent or shaped differently; the comma-ok
	// form here mirrors AppInstall's safe pattern.
	var images []Image
	if imagesData, ok := task.Metadata["images"]; ok && imagesData != nil {
		if imagesSlice, ok := imagesData.([]Image); ok {
			images = imagesSlice
		}
	}

	// Field-for-field conversion to the appservice wire types. The
	// duplicate task-package types (AppEnvVar / Image) are kept
	// because pkg/v2/api/task.go still references them when assembling
	// taskMetadata; once that handler migrates, the duplicates can
	// retire in favour of the appservice types directly.
	asEnvs := make([]appservice.AppEnvVar, len(envs))
	for i, e := range envs {
		asEnvs[i] = appservice.AppEnvVar{EnvName: e.EnvName, Value: e.Value}
		if e.ValueFrom != nil {
			asEnvs[i].ValueFrom = &appservice.AppEnvValueFrom{EnvName: e.ValueFrom.EnvName}
		}
	}
	asImages := make([]appservice.Image, len(images))
	for i, img := range images {
		asImages[i] = appservice.Image{Name: img.Name, Size: img.Size}
	}

	opts := appservice.UpgradeOptions{
		RepoUrl:      getRepoUrl(),
		Version:      version,
		User:         user,
		Source:       apiSource,
		MarketSource: source,
		Images:       asImages,
		Envs:         asEnvs,
	}
	hdr := appservice.OpHeaders{
		Token:        token,
		User:         task.User,
		MarketUser:   user,
		MarketSource: source,
	}

	envelope := BaseUpgradeEnvelope(task, source, apiSource, version)

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for upgrade task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	if isCloneApp && rawAppName != "" {
		glog.Infof("[APP] Sending HTTP request for clone app upgrade: task=%s cloneAppName=%s rawAppName=%s version=%s",
			task.ID, appName, rawAppName, version)
	} else {
		glog.Infof("[APP] Sending HTTP request for app upgrade: task=%s, version=%s", task.ID, version)
	}

	opResp, opHttpStatus, err := client.UpgradeApp(context.Background(), appName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service upgrade failed for task=%s: %v", task.ID, err)
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

	glog.Infof("App upgrade completed successfully: task=%s", task.ID)

	return envelope, nil
}
