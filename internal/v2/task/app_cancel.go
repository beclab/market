package task

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// AppCancel cancels a running task, like app installation.
// It retrieves the task's UID, type, and an authentication token from the task's metadata.
// It then constructs a request to the app service's cancel endpoint.
func (tm *TaskModule) AppCancel(task *Task) (string, error) {
	log.Printf("Starting app cancel: app=%s, user=%s, task_id=%s", task.AppName, task.User, task.ID)

	// a uid is required to identify which task to cancel.
	uid, ok := task.Metadata["uid"].(string)
	if !ok {
		log.Printf("Missing uid in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing uid in task metadata")
	}

	// 't' from the original AppCancel function is assumed to be the 'type' of task, e.g., "install".
	taskType, ok := task.Metadata["type"].(string)
	if !ok {
		log.Printf("Missing type in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing type in task metadata")
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		log.Printf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// The URL format is based on the legacy AppCancel function's parameters (uid, t)
	// and adapted to the v2 API style. The original function signature was AppCancel(uid, t, token).
	urlStr := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/cancel?type=operate", appServiceHost, appServicePort, uid)

	log.Printf("App service URL: %s for task: %s, uid: %s, task_type: %s", urlStr, task.ID, uid, taskType)

	headers := map[string]string{
		"X-Authorization": token,
		"Content-Type":    "application/json",
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app cancel: task=%s, uid=%s, task_type=%s", task.ID, uid, taskType)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, nil)
	if err != nil {
		log.Printf("HTTP request failed for app cancel: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "cancel",
			"app_name":  task.AppName,
			"user":      task.User,
			"uid":       uid,
			"task_type": taskType,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app cancel: task=%s, response_length=%d", task.ID, len(response))

	// Create success result
	successResult := map[string]interface{}{
		"operation": "cancel",
		"app_name":  task.AppName,
		"user":      task.User,
		"uid":       uid,
		"task_type": taskType,
		"url":       urlStr,
		"response":  response,
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App cancel completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
