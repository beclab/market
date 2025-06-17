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
	Source  string `json:"source,omitempty"`
}

// AppInstall installs an application using the app service
func (tm *TaskModule) AppInstall(appName, source, token string) (string, error) {
	appServiceHost := os.Getenv("APP_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/api/v1/apps/%s/install", appServiceHost, appServicePort, appName)

	installInfo := &InstallOptions{
		RepoUrl: getRepoUrl(),
		Source:  source,
	}
	ms, err := json.Marshal(installInfo)
	if err != nil {
		return "", err
	}
	log.Printf("installUrl:%s, installInfo:%s, token:%s\n", urlStr, string(ms), token)

	return sendHttpRequestWithToken(http.MethodPost, urlStr, token, strings.NewReader(string(ms)))
}

// getRepoUrl returns the repository URL
func getRepoUrl() string {
	return os.Getenv("REPO_URL")
}

// sendHttpRequestWithToken sends an HTTP request with the given token
func sendHttpRequestWithToken(method, url, token string, body *strings.Reader) (string, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")

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
