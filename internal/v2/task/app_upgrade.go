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

	token, ok := task.Metadata["token"].(string)
	if !ok {
		return "", fmt.Errorf("missing token in task metadata")
	}

	source, ok := task.Metadata["source"].(string)
	if !ok {
		source = "store" // Default source
	}

	version, ok := task.Metadata["version"].(string)
	if !ok {
		return "", fmt.Errorf("missing version in task metadata for upgrade")
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/api/v1/apps/%s/upgrade", appServiceHost, appServicePort, appName)

	upgradeInfo := &UpgradeOptions{
		RepoUrl: getRepoUrl(),
		Version: version,
		User:    user,
		Source:  source,
	}
	ms, err := json.Marshal(upgradeInfo)
	if err != nil {
		return "", err
	}
	log.Printf("upgradeUrl:%s, upgradeInfo:%s, token:%s\n", urlStr, string(ms), token)

	headers := map[string]string{
		"Authorization":   token,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": source,
	}

	return sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
}
