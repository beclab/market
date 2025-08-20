package task

import (
	"bytes"
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

	// Get cfgType from metadata
	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		log.Printf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app" // Default to app type
	}

	// Get all parameter from metadata
	all, ok := task.Metadata["all"].(bool)
	if !ok {
		log.Printf("Missing all parameter in task metadata for task: %s, using default false", task.ID)
		all = false // Default to false
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// Choose API endpoint based on cfgType
	var urlStr string
	if cfgType == "middleware" {
		// Use middleware API for middleware type
		urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/%s/uninstall", appServiceHost, appServicePort, appName)
		log.Printf("Using middleware API for uninstall: %s", urlStr)
	} else if cfgType == "recommend" {
		urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/recommends/%s/uninstall", appServiceHost, appServicePort, appName)
		log.Printf("Using middleware API for installation: %s", urlStr)
	} else {
		// Use regular app API for other types
		urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/uninstall", appServiceHost, appServicePort, appName)
		log.Printf("Using regular app API for uninstall: %s", urlStr)
	}

	log.Printf("App service URL: %s for task: %s, cfgType: %s", urlStr, task.ID, cfgType)

	headers := map[string]string{
		"X-Authorization": token,
		// Content-Type is not strictly necessary for a request with no body,
		// but we include it for consistency.
		"Content-Type": "application/json",
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app uninstallation: task=%s, all=%v", task.ID, all)

	// Create request body with all parameter
	requestBody := map[string]interface{}{
		"all": all,
	}
	requestBodyBytes, _ := json.Marshal(requestBody)

	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, bytes.NewReader(requestBodyBytes))
	if err != nil {
		log.Printf("HTTP request failed for app uninstallation: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation": "uninstall",
			"app_name":  appName,
			"user":      task.User,
			"cfgType":   cfgType,
			"all":       all,
			"url":       urlStr,
			"error":     err.Error(),
			"status":    "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app uninstallation: task=%s, response_length=%d", task.ID, len(response))

	// Parse response to extract opID if uninstallation is successful
	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(response), &responseData); err != nil {
		log.Printf("Failed to parse response JSON for task %s: %v", task.ID, err)
	} else {
		// Check if uninstallation was successful by checking code field
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
			log.Printf("Uninstallation code is not 200 for task: %s, code: %v", task.ID, code)
		}
	}

	// Create success result
	successResult := map[string]interface{}{
		"operation": "uninstall",
		"app_name":  appName,
		"user":      task.User,
		"cfgType":   cfgType,
		"all":       all,
		"url":       urlStr,
		"response":  response,
		"opID":      task.OpID, // Include opID in result
		"status":    "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App uninstallation completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
