package task

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"market/internal/v2/appservice"

	"github.com/golang/glog"
)

// appServiceClientOnce / appServiceClient / appServiceClientErr cache a
// process-wide typed client built from APP_SERVICE_SERVICE_HOST/PORT.
// All five task executors (install / clone / uninstall / upgrade /
// cancel) now share this single client — the previous hand-rolled
// http.Client + JSON-envelope parsing has been retired across the
// package.
//
// WithTimeout is raised to 2 minutes (vs the package default of 30s)
// because operations today return synchronously after app-service
// acknowledges the request and assigns an OpID; under load that can
// occasionally exceed 30s. Subsequent state transitions are NATS-driven
// and do not flow through this client.
var (
	appServiceClient     appservice.Client
	appServiceClientErr  error
	appServiceClientOnce sync.Once
)

func getAppServiceClient() (appservice.Client, error) {
	appServiceClientOnce.Do(func() {
		appServiceClient, appServiceClientErr = appservice.NewClient(
			appservice.WithTimeout(2 * time.Minute),
		)
	})
	return appServiceClient, appServiceClientErr
}

type AppEnvVar struct {
	EnvName   string           `json:"envName" yaml:"envName" validate:"required"`
	Value     string           `json:"value,omitempty" yaml:"value,omitempty"`
	ValueFrom *AppEnvValueFrom `json:"valueFrom,omitempty" yaml:"valueFrom,omitempty"`
}

type AppEnvValueFrom struct {
	EnvName string `json:"envName,omitempty" yaml:"envName,omitempty"`
}

type Image struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// AppInstall installs an application using the app service
func (tm *TaskModule) AppInstall(task *Task) (*AppOperationMessage, error) {
	appName := task.AppName
	user := task.User

	glog.V(2).Infof("Starting app installation: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing token in task metadata")
	}

	// Get app source from metadata
	appSource, ok := task.Metadata["source"].(string)
	if !ok {
		glog.Warningf("Missing source in task metadata for task: %s", task.ID)
		return BaseParamInvalid(), fmt.Errorf("missing source in task metadata")
	}

	// Get cfgType from metadata
	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app" // Default to app type
	}

	// Convert app source to API source parameter
	// If app source is "local", use "custom" for API
	// Otherwise, use "market" for API
	var apiSource string
	if appSource == "upload" {
		apiSource = "custom"
	} else {
		apiSource = "market"
	}

	envelope := BaseInstallEnvelope(task, cfgType, appSource, apiSource)

	tm.mu.RLock()
	for _, runningTask := range tm.runningTasks {
		if runningTask.Type == InstallApp && runningTask.AppName == appName && runningTask.ID != task.ID {
			tm.mu.RUnlock()
			envelope.SetExistsEnvelope("Another installation task is already running for this app", "failed", runningTask.ID)

			return envelope, fmt.Errorf("another installation task is already running, runningTaskId: %s", runningTask.ID)
		}
	}
	tm.mu.RUnlock()

	glog.V(2).Infof("App source: %s, API source: %s, cfgType: %s for task: %s", appSource, apiSource, cfgType, task.ID)

	// Get envs from metadata
	var envs []AppEnvVar
	if envsData, ok := task.Metadata["envs"]; ok && envsData != nil {
		envs, _ = envsData.([]AppEnvVar)
		glog.V(3).Infof("Retrieved %d environment variables for task: %s", len(envs), task.ID)
	}

	// Get VC from purchase receipt and inject into environment variables
	tm.mu.RLock()
	settingsManager := tm.settingsManager
	tm.mu.RUnlock()

	if settingsManager != nil {
		vcAppID := appName
		if realIDRaw, ok := task.Metadata["realAppID"].(string); ok && strings.TrimSpace(realIDRaw) != "" {
			vcAppID = strings.TrimSpace(realIDRaw)
		}
		glog.V(2).Infof("App install VC lookup using appID=%s (display name %s) for task: %s", vcAppID, appName, task.ID)

		vc := getVCForInstall(settingsManager, user, vcAppID, task.Metadata)
		if vc != "" {
			// Check if VERIFIABLE_CREDENTIAL already exists in envs
			vcExists := false
			for i := range envs {
				if envs[i].EnvName == "VERIFIABLE_CREDENTIAL" {
					envs[i].Value = vc
					vcExists = true
					glog.V(2).Infof("Updated VERIFIABLE_CREDENTIAL in envs for task: %s", task.ID)
					break
				}
			}
			// If not exists, add it
			if !vcExists {
				envs = append(envs, AppEnvVar{
					EnvName: "VERIFIABLE_CREDENTIAL",
					Value:   vc,
				})
				glog.V(2).Infof("Added VERIFIABLE_CREDENTIAL to envs for task: %s", task.ID)
			}
		} else {
			glog.V(3).Infof("VC not found for app installation, skipping VERIFIABLE_CREDENTIAL injection for task: %s", task.ID)
		}
	} else {
		glog.V(3).Infof("Settings manager not available, skipping VC injection for task: %s", task.ID)
	}

	// Get images from metadata
	var images []Image
	if imagesData, ok := task.Metadata["images"]; ok && imagesData != nil {
		if imagesSlice, ok := imagesData.([]Image); ok {
			images = imagesSlice
		}
	}

	// Convert task-package wire types to the appservice client's
	// counterparts. Field-for-field copies; the duplicate types in
	// this package are kept in place because pkg/v2/api/task.go and
	// the other four executors still reference them. Once those
	// callers migrate, the duplicates can be retired in favour of
	// the appservice types directly.
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

	opts := appservice.InstallOptions{
		RepoUrl:      getRepoUrl(),
		Source:       apiSource,
		User:         user,
		MarketSource: appSource,
		Images:       asImages,
		Envs:         asEnvs,
	}
	hdr := appservice.OpHeaders{
		Token:        token,
		User:         task.User,
		MarketUser:   user,
		MarketSource: appSource,
	}

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for install task=%s: %v", task.ID, err)
		envelope.SetFailed(err.Error())
		return envelope, err
	}

	glog.Infof("[APP] Sending HTTP request for app installation: task=%s", task.ID)
	opResp, opHttpStatus, err := client.InstallApp(context.Background(), appName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service install failed for task=%s: %v", task.ID, err)
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

// getRepoUrl returns the repository URL passed through to app-service in
// install / upgrade / clone request bodies as RepoUrl. Sourced from the
// REPO_URL_HOST / REPO_URL_PORT env vars set by the chart-repo K8s
// service. Kept as a package-level helper because all four operation
// executors share it (and pkg/v2/api/task.go does not).
func getRepoUrl() string {
	repoServiceHost := os.Getenv("REPO_URL_HOST")
	repoStoreServicePort := os.Getenv("REPO_URL_PORT")
	return fmt.Sprintf("http://%s:%s/", repoServiceHost, repoStoreServicePort)
}
