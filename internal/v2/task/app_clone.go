package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"market/internal/v2/appservice"
	"market/internal/v2/store"

	"github.com/golang/glog"
)

// AppEntrance represents an entrance configuration for a cloned app.
// Carried in task.Metadata["entrances"] from pkg/v2/api/task.go's
// CloneAppRequest. Distinct from appservice.AppEntrance only because
// the API handler still references this type when validating /
// hashing the wire request (see calculateCloneRequestHash); once that
// migrates, this duplicate can retire in favour of appservice.AppEntrance.
type AppEntrance struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// AppClone clones an application using the typed appservice client.
// The wire-level URL path uses rawAppName + requestHash so app-service
// can disambiguate the clone from the original chart at install time;
// see CloneOptions / cloneAppIDFromName in the API layer for how the
// pieces are derived. RawAppName / RequestHash / Title also ride in the
// request body so app-service can populate display-side metadata.
//
// Behaviour change vs the legacy hand-rolled HTTP path: HTTP 200 +
// Code != 200 now surfaces as a hard failure (the typed client maps
// it to *appservice.OpError). The legacy path returned a "failed"
// envelope with the upstream payload attached on the same condition,
// so user-visible behaviour is preserved — only the in-process error
// signal is now strongly typed.
func (tm *TaskModule) AppClone(task *Task) (string, error) {
	appName := task.AppName
	user := task.User

	glog.Infof("Starting app clone: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	// Reject duplicate clone tasks for the same app to mirror
	// AppInstall's pre-flight guard. This is a best-effort window —
	// two near-simultaneous clicks may still slip through but the PG
	// path's ON CONFLICT keeps state consistent regardless.
	tm.mu.RLock()
	for _, runningTask := range tm.runningTasks {
		if runningTask.Type == CloneApp && runningTask.AppName == appName && runningTask.ID != task.ID {
			tm.mu.RUnlock()
			glog.Infof("Clone failed: another clone task is already running for app: %s, existing task ID: %s", appName, runningTask.ID)
			errorResult := map[string]interface{}{
				"operation":        "clone",
				"app_name":         appName,
				"user":             user,
				"error":            "Another clone task is already running for this app",
				"status":           "failed",
				"existing_task_id": runningTask.ID,
			}
			errorJSON, _ := json.Marshal(errorResult)
			return string(errorJSON), fmt.Errorf("another clone task is already running for app: %s", appName)
		}
	}
	tm.mu.RUnlock()

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	appSource, ok := task.Metadata["source"].(string)
	if !ok {
		glog.Warningf("Missing source in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing source in task metadata")
	}

	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app"
	}

	rawAppName, ok := task.Metadata["rawAppName"].(string)
	if !ok || rawAppName == "" {
		glog.Warningf("Missing rawAppName in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing rawAppName in task metadata")
	}

	requestHash, ok := task.Metadata["requestHash"].(string)
	if !ok || requestHash == "" {
		glog.Warningf("Missing requestHash in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing requestHash in task metadata")
	}

	title, _ := task.Metadata["title"].(string)

	// urlAppName is the path segment app-service uses to address the
	// clone — concatenation of the original chart's name and the
	// per-request hash. Same value as the API layer's newAppName.
	urlAppName := rawAppName + requestHash
	glog.Infof("Clone operation: rawAppName=%s, requestHash=%s, title=%s, urlAppName=%s for task: %s", rawAppName, requestHash, title, urlAppName, task.ID)

	var apiSource string
	if appSource == "upload" {
		apiSource = "custom"
	} else {
		apiSource = "market"
	}
	glog.Infof("App source: %s, API source: %s, cfgType: %s for task: %s", appSource, apiSource, cfgType, task.ID)

	var envs []AppEnvVar
	if envsData, ok := task.Metadata["envs"]; ok && envsData != nil {
		envs, _ = envsData.([]AppEnvVar)
		glog.Infof("Retrieved %d environment variables for task: %s", len(envs), task.ID)
	}

	var entrances []AppEntrance
	if entrancesData, ok := task.Metadata["entrances"]; ok && entrancesData != nil {
		if entrancesSlice, ok := entrancesData.([]AppEntrance); ok {
			entrances = entrancesSlice
		}
		glog.Infof("Retrieved %d entrances for task: %s", len(entrances), task.ID)
	}

	// VC injection: clone uses rawAppName for receipt lookup since
	// the clone alias is not the productID app-service / payment
	// modules know about. The injected envvar overwrites any caller-
	// supplied value with the same name to guarantee the receipt-side
	// VC is the one app-service receives.
	tm.mu.RLock()
	settingsManager := tm.settingsManager
	tm.mu.RUnlock()
	if settingsManager != nil {
		vc := getVCForClone(settingsManager, user, rawAppName, task.Metadata)
		if vc != "" {
			vcExists := false
			for i := range envs {
				if envs[i].EnvName == "VERIFIABLE_CREDENTIAL" {
					envs[i].Value = vc
					vcExists = true
					glog.V(3).Infof("Updated VERIFIABLE_CREDENTIAL in envs for task: %s", task.ID)
					break
				}
			}
			if !vcExists {
				envs = append(envs, AppEnvVar{
					EnvName: "VERIFIABLE_CREDENTIAL",
					Value:   vc,
				})
				glog.Infof("Added VERIFIABLE_CREDENTIAL to envs for task: %s", task.ID)
			}
		} else {
			glog.Infof("VC not found for app clone, skipping VERIFIABLE_CREDENTIAL injection for task: %s", task.ID)
		}
	} else {
		glog.Infof("Settings manager not available, skipping VC injection for task: %s", task.ID)
	}

	var images []Image
	if imagesData, ok := task.Metadata["images"]; ok && imagesData != nil {
		if imagesSlice, ok := imagesData.([]Image); ok {
			images = imagesSlice
		}
	}

	// Field-for-field conversion to the appservice wire types. Same
	// pattern as AppInstall / AppUpgrade: the duplicate task-package
	// types stay because pkg/v2/api/task.go still references them
	// when assembling taskMetadata.
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
	asEntrances := make([]appservice.AppEntrance, len(entrances))
	for i, e := range entrances {
		asEntrances[i] = appservice.AppEntrance{Name: e.Name, Title: e.Title}
	}

	opts := appservice.CloneOptions{
		RepoUrl:      getRepoUrl(),
		Source:       apiSource,
		User:         user,
		MarketSource: appSource,
		Images:       asImages,
		Envs:         asEnvs,
		Entrances:    asEntrances,
		RawAppName:   rawAppName,
		RequestHash:  requestHash,
		Title:        title,
	}
	hdr := appservice.OpHeaders{
		Token:        token,
		User:         task.User,
		MarketUser:   user,
		MarketSource: appSource,
	}

	// baseEnvelope mirrors the legacy success / failure result shape
	// so API-side consumers (sync wait + frontend backend_response
	// viewer) keep the exact same field set as before the migration.
	// The legacy "url" field is dropped (typed client owns transport).
	baseEnvelope := func() map[string]interface{} {
		return map[string]interface{}{
			"operation":   "clone",
			"app_name":    urlAppName,
			"user":        user,
			"app_source":  appSource,
			"api_source":  apiSource,
			"cfgType":     cfgType,
			"rawAppName":  rawAppName,
			"requestHash": requestHash,
			"title":       title,
		}
	}

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for clone task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), err
	}

	glog.Infof("[APP] Sending HTTP request for app clone: task=%s urlAppName=%s", task.ID, urlAppName)
	opResp, err := client.CloneApp(context.Background(), urlAppName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service clone failed for task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
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

	// Mirror AppInstall's strict opID check: clone success WITHOUT an
	// opID is treated as a failure, matching the legacy code's
	// "opID not found in response data" branch which already returned
	// the same shape.
	if opResp == nil || opResp.Data == nil || opResp.Data.OpID == "" {
		glog.Infof("opID not found in response data for task: %s", task.ID)
		envelope := baseEnvelope()
		backend := map[string]interface{}{}
		if opResp != nil {
			backend["code"] = opResp.Code
			backend["message"] = opResp.Message
		}
		envelope["backend_response"] = backend
		envelope["error"] = "opID not found in response data"
		envelope["status"] = "failed"
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), fmt.Errorf("opID not found in response data")
	}

	task.OpID = opResp.Data.OpID
	glog.Infof("Successfully extracted opID: %s for task: %s", opResp.Data.OpID, task.ID)

	// Materialise the clone's user_applications + user_application_states
	// rows AFTER app-service accepts the clone (i.e. opID issued). Doing
	// this in the HTTP handler before AddTask leaks "ghost" rows whenever
	// app-service rejects the request — most commonly HTTP 422 when the
	// user has not yet filled in required envs/entrances. Each retry with
	// different request body hashes to a distinct newAppID (because
	// requestHash covers Title/Envs/Entrances), each insert hits a fresh
	// (user, source, app_id) tuple, and the UNIQUE constraint does not
	// catch the duplicates.
	//
	// CloneUserApplication runs both the user_applications insert (with
	// per-clone title / entrance-title overrides patched into the source
	// row's JSONB columns) and the user_application_states pending row
	// in a single PG transaction; partial failures cannot leave Market
	// with a half-materialised clone.
	//
	// On failure here we still return the success envelope because
	// app-service has already accepted the clone; the operation will
	// proceed on the cluster. The missing rows are logged loudly so
	// they show up in monitoring; a follow-up reconciliation pass can
	// rematerialise them from NATS state events (UpsertStateFromNATS
	// will silently skip until the user_applications row appears).
	newAppID := metadataString(task.Metadata, "realAppID")
	version := metadataString(task.Metadata, "version")
	if newAppID == "" {
		glog.Errorf("AppClone: missing realAppID in metadata after app-service success (task=%s app=%s opID=%s); skipping user_applications materialisation",
			task.ID, urlAppName, task.OpID)
	} else {
		// entranceTitles maps positionally onto user_applications.entrances
		// per the clone-request contract (index-aligned, empty title at
		// a covered index clears the inherited title, indices beyond the
		// source's entrances are silently dropped). entrances above is
		// already the typed []AppEntrance carried in task.Metadata; reuse
		// it without re-reading the metadata map.
		entranceTitles := make([]string, len(entrances))
		for i, e := range entrances {
			entranceTitles[i] = e.Title
		}
		if err := store.CloneUserApplication(context.Background(), store.CloneUserApplicationInput{
			UserID:              user,
			SourceID:            appSource,
			SrcAppName:          rawAppName,
			NewAppID:            newAppID,
			NewAppName:          urlAppName,
			Title:               title,
			EntranceTitles:      entranceTitles,
			PendingStateVersion: version,
		}); err != nil {
			glog.Errorf("AppClone: failed to materialise clone rows after app-service success (task=%s user=%s source=%s src=%s -> new=%s/%s opID=%s): %v",
				task.ID, user, appSource, rawAppName, urlAppName, newAppID, task.OpID, err)
		}
	}

	tm.linkStateOpID(task, task.AppName, "clone")

	envelope := baseEnvelope()
	envelope["backend_response"] = map[string]interface{}{
		"code":    opResp.Code,
		"message": opResp.Message,
		"data":    map[string]interface{}{"opID": opResp.Data.OpID},
	}
	envelope["opID"] = task.OpID
	envelope["status"] = "success"
	successJSON, _ := json.Marshal(envelope)
	glog.Infof("App clone completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
