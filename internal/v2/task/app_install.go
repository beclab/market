package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// First step of replacing the hand-rolled http.Client + JSON-envelope
// parsing across the task package; the other four executors (uninstall
// / cancel / upgrade / clone) keep their legacy sendHttpRequest path
// until they migrate in subsequent commits.
//
// WithTimeout is raised to 2 minutes (vs the package default of 30s)
// because install today returns synchronously after app-service
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
func (tm *TaskModule) AppInstall(task *Task) (string, error) {
	appName := task.AppName
	user := task.User

	glog.V(2).Infof("Starting app installation: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	// Check if there's already a running or pending install task for the same app
	tm.mu.RLock()

	// Check running tasks
	for _, runningTask := range tm.runningTasks {
		if runningTask.Type == InstallApp && runningTask.AppName == appName && runningTask.ID != task.ID {
			tm.mu.RUnlock()
			glog.Warningf("Installation failed: another install task is already running for app: %s, existing task ID: %s", appName, runningTask.ID)
			errorResult := map[string]interface{}{
				"operation":        "install",
				"app_name":         appName,
				"user":             user,
				"error":            "Another installation task is already running for this app",
				"status":           "failed",
				"existing_task_id": runningTask.ID,
			}
			errorJSON, _ := json.Marshal(errorResult)
			return string(errorJSON), fmt.Errorf("another installation task is already running for app: %s", appName)
		}
	}

	tm.mu.RUnlock()

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	// Get app source from metadata
	appSource, ok := task.Metadata["source"].(string)
	if !ok {
		glog.Warningf("Missing source in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing source in task metadata")
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

	glog.V(2).Infof("App source: %s, API source: %s, cfgType: %s for task: %s", appSource, apiSource, cfgType, task.ID)

	// urlStr is composed for diagnostic / envelope use only; the real
	// HTTP call is dispatched through the typed appservice.Client
	// below, which resolves the same APP_SERVICE_SERVICE_HOST/PORT env
	// vars at construction time.
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/install", appServiceHost, appServicePort, appName)

	glog.V(2).Infof("App service URL: %s for task: %s", urlStr, task.ID)

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

	// Compose a base envelope shared by every return path so the wire
	// shape stays compatible with the legacy raw-map projection that
	// the API layer and frontend already consume.
	baseEnvelope := func() map[string]interface{} {
		return map[string]interface{}{
			"operation":  "install",
			"app_name":   appName,
			"user":       user,
			"app_source": appSource,
			"api_source": apiSource,
			"cfgType":    cfgType,
			"url":        urlStr,
		}
	}

	client, err := getAppServiceClient()
	if err != nil {
		glog.Errorf("App-service client unavailable for install task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), err
	}

	glog.Infof("[APP] Sending HTTP request for app installation: task=%s", task.ID)
	opResp, err := client.InstallApp(context.Background(), appName, opts, hdr)
	if err != nil {
		glog.Errorf("App-service install failed for task=%s: %v", task.ID, err)
		envelope := baseEnvelope()
		envelope["error"] = err.Error()
		envelope["status"] = "failed"
		// Surface the upstream business code/message envelope when we
		// have one (HTTP 200 + Code != 200 path); transport-level
		// failures (5xx / network) carry no useful body to expose.
		var opErr *appservice.OpError
		if errors.As(err, &opErr) {
			backend := map[string]interface{}{
				"code":    opErr.Code,
				"message": opErr.Message,
			}
			if opErr.OpID != "" {
				backend["data"] = map[string]interface{}{"opID": opErr.OpID}
			}
			envelope["backend_response"] = backend
		}
		errorJSON, _ := json.Marshal(envelope)
		return string(errorJSON), err
	}

	// HTTP 200 + Code 200. Validate that the upstream actually shipped
	// an OpID; legacy code returned this as a failure with backend
	// envelope, preserve that semantic.
	if opResp == nil || opResp.Data == nil || opResp.Data.OpID == "" {
		glog.V(3).Infof("opID not found in install response for task: %s", task.ID)
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
	glog.V(3).Infof("Successfully extracted opID: %s for task: %s", opResp.Data.OpID, task.ID)
	tm.linkStateOpID(task, appName, "install")

	envelope := baseEnvelope()
	envelope["backend_response"] = map[string]interface{}{
		"code":    opResp.Code,
		"message": opResp.Message,
		"data":    map[string]interface{}{"opID": opResp.Data.OpID},
	}
	envelope["opID"] = task.OpID
	envelope["status"] = "success"
	successJSON, _ := json.Marshal(envelope)
	glog.V(2).Infof("App installation completed successfully: task=%s, result_length=%d, result=%s", task.ID, len(successJSON), string(successJSON))
	return string(successJSON), nil
}

// getRepoUrl returns the repository URL
func getRepoUrl() string {
	repoServiceHost := os.Getenv("REPO_URL_HOST")
	repoStoreServicePort := os.Getenv("REPO_URL_PORT")
	return fmt.Sprintf("http://%s:%s/", repoServiceHost, repoStoreServicePort)
}

// sendHttpRequest sends an HTTP request with the given token
func sendHttpRequest(method, urlStr string, headers map[string]string, body io.Reader) (string, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return "", err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(bodyBytes), nil
}
