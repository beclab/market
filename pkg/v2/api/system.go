package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"market/internal/v2/settings"
	"market/internal/v2/utils"

	"github.com/emicklei/go-restful/v3"
)

// settingsManager holds the global settings manager instance
var settingsManager *settings.SettingsManager

// SetSettingsManager sets the global settings manager
func SetSettingsManager(sm *settings.SettingsManager) {
	settingsManager = sm
}

// MarketSourceRequest represents the request body for setting market source
type MarketSourceRequest struct {
	URL string `json:"url"`
}

// ResumeAppRequest represents the request body for resuming an application
// Only supports 'app' type
type ResumeAppRequest struct {
	AppName string `json:"appName"`
	Type    string `json:"type"` // Should be "app"
}

// StopAppRequest represents the request body for stopping an application
// Only supports 'app' type
type StopAppRequest struct {
	AppName string `json:"appName"`
	Type    string `json:"type"` // Should be "app"
}

// SystemStatusResponse
// Aggregated system status response
type SystemStatusResponse struct {
	UserInfo        interface{} `json:"user_info"`
	CurUserResource interface{} `json:"cur_user_resource"`
	ClusterResource interface{} `json:"cluster_resource"`
	TerminusVersion interface{} `json:"terminus_version"`
}

// OpenAppRequest represents the request body for opening an application
type OpenAppRequest struct {
	ID string `json:"id"`
}

// ListVersionResponse for version history response
// VersionInfo struct definition

type VersionInfo struct {
	ID                 string     `json:"-"`
	AppName            string     `json:"appName"`
	Version            string     `json:"version"`
	VersionName        string     `json:"versionName"`
	MergedAt           *time.Time `json:"mergedAt"`
	UpgradeDescription string     `json:"upgradeDescription"`
}

type ListVersionResponse struct {
	Data []*VersionInfo `json:"data"`
}

// getMarketSource handles GET /api/v2/settings/market-source
func (s *Server) getMarketSource(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/settings/market-source - Getting market source configuration")

	if settingsManager == nil {
		log.Println("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	config := settingsManager.GetMarketSource()
	if config == nil {
		log.Println("Market source configuration not found")
		s.sendResponse(w, http.StatusNotFound, false, "Market source configuration not found", nil)
		return
	}

	log.Printf("Market source configuration retrieved: %s", config.BaseURL)
	s.sendResponse(w, http.StatusOK, true, "Market source configuration retrieved successfully", config)
}

// setMarketSource handles PUT /api/v2/settings/market-source
func (s *Server) setMarketSource(w http.ResponseWriter, r *http.Request) {
	log.Println("PUT /api/v2/settings/market-source - Setting market source configuration")

	if settingsManager == nil {
		log.Println("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	var req MarketSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.URL == "" {
		log.Println("Market source URL is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Market source URL cannot be empty", nil)
		return
	}

	if err := settingsManager.SetMarketSource(req.URL); err != nil {
		log.Printf("Failed to set market source: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to set market source configuration", nil)
		return
	}

	// Get the updated configuration to return
	config := settingsManager.GetMarketSource()

	log.Printf("Market source configuration updated to: %s", req.URL)
	s.sendResponse(w, http.StatusOK, true, "Market source configuration updated successfully", config)
}

// getSystemStatus handles GET /api/v2/settings/system-status
// Aggregates system status from multiple sources
func (s *Server) getSystemStatus(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/v2/settings/system-status - Aggregating system status")

	if settingsManager == nil {
		log.Println("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	// 使用 utils.GetTokenFromRequest 获取 token
	restfulReq := &restful.Request{Request: r}
	token := utils.GetTokenFromRequest(restfulReq)

	userInfoURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/user-info"
	curUserResourceURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/user/resource"
	clusterResourceURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/cluster/resource"
	terminusVersionURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/terminus/version"

	log.Printf("Requesting userInfo from %s", userInfoURL)
	userInfo, err1 := doGetWithToken(userInfoURL, token)
	if err1 != nil {
		log.Printf("Failed to get userInfo: %v", err1)
	}
	log.Printf("userInfo: %v", userInfo)

	log.Printf("Requesting curUserResource from %s", curUserResourceURL)
	curUserResource, err2 := doGetWithToken(curUserResourceURL, token)
	if err2 != nil {
		log.Printf("Failed to get curUserResource: %v", err2)
	}
	log.Printf("curUserResource: %v", curUserResource)

	log.Printf("Requesting clusterResource from %s", clusterResourceURL)
	clusterResource, err3 := doGetWithToken(clusterResourceURL, token)
	if err3 != nil {
		log.Printf("Failed to get clusterResource: %v", err3)
	}
	log.Printf("clusterResource: %v", clusterResource)

	log.Printf("Requesting terminusVersion from %s", terminusVersionURL)
	terminusVersion, err4 := doGetWithToken(terminusVersionURL, token)
	if err4 != nil {
		log.Printf("Failed to get terminusVersion: %v", err4)
	}
	log.Printf("terminusVersion: %v", terminusVersion)

	// null 兜底
	if userInfo == nil {
		userInfo = map[string]interface{}{}
	}
	if curUserResource == nil {
		curUserResource = map[string]interface{}{}
	}
	if clusterResource == nil {
		clusterResource = map[string]interface{}{}
	}
	if terminusVersion == nil {
		terminusVersion = map[string]interface{}{}
	}

	resp := SystemStatusResponse{
		UserInfo:        userInfo,
		CurUserResource: curUserResource,
		ClusterResource: clusterResource,
		TerminusVersion: terminusVersion,
	}

	log.Printf("System status aggregation complete, resp: %+v", resp)
	s.sendResponse(w, http.StatusOK, true, "System status aggregation complete", resp)
}

// Simple GET with token, returns parsed JSON or raw string
func doGetWithToken(url, token string) (interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	log.Printf("doGetWithToken: url=%s, token=%s", url, token)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("doGetWithToken: failed to create request: %v", err)
		return map[string]interface{}{}, err
	}
	if token != "" {
		req.Header.Set("X-Authorization", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("doGetWithToken: request failed: %v", err)
		return map[string]interface{}{}, err
	}
	defer resp.Body.Close()
	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("doGetWithToken: decode failed: %v", err)
		return map[string]interface{}{}, err
	}
	if result == nil {
		log.Printf("doGetWithToken: result is nil, return empty map")
		return map[string]interface{}{}, nil
	}
	return result, nil
}

func getenv(key string) string {
	return os.Getenv(key)
}

// openApp handles POST /api/v2/apps/open
func (s *Server) openApp(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/apps/open - Opening application")

	var req OpenAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.ID == "" {
		log.Println("Application ID is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Application ID cannot be empty", nil)
		return
	}

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		log.Printf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	log.Printf("Retrieved user ID for open app request: %s", userID)

	// Step 2: Construct the system server address using user information
	systemServer := fmt.Sprintf("system-server.user-system-%s", userID)
	log.Printf("Using system server: %s", systemServer)

	// Step 3: Construct the intent URL
	intentURL := fmt.Sprintf("http://%s/legacy/v1alpha1/api.intent/v1/server/intent/send", systemServer)
	log.Printf("Sending intent to: %s", intentURL)

	// Step 4: Prepare the JSON payload
	jsonData := fmt.Sprintf(`{
		"action": "view",
		"category": "launcher",
		"data": {
			"appid": "%s"
		}
	}`, req.ID)

	// Step 5: Create HTTP request
	request, err := http.NewRequest(http.MethodPost, intentURL, bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		log.Printf("Failed to create HTTP request: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create request", nil)
		return
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	// Step 6: Send the request
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		log.Printf("Failed to send request: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to send request to system server", nil)
		return
	}
	defer response.Body.Close()

	// Step 7: Read response body
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Printf("Failed to read response body: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to read response", nil)
		return
	}

	log.Printf("System server response status: %s", response.Status)
	log.Printf("System server response body: %s", string(body))

	// Step 8: Check if the request was successful
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		log.Printf("Application opened successfully: %s for user: %s", req.ID, userID)
		s.sendResponse(w, http.StatusOK, true, "Application opened successfully", nil)
	} else {
		log.Printf("Failed to open application, status: %s, body: %s", response.Status, string(body))
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to open application", nil)
	}
}

// getAppVersionHistoryHandler handles POST /api/v2/apps/version-history
func getAppVersionHistoryHandler(w http.ResponseWriter, r *http.Request) {
	// Log
	log.Println("POST /api/v2/apps/version-history - Getting app version history")

	// Parse request body
	var req struct {
		AppName string `json:"appName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}
	if req.AppName == "" {
		log.Println("appName is empty")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "appName cannot be empty",
		})
		return
	}

	// Query version history
	versionList, err := getAppVersionHistory(req.AppName)
	if err != nil {
		log.Printf("Failed to get version history: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to get version history",
		})
		return
	}

	resp := ListVersionResponse{
		Data: versionList,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Version history retrieved successfully",
		"data":    resp,
	})
}

// getAppVersionHistory queries app version history
func getAppVersionHistory(appName string) ([]*VersionInfo, error) {
	// Use appService address like getSystemStatus
	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	url := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/applications/version-history/" + appName
	log.Printf("Requesting version history from %s", url)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Failed to request version history: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Version history API returned status: %d", resp.StatusCode)
		return nil, fmt.Errorf("version history api status: %d", resp.StatusCode)
	}
	var result ListVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Failed to decode version history response: %v", err)
		return nil, err
	}
	return result.Data, nil
}

// resumeApp handles POST /api/v2/apps/resume
func (s *Server) resumeApp(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/apps/resume - Resuming application")

	// Parse request body
	var req ResumeAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Validate required fields
	if req.AppName == "" {
		log.Println("AppName is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "AppName cannot be empty", nil)
		return
	}

	if req.Type == "" {
		log.Println("Type is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Type cannot be empty", nil)
		return
	}

	if req.Type != "app" {
		log.Printf("Unsupported type: %s, only 'app' type is supported", req.Type)
		s.sendResponse(w, http.StatusBadRequest, false, "Only 'app' type is supported", nil)
		return
	}

	// Get token from request
	restfulReq := &restful.Request{Request: r}
	token := utils.GetTokenFromRequest(restfulReq)
	if token == "" {
		log.Println("Access token not found")
		s.sendResponse(w, http.StatusUnauthorized, false, "Access token not found", nil)
		return
	}

	// Resume application by type
	resBody, err := resumeByType(req.AppName, token, req.Type)
	if err != nil {
		log.Printf("Failed to resume %s type:%s resp:%s, err:%s", req.AppName, req.Type, resBody, err.Error())
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to resume application", nil)
		return
	}

	log.Printf("Application resumed successfully: %s, type: %s", req.AppName, req.Type)
	s.sendResponse(w, http.StatusOK, true, "Application resumed successfully", map[string]interface{}{
		"appName": req.AppName,
		"type":    req.Type,
		"result":  resBody,
	})
}

// resumeByType resumes application by type (copied from handler_suspend.go)
func resumeByType(name, token, ty string) (string, error) {
	// Get app service host and port
	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := os.Getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := os.Getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	// Only support app type
	if ty != "app" {
		return "", fmt.Errorf("%s type %s invalid, only 'app' type is supported", name, ty)
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/resume", appServiceHost, appServicePort, name)
	return doPostWithToken(url, token)
}

// doPostWithToken performs POST request with token
func doPostWithToken(url, token string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	log.Printf("doPostWithToken: url=%s, token=%s", url, token)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		log.Printf("doPostWithToken: failed to create request: %v", err)
		return "", err
	}

	if token != "" {
		req.Header.Set("X-Authorization", token)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("doPostWithToken: request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("doPostWithToken: failed to read response body: %v", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("doPostWithToken: response status: %d, body: %s", resp.StatusCode, string(body))
		return string(body), fmt.Errorf("HTTP status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// stopApp handles POST /api/v2/apps/stop
func (s *Server) stopApp(w http.ResponseWriter, r *http.Request) {
	log.Println("POST /api/v2/apps/stop - Stopping application")

	// Parse request body
	var req StopAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Validate required fields
	if req.AppName == "" {
		log.Println("AppName is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "AppName cannot be empty", nil)
		return
	}

	if req.Type == "" {
		log.Println("Type is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Type cannot be empty", nil)
		return
	}

	if req.Type != "app" {
		log.Printf("Unsupported type: %s, only 'app' type is supported", req.Type)
		s.sendResponse(w, http.StatusBadRequest, false, "Only 'app' type is supported", nil)
		return
	}

	// Get token from request
	restfulReq := &restful.Request{Request: r}
	token := utils.GetTokenFromRequest(restfulReq)
	if token == "" {
		log.Println("Access token not found")
		s.sendResponse(w, http.StatusUnauthorized, false, "Access token not found", nil)
		return
	}

	// Stop application by type
	resBody, err := stopByType(req.AppName, token, req.Type)
	if err != nil {
		log.Printf("Failed to stop %s type:%s resp:%s, err:%s", req.AppName, req.Type, resBody, err.Error())
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to stop application", nil)
		return
	}

	log.Printf("Application stopped successfully: %s, type: %s", req.AppName, req.Type)
	s.sendResponse(w, http.StatusOK, true, "Application stopped successfully", map[string]interface{}{
		"appName": req.AppName,
		"type":    req.Type,
		"result":  resBody,
	})
}

// stopByType stops application by type (copied from handler_suspend.go suspendByType)
func stopByType(name, token, ty string) (string, error) {
	// Get app service host and port
	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := os.Getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := os.Getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	// Only support app type
	if ty != "app" {
		return "", fmt.Errorf("%s type %s invalid, only 'app' type is supported", name, ty)
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/suspend", appServiceHost, appServicePort, name)
	return doPostWithToken(url, token)
}
