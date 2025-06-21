package task

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// InstallOptions represents the options for app installation
type InstallOptions struct {
	App     string `json:"appName,omitempty"`
	Dev     bool   `json:"devMode,omitempty"`
	RepoUrl string `json:"repoUrl,omitempty"`
	CfgUrl  string `json:"cfgUrl,omitempty"`
	Version string `json:"version,omitempty"`
	User    string `json:"x_market_user,omitempty"`
	Source  string `json:"x_market_source,omitempty"`
}

// AppInstall installs an application using the app service
func (tm *TaskModule) AppInstall(task *Task) (string, error) {
	appName := task.AppName
	user := task.User

	log.Printf("Starting app installation: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		log.Printf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	source, ok := task.Metadata["source"].(string)
	if !ok {
		source = "store" // Default source
		log.Printf("Using default source 'store' for task: %s", task.ID)
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/install", appServiceHost, appServicePort, appName)

	log.Printf("App service URL: %s for task: %s", urlStr, task.ID)

	installInfo := &InstallOptions{
		RepoUrl: getRepoUrl(),
		User:    user,
		Source:  source,
	}
	ms, err := json.Marshal(installInfo)
	if err != nil {
		log.Printf("Failed to marshal install info for task %s: %v", task.ID, err)
		return "", err
	}
	log.Printf("Install request prepared: url=%s, installInfo=%s, task_id=%s", urlStr, string(ms), task.ID)

	headers := map[string]string{
		"X-Authorization": token,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": source,
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app installation: task=%s", task.ID)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
	if err != nil {
		log.Printf("HTTP request failed for app installation: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "install",
			"app_name":  appName,
			"user":      user,
			"source":    source,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app installation: task=%s, response_length=%d", task.ID, len(response))

	// Create success result
	successResult := map[string]interface{}{
		"operation": "install",
		"app_name":  appName,
		"user":      user,
		"source":    source,
		"url":       urlStr,
		"response":  response,
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App installation completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}

// getRepoUrl returns the repository URL
func getRepoUrl() string {
	repoServiceHost := os.Getenv("REPO_URL_HOST")
	repoStoreServicePort := os.Getenv("REPO_URL_PORT")
	return fmt.Sprintf("http://%s:%s/charts", repoServiceHost, repoStoreServicePort)
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
