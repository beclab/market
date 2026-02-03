package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/golang/glog"
)

// AppUninstall uninstalls an application using the app service.
func (tm *TaskModule) AppUninstall(task *Task) (string, error) {
	appName := task.AppName

	glog.Infof("Starting app uninstallation: app=%s, user=%s, task_id=%s", appName, task.User, task.ID)

	token, ok := task.Metadata["token"].(string)
	if !ok {
		glog.Errorf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	// Get cfgType from metadata
	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		glog.Warningf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app" // Default to app type
	}

	// Get all parameter from metadata
	all, ok := task.Metadata["all"].(bool)
	if !ok {
		glog.Warningf("Missing all parameter in task metadata for task: %s, using default false", task.ID)
		all = false // Default to false
	}

	deleteData, ok := task.Metadata["deleteData"].(bool)
	if !ok {
		glog.Warningf("Missing deleteData parameter in task metadata for task: %s, using default false", task.ID)
		deleteData = false
	}

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// Choose API endpoint based on cfgType
	var urlStr string
	// if cfgType == "middleware" {
	// 	// Use middleware API for middleware type
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/%s/uninstall", appServiceHost, appServicePort, appName)
	// 	glog.Infof("Using middleware API for uninstall: %s", urlStr)
	// } else if cfgType == "recommend" {
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/recommends/%s/uninstall", appServiceHost, appServicePort, appName)
	// 	glog.Infof("Using middleware API for installation: %s", urlStr)
	// } else {
	// 	// Use regular app API for other types
	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/uninstall", appServiceHost, appServicePort, appName)
	glog.Infof("Using regular app API for uninstall: %s", urlStr)
	// }

	glog.Infof("App service URL: %s for task: %s, cfgType: %s", urlStr, task.ID, cfgType)

	headers := map[string]string{
		"X-Authorization": token,
		"X-Bfl-User":      task.User,
		// Content-Type is not strictly necessary for a request with no body,
		// but we include it for consistency.
		"Content-Type": "application/json",
	}

	// Send HTTP request and get response
	glog.Infof("Sending HTTP request for app uninstallation: task=%s, all=%v", task.ID, all)

	// Create request body with all parameter
	requestBody := map[string]interface{}{
		"all":        all,
		"deleteData": deleteData,
	}
	requestBodyBytes, _ := json.Marshal(requestBody)

	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, bytes.NewReader(requestBodyBytes))
	if err != nil {
		glog.Errorf("HTTP request failed for app uninstallation: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation":  "uninstall",
			"app_name":   appName,
			"user":       task.User,
			"cfgType":    cfgType,
			"all":        all,
			"deleteData": deleteData,
			"url":        urlStr,
			"error":      err.Error(),
			"status":     "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	glog.Infof("HTTP request completed successfully for app uninstallation: task=%s, response_length=%d", task.ID, len(response))

	// Parse response to extract opID if uninstallation is successful
	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(response), &responseData); err != nil {
		glog.Errorf("Failed to parse response JSON for task %s: %v", task.ID, err)
	} else {
		// Check if uninstallation was successful by checking code field
		if code, ok := responseData["code"].(float64); ok && code == 200 {
			if data, ok := responseData["data"].(map[string]interface{}); ok {
				if opID, ok := data["opID"].(string); ok && opID != "" {
					task.OpID = opID
					glog.Infof("Successfully extracted opID: %s for task: %s", opID, task.ID)
				} else {
					glog.Infof("opID not found in response data for task: %s", task.ID)
				}
			} else {
				glog.Infof("Data field not found or not a map in response for task: %s", task.ID)
			}
		} else {
			glog.Infof("Uninstallation code is not 200 for task: %s, code: %v", task.ID, code)
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
	glog.Infof("App uninstallation completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
