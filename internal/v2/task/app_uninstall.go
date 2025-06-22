package task

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// AppUninstall uninstalls an application using the app service.
func (tm *TaskModule) AppUninstall(task *Task) (string, error) {
	appName := task.AppName

	log.Printf("Starting app uninstallation: app=%s, user=%s, task_id=%s", appName, task.User, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		log.Printf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")
	urlStr := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/uninstall", appServiceHost, appServicePort, appName)

	log.Printf("App service URL: %s for task: %s", urlStr, task.ID)

	headers := map[string]string{
		"X-Authorization": token,
		// Content-Type is not strictly necessary for a request with no body,
		// but we include it for consistency.
		"Content-Type": "application/json",
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app uninstallation: task=%s", task.ID)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, nil)
	if err != nil {
		log.Printf("HTTP request failed for app uninstallation: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "uninstall",
			"app_name":  appName,
			"user":      task.User,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app uninstallation: task=%s, response_length=%d", task.ID, len(response))

	// Create success result
	successResult := map[string]interface{}{
		"operation": "uninstall",
		"app_name":  appName,
		"user":      task.User,
		"url":       urlStr,
		"response":  response,
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App uninstallation completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
