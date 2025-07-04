package task

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// UpgradeOptions represents the options for app upgrade.
type UpgradeOptions struct {
	RepoUrl string `json:"repoUrl,omitempty"`
	Version string `json:"version,omitempty"`
	User    string `json:"x_market_user,omitempty"`
	Source  string `json:"x_market_source,omitempty"`
}

// AppUpgrade upgrades an application using the app service.
func (tm *TaskModule) AppUpgrade(task *Task) (string, error) {
	appName := task.AppName
	user := task.User

	log.Printf("Starting app upgrade: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		log.Printf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	source, ok := task.Metadata["source"].(string)
	if !ok {
		source = "market" // Default source
		log.Printf("Using default source 'store' for task: %s", task.ID)
	}

	version, ok := task.Metadata["version"].(string)
	if !ok {
		log.Printf("Missing version in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing version in task metadata for upgrade")
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/upgrade", appServiceHost, appServicePort, appName)

	log.Printf("App service URL: %s for task: %s, version: %s", urlStr, task.ID, version)

	upgradeInfo := &UpgradeOptions{
		RepoUrl: getRepoUrl(),
		Version: version,
		User:    user,
		Source:  source,
	}
	ms, err := json.Marshal(upgradeInfo)
	if err != nil {
		log.Printf("Failed to marshal upgrade info for task %s: %v", task.ID, err)
		return "", err
	}
	log.Printf("Upgrade request prepared: url=%s, upgradeInfo=%s, task_id=%s", urlStr, string(ms), task.ID)

	headers := map[string]string{
		"X-Authorization": token,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": source,
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app upgrade: task=%s, version=%s", task.ID, version)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
	if err != nil {
		log.Printf("HTTP request failed for app upgrade: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "upgrade",
			"app_name":  appName,
			"user":      user,
			"source":    source,
			"version":   version,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app upgrade: task=%s, response_length=%d", task.ID, len(response))

	// Create success result
	successResult := map[string]interface{}{
		"operation": "upgrade",
		"app_name":  appName,
		"user":      user,
		"source":    source,
		"version":   version,
		"url":       urlStr,
		"response":  response,
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App upgrade completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
