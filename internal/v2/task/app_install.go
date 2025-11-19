package task

import (
	"encoding/json"
	"fmt"
	"io"
	"log"

	"net/http"
	"os"
	"strings"

	"market/internal/v2/settings"
)

// InstallOptions represents the options for app installation
type InstallOptions struct {
	App          string      `json:"appName,omitempty"`
	Dev          bool        `json:"devMode,omitempty"`
	RepoUrl      string      `json:"repoUrl,omitempty"`
	CfgUrl       string      `json:"cfgUrl,omitempty"`
	Version      string      `json:"version,omitempty"`
	Source       string      `json:"source,omitempty"`
	User         string      `json:"x_market_user,omitempty"`
	MarketSource string      `json:"x_market_source,omitempty"`
	Images       []Image     `json:"images,omitempty"`
	Envs         []AppEnvVar `json:"envs"`
}

type AppEnvVar struct {
	EnvName string `json:"envName" yaml:"envName" validate:"required"`
	Value   string `json:"value,omitempty" yaml:"value,omitempty"`
}

type Image struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// AppInstall installs an application using the app service
func (tm *TaskModule) AppInstall(task *Task) (string, error) {
	appName := task.AppName
	user := task.User

	log.Printf("Starting app installation: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	// Check if there's already a running or pending install task for the same app
	tm.mu.RLock()

	// Check running tasks
	for _, runningTask := range tm.runningTasks {
		if runningTask.Type == InstallApp && runningTask.AppName == appName && runningTask.ID != task.ID {
			tm.mu.RUnlock()
			log.Printf("Installation failed: another install task is already running for app: %s, existing task ID: %s", appName, runningTask.ID)
			errorResult := map[string]interface{}{
				"operation":        "install",
				"app_name":         appName,
				"user":             user,
				"error":            "Another installation task is already running for this app",
				"status":           "failed",
				"existing_task_id": runningTask.ID,
			}
			errorJSON, _ := json.Marshal(errorResult)
			return string(errorJSON), fmt.Errorf("another installation task is already running for app: %s", appName)
		}
	}

	tm.mu.RUnlock()

	token, ok := task.Metadata["token"].(string)
	if !ok {
		log.Printf("Missing token in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing token in task metadata")
	}

	// Get app source from metadata
	appSource, ok := task.Metadata["source"].(string)
	if !ok {
		log.Printf("Missing source in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing source in task metadata")
	}

	// Get cfgType from metadata
	cfgType, ok := task.Metadata["cfgType"].(string)
	if !ok {
		log.Printf("Missing cfgType in task metadata for task: %s, using default 'app'", task.ID)
		cfgType = "app" // Default to app type
	}

	// Convert app source to API source parameter
	// If app source is "local", use "custom" for API
	// Otherwise, use "market" for API
	var apiSource string
	if appSource == "upload" {
		apiSource = "custom"
	} else {
		apiSource = "market"
	}

	log.Printf("App source: %s, API source: %s, cfgType: %s for task: %s", appSource, apiSource, cfgType, task.ID)

	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	// Choose API endpoint based on cfgType
	var urlStr string
	// if cfgType == "middleware" {
	// 	// Use middleware API for middleware type
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/%s/install", appServiceHost, appServicePort, appName)
	// 	log.Printf("Using middleware API for installation: %s", urlStr)
	// } else if cfgType == "recommend" {
	// 	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/recommends/%s/install", appServiceHost, appServicePort, appName)
	// 	log.Printf("Using middleware API for installation: %s", urlStr)
	// } else {
	// 	// Use regular app API for other types
	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/install", appServiceHost, appServicePort, appName)
	log.Printf("Using regular app API for installation: %s", urlStr)
	// }

	log.Printf("App service URL: %s for task: %s", urlStr, task.ID)

	// Get envs from metadata
	var envs []AppEnvVar
	if envsData, ok := task.Metadata["envs"]; ok && envsData != nil {
		envs, _ = envsData.([]AppEnvVar)
		log.Printf("Retrieved %d environment variables for task: %s", len(envs), task.ID)
	}

	// Get VC from purchase receipt and inject into environment variables
	var settingsManager *settings.SettingsManager
	if tm.mu.TryRLock() {
		settingsManager = tm.settingsManager
		tm.mu.RUnlock()
	} else {
		log.Printf("Failed to acquire read lock for settingsManager, skipping VC injection for task: %s", task.ID)
	}

	if settingsManager != nil {
		vc := getVCForInstall(settingsManager, user, appName, task.Metadata)
		if vc != "" {
			// Check if VERIFIABLE_CREDENTIAL already exists in envs
			vcExists := false
			for i := range envs {
				if envs[i].EnvName == "VERIFIABLE_CREDENTIAL" {
					envs[i].Value = vc
					vcExists = true
					log.Printf("Updated VERIFIABLE_CREDENTIAL in envs for task: %s", task.ID)
					break
				}
			}
			// If not exists, add it
			if !vcExists {
				envs = append(envs, AppEnvVar{
					EnvName: "VERIFIABLE_CREDENTIAL",
					Value:   vc,
				})
				log.Printf("Added VERIFIABLE_CREDENTIAL to envs for task: %s", task.ID)
			}
		} else {
			log.Printf("VC not found for app installation, skipping VERIFIABLE_CREDENTIAL injection for task: %s", task.ID)
		}
	} else {
		log.Printf("Settings manager not available, skipping VC injection for task: %s", task.ID)
	}

	// Get images from metadata
	var images []Image
	if imagesData, ok := task.Metadata["images"]; ok && imagesData != nil {
		if imagesSlice, ok := imagesData.([]Image); ok {
			images = imagesSlice
		}
	}

	installInfo := &InstallOptions{
		RepoUrl:      getRepoUrl(),
		Source:       apiSource, // Use converted API source
		User:         user,
		MarketSource: appSource,
		Images:       images,
		Envs:         envs,
	}
	ms, err := json.Marshal(installInfo)
	if err != nil {
		log.Printf("Failed to marshal install info for task %s: %v", task.ID, err)
		return "", err
	}
	log.Printf("Install request prepared: url=%s, installInfo=%s, task_id=%s", urlStr, string(ms), task.ID)

	headers := map[string]string{
		"X-Authorization": token,
		"X-Bfl-User":      task.User,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": appSource,
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app installation: task=%s", task.ID)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
	if err != nil {
		log.Printf("HTTP request failed for app installation: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation":  "install",
			"app_name":   appName,
			"user":       user,
			"app_source": appSource, // Log original app source
			"api_source": apiSource, // Log converted API source
			"cfgType":    cfgType,   // Log cfgType
			"url":        urlStr,
			"error":      err.Error(),
			"status":     "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app installation: task=%s, response_length=%d", task.ID, len(response))

	// Parse response to extract opID if installation is successful
	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(response), &responseData); err != nil {
		log.Printf("Failed to parse response JSON for task %s: %v", task.ID, err)
		// Create error result for JSON parsing failure
		errorResult := map[string]interface{}{
			"operation":    "install",
			"app_name":     appName,
			"user":         user,
			"app_source":   appSource,
			"api_source":   apiSource,
			"cfgType":      cfgType,
			"url":          urlStr,
			"raw_response": response,
			"error":        fmt.Sprintf("Failed to parse response JSON: %v", err),
			"status":       "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), fmt.Errorf("failed to parse response JSON: %v", err)
	}

	// Check if installation was successful by checking code field
	if code, ok := responseData["code"].(float64); ok && code == 200 {
		if data, ok := responseData["data"].(map[string]interface{}); ok {
			if opID, ok := data["opID"].(string); ok && opID != "" {
				task.OpID = opID
				log.Printf("Successfully extracted opID: %s for task: %s", opID, task.ID)
			} else {
				log.Printf("opID not found in response data for task: %s", task.ID)
				// Return backend response with additional context
				errorResult := map[string]interface{}{
					"operation":        "install",
					"app_name":         appName,
					"user":             user,
					"app_source":       appSource,
					"api_source":       apiSource,
					"cfgType":          cfgType,
					"url":              urlStr,
					"backend_response": responseData,
					"error":            "opID not found in response data",
					"status":           "failed",
				}
				errorJSON, _ := json.Marshal(errorResult)
				return string(errorJSON), fmt.Errorf("opID not found in response data")
			}
		} else {
			log.Printf("Data field not found or not a map in response for task: %s", task.ID)
			// Return backend response with additional context
			errorResult := map[string]interface{}{
				"operation":        "install",
				"app_name":         appName,
				"user":             user,
				"app_source":       appSource,
				"api_source":       apiSource,
				"cfgType":          cfgType,
				"url":              urlStr,
				"backend_response": responseData,
				"error":            "Data field not found or not a map in response",
				"status":           "failed",
			}
			errorJSON, _ := json.Marshal(errorResult)
			return string(errorJSON), fmt.Errorf("data field not found or not a map in response")
		}
	} else {
		log.Printf("Installation code is not 200 for task: %s, code: %v", task.ID, code)
		// Return backend response with additional context
		errorResult := map[string]interface{}{
			"operation":        "install",
			"app_name":         appName,
			"user":             user,
			"app_source":       appSource,
			"api_source":       apiSource,
			"cfgType":          cfgType,
			"url":              urlStr,
			"backend_response": responseData,
			"error":            fmt.Sprintf("Installation failed with code: %v", code),
			"status":           "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), fmt.Errorf("installation failed with code: %v", code)
	}

	// Return backend response with additional context on success
	successResult := map[string]interface{}{
		"operation":        "install",
		"app_name":         appName,
		"user":             user,
		"app_source":       appSource,
		"api_source":       apiSource,
		"cfgType":          cfgType,
		"url":              urlStr,
		"backend_response": responseData,
		"opID":             task.OpID,
		"status":           "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App installation completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}

// getRepoUrl returns the repository URL
func getRepoUrl() string {
	repoServiceHost := os.Getenv("REPO_URL_HOST")
	repoStoreServicePort := os.Getenv("REPO_URL_PORT")
	return fmt.Sprintf("http://%s:%s/", repoServiceHost, repoStoreServicePort)
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
