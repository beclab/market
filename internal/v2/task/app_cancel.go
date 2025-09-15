package task

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

// AppCancel cancels a running task, like app installation.
// It retrieves the task's app_name and an authentication token from the task's metadata.
// It then constructs a request to the app service's cancel endpoint.
func (tm *TaskModule) AppCancel(task *Task) (string, error) {
	log.Printf("Starting app cancel: app=%s, user=%s, task_id=%s", task.AppName, task.User, task.ID)

	// app_name is required to identify which task to cancel.
	appName, ok := task.Metadata["app_name"].(string)
	if !ok {
		log.Printf("Missing app_name in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing app_name in task metadata")
	}

	token, ok := task.Metadata["token"].(string)
	if !ok {
		log.Printf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	// Get cfgType from metadata
	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		log.Printf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app" // Default to app type
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// Choose API endpoint based on cfgType
	var urlStr string
	// if cfgType == "middleware" {
	// 	// Use middleware API for middleware type
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/%s/cancel?type=operate", appServiceHost, appServicePort, appName)
	// 	log.Printf("Using middleware API for cancel: %s", urlStr)
	// } else {
	// 	// Use regular app API for other types
	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/cancel?type=operate", appServiceHost, appServicePort, appName)
	log.Printf("Using regular app API for cancel: %s", urlStr)
	// }

	log.Printf("App service URL: %s for task: %s, app_name: %s, cfgType: %s", urlStr, task.ID, appName, cfgType)

	headers := map[string]string{
		"X-Authorization": token,
		"X-Bfl-User":      task.User,
		"Content-Type":    "application/json",
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app cancel: task=%s, app_name=%s", task.ID, appName)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, nil)
	if err != nil {
		log.Printf("HTTP request failed for app cancel: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "cancel",
			"app_name":  task.AppName,
			"user":      task.User,
			"uid":       appName,
			"cfgType":   cfgType,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app cancel: task=%s, response_length=%d", task.ID, len(response))

	// Parse response to extract opID if cancel is successful
	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(response), &responseData); err != nil {
		log.Printf("Failed to parse response JSON for task %s: %v", task.ID, err)
	} else {
		// Check if cancel was successful by checking code field
		if code, ok := responseData["code"].(float64); ok && code == 200 {
			if data, ok := responseData["data"].(map[string]interface{}); ok {
				if opID, ok := data["opID"].(string); ok && opID != "" {
					task.OpID = opID
					log.Printf("Successfully extracted opID: %s for task: %s", opID, task.ID)
				} else {
					log.Printf("opID not found in response data for task: %s", task.ID)
				}
			} else {
				log.Printf("Data field not found or not a map in response for task: %s", task.ID)
			}
		} else {
			log.Printf("Cancel code is not 200 for task: %s, code: %v", task.ID, code)
		}
	}

	// Create success result
	successResult := map[string]interface{}{
		"operation": "cancel",
		"app_name":  task.AppName,
		"user":      task.User,
		"uid":       appName,
		"cfgType":   cfgType,
		"url":       urlStr,
		"response":  response,
		"opID":      task.OpID, // Include opID in result
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App cancel completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
