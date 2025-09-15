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
	RepoUrl      string  `json:"repoUrl,omitempty"`
	Version      string  `json:"version,omitempty"`
	User         string  `json:"x_market_user,omitempty"`
	Source       string  `json:"source,omitempty"`
	MarketSource string  `json:"x_market_source,omitempty"`
	Images       []Image `json:"images,omitempty"`
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
		log.Printf("undefine source for task: %s", task.ID)
	}

	// Get cfgType from metadata
	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		log.Printf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app" // Default to app type
	}

	version, ok := task.Metadata["version"].(string)
	if !ok {
		log.Printf("Missing version in task metadata for task: %s", task.ID)
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

	var urlStr string
	// if cfgType == "recommend" {
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/recommends/%s/upgrade", appServiceHost, appServicePort, appName)
	// 	log.Printf("App service URL: %s for task: %s, version: %s", urlStr, task.ID, version)
	// } else {
	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/upgrade", appServiceHost, appServicePort, appName)
	log.Printf("App service URL: %s for task: %s, version: %s", urlStr, task.ID, version)

	// }

	log.Printf("App source: %s, API source: %s: %s for task: %s", source, apiSource, task.ID)

	upgradeInfo := &UpgradeOptions{
		RepoUrl:      getRepoUrl(),
		Version:      version,
		User:         user,
		Source:       apiSource,
		MarketSource: source,
		Images:       task.Metadata["images"].([]Image),
	}
	ms, err := json.Marshal(upgradeInfo)
	if err != nil {
		log.Printf("Failed to marshal upgrade info for task %s: %v", task.ID, err)
		return "", err
	}
	log.Printf("Upgrade request prepared: url=%s, upgradeInfo=%s, task_id=%s", urlStr, string(ms), task.ID)

	headers := map[string]string{
		"Accept":          "*/*",
		"X-Authorization": token,
		"X-Bfl-User":      task.User,
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
			"apiSource": apiSource,
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
		"apiSource": apiSource,
		"version":   version,
		"url":       urlStr,
		"response":  response,
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App upgrade completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
