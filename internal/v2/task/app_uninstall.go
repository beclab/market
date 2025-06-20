package task

import (
	"fmt"
	"net/http"
	"os"
)

// AppUninstall uninstalls an application using the app service.
func (tm *TaskModule) AppUninstall(task *Task) (string, error) {
	appName := task.AppName

	token, ok := task.Metadata["token"].(string)
	if !ok {
		return "", fmt.Errorf("missing token in task metadata")
	}

	appServiceHost := os.Getenv("APP_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/api/v1/apps/%s/uninstall", appServiceHost, appServicePort, appName)

	headers := map[string]string{
		"Authorization": token,
		// Content-Type is not strictly necessary for a request with no body,
		// but we include it for consistency.
		"Content-Type": "application/json",
	}

	// The app service uninstall endpoint uses a POST request with an empty body.
	return sendHttpRequest(http.MethodPost, urlStr, headers, nil)
}
