package api

import (
	"encoding/json"
	"fmt"
	"log"
	"market/internal/v2/task"
	"market/internal/v2/types"
	"market/internal/v2/utils"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

// InstallAppRequest represents the request body for app installation
type InstallAppRequest struct {
	Source  string `json:"source"`
	AppName string `json:"app_name"`
	Version string `json:"version"`
}

// 6. Install application (single)
func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	log.Printf("POST /api/v2/apps/%s/install - Installing app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for install request: %s", userID)

	// Step 2: Parse request body
	var request InstallAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Printf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" || request.Version == "" {
		log.Printf("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source, app_name, and version are required", nil)
		return
	}

	// Step 4: Check if cache manager is available
	if s.cacheManager == nil {
		log.Printf("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 5: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		log.Printf("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 6: Get source data
	sourceData := userData.Sources[request.Source]
	if sourceData == nil {
		log.Printf("Source data not found: %s for user: %s", request.Source, userID)
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
		log.Printf("App not found: %s version %s in source: %s", request.AppName, request.Version, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	// Step 8: Verify chart package exists
	chartFilename := fmt.Sprintf("%s-%s.tgz", request.AppName, request.Version)
	chartPath := filepath.Join(targetApp.RenderedPackage, chartFilename)

	if _, err := os.Stat(chartPath); err != nil {
		log.Printf("Chart package not found at path: %s", chartPath)
		s.sendResponse(w, http.StatusNotFound, false, "Chart package not found", nil)
		return
	}

	// Step 9: Get app cfgType from cache
	var cfgType string
	if targetApp != nil && targetApp.RawData != nil {
		cfgType = targetApp.RawData.CfgType
		log.Printf("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	} else {
		log.Printf("Warning: Could not retrieve cfgType for app: %s, using default", request.AppName)
		cfgType = "app" // Default to app type
	}

	// Step 10: Create installation task
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"source":     request.Source,
		"app_name":   request.AppName,
		"version":    request.Version,
		"chart_path": chartPath,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType, // Add cfgType to metadata
	}

	task := s.taskModule.AddTask(task.InstallApp, request.AppName, userID, taskMetadata)
	if task == nil {
		log.Printf("Failed to create installation task for app: %s", request.AppName)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create installation task", nil)
		return
	}

	log.Printf("Created installation task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

	s.sendResponse(w, http.StatusOK, true, "App installation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 7. Cancel installation (single)
func (s *Server) cancelInstall(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appName := vars["id"]
	log.Printf("DELETE /api/v2/apps/%s/install - Canceling app installation", appName)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for cancel request: %s", userID)

	// Step 2: Check if cache manager is available
	if s.cacheManager == nil {
		log.Printf("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 3: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		log.Printf("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 4: Get app cfgType from cache
	var cfgType string
	// Try to find the app in user data to get cfgType
	if userData != nil {
		// Search through all sources to find the app
		for sourceID, sourceData := range userData.Sources {
			if sourceData != nil {
				for _, appInfoData := range sourceData.AppInfoLatest {
					if appInfoData != nil && appInfoData.RawData != nil && appInfoData.RawData.Name == appName {
						cfgType = appInfoData.RawData.CfgType
						log.Printf("Retrieved cfgType: %s for app: %s from source: %s", cfgType, appName, sourceID)
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
		log.Printf("Warning: Could not retrieve cfgType for app: %s, using default 'app'", appName)
		cfgType = "app" // Default to app type
	}

	// Step 5: Create cancel installation task
	taskMetadata := map[string]interface{}{
		"user_id":  userID,
		"app_name": appName,
		"token":    utils.GetTokenFromRequest(restfulReq),
		"cfgType":  cfgType, // Use retrieved cfgType
	}

	task := s.taskModule.AddTask(task.CancelAppInstall, appName, userID, taskMetadata)
	if task == nil {
		log.Printf("Failed to create cancel installation task for app: %s", appName)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create cancel installation task", nil)
		return
	}

	log.Printf("Created cancel installation task: ID=%s for app: %s", task.ID, appName)

	s.sendResponse(w, http.StatusOK, true, "App installation cancellation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 8. Uninstall application (single)
func (s *Server) uninstallApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appName := vars["id"]
	log.Printf("DELETE /api/v2/apps/%s - Uninstalling app", appName)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for uninstall request: %s", userID)

	// Step 2: Check if cache manager is available
	if s.cacheManager == nil {
		log.Printf("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 3: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		log.Printf("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 4: Get app cfgType from cache
	var cfgType string
	// Try to find the app in user data to get cfgType
	if userData != nil {
		// Search through all sources to find the app
		for sourceID, sourceData := range userData.Sources {
			if sourceData != nil {
				for _, appInfoData := range sourceData.AppInfoLatest {
					if appInfoData != nil && appInfoData.RawData != nil && appInfoData.RawData.Name == appName {
						cfgType = appInfoData.RawData.CfgType
						log.Printf("Retrieved cfgType: %s for app: %s from source: %s", cfgType, appName, sourceID)
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
		log.Printf("Warning: Could not retrieve cfgType for app: %s, using default 'app'", appName)
		cfgType = "app" // Default to app type
	}

	// Step 5: Create uninstallation task
	taskMetadata := map[string]interface{}{
		"user_id":  userID,
		"app_name": appName,
		"token":    utils.GetTokenFromRequest(restfulReq),
		"cfgType":  cfgType, // Use retrieved cfgType
	}

	task := s.taskModule.AddTask(task.UninstallApp, appName, userID, taskMetadata)
	if task == nil {
		log.Printf("Failed to create uninstallation task for app: %s", appName)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create uninstallation task", nil)
		return
	}

	log.Printf("Created uninstallation task: ID=%s for app: %s", task.ID, appName)

	s.sendResponse(w, http.StatusOK, true, "App uninstallation started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}

// 9. Upgrade application (single)
func (s *Server) upgradeApp(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	appID := vars["id"]
	log.Printf("PUT /api/v2/apps/%s/upgrade - Upgrading app", appID)

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for upgrade request: %s", userID)

	// Step 2: Parse request body
	var request InstallAppRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		log.Printf("Failed to parse request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request format", nil)
		return
	}

	// Step 3: Validate required fields
	if request.Source == "" || request.AppName == "" || request.Version == "" {
		log.Printf("Missing required fields in request")
		s.sendResponse(w, http.StatusBadRequest, false, "Missing required fields: source, app_name, and version are required", nil)
		return
	}

	// Step 4: Check if cache manager is available
	if s.cacheManager == nil {
		log.Printf("Cache manager is not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Cache manager not available", nil)
		return
	}

	// Step 5: Get user data from cache
	userData := s.cacheManager.GetUserData(userID)
	if userData == nil {
		log.Printf("User data not found for user: %s", userID)
		s.sendResponse(w, http.StatusNotFound, false, "User data not found", nil)
		return
	}

	// Step 6: Get source data
	sourceData := userData.Sources[request.Source]
	if sourceData == nil {
		log.Printf("Source data not found: %s for user: %s", request.Source, userID)
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
		log.Printf("App not found: %s version %s in source: %s", request.AppName, request.Version, request.Source)
		s.sendResponse(w, http.StatusNotFound, false, "App not found", nil)
		return
	}

	// Step 8: Verify chart package exists
	chartFilename := fmt.Sprintf("%s-%s.tgz", request.AppName, request.Version)
	chartPath := filepath.Join(targetApp.RenderedPackage, chartFilename)

	if _, err := os.Stat(chartPath); err != nil {
		log.Printf("Chart package not found at path: %s", chartPath)
		s.sendResponse(w, http.StatusNotFound, false, "Chart package not found", nil)
		return
	}

	// Step 9: Get app cfgType from cache
	var cfgType string
	if targetApp != nil && targetApp.RawData != nil {
		cfgType = targetApp.RawData.CfgType
		log.Printf("Retrieved cfgType: %s for app: %s", cfgType, request.AppName)
	} else {
		log.Printf("Warning: Could not retrieve cfgType for app: %s, using default", request.AppName)
		cfgType = "app" // Default to app type
	}

	// Step 10: Create upgrade task
	taskMetadata := map[string]interface{}{
		"user_id":    userID,
		"source":     request.Source,
		"app_name":   request.AppName,
		"version":    request.Version,
		"chart_path": chartPath,
		"token":      utils.GetTokenFromRequest(restfulReq),
		"cfgType":    cfgType, // Add cfgType to metadata
	}

	task := s.taskModule.AddTask(task.UpgradeApp, request.AppName, userID, taskMetadata)
	if task == nil {
		log.Printf("Failed to create upgrade task for app: %s", request.AppName)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create upgrade task", nil)
		return
	}

	log.Printf("Created upgrade task: ID=%s for app: %s version: %s", task.ID, request.AppName, request.Version)

	s.sendResponse(w, http.StatusOK, true, "App upgrade started successfully", map[string]interface{}{
		"task_id": task.ID,
	})
}
