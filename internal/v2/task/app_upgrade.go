package task

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang/glog"
)

// UpgradeOptions represents the options for app upgrade.
type UpgradeOptions struct {
	RepoUrl      string      `json:"repoUrl,omitempty"`
	Version      string      `json:"version,omitempty"`
	User         string      `json:"x_market_user,omitempty"`
	Source       string      `json:"source,omitempty"`
	MarketSource string      `json:"x_market_source,omitempty"`
	Images       []Image     `json:"images,omitempty"`
	Envs         []AppEnvVar `json:"envs"`
}

// AppUpgrade upgrades an application using the app service.
func (tm *TaskModule) AppUpgrade(task *Task) (string, error) {
	// Use task.AppName which is the clone app name (e.g., windowstest) for clone apps
	// or the original app name for regular apps
	appName := task.AppName
	user := task.User

	// Check if this is a clone app upgrade
	rawAppName, isCloneApp := task.Metadata["rawAppName"].(string)
	if isCloneApp && rawAppName != "" {
		glog.Infof("Starting clone app upgrade: cloneAppName=%s, rawAppName=%s, user=%s, task_id=%s", appName, rawAppName, user, task.ID)
	} else {
		glog.Infof("Starting app upgrade: app=%s, user=%s, task_id=%s", appName, user, task.ID)
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Warningf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	source, ok := task.Metadata["source"].(string)
	if !ok {
		glog.Warningf("undefine source for task: %s", task.ID)
	}

	// Get cfgType from metadata
	// cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		// cfgType = "app" // Default to app type
	}

	version, ok := task.Metadata["version"].(string)
	if !ok {
		glog.Warningf("Missing version in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing version in task metadata for upgrade")
	}

	var apiSource string
	if source == "upload" {
		apiSource = "custom"
	} else {
		apiSource = "market"
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// Use appName (clone app name for clone apps, original app name for regular apps) in URL
	var urlStr string
	// if cfgType == "recommend" {
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/recommends/%s/upgrade", appServiceHost, appServicePort, appName)
	// 	glog.Infof("App service URL: %s for task: %s, version: %s", urlStr, task.ID, version)
	// } else {
	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/upgrade", appServiceHost, appServicePort, appName)
	if isCloneApp && rawAppName != "" {
		glog.Infof("App service URL for clone app upgrade: %s (cloneAppName=%s, rawAppName=%s) for task: %s, version: %s", urlStr, appName, rawAppName, task.ID, version)
	} else {
		glog.Infof("App service URL: %s for task: %s, version: %s", urlStr, task.ID, version)
	}

	// }

	glog.Infof("App source: %s, API source: %s: %s for task: %s", source, apiSource, task.ID)

	// Get envs from metadata
	var envs []AppEnvVar
	if envsData, ok := task.Metadata["envs"]; ok && envsData != nil {
		envs, _ = envsData.([]AppEnvVar)
		glog.Infof("Retrieved %d environment variables for task: %s", len(envs), task.ID)
	}

	upgradeInfo := &UpgradeOptions{
		RepoUrl:      getRepoUrl(),
		Version:      version,
		User:         user,
		Source:       apiSource,
		MarketSource: source,
		Images:       task.Metadata["images"].([]Image),
		Envs:         envs,
	}
	ms, err := json.Marshal(upgradeInfo)
	if err != nil {
		glog.Errorf("Failed to marshal upgrade info for task %s: %v", task.ID, err)
		return "", err
	}
	glog.Infof("Upgrade request prepared: url=%s, upgradeInfo=%s, task_id=%s", urlStr, string(ms), task.ID)

	headers := map[string]string{
		"Accept":          "*/*",
		"X-Authorization": token,
		"X-Bfl-User":      task.User,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": source,
	}

	// Send HTTP request and get response
	glog.Infof("Sending HTTP request for app upgrade: task=%s, version=%s", task.ID, version)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
	if err != nil {
		glog.Errorf("HTTP request failed for app upgrade: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "upgrade",
			"app_name":  appName,
			"user":      user,
			"source":    source,
			"apiSource": apiSource,
			"version":   version,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	glog.Infof("HTTP request completed successfully for app upgrade: task=%s, response_length=%d", task.ID, len(response))

	// Create success result
	successResult := map[string]interface{}{
		"operation": "upgrade",
		"app_name":  appName,
		"user":      user,
		"source":    source,
		"apiSource": apiSource,
		"version":   version,
		"url":       urlStr,
		"response":  response,
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	glog.Infof("App upgrade completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
