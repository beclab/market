package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/golang/glog"
	"github.com/gorilla/mux"

	"market/internal/v2/task"
	"market/internal/v2/types"
	"market/internal/v2/utils"
)

// InstallAppRequest represents the request body for app installation
type InstallAppRequest struct {
	Source  string           `json:"source"`
	AppName string           `json:"app_name"`
	Version string           `json:"version"`
	Sync    bool             `json:"sync"` // Whether this is a synchronous request
	Envs    []task.AppEnvVar `json:"envs,omitempty"`
}

// CloneAppRequest represents the request body for app clone
type CloneAppRequest struct {
	Source    string             `json:"source"`
	AppName   string             `json:"app_name"`
	Title     string             `json:"title"` // Title for cloned app (used for display purposes)
	Sync      bool               `json:"sync"`  // Whether this is a synchronous request
	Envs      []task.AppEnvVar   `json:"envs,omitempty"`
	Entrances []task.AppEntrance `json:"entrances,omitempty"`
}

// CancelInstallRequest represents the request body for cancel installation
type CancelInstallRequest struct {
	Sync bool `json:"sync"` // Whether this is a synchronous request
}

// calculateCloneRequestHash calculates SHA256 hash of the entire clone request (excluding Sync field) and returns first 6 characters
// The hash is based on: Source, AppName, Title, Envs, and Entrances
func calculateCloneRequestHash(request CloneAppRequest) string {
	// Create a struct for hashing that excludes Sync field
	type CloneRequestForHash struct {
		Source    string             `json:"source"`
		AppName   string             `json:"app_name"`
		Title     string             `json:"title"`
		Envs      []task.AppEnvVar   `json:"envs,omitempty"`
		Entrances []task.AppEntrance `json:"entrances,omitempty"`
	}

	hashData := CloneRequestForHash{
		Source:    request.Source,
		AppName:   request.AppName,
		Title:     request.Title,
		Envs:      request.Envs,
		Entrances: request.Entrances,
	}

	// Sort envs by name for consistent hashing
	if len(hashData.Envs) > 0 {
		sortedEnvs := make([]task.AppEnvVar, len(hashData.Envs))
		copy(sortedEnvs, hashData.Envs)
		sort.Slice(sortedEnvs, func(i, j int) bool {
			return sortedEnvs[i].EnvName < sortedEnvs[j].EnvName
		})
		hashData.Envs = sortedEnvs
	}

	// Sort entrances by name for consistent hashing
	if len(hashData.Entrances) > 0 {
		sortedEntrances := make([]task.AppEntrance, len(hashData.Entrances))
		copy(sortedEntrances, hashData.Entrances)
		sort.Slice(sortedEntrances, func(i, j int) bool {
			return sortedEntrances[i].Name < sortedEntrances[j].Name
		})
		hashData.Entrances = sortedEntrances
	}

	// Marshal to JSON for hashing
	requestJSON, err := json.Marshal(hashData)
	if err != nil {
		glog.Errorf("Failed to marshal clone request for hash calculation: %v", err)
		// Fallback: hash the error
		hash := sha256.Sum256([]byte(fmt.Sprintf("error:%v", err)))
		return hex.EncodeToString(hash[:])[:6]
	}

	// Calculate SHA256 hash
	hash := sha256.Sum256(requestJSON)
	hashStr := hex.EncodeToString(hash[:])

	// Return first 6 characters
	if len(hashStr) >= 6 {
		return hashStr[:6]
	}
	return hashStr
}

// extractInstallProductMetadata returns productID & developerName to help VC injection
func extractInstallProductMetadata(appInfo *types.AppInfo) (string, string) {
	if appInfo == nil {
		return "", ""
	}

	var productID string
	var developerName string

	if appInfo.Price != nil {
		developerName = strings.TrimSpace(appInfo.Price.Developer)

		if appInfo.Price.Paid != nil {
			if pid := strings.TrimSpace(appInfo.Price.Paid.ProductID); pid != "" {
				productID = pid
			} else if len(appInfo.Price.Paid.Price) > 0 {
				if appInfo.AppEntry != nil && appInfo.AppEntry.ID != "" {
					productID = appInfo.AppEntry.ID
				}
			}
		}

		if productID == "" {
			for _, product := range appInfo.Price.Products {
				if pid := strings.TrimSpace(product.ProductID); pid != "" {
					productID = pid
					break
				}
			}
		}
	}

	if productID == "" && appInfo.AppEntry != nil && appInfo.AppEntry.ID != "" {
		productID = appInfo.AppEntry.ID
	}

	return productID, developerName
}

// 6. Install application (single)
func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	glog.V(2).Infof("POST /api/v2/apps/%s/install - Installing app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for install request: %s", userID)

	// Step 2: Parse request body
	var request InstallAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		glog.Errorf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" || request.Version == "" {
		glog.V(3).Infof("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source, app_name, and version are required", nil)
		return
	}

	// Step 4: Check if cache manager is available
	if s.cacheManager == nil {
		glog.V(3).Infof("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 5: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		glog.V(3).Infof("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 6: Get source data
	sourceData := userData.Sources[request.Source]
	if sourceData == nil {
		glog.V(3).Infof("Source data not found: %s for user: %s", request.Source, userID)
		s.sendResponse(w, http.StatusNotFound, false, "Source data not found", nil)
		return
	}

	// Step 7: Find the app in AppInfoLatest
	var targetApp *types.AppInfoLatestData
	for _, appInfoData := range sourceData.AppInfoLatest {
		if appInfoData == nil || appInfoData.RawData == nil {
			continue
		}

		// Check if app matches the requested name and version
		if appInfoData.RawData.Name == request.AppName && appInfoData.RawData.Version == request.Version {
			targetApp = appInfoData
			break
		}
	}

	if targetApp == nil {
		glog.V(3).Infof("App not found: %s version %s in source: %s", request.AppName, request.Version, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	if targetApp.AppInfo == nil {
		glog.V(3).Infof("installApp: targetApp.AppInfo is nil for app=%s source=%s", request.AppName, request.Source)
	} else if targetApp.AppInfo.Price == nil {
		glog.V(3).Infof("installApp: targetApp.AppInfo.Price is nil for app=%s source=%s", request.AppName, request.Source)
	} else {
		glog.V(2).Infof("installApp: targetApp.AppInfo.Price detected for app=%s source=%s", request.AppName, request.Source)
	}

	// Step 8: Verify chart package exists
	chartFilename := fmt.Sprintf("%s-%s.tgz", request.AppName, request.Version)
	chartPath := filepath.Join(targetApp.RenderedPackage, chartFilename)

	// if _, err := os.Stat(chartPath); err != nil {
	// 	glog.Errorf("Chart package not found at path: %s", chartPath)
	// 	s.sendResponse(w, http.StatusNotFound, false, "Chart package not found", nil)
	// 	return
	// }

	// Step 9: Get app cfgType from cache
	var cfgType string
	if targetApp != nil && targetApp.RawData != nil {
		cfgType = targetApp.RawData.CfgType
		glog.V(2).Infof("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	} else {
		glog.V(3).Infof("Warning: Could not retrieve cfgType for app: %s, using default", request.AppName)
		cfgType = "app" // Default to app type
	}

	images := []task.Image{}
	if targetApp.AppInfo != nil && targetApp.AppInfo.ImageAnalysis != nil {
		for _, image := range targetApp.AppInfo.ImageAnalysis.Images {
			images = append(images, task.Image{
				Name: image.Name,
				Size: image.TotalSize,
			})
		}
	}

	productID, developerName := extractInstallProductMetadata(targetApp.AppInfo)
	glog.V(2).Infof("installApp: extracted product metadata app=%s source=%s productID=%s developer=%s", request.AppName, request.Source, productID, developerName)

	realAppID := request.AppName
	if targetApp.AppInfo != nil && targetApp.AppInfo.AppEntry != nil && targetApp.AppInfo.AppEntry.ID != "" {
		realAppID = targetApp.AppInfo.AppEntry.ID
	} else if targetApp.RawData != nil && targetApp.RawData.AppID != "" {
		realAppID = targetApp.RawData.AppID
	}
	glog.V(2).Infof("installApp: resolved realAppID=%s for app=%s source=%s", realAppID, request.AppName, request.Source)

	// Step 10: Create installation task
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"source":     request.Source,
		"app_name":   request.AppName,
		"version":    request.Version,
		"chart_path": chartPath,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType, // Add cfgType to metadata
		"images":     images,
		"envs":       request.Envs,
	}
	if productID != "" {
		taskMetadata["productID"] = productID
		glog.V(3).Infof("installApp: added productID=%s to metadata for app=%s source=%s", productID, request.AppName, request.Source)
	}
	if developerName != "" {
		taskMetadata["developerName"] = developerName
		glog.V(3).Infof("installApp: added developerName=%s to metadata for app=%s source=%s", developerName, request.AppName, request.Source)
	}
	if realAppID != "" {
		taskMetadata["realAppID"] = realAppID
		glog.V(3).Infof("installApp: added realAppID=%s to metadata for app=%s source=%s", realAppID, request.AppName, request.Source)
	}

	// Handle synchronous requests with proper blocking
	if request.Sync {
		// Create channel to wait for task completion
		done := make(chan struct{})
		var taskResult string
		var taskError error

		// Create callback function that will be called when task completes
		callback := func(result string, err error) {
			taskResult = result
			taskError = err
			close(done)
		}

		// Start the task
		task, err := s.taskModule.AddTask(task.InstallApp, request.AppName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create installation task for app: %s, error: %v", request.AppName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create installation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous installation task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

		// Wait for task completion
		<-done

		// Parse taskResult to avoid nested JSON strings
		var resultData map[string]interface{}
		if taskResult != "" {
			if err := json.Unmarshal([]byte(taskResult), &resultData); err != nil {
				// If parsing fails, return raw result
				glog.Errorf("Failed to parse task result as JSON: %v", err)
				resultData = map[string]interface{}{
					"raw_result": taskResult,
				}
			}
		}

		// Send response based on task result
		if taskError != nil {
			glog.Errorf("Synchronous installation failed for app: %s, error: %v", request.AppName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Installation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous installation completed successfully for app: %s", request.AppName)
			s.sendResponse(w, http.StatusOK, true, "App installation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result string, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous installation failed for app: %s, error: %v", request.AppName, err)
		} else {
			glog.V(2).Infof("Asynchronous installation completed successfully for app: %s", request.AppName)
		}
	}

	task, err := s.taskModule.AddTask(task.InstallApp, request.AppName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create installation task for app: %s, error: %v", request.AppName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create installation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous installation task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App installation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 6.1. Clone application (single)
func (s *Server) cloneApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	glog.V(2).Infof("POST /api/v2/apps/%s/clone - Cloning app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for clone request: %s", userID)

	// Step 2: Parse request body
	var request CloneAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		glog.Errorf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" {
		glog.V(3).Info("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source and app_name are required", nil)
		return
	}

	// Step 4: Check if cache manager is available
	if s.cacheManager == nil {
		glog.V(3).Info("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 5: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		glog.V(3).Info("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 6: Get source data
	sourceData := userData.Sources[request.Source]
	if sourceData == nil {
		glog.V(3).Info("Source data not found: %s for user: %s", request.Source, userID)
		s.sendResponse(w, http.StatusNotFound, false, "Source data not found", nil)
		return
	}

	// Step 7: Find the app in AppInfoLatest (by name only, no version check)
	var targetApp *types.AppInfoLatestData
	for _, appInfoData := range sourceData.AppInfoLatest {
		if appInfoData == nil || appInfoData.RawData == nil {
			continue
		}

		// Check if app matches the requested name (use latest version found)
		if appInfoData.RawData.Name == request.AppName {
			targetApp = appInfoData
			break
		}
	}

	if targetApp == nil {
		glog.V(3).Infof("App not found: %s in source: %s", request.AppName, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	// Step 8: Get rawAppName from app info (应用信息中的名字)
	rawAppName := targetApp.RawData.Name
	if rawAppName == "" {
		glog.V(3).Infof("Raw app name not found for app: %s", request.AppName)
		s.sendResponse(w, http.StatusBadRequest, false, "Raw app name not found", nil)
		return
	}

	// Step 9: Calculate hash suffix from entire clone request and construct new app name: rawAppName + hash
	requestHash := calculateCloneRequestHash(request)
	newAppName := rawAppName + requestHash

	// For clone app, get version from installed original app's state
	// Clone operation requires the original app to be installed
	var appVersion string
	if s.cacheManager == nil {
		glog.V(3).Infof("Cache manager not available, cannot verify installed original app")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	stateVersion, found := s.cacheManager.GetAppVersionFromState(userID, request.Source, rawAppName)
	if !found || stateVersion == "" {
		glog.V(3).Infof("Original app not found in installed state: %s in source: %s for user: %s", rawAppName, request.Source, userID)
		s.sendResponse(w, http.StatusNotFound, false, fmt.Sprintf("Original app %s is not installed in source %s", rawAppName, request.Source), nil)
		return
	}

	appVersion = stateVersion
	glog.V(2).Infof("Cloning app: rawAppName=%s, requestHash=%s, title=%s, newAppName=%s, version=%s (from installed original app)", rawAppName, requestHash, request.Title, newAppName, appVersion)

	// Step 10: Verify chart package exists (use version from targetApp)
	chartFilename := fmt.Sprintf("%s-%s.tgz", request.AppName, appVersion)
	chartPath := filepath.Join(targetApp.RenderedPackage, chartFilename)

	// Step 11: Get app cfgType from cache
	var cfgType string
	if targetApp != nil && targetApp.RawData != nil {
		cfgType = targetApp.RawData.CfgType
		glog.V(2).Infof("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	} else {
		glog.V(3).Infof("Warning: Could not retrieve cfgType for app: %s, using default", request.AppName)
		cfgType = "app" // Default to app type
	}

	images := []task.Image{}
	if targetApp.AppInfo != nil && targetApp.AppInfo.ImageAnalysis != nil {
		for _, image := range targetApp.AppInfo.ImageAnalysis.Images {
			images = append(images, task.Image{
				Name: image.Name,
				Size: image.TotalSize,
			})
		}
	}

	// Step 12: Create clone installation task
	taskMetadata := map[string]interface{}{
		"user_id":     userID,
		"source":      request.Source,
		"version":     appVersion, // Use version from targetApp
		"chart_path":  chartPath,
		"token":       utils.GetTokenFromRequest(restfulReq),
		"cfgType":     cfgType,
		"images":      images,
		"envs":        request.Envs,
		"entrances":   request.Entrances,
		"rawAppName":  rawAppName,    // Pass rawAppName in metadata
		"requestHash": requestHash,   // Pass requestHash in metadata
		"title":       request.Title, // Pass title in metadata (for display purposes)
	}

	// Handle synchronous requests with proper blocking
	if request.Sync {
		// Create channel to wait for task completion
		done := make(chan struct{})
		var taskResult string
		var taskError error

		// Create callback function that will be called when task completes
		callback := func(result string, err error) {
			taskResult = result
			taskError = err
			close(done)
		}

		// Start the task with CloneApp type, using newAppName (rawAppName+requestHash) as appName
		task, err := s.taskModule.AddTask(task.CloneApp, newAppName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create clone installation task for app: %s, error: %v", newAppName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create clone installation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous clone installation task: ID=%s for app: %s (rawAppName=%s, requestHash=%s, title=%s)", task.ID, newAppName, rawAppName, requestHash, request.Title)

		// Wait for task completion
		<-done

		// Parse taskResult to avoid nested JSON strings
		var resultData map[string]interface{}
		if taskResult != "" {
			if err := json.Unmarshal([]byte(taskResult), &resultData); err != nil {
				// If parsing fails, return raw result
				glog.Errorf("Failed to parse task result as JSON: %v", err)
				resultData = map[string]interface{}{
					"raw_result": taskResult,
				}
			}
		}

		// Send response based on task result
		if taskError != nil {
			glog.Errorf("Synchronous clone installation failed for app: %s, error: %v", newAppName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Clone installation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous clone installation completed successfully for app: %s", newAppName)
			s.sendResponse(w, http.StatusOK, true, "App clone installation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result string, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous clone installation failed for app: %s, error: %v", newAppName, err)
		} else {
			glog.V(2).Infof("Asynchronous clone installation completed successfully for app: %s", newAppName)
		}
	}

	task, err := s.taskModule.AddTask(task.CloneApp, newAppName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create clone installation task for app: %s, error: %v", newAppName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create clone installation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous clone installation task: ID=%s for app: %s (rawAppName=%s, requestHash=%s, title=%s)", task.ID, newAppName, rawAppName, requestHash, request.Title)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App clone installation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 7. Cancel installation (single)
func (s *Server) cancelInstall(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appName := vars["id"]
	glog.V(2).Infof("DELETE /api/v2/apps/%s/install - Canceling app installation", appName)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for cancel request: %s", userID)

	// Step 2: Parse request body for sync parameter
	var request CancelInstallRequest
	var sync bool
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err == nil {
			sync = request.Sync
		}
	}

	// Step 3: Check if cache manager is available
	if s.cacheManager == nil {
		glog.V(3).Infof("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 4: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		glog.V(3).Infof("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 5: Get app cfgType from cache
	var cfgType string
	// Try to find the app in user data to get cfgType
	if userData != nil {
		// Search through all sources to find the app
		for sourceID, sourceData := range userData.Sources {
			if sourceData != nil {
				for _, appInfoData := range sourceData.AppInfoLatest {
					if appInfoData != nil && appInfoData.RawData != nil && appInfoData.RawData.Name == appName {
						cfgType = appInfoData.RawData.CfgType
						glog.V(3).Infof("Retrieved cfgType: %s for app: %s from source: %s", cfgType, appName, sourceID)
						break
					}
				}
				if cfgType != "" {
					break
				}
			}
		}
	}

	if cfgType == "" {
		glog.V(3).Infof("Warning: Could not retrieve cfgType for app: %s, using default 'app'", appName)
		cfgType = "app" // Default to app type
	}

	// Step 6: Create cancel installation task
	taskMetadata := map[string]interface{}{
		"user_id":  userID,
		"app_name": appName,
		"token":    utils.GetTokenFromRequest(restfulReq),
		"cfgType":  cfgType, // Use retrieved cfgType
	}

	// Handle synchronous requests with proper blocking
	if sync {
		// Create channel to wait for task completion
		done := make(chan struct{})
		var taskResult string
		var taskError error

		// Create callback function that will be called when task completes
		callback := func(result string, err error) {
			taskResult = result
			taskError = err
			close(done)
		}

		// Start the task
		task, err := s.taskModule.AddTask(task.CancelAppInstall, appName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create cancel installation task for app: %s, error: %v", appName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create cancel installation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous cancel installation task: ID=%s for app: %s", task.ID, appName)

		// Wait for task completion
		<-done

		// Parse taskResult to avoid nested JSON strings
		var resultData map[string]interface{}
		if taskResult != "" {
			if err := json.Unmarshal([]byte(taskResult), &resultData); err != nil {
				// If parsing fails, return raw result
				glog.Errorf("Failed to parse task result as JSON: %v", err)
				resultData = map[string]interface{}{
					"raw_result": taskResult,
				}
			}
		}

		// Send response based on task result
		if taskError != nil {
			glog.Errorf("Synchronous cancel installation failed for app: %s, error: %v", appName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Cancel installation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous cancel installation completed successfully for app: %s", appName)
			s.sendResponse(w, http.StatusOK, true, "App installation cancellation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result string, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous cancel installation failed for app: %s, error: %v", appName, err)
		} else {
			glog.V(3).Infof("Asynchronous cancel installation completed successfully for app: %s", appName)
		}
	}

	task, err := s.taskModule.AddTask(task.CancelAppInstall, appName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create cancel installation task for app: %s, error: %v", appName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create cancel installation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous cancel installation task: ID=%s for app: %s", task.ID, appName)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App installation cancellation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 8. Uninstall application (single)
func (s *Server) uninstallApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appName := vars["id"]
	glog.V(2).Infof("DELETE /api/v2/apps/%s - Uninstalling app", appName)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for uninstall request: %s", userID)

	// Step 2: Parse request body for sync parameter and all parameter
	var requestBody map[string]interface{}
	var sync bool
	var all bool
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err == nil {
			if syncVal, ok := requestBody["sync"].(bool); ok {
				sync = syncVal
			}
			if allVal, ok := requestBody["all"].(bool); ok {
				all = allVal
			}
		}
	}

	// Step 3: Check if cache manager is available
	if s.cacheManager == nil {
		glog.V(3).Infof("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 4: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		glog.V(3).Infof("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 5: Get app cfgType from cache
	var cfgType string
	// Try to find the app in user data to get cfgType
	if userData != nil {
		// Search through all sources to find the app
		for sourceID, sourceData := range userData.Sources {
			if sourceData != nil {
				for _, appInfoData := range sourceData.AppInfoLatest {
					if appInfoData != nil && appInfoData.RawData != nil && appInfoData.RawData.Name == appName {
						cfgType = appInfoData.RawData.CfgType
						glog.V(3).Infof("Retrieved cfgType: %s for app: %s from source: %s", cfgType, appName, sourceID)
						break
					}
				}
				if cfgType != "" {
					break
				}
			}
		}
	}

	if cfgType == "" {
		glog.V(3).Infof("Warning: Could not retrieve cfgType for app: %s, using default 'app'", appName)
		cfgType = "app" // Default to app type
	}

	// Step 6: Create uninstallation task
	taskMetadata := map[string]interface{}{
		"user_id":  userID,
		"app_name": appName,
		"token":    utils.GetTokenFromRequest(restfulReq),
		"cfgType":  cfgType, // Use retrieved cfgType
		"all":      all,     // Add all parameter to metadata
	}

	// Handle synchronous requests with proper blocking
	if sync {
		// Create channel to wait for task completion
		done := make(chan struct{})
		var taskResult string
		var taskError error

		// Create callback function that will be called when task completes
		callback := func(result string, err error) {
			taskResult = result
			taskError = err
			close(done)
		}

		// Start the task
		task, err := s.taskModule.AddTask(task.UninstallApp, appName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create uninstallation task for app: %s, error: %v", appName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create uninstallation task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous uninstallation task: ID=%s for app: %s", task.ID, appName)

		// Wait for task completion
		<-done

		// Parse taskResult to avoid nested JSON strings
		var resultData map[string]interface{}
		if taskResult != "" {
			if err := json.Unmarshal([]byte(taskResult), &resultData); err != nil {
				// If parsing fails, return raw result
				glog.Errorf("Failed to parse task result as JSON: %v", err)
				resultData = map[string]interface{}{
					"raw_result": taskResult,
				}
			}
		}

		// Send response based on task result
		if taskError != nil {
			glog.Errorf("Synchronous uninstallation failed for app: %s, error: %v", appName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Uninstallation failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous uninstallation completed successfully for app: %s", appName)
			s.sendResponse(w, http.StatusOK, true, "App uninstallation completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result string, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous uninstallation failed for app: %s, error: %v", appName, err)
		} else {
			glog.V(2).Infof("Asynchronous uninstallation completed successfully for app: %s", appName)
		}
	}

	task, err := s.taskModule.AddTask(task.UninstallApp, appName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create uninstallation task for app: %s, error: %v", appName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create uninstallation task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous uninstallation task: ID=%s for app: %s", task.ID, appName)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App uninstallation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 9. Upgrade application (single)
func (s *Server) upgradeApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	glog.V(2).Infof("PUT /api/v2/apps/%s/upgrade - Upgrading app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for upgrade request: %s", userID)

	// Step 2: Parse request body
	var request InstallAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		glog.Errorf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" || request.Version == "" {
		glog.V(3).Infof("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source, app_name, and version are required", nil)
		return
	}

	// Step 4: Check if cache manager is available
	if s.cacheManager == nil {
		glog.V(3).Infof("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 5: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		glog.V(3).Infof("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 6: Get source data
	sourceData := userData.Sources[request.Source]
	if sourceData == nil {
		glog.V(3).Infof("Source data not found: %s for user: %s", request.Source, userID)
		s.sendResponse(w, http.StatusNotFound, false, "Source data not found", nil)
		return
	}

	// Step 7: Find the app in AppInfoLatest
	var targetApp *types.AppInfoLatestData
	var rawAppName string
	for _, appInfoData := range sourceData.AppInfoLatest {
		if appInfoData == nil || appInfoData.RawData == nil {
			continue
		}

		// Check if app matches the requested name and version
		if appInfoData.RawData.Name == request.AppName && appInfoData.RawData.Version == request.Version {
			targetApp = appInfoData
			break
		}
	}

	// If not found in AppInfoLatest, check AppStateLatest for clone apps
	if targetApp == nil {
		glog.V(3).Infof("App not found in AppInfoLatest: %s version %s in source: %s, checking AppStateLatest", request.AppName, request.Version, request.Source)

		// Search in AppStateLatest for clone apps
		var foundStateApp *types.AppStateLatestData
		for _, appStateData := range sourceData.AppStateLatest {
			if appStateData == nil {
				continue
			}

			// Check if app name matches and has rawAppName (indicating it's a clone app)
			if appStateData.Status.Name == request.AppName && appStateData.Status.RawAppName != "" {
				foundStateApp = appStateData
				rawAppName = appStateData.Status.RawAppName
				glog.V(3).Infof("Found clone app in AppStateLatest: %s with rawAppName: %s", request.AppName, rawAppName)
				break
			}
		}

		// If found in AppStateLatest with rawAppName, search for the original app in AppInfoLatest
		if foundStateApp != nil && rawAppName != "" {
			glog.V(3).Infof("Searching for original app in AppInfoLatest: rawAppName=%s, version=%s", rawAppName, request.Version)

			// Search for the original app using rawAppName
			for _, appInfoData := range sourceData.AppInfoLatest {
				if appInfoData == nil || appInfoData.RawData == nil {
					continue
				}

				// Check if app matches the rawAppName and version
				if appInfoData.RawData.Name == rawAppName && appInfoData.RawData.Version == request.Version {
					targetApp = appInfoData
					glog.V(3).Infof("Found original app in AppInfoLatest: %s version %s for clone app: %s", rawAppName, request.Version, request.AppName)
					break
				}
			}

			// If still not found, return error
			if targetApp == nil {
				glog.V(3).Infof("Original app not found: %s version %s in source: %s for clone app: %s", rawAppName, request.Version, request.Source, request.AppName)
				s.sendResponse(w, http.StatusNotFound, false, fmt.Sprintf("Original app not found: %s version %s", rawAppName, request.Version), nil)
				return
			}
		} else {
			// Not found in AppStateLatest either
			glog.V(3).Infof("App not found: %s version %s in source: %s", request.AppName, request.Version, request.Source)
			s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
			return
		}
	}

	// Step 8: Verify chart package exists
	// Use targetApp.RawData.Name for chart filename (handles both direct and clone apps)
	chartAppName := targetApp.RawData.Name
	chartFilename := fmt.Sprintf("%s-%s.tgz", chartAppName, request.Version)
	chartPath := filepath.Join(targetApp.RenderedPackage, chartFilename)

	// if _, err := os.Stat(chartPath); err != nil {
	// 	glog.Errorf("Chart package not found at path: %s", chartPath)
	// 	s.sendResponse(w, http.StatusNotFound, false, "Chart package not found", nil)
	// 	return
	// }

	// Step 9: Get app cfgType from cache
	var cfgType string
	if targetApp != nil && targetApp.RawData != nil {
		cfgType = targetApp.RawData.CfgType
		glog.V(3).Infof("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	} else {
		glog.V(3).Infof("Warning: Could not retrieve cfgType for app: %s, using default", request.AppName)
		cfgType = "app" // Default to app type
	}

	images := []task.Image{}
	for _, image := range targetApp.AppInfo.ImageAnalysis.Images {
		images = append(images, task.Image{
			Name: image.Name,
			Size: image.TotalSize,
		})
	}

	// Step 10: Create upgrade task
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"source":     request.Source,
		"app_name":   request.AppName, // Use request.AppName (clone app name) for upgrade task
		"version":    request.Version,
		"chart_path": chartPath,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType, // Add cfgType to metadata
		"images":     images,
		"envs":       request.Envs,
	}

	// If this is a clone app, add rawAppName to metadata
	if rawAppName != "" {
		taskMetadata["rawAppName"] = rawAppName
		glog.V(3).Infof("Adding rawAppName to upgrade task metadata: %s for clone app: %s", rawAppName, request.AppName)
	}

	// Handle synchronous requests with proper blocking
	if request.Sync {
		// Create channel to wait for task completion
		done := make(chan struct{})
		var taskResult string
		var taskError error

		// Create callback function that will be called when task completes
		callback := func(result string, err error) {
			taskResult = result
			taskError = err
			close(done)
		}

		// Start the task
		task, err := s.taskModule.AddTask(task.UpgradeApp, request.AppName, userID, taskMetadata, callback)
		if err != nil {
			glog.Errorf("Failed to create upgrade task for app: %s, error: %v", request.AppName, err)
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create upgrade task", nil)
			return
		}

		glog.V(2).Infof("Created synchronous upgrade task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

		// Wait for task completion
		<-done

		// Parse taskResult to avoid nested JSON strings
		var resultData map[string]interface{}
		if taskResult != "" {
			if err := json.Unmarshal([]byte(taskResult), &resultData); err != nil {
				// If parsing fails, return raw result
				glog.Errorf("Failed to parse task result as JSON: %v", err)
				resultData = map[string]interface{}{
					"raw_result": taskResult,
				}
			}
		}

		// Send response based on task result
		if taskError != nil {
			glog.Errorf("Synchronous upgrade failed for app: %s, error: %v", request.AppName, taskError)
			s.sendResponse(w, http.StatusInternalServerError, false, fmt.Sprintf("Upgrade failed: %v", taskError), resultData)
		} else {
			glog.V(2).Infof("Synchronous upgrade completed successfully for app: %s", request.AppName)
			s.sendResponse(w, http.StatusOK, true, "App upgrade completed successfully", resultData)
		}
		return
	}

	// Handle asynchronous requests
	callback := func(result string, err error) {
		// For async requests, callback is just for logging
		if err != nil {
			glog.Errorf("Asynchronous upgrade failed for app: %s, error: %v", request.AppName, err)
		} else {
			glog.V(3).Infof("Asynchronous upgrade completed successfully for app: %s", request.AppName)
		}
	}

	task, err := s.taskModule.AddTask(task.UpgradeApp, request.AppName, userID, taskMetadata, callback)
	if err != nil {
		glog.Errorf("Failed to create upgrade task for app: %s, error: %v", request.AppName, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create upgrade task", nil)
		return
	}

	glog.V(2).Infof("Created asynchronous upgrade task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

	// Return immediately for asynchronous requests
	s.sendResponse(w, http.StatusOK, true, "App upgrade started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}
