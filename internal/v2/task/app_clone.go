package task

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"market/internal/v2/appservice"
	"market/internal/v2/db/models"
	"market/internal/v2/store"
	"market/internal/v2/types"

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
func (tm *TaskModule) AppClone(task *Task) (*AppOperationMessage, error) {
	appName := task.AppName
	user := task.User

	glog.Infof("Starting app clone: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	tm.mu.RLock()
	for _, runningTask := range tm.runningTasks {
		if runningTask.Type == CloneApp && runningTask.AppName == appName && runningTask.ID != task.ID {
			tm.mu.RUnlock()
			glog.Infof("Clone failed: another clone task is already running for app: %s, existing task ID: %s", appName, runningTask.ID)
			existsEnvelope := BaseCloneExistsEnvelope(task, runningTask.ID)
			return existsEnvelope, fmt.Errorf("another clone task is already running for app: %s", appName)
		}
	}
	tm.mu.RUnlock()

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing token in task metadata")
	}

	appSource, ok := task.Metadata["source"].(string)
	if !ok {
		glog.Warningf("Missing source in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing source in task metadata")
	}

	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app"
	}

	rawAppName, ok := task.Metadata["rawAppName"].(string)
	if !ok || rawAppName == "" {
		glog.Warningf("Missing rawAppName in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing rawAppName in task metadata")
	}

	requestHash, ok := task.Metadata["requestHash"].(string)
	if !ok || requestHash == "" {
		glog.Warningf("Missing requestHash in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing requestHash in task metadata")
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

	envelope := BaseCloneEnvelope(task, urlAppName, cfgType, appSource, apiSource, rawAppName, requestHash, title)

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

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for clone task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	glog.Infof("[APP] Sending HTTP request for app clone: task=%s urlAppName=%s", task.ID, urlAppName)

	opResp, opHttpStatus, err := client.CloneApp(context.Background(), urlAppName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service clone failed for task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	if opResp.Code != http.StatusOK {
		envelope.SetFailed("opID not found in response data")
		envelope.SetResponse(opResp, opResp.Code, opResp.Data.String(), opHttpStatus)
		return envelope, fmt.Errorf("opID not found in response data")
	}

	task.OpID = opResp.Data.OpId
	glog.Infof("Successfully extracted opID: %s for task: %s", opResp.Data.OpId, task.ID)

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
		} else {
			tm.notifyCloneNewAppReady(context.Background(), task.ID, user, appSource, newAppID, version)
		}
	}

	tm.linkStateOpID(task, task.AppName, "clone")

	envelope.SetSuccess()
	envelope.SetOpId(opResp.Data.OpId)
	envelope.SetResponse(opResp, opResp.Code, opResp.Data.String(), opHttpStatus)

	glog.Infof("App clone completed successfully: task=%s", task.ID)

	return envelope, nil
}

// notifyCloneNewAppReady mirrors hydration's new_app_ready push for clone
// materialisation. The clone row has already been copied into
// user_applications, so rebuild app_info from PG instead of reusing the
// original app's catalog entry.
func (tm *TaskModule) notifyCloneNewAppReady(ctx context.Context, taskID, userID, sourceID, appID, appVersion string) {
	if tm.dataSender == nil {
		return
	}

	row, err := store.GetUserApplication(ctx, userID, sourceID, appID)
	if err != nil {
		glog.Errorf("AppClone: read clone user_applications for new_app_ready failed (task=%s user=%s source=%s app=%s): %v",
			taskID, userID, sourceID, appID, err)
		return
	}
	if row == nil {
		glog.Warningf("AppClone: clone user_applications missing for new_app_ready (task=%s user=%s source=%s app=%s)",
			taskID, userID, sourceID, appID)
		return
	}

	if appVersion == "" {
		appVersion = userApplicationMetadataVersion(row)
	}

	appInfo, err := composeUserApplicationInfo(row, appVersion)
	if err != nil {
		glog.Errorf("AppClone: compose clone app_info failed for new_app_ready (task=%s user=%s source=%s app=%s): %v",
			taskID, userID, sourceID, appID, err)
		appInfo = &types.ApplicationInfoEntry{
			ID:         row.AppID,
			AppID:      row.AppID,
			Name:       row.AppName,
			Version:    appVersion,
			CfgType:    row.ManifestType,
			ApiVersion: row.APIVersion,
		}
	}

	update := types.MarketSystemUpdate{
		Timestamp:  time.Now().Unix(),
		User:       userID,
		NotifyType: "market_system_point",
		Point:      "new_app_ready",
		Extensions: map[string]string{
			"app_name":    row.AppName,
			"app_version": appVersion,
			"source":      row.SourceID,
		},
		ExtensionsObj: map[string]interface{}{
			"app_info": appInfo,
		},
	}
	if err := tm.dataSender.SendMarketSystemUpdate(update); err != nil {
		glog.Errorf("AppClone: send new_app_ready failed (task=%s user=%s source=%s app=%s): %v",
			taskID, userID, sourceID, appID, err)
	}
}

func composeUserApplicationInfo(row *models.UserApplication, appVersion string) (*types.ApplicationInfoEntry, error) {
	manifest := userApplicationManifest(row)
	scalars := types.EntryScalars{
		ID:         row.AppID,
		AppID:      row.AppID,
		Version:    appVersion,
		CfgType:    row.ManifestType,
		ApiVersion: row.APIVersion,
	}
	if row.I18n != nil {
		scalars.I18n = types.NestI18nForEntry(row.I18n.Data)
	}
	if row.VersionHistory != nil && len(row.VersionHistory.Data) > 0 {
		versionHistory := make([]*types.VersionInfo, 0, len(row.VersionHistory.Data))
		for i := range row.VersionHistory.Data {
			versionHistory = append(versionHistory, &row.VersionHistory.Data[i])
		}
		scalars.VersionHistory = versionHistory
	}
	entry, err := types.ComposeApplicationInfoEntry(manifest, scalars)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		entry.CreateTime = row.UpdatedAt.Unix()
		entry.UpdateTime = row.UpdatedAt.Unix()
	}
	return entry, nil
}

func userApplicationManifest(row *models.UserApplication) *types.UserAppManifest {
	m := &types.UserAppManifest{}
	if row == nil {
		return m
	}
	if row.Metadata != nil {
		m.Metadata = row.Metadata.Data
	}
	if row.Spec != nil {
		m.Spec = row.Spec.Data
	}
	if row.Resources != nil {
		m.Resources = row.Resources.Data
	}
	if row.Options != nil {
		m.Options = row.Options.Data
	}
	if row.Tailscale != nil {
		m.Tailscale = row.Tailscale.Data
	}
	if row.Permission != nil {
		m.Permission = row.Permission.Data
	}
	if row.Middleware != nil {
		m.Middleware = row.Middleware.Data
	}
	if row.Entrances != nil {
		m.Entrances = row.Entrances.Data
	}
	if row.SharedEntrances != nil {
		m.SharedEntrances = row.SharedEntrances.Data
	}
	if row.Ports != nil {
		m.Ports = row.Ports.Data
	}
	if row.Envs != nil {
		m.Envs = row.Envs.Data
	}
	return m
}

func userApplicationMetadataVersion(row *models.UserApplication) string {
	if row == nil || row.Metadata == nil {
		return ""
	}
	if v, ok := row.Metadata.Data["version"].(string); ok {
		return v
	}
	return ""
}
