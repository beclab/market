package task

import (
	"fmt"
	"net/http"
	"os"
)

// AppCancel cancels a running task, like app installation.
// It retrieves the task's UID, type, and an authentication token from the task's metadata.
// It then constructs a request to the app service's cancel endpoint.
func (tm *TaskModule) AppCancel(task *Task) (string, error) {
	// a uid is required to identify which task to cancel.
	uid, ok := task.Metadata["uid"].(string)
	if !ok {
		return "", fmt.Errorf("missing uid in task metadata")
	}

	// 't' from the original AppCancel function is assumed to be the 'type' of task, e.g., "install".
	taskType, ok := task.Metadata["type"].(string)
	if !ok {
		return "", fmt.Errorf("missing type in task metadata")
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		return "", fmt.Errorf("missing token in task metadata")
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// The URL format is based on the legacy AppCancel function's parameters (uid, t)
	// and adapted to the v2 API style. The original function signature was AppCancel(uid, t, token).
	urlStr := fmt.Sprintf("http://%s:%s/api/v1/tasks/cancel/%s/%s", appServiceHost, appServicePort, uid, taskType)

	headers := map[string]string{
		"Authorization": token,
		"Content-Type":  "application/json",
	}

	// The cancel endpoint uses a POST request, presumably with an empty body,
	// similar to the uninstall endpoint.
	return sendHttpRequest(http.MethodPost, urlStr, headers, nil)
}
