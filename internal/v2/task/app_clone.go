package task

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"market/internal/v2/settings"
)

// AppEntrance represents an entrance configuration for cloned app
type AppEntrance struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// CloneOptions represents the options for app clone installation
type CloneOptions struct {
	App          string        `json:"appName,omitempty"`
	Dev          bool          `json:"devMode,omitempty"`
	RepoUrl      string        `json:"repoUrl,omitempty"`
	CfgUrl       string        `json:"cfgUrl,omitempty"`
	Version      string        `json:"version,omitempty"`
	Source       string        `json:"source,omitempty"`
	User         string        `json:"x_market_user,omitempty"`
	MarketSource string        `json:"x_market_source,omitempty"`
	Images       []Image       `json:"images,omitempty"`
	Envs         []AppEnvVar   `json:"envs"`
	Entrances    []AppEntrance `json:"entrances,omitempty"`
	RawAppName   string        `json:"rawAppName,omitempty"`  // Raw app name for clone operations
	RequestHash  string        `json:"requestHash,omitempty"` // Hash of entire clone request (first 6 chars of SHA256 hash of source, app_name, title, envs, and entrances)
	Title        string        `json:"title,omitempty"`       // Title for clone operations (for display purposes)
}

// AppClone clones an application using the app service
func (tm *TaskModule) AppClone(task *Task) (string, error) {
	appName := task.AppName
	user := task.User

	log.Printf("Starting app clone: app=%s, user=%s, task_id=%s", appName, user, task.ID)

	// Check if there's already a running or pending clone task for the same app
	tm.mu.RLock()

	// Check running tasks
	for _, runningTask := range tm.runningTasks {
		if runningTask.Type == CloneApp && runningTask.AppName == appName && runningTask.ID != task.ID {
			tm.mu.RUnlock()
			log.Printf("Clone failed: another clone task is already running for app: %s, existing task ID: %s", appName, runningTask.ID)
			errorResult := map[string]interface{}{
				"operation":        "clone",
				"app_name":         appName,
				"user":             user,
				"error":            "Another clone task is already running for this app",
				"status":           "failed",
				"existing_task_id": runningTask.ID,
			}
			errorJSON, _ := json.Marshal(errorResult)
			return string(errorJSON), fmt.Errorf("another clone task is already running for app: %s", appName)
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

	// Get rawAppName and requestHash from metadata
	rawAppName, ok := task.Metadata["rawAppName"].(string)
	if !ok || rawAppName == "" {
		log.Printf("Missing rawAppName in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing rawAppName in task metadata")
	}

	requestHash, ok := task.Metadata["requestHash"].(string)
	if !ok || requestHash == "" {
		log.Printf("Missing requestHash in task metadata for task: %s", task.ID)
		return "", fmt.Errorf("missing requestHash in task metadata")
	}

	// Get title from metadata (optional, for display purposes)
	title, _ := task.Metadata["title"].(string)

	// Construct URL app name: rawAppName + requestHash (not rawAppName + Title)
	urlAppName := rawAppName + requestHash
	log.Printf("Clone operation: rawAppName=%s, requestHash=%s, title=%s, urlAppName=%s for task: %s", rawAppName, requestHash, title, urlAppName, task.ID)

	// Convert app source to API source parameter
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
	// Use rawAppName+requestHash for URL in clone operations
	urlStr = fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/install", appServiceHost, appServicePort, urlAppName)
	log.Printf("Using app API for clone installation: %s", urlStr)

	log.Printf("App service URL: %s for task: %s", urlStr, task.ID)

	// Get envs from metadata
	var envs []AppEnvVar
	if envsData, ok := task.Metadata["envs"]; ok && envsData != nil {
		envs, _ = envsData.([]AppEnvVar)
		log.Printf("Retrieved %d environment variables for task: %s", len(envs), task.ID)
	}

	// Get entrances from metadata
	var entrances []AppEntrance
	if entrancesData, ok := task.Metadata["entrances"]; ok && entrancesData != nil {
		if entrancesSlice, ok := entrancesData.([]AppEntrance); ok {
			entrances = entrancesSlice
		}
		log.Printf("Retrieved %d entrances for task: %s", len(entrances), task.ID)
	}

	// Get VC from purchase receipt using rawAppName and inject into environment variables
	var settingsManager *settings.SettingsManager
	if tm.mu.TryRLock() {
		settingsManager = tm.settingsManager
		tm.mu.RUnlock()
	} else {
		log.Printf("Failed to acquire read lock for settingsManager, skipping VC injection for task: %s", task.ID)
	}

	if settingsManager != nil {
		vc := getVCForClone(settingsManager, user, rawAppName, task.Metadata)
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
			log.Printf("VC not found for app clone, skipping VERIFIABLE_CREDENTIAL injection for task: %s", task.ID)
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

	// Create clone options with rawAppName, requestHash, and Title
	cloneInfo := &CloneOptions{
		RepoUrl:      getRepoUrl(),
		Source:       apiSource,
		User:         user,
		MarketSource: appSource,
		Images:       images,
		Envs:         envs,
		Entrances:    entrances,
		RawAppName:   rawAppName,  // Pass rawAppName
		RequestHash:  requestHash, // Pass requestHash
		Title:        title,       // Pass title (for display purposes)
	}

	ms, err := json.Marshal(cloneInfo)
	if err != nil {
		log.Printf("Failed to marshal clone info for task %s: %v", task.ID, err)
		return "", err
	}
	log.Printf("Clone request prepared: url=%s, cloneInfo=%s, task_id=%s", urlStr, string(ms), task.ID)

	headers := map[string]string{
		"X-Authorization": token,
		"X-Bfl-User":      task.User,
		"Content-Type":    "application/json",
		"X-Market-User":   user,
		"X-Market-Source": appSource,
	}

	// Send HTTP request and get response
	log.Printf("Sending HTTP request for app clone: task=%s", task.ID)
	response, err := sendHttpRequest(http.MethodPost, urlStr, headers, strings.NewReader(string(ms)))
	if err != nil {
		log.Printf("HTTP request failed for app clone: task=%s, error=%v", task.ID, err)
		// Create detailed error result
		errorResult := map[string]interface{}{
			"operation":   "clone",
			"app_name":    urlAppName,
			"user":        user,
			"app_source":  appSource,
			"api_source":  apiSource,
			"cfgType":     cfgType,
			"url":         urlStr,
			"rawAppName":  rawAppName,
			"requestHash": requestHash,
			"title":       title,
			"error":       err.Error(),
			"status":      "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), err
	}

	log.Printf("HTTP request completed successfully for app clone: task=%s, response_length=%d", task.ID, len(response))

	// Parse response to extract opID if clone is successful
	var responseData map[string]interface{}
	if err := json.Unmarshal([]byte(response), &responseData); err != nil {
		log.Printf("Failed to parse response JSON for task %s: %v", task.ID, err)
		// Create error result for JSON parsing failure
		errorResult := map[string]interface{}{
			"operation":    "clone",
			"app_name":     urlAppName,
			"user":         user,
			"app_source":   appSource,
			"api_source":   apiSource,
			"cfgType":      cfgType,
			"url":          urlStr,
			"rawAppName":   rawAppName,
			"requestHash":  requestHash,
			"title":        title,
			"raw_response": response,
			"error":        fmt.Sprintf("Failed to parse response JSON: %v", err),
			"status":       "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), fmt.Errorf("failed to parse response JSON: %v", err)
	}

	// Check if clone was successful by checking code field
	if code, ok := responseData["code"].(float64); ok && code == 200 {
		if data, ok := responseData["data"].(map[string]interface{}); ok {
			if opID, ok := data["opID"].(string); ok && opID != "" {
				task.OpID = opID
				log.Printf("Successfully extracted opID: %s for task: %s", opID, task.ID)
			} else {
				log.Printf("opID not found in response data for task: %s", task.ID)
				// Return backend response with additional context
				errorResult := map[string]interface{}{
					"operation":        "clone",
					"app_name":         urlAppName,
					"user":             user,
					"app_source":       appSource,
					"api_source":       apiSource,
					"cfgType":          cfgType,
					"url":              urlStr,
					"rawAppName":       rawAppName,
					"requestHash":      requestHash,
					"title":            title,
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
				"operation":        "clone",
				"app_name":         urlAppName,
				"user":             user,
				"app_source":       appSource,
				"api_source":       apiSource,
				"cfgType":          cfgType,
				"url":              urlStr,
				"rawAppName":       rawAppName,
				"requestHash":      requestHash,
				"backend_response": responseData,
				"error":            "Data field not found or not a map in response",
				"status":           "failed",
			}
			errorJSON, _ := json.Marshal(errorResult)
			return string(errorJSON), fmt.Errorf("data field not found or not a map in response")
		}
	} else {
		log.Printf("Clone code is not 200 for task: %s, code: %v", task.ID, code)
		// Return backend response with additional context
		errorResult := map[string]interface{}{
			"operation":        "clone",
			"app_name":         urlAppName,
			"user":             user,
			"app_source":       appSource,
			"api_source":       apiSource,
			"cfgType":          cfgType,
			"url":              urlStr,
			"rawAppName":       rawAppName,
			"requestHash":      requestHash,
			"backend_response": responseData,
			"error":            fmt.Sprintf("Clone failed with code: %v", code),
			"status":           "failed",
		}
		errorJSON, _ := json.Marshal(errorResult)
		return string(errorJSON), fmt.Errorf("clone failed with code: %v", code)
	}

	// Return backend response with additional context on success
	successResult := map[string]interface{}{
		"operation":        "clone",
		"app_name":         urlAppName,
		"user":             user,
		"app_source":       appSource,
		"api_source":       apiSource,
		"cfgType":          cfgType,
		"url":              urlStr,
		"rawAppName":       rawAppName,
		"requestHash":      requestHash,
		"title":            title,
		"backend_response": responseData,
		"opID":             task.OpID,
		"status":           "success",
	}
	successJSON, _ := json.Marshal(successResult)
	log.Printf("App clone completed successfully: task=%s, result_length=%d", task.ID, len(successJSON))
	return string(successJSON), nil
}
