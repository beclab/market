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

	token, ok := task.Metadata["token"].(string)
	if !ok {
		return "", fmt.Errorf("missing token in task metadata")
	}

	source, ok := task.Metadata["source"].(string)
	if !ok {
		source = "store" // Default source
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/api/v1/apps/%s/install", appServiceHost, appServicePort, appName)

	installInfo := &InstallOptions{
		RepoUrl: getRepoUrl(),
		User:    user,
		Source:  source,
	}
	ms, err := json.Marshal(installInfo)
	if err != nil {
		return "", err
	}
	log.Printf("installUrl:%s, installInfo:%s, token:%s\n", urlStr, string(ms), token)

	headers := map[string]string{
		"Authorization":   token,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": source,
	}

	return sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
}

// getRepoUrl returns the repository URL
func getRepoUrl() string {
	return os.Getenv("REPO_URL")
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
