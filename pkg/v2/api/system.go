package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"market/internal/v2/appinfo"
	"market/internal/v2/paymentnew"
	"market/internal/v2/settings"
	"market/internal/v2/utils"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
	"github.com/gorilla/mux"
)

// settingsManager holds the global settings manager instance
var settingsManager *settings.SettingsManager

// SetSettingsManager sets the global settings manager
func SetSettingsManager(sm *settings.SettingsManager) {
	settingsManager = sm
}

type AppControllerFailedResp struct {
	Code     int    `json:"code"`
	Resource string `json:"resource"`
	Message  string `json:"message"`
	Reason   string `json:"reason"`
}

// MarketSourceRequest represents the request body for adding market source
type MarketSourceRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	BaseURL     string `json:"base_url"`
	Description string `json:"description,omitempty"`
}

// ResumeAppRequest represents the request body for resuming an application
type ResumeAppRequest struct {
	AppName string `json:"appName"`
}

// StopAppRequest represents the request body for stopping an application
type StopAppRequest struct {
	AppName string `json:"appName"`
	All     bool   `json:"all,omitempty"` // Optional: whether to stop all instances
}

// SystemStatusResponse
// Aggregated system status response
type SystemStatusResponse struct {
	UserInfo        interface{} `json:"user_info"`
	AllUsers        interface{} `json:"users"`
	CurUserResource interface{} `json:"cur_user_resource"`
	ClusterResource interface{} `json:"cluster_resource"`
	TerminusVersion interface{} `json:"terminus_version"`
}

// OpenAppRequest represents the request body for opening an application
type OpenAppRequest struct {
	ID string `json:"id"`
}

// ApplicationVerifiableCredential represents the application verifiable credential structure
type ApplicationVerifiableCredential struct {
	ProductID string `json:"productId"`
	TxHash    string `json:"txHash"`
}

// SubmitSignatureRequest represents the request body for submitting signature
type SubmitSignatureRequest struct {
	JWS                             string                           `json:"jws"`
	ApplicationVerifiableCredential *ApplicationVerifiableCredential `json:"application_verifiable_credential"`
	ProductCredentialManifest       json.RawMessage                  `json:"product_credential_manifest"`
}

// FetchSignatureCallbackRequest represents the request from fetch-signature callback
type FetchSignatureCallbackRequest struct {
	Signed                          bool                             `json:"signed"`
	JWS                             string                           `json:"jws"`
	ApplicationVerifiableCredential *ApplicationVerifiableCredential `json:"application_verifiable_credential,omitempty"`
	ProductCredentialManifest       json.RawMessage                  `json:"product_credential_manifest"`
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
	glog.V(2).Info("GET /api/v2/settings/market-source - Getting market source configuration")

	if settingsManager == nil {
		glog.V(3).Info("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	config := settingsManager.GetMarketSource()
	if config == nil {
		glog.V(3).Info("Market source configuration not found")
		s.sendResponse(w, http.StatusNotFound, false, "Market source configuration not found", nil)
		return
	}

	glog.V(2).Infof("Market source configuration retrieved: %s", config)
	s.sendResponse(w, http.StatusOK, true, "Market source configuration retrieved successfully", config)
}

// addMarketSource handles POST /api/v2/settings/market-source
func (s *Server) addMarketSource(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("POST /api/v2/settings/market-source - Adding market source")

	if settingsManager == nil {
		glog.V(3).Info("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	var req MarketSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.ID == "" {
		glog.V(3).Info("Market source ID is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Market source ID cannot be empty", nil)
		return
	}

	if req.Type == "" {
		glog.V(3).Info("Market source Type is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Market source Type cannot be empty", nil)
		return
	}

	if req.Type != "local" && req.Type != "remote" {
		glog.V(3).Infof("Invalid market source Type: %s", req.Type)
		s.sendResponse(w, http.StatusBadRequest, false, "Market source Type must be 'local' or 'remote'", nil)
		return
	}

	// Create MarketSource struct
	source := &settings.MarketSource{
		ID:          req.ID,
		Name:        req.Name,
		Type:        req.Type,
		BaseURL:     req.BaseURL,
		Description: req.Description,
		IsActive:    true,
	}

	if err := settingsManager.AddMarketSource(source); err != nil {
		glog.Errorf("Failed to add market source: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, err.Error(), nil)
		return
	}

	config := settingsManager.GetMarketSources()
	glog.V(2).Infof("Market source added: %s", req.ID)
	s.sendResponse(w, http.StatusOK, true, "Market source added successfully", config)
}

// deleteMarketSource handles DELETE /api/v2/settings/market-source/{id}
func (s *Server) deleteMarketSource(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("DELETE /api/v2/settings/market-source/{id} - Deleting market source")

	if settingsManager == nil {
		glog.V(3).Info("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	// Extract ID from URL path
	vars := mux.Vars(r)
	id := vars["id"]
	if id == "" {
		glog.V(3).Info("Market source ID is empty in path")
		s.sendResponse(w, http.StatusBadRequest, false, "Market source ID cannot be empty", nil)
		return
	}

	if err := settingsManager.DeleteMarketSource(id); err != nil {
		glog.Errorf("Failed to delete market source: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, err.Error(), nil)
		return
	}

	config := settingsManager.GetMarketSources()
	glog.V(2).Infof("Market source deleted: %s", id)
	s.sendResponse(w, http.StatusOK, true, "Market source deleted successfully", config)
}

// getSystemStatus handles GET /api/v2/settings/system-status
// Aggregates system status from multiple sources
func (s *Server) getSystemStatus(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("GET /api/v2/settings/system-status - Aggregating system status")

	if settingsManager == nil {
		glog.V(3).Info("Settings manager not initialized")
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

	restfulReq := &restful.Request{Request: r}
	token := utils.GetTokenFromRequest(restfulReq)
	bflUser := restfulReq.HeaderParameter("X-Bfl-User")

	userInfoURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/user-info"
	curUserResourceURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/user/resource"
	clusterResourceURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/cluster/resource"
	terminusVersionURL := "http://" + appServiceHost + ":" + appServicePort + "/app-service/v1/terminus/version"

	// Declare variables for storing responses
	var userInfo, curUserResource, clusterResource, terminusVersion interface{}
	var allUsers []map[string]string
	var err1, err2, err3, err4 error

	// Check if account is from header
	if utils.IsAccountFromHeader() {
		glog.V(2).Infof("Account from header detected, using bflUser: %s", bflUser)

		allUsers, err1 = doGetUsers(s.cacheManager)
		if err1 != nil {
			glog.Errorf("Failed to get users: %v", err1)
		}
		glog.V(2).Infof("users: %v", allUsers)

		glog.V(3).Infof("Requesting userInfo from %s", userInfoURL)
		userInfo, err1 = doGetWithBflUser(userInfoURL, bflUser)
		if err1 != nil {
			glog.Errorf("Failed to get userInfo: %v", err1)
		}
		glog.V(3).Infof("userInfo: %v", userInfo)

		glog.V(3).Infof("Requesting curUserResource from %s", curUserResourceURL)
		curUserResource, err2 = doGetWithBflUser(curUserResourceURL, bflUser)
		if err2 != nil {
			glog.Errorf("Failed to get curUserResource: %v", err2)
		}
		glog.V(3).Infof("curUserResource: %v", curUserResource)

		glog.V(3).Infof("Requesting clusterResource from %s", clusterResourceURL)
		clusterResource, err3 = doGetWithBflUser(clusterResourceURL, bflUser)
		if err3 != nil {
			glog.Errorf("Failed to get clusterResource: %v", err3)
		}
		glog.V(3).Infof("clusterResource: %v", clusterResource)

		glog.V(3).Infof("Requesting terminusVersion from %s", terminusVersionURL)
		terminusVersion, err4 = doGetWithBflUser(terminusVersionURL, bflUser)
		if err4 != nil {
			glog.Errorf("Failed to get terminusVersion: %v", err4)
		}
		glog.V(3).Infof("terminusVersion: %v", terminusVersion)
	} else {
		glog.V(2).Infof("Account from token, using token: %s", token)

		glog.V(3).Infof("Requesting userInfo from %s", userInfoURL)
		userInfo, err1 = doGetWithToken(userInfoURL, token)
		if err1 != nil {
			glog.Errorf("Failed to get userInfo: %v", err1)
		}
		glog.V(3).Infof("userInfo: %v", userInfo)

		glog.V(3).Infof("Requesting curUserResource from %s", curUserResourceURL)
		curUserResource, err2 = doGetWithToken(curUserResourceURL, token)
		if err2 != nil {
			glog.Errorf("Failed to get curUserResource: %v", err2)
		}
		glog.V(3).Infof("curUserResource: %v", curUserResource)

		glog.V(3).Infof("Requesting clusterResource from %s", clusterResourceURL)
		clusterResource, err3 = doGetWithToken(clusterResourceURL, token)
		if err3 != nil {
			glog.Errorf("Failed to get clusterResource: %v", err3)
		}
		glog.V(3).Infof("clusterResource: %v", clusterResource)

		glog.V(3).Infof("Requesting terminusVersion from %s", terminusVersionURL)
		terminusVersion, err4 = doGetWithToken(terminusVersionURL, token)
		if err4 != nil {
			glog.Errorf("Failed to get terminusVersion: %v", err4)
		}
		glog.V(3).Infof("terminusVersion: %v", terminusVersion)
	}

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
		AllUsers:        allUsers,
		CurUserResource: curUserResource,
		ClusterResource: clusterResource,
		TerminusVersion: terminusVersion,
	}

	glog.V(2).Infof("System status aggregation complete, resp: %+v", resp)
	s.sendResponse(w, http.StatusOK, true, "System status aggregation complete", resp)
}

// Simple GET with token, returns parsed JSON or raw string
func doGetWithToken(url, token string) (interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	glog.V(2).Infof("doGetWithToken: url=%s, token=%s", url, token)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		glog.Errorf("doGetWithToken: failed to create request: %v", err)
		return map[string]interface{}{}, err
	}
	if token != "" {
		req.Header.Set("X-Authorization", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("doGetWithToken: request failed: %v", err)
		return map[string]interface{}{}, err
	}
	defer resp.Body.Close()
	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		glog.Errorf("doGetWithToken: decode failed: %v", err)
		return map[string]interface{}{}, err
	}
	if result == nil {
		glog.V(3).Infof("doGetWithToken: result is nil, return empty map")
		return map[string]interface{}{}, nil
	}
	return result, nil
}

// Simple GET with token, returns parsed JSON or raw string
func doGetWithBflUser(url, bflUser string) (interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	glog.V(2).Infof("doGetWithBflUser: url=%s, bflUser=%s", url, bflUser)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		glog.Errorf("doGetWithBflUser: failed to create request: %v", err)
		return map[string]interface{}{}, err
	}
	if bflUser != "" {
		req.Header.Set("X-Bfl-User", bflUser)
	}
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("doGetWithBflUser: request failed: %v", err)
		return map[string]interface{}{}, err
	}
	defer resp.Body.Close()
	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		glog.Errorf("doGetWithBflUser: decode failed: %v", err)
		return map[string]interface{}{}, err
	}
	if result == nil {
		glog.V(3).Info("doGetWithBflUser: result is nil, return empty map")
		return map[string]interface{}{}, nil
	}
	return result, nil
}

func doGetUsers(cm *appinfo.CacheManager) ([]map[string]string, error) {
	if ok := cm.TryRLock(); !ok {
		glog.Warning("[TryRLock] doGetUsers: CacheManager read lock not available")
		return nil, nil
	}
	defer cm.RUnlock()

	var usersInfo []map[string]string

	getUsers := cm.GetCache().Users
	for _, v := range getUsers {
		if v.UserInfo != nil {
			usersInfo = append(usersInfo, v.UserInfo)
		}
	}

	return usersInfo, nil
}

func getenv(key string) string {
	return os.Getenv(key)
}

// openApp handles POST /api/v2/apps/open
func (s *Server) openApp(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("POST /api/v2/apps/open - Opening application")

	var req OpenAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.ID == "" {
		glog.V(3).Info("Application ID is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "Application ID cannot be empty", nil)
		return
	}

	// Step 1: Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(2).Infof("Retrieved user ID for open app request: %s", userID)

	// Step 2: Construct the system server address using user information
	systemServer := fmt.Sprintf("system-server.user-system-%s", userID)
	glog.V(3).Infof("Using system server: %s", systemServer)

	// Step 3: Construct the intent URL
	intentURL := fmt.Sprintf("http://%s/legacy/v1alpha1/api.intent/v1/server/intent/send", systemServer)
	glog.V(2).Infof("Sending intent to: %s", intentURL)

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
		glog.Errorf("Failed to create HTTP request: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to create request", nil)
		return
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")

	// Step 6: Send the request
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		glog.Errorf("Failed to send request: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to send request to system server", nil)
		return
	}
	defer response.Body.Close()

	// Step 7: Read response body
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		glog.Errorf("Failed to read response body: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to read response", nil)
		return
	}

	glog.V(2).Infof("System server response status: %s", response.Status)
	glog.V(3).Infof("System server response body: %s", string(body))

	// Step 8: Check if the request was successful
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		glog.V(2).Infof("Application opened successfully: %s for user: %s", req.ID, userID)
		s.sendResponse(w, http.StatusOK, true, "Application opened successfully", nil)
	} else {
		glog.Errorf("Failed to open application, status: %s, body: %s", response.Status, string(body))
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to open application", nil)
	}
}

// getAppVersionHistoryHandler handles POST /api/v2/apps/version-history
func getAppVersionHistoryHandler(w http.ResponseWriter, r *http.Request) {
	// Log
	glog.V(2).Info("POST /api/v2/apps/version-history - Getting app version history")

	// Parse request body
	var req struct {
		AppName string `json:"appName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}
	if req.AppName == "" {
		glog.V(3).Info("appName is empty")
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
		glog.Errorf("Failed to get version history: %v", err)
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
	glog.V(2).Infof("Requesting version history from %s", url)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		glog.Errorf("Failed to request version history: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		glog.Errorf("Version history API returned status: %d", resp.StatusCode)
		return nil, fmt.Errorf("version history api status: %d", resp.StatusCode)
	}
	var result ListVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		glog.Errorf("Failed to decode version history response: %v", err)
		return nil, err
	}
	return result.Data, nil
}

// resumeApp handles POST /api/v2/apps/resume
func (s *Server) resumeApp(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("POST /api/v2/apps/resume - Resuming application")

	// Parse request body
	var req ResumeAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Validate required fields
	if req.AppName == "" {
		glog.V(3).Info("AppName is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "AppName cannot be empty", nil)
		return
	}

	// Get token from request
	restfulReq := &restful.Request{Request: r}
	token := utils.GetTokenFromRequest(restfulReq)
	// if token == "" {
	// 	glog.Warning("Access token not found")
	// 	s.sendResponse(w, http.StatusUnauthorized, false, "Access token not found", nil)
	// 	return
	// }

	bflUser := restfulReq.HeaderParameter("X-Bfl-User")

	// Resume application
	resBody, err := resumeByType(req.AppName, token, bflUser)
	if err != nil {
		glog.Errorf("Failed to resume %s resp:%s, err:%s", req.AppName, resBody, err.Error())
		var reason *AppControllerFailedResp
		if e := json.Unmarshal([]byte(resBody), &reason); e != nil {
			s.sendResponse(w, http.StatusInternalServerError, false, "Failed to resume application", nil)
		} else {
			s.sendResponse(w, http.StatusInternalServerError, false, reason.Message, nil)
		}

		return
	}

	glog.V(2).Infof("Application resumed successfully: %s", req.AppName)
	s.sendResponse(w, http.StatusOK, true, "Application resumed successfully", map[string]interface{}{
		"appName": req.AppName,
		"result":  resBody,
	})
}

// resumeByType resumes application (copied from handler_suspend.go)
func resumeByType(name, token, bflUser string) (string, error) {
	// Get app service host and port
	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := os.Getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := os.Getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/resume", appServiceHost, appServicePort, name)

	if utils.IsAccountFromHeader() {
		return doPostWithBflUser(url, bflUser)
	} else {
		return doPostWithToken(url, token)
	}
}

// doPostWithToken performs POST request with token
func doPostWithToken(url, token string) (string, error) {
	return doPostWithTokenAndBody(url, token, nil)
}

// doPostWithTokenAndBody performs POST request with token and request body
func doPostWithTokenAndBody(url, token string, bodyBytes []byte) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	glog.V(2).Infof("doPostWithTokenAndBody: url=%s, token=%s, body=%v", url, token, string(bodyBytes))

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest("POST", url, bodyReader)
	if err != nil {
		glog.Errorf("doPostWithTokenAndBody: failed to create request: %v", err)
		return "", err
	}

	if token != "" {
		req.Header.Set("X-Authorization", token)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("doPostWithTokenAndBody: request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf("doPostWithTokenAndBody: failed to read response body: %v", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		glog.Errorf("doPostWithTokenAndBody: response status: %d, body: %s", resp.StatusCode, string(body))
		return string(body), fmt.Errorf("HTTP status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// doPostWithBflUser performs POST request with bflUser
func doPostWithBflUser(url, bflUser string) (string, error) {
	return doPostWithBflUserAndBody(url, bflUser, nil)
}

// doPostWithBflUserAndBody performs POST request with bflUser and request body
func doPostWithBflUserAndBody(url, bflUser string, bodyBytes []byte) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	glog.V(2).Infof("doPostWithBflUserAndBody: url=%s, bflUser=%s, body=%v", url, bflUser, string(bodyBytes))

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest("POST", url, bodyReader)
	if err != nil {
		glog.Errorf("doPostWithBflUserAndBody: failed to create request: %v", err)
		return "", err
	}

	if bflUser != "" {
		req.Header.Set("X-Bfl-User", bflUser)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("doPostWithBflUserAndBody: request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf("doPostWithBflUserAndBody: failed to read response body: %v", err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		glog.Errorf("doPostWithBflUserAndBody: response status: %d, body: %s", resp.StatusCode, string(body))
		return string(body), fmt.Errorf("HTTP status %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// stopApp handles POST /api/v2/apps/stop
func (s *Server) stopApp(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("POST /api/v2/apps/stop - Stopping application")

	// Parse request body
	var req StopAppRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Validate required fields
	if req.AppName == "" {
		glog.V(3).Info("AppName is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "AppName cannot be empty", nil)
		return
	}

	// Get token from request
	restfulReq := &restful.Request{Request: r}
	token := utils.GetTokenFromRequest(restfulReq)
	// if token == "" {
	// 	glog.Warning("Access token not found")
	// 	s.sendResponse(w, http.StatusUnauthorized, false, "Access token not found", nil)
	// 	return
	// }
	bflUser := restfulReq.HeaderParameter("X-Bfl-User")

	// Stop application
	resBody, err := stopByType(req.AppName, token, bflUser, req.All)
	if err != nil {
		glog.Errorf("Failed to stop %s resp:%s, err:%s", req.AppName, resBody, err.Error())
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to stop application", nil)
		return
	}

	glog.V(2).Infof("Application stopped successfully: %s", req.AppName)
	s.sendResponse(w, http.StatusOK, true, "Application stopped successfully", map[string]interface{}{
		"appName": req.AppName,
		"result":  resBody,
	})
}

// stopByType stops application (copied from handler_suspend.go suspendByType)
func stopByType(name, token, bflUser string, all bool) (string, error) {
	// Get app service host and port
	appServiceHost := "127.0.0.1"
	appServicePort := "8080"
	if host := os.Getenv("APP_SERVICE_SERVICE_HOST"); host != "" {
		appServiceHost = host
	}
	if port := os.Getenv("APP_SERVICE_SERVICE_PORT"); port != "" {
		appServicePort = port
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/apps/%s/suspend", appServiceHost, appServicePort, name)

	// Create request body with all parameter
	requestBody := map[string]interface{}{
		"all": all,
	}
	requestBodyBytes, _ := json.Marshal(requestBody)

	if utils.IsAccountFromHeader() {
		return doPostWithBflUserAndBody(url, bflUser, requestBodyBytes)
	} else {
		return doPostWithTokenAndBody(url, token, requestBodyBytes)
	}
}

// getMarketSettings handles GET /api/v2/settings/market-settings
func (s *Server) getMarketSettings(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("GET /api/v2/settings/market-settings - Getting market settings")

	if settingsManager == nil {
		glog.V(3).Info("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	// Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(4).Infof("Retrieved user ID for market settings request: %s", userID)

	settings, err := settingsManager.GetMarketSettings(userID)
	if err != nil {
		glog.Errorf("Failed to get market settings for user %s: %v", userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to get market settings", nil)
		return
	}

	glog.V(2).Infof("Market settings retrieved successfully for user: %s", userID)
	s.sendResponse(w, http.StatusOK, true, "Market settings retrieved successfully", settings)
}

// updateMarketSettings handles PUT /api/v2/settings/market-settings
func (s *Server) updateMarketSettings(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("PUT /api/v2/settings/market-settings - Updating market settings")

	if settingsManager == nil {
		glog.V(3).Info("Settings manager not initialized")
		s.sendResponse(w, http.StatusInternalServerError, false, "Settings manager not initialized", nil)
		return
	}

	// Get user information from request
	restfulReq := s.httpToRestfulRequest(r)
	userID, err := utils.GetUserInfoFromRequest(restfulReq)
	if err != nil {
		glog.Errorf("Failed to get user from request: %v", err)
		s.sendResponse(w, http.StatusUnauthorized, false, "Failed to get user information", nil)
		return
	}
	glog.V(4).Infof("Retrieved user ID for market settings update request: %s", userID)

	var settings settings.MarketSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if err := settingsManager.UpdateMarketSettings(userID, &settings); err != nil {
		glog.Errorf("Failed to update market settings for user %s: %v", userID, err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to update market settings", nil)
		return
	}

	glog.V(2).Infof("Market settings updated successfully for user: %s", userID)
	s.sendResponse(w, http.StatusOK, true, "Market settings updated successfully", settings)
}

// submitSignature handles POST /api/v2/payment/submit-signature
func (s *Server) submitSignature(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("POST /api/v2/payment/submit-signature - Processing signature submission")

	// Log all HTTP headers for debugging
	glog.V(2).Info("=== REQUEST HEADERS ===")
	for key, values := range r.Header {
		for _, value := range values {
			glog.V(3).Infof("Header: %s = %s", key, value)
		}
	}

	// Read request body for logging
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		glog.Errorf("Failed to read request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Failed to read request body", nil)
		return
	}

	// Log raw request body
	glog.V(2).Info("=== RAW REQUEST BODY ===")
	glog.V(2).Infof("Body length: %d bytes", len(bodyBytes))
	glog.V(2).Infof("Body content: %s", string(bodyBytes))

	// Parse request body from the read bytes
	var req SubmitSignatureRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	// Debug: Log parsed request fields
	glog.V(2).Info("=== PARSED REQUEST PARAMETERS ===")
	glog.V(2).Infof("req.JWS: present=%v, length=%d", req.JWS != "", len(req.JWS))
	glog.V(2).Infof("req.ApplicationVerifiableCredential: present=%v", req.ApplicationVerifiableCredential != nil)
	if req.ApplicationVerifiableCredential != nil {
		glog.V(2).Infof("  - productId: %s", req.ApplicationVerifiableCredential.ProductID)
		glog.V(2).Infof("  - txHash: %s", req.ApplicationVerifiableCredential.TxHash)
	}
	glog.V(2).Infof("req.ProductCredentialManifest: present=%v, length=%d", len(req.ProductCredentialManifest) > 0, len(req.ProductCredentialManifest))

	// Validate required fields
	if req.JWS == "" {
		glog.V(3).Info("JWS is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "JWS cannot be empty", nil)
		return
	}

	// Reconstruct SignBody from the components
	signBody := make(map[string]interface{})

	// Parse product_credential_manifest from json.RawMessage
	var productCredentialManifest interface{}
	if len(req.ProductCredentialManifest) > 0 {
		if err := json.Unmarshal(req.ProductCredentialManifest, &productCredentialManifest); err != nil {
			glog.Errorf("Failed to parse product_credential_manifest: %v", err)
			s.sendResponse(w, http.StatusBadRequest, false, "Invalid product_credential_manifest format", nil)
			return
		}
		signBody["product_credential_manifest"] = productCredentialManifest
	}

	// Add application_verifiable_credential if present
	if req.ApplicationVerifiableCredential != nil {
		signBody["application_verifiable_credential"] = req.ApplicationVerifiableCredential
	}

	// Serialize signBody to JSON string
	signBodyJSON, err := json.Marshal(signBody)
	if err != nil {
		glog.Errorf("Failed to marshal signBody: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid sign body format", nil)
		return
	}
	signBodyStr := string(signBodyJSON)

	glog.V(2).Infof("Constructed SignBody length: %d", len(signBodyStr))

	// Get bflUser and X-Forwarded-Host from headers
	restfulReq := &restful.Request{Request: r}
	bflUser := restfulReq.HeaderParameter("X-Bfl-User")
	xForwardedHost := r.Header.Get("X-Forwarded-Host")

	glog.V(2).Info("=== REQUEST VALIDATION ===")
	glog.V(2).Infof("JWS length: %d", len(req.JWS))
	glog.V(2).Infof("SignBody length: %d", len(signBodyStr))
	glog.V(2).Infof("X-Bfl-User header: %s", bflUser)
	glog.V(2).Infof("X-Forwarded-Host: %s", xForwardedHost)

	// Validate user from header
	if bflUser == "" {
		glog.V(3).Info("X-Bfl-User header is empty")
		s.sendResponse(w, http.StatusBadRequest, false, "X-Bfl-User header is required", nil)
		return
	}

	// Call payment module for business logic processing
	processErr := processSignatureSubmission(req.JWS, signBodyStr, bflUser, xForwardedHost)
	if processErr != nil {
		glog.Errorf("Failed to process signature submission: %v", processErr)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to process signature submission", nil)
		return
	}

	glog.V(2).Infof("Signature submission processed successfully for user: %s", bflUser)
	s.sendResponse(w, http.StatusOK, true, "Signature submission processed successfully", map[string]interface{}{
		"jws":       req.JWS,
		"sign_body": signBodyStr,
		"user":      bflUser,
		"status":    "processed",
	})
}

// fetchSignatureCallback handles POST /api/v2/payment/fetch-signature-callback
func (s *Server) fetchSignatureCallback(w http.ResponseWriter, r *http.Request) {
	glog.V(2).Info("POST /api/v2/payment/fetch-signature-callback - Processing fetch signature callback")

	// Log headers
	for key, values := range r.Header {
		for _, value := range values {
			glog.V(2).Infof("Header: %s = %s", key, value)
		}
	}

	// Read raw body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		glog.Errorf("Failed to read request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Failed to read request body", nil)
		return
	}
	glog.V(2).Infof("RAW BODY len=%d content=%s", len(bodyBytes), string(bodyBytes))

	// Parse JSON
	var req FetchSignatureCallbackRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		glog.Errorf("Failed to decode request body: %v", err)
		s.sendResponse(w, http.StatusBadRequest, false, "Invalid request body", nil)
		return
	}

	if req.Signed && req.JWS == "" {
		glog.V(3).Info("Signed flag is true but JWS is empty in fetch-signature callback")
		s.sendResponse(w, http.StatusBadRequest, false, "JWS cannot be empty when signed is true", nil)
		return
	}

	// Prepare signBody as raw JSON string passthrough
	signBodyStr := string(bodyBytes)

	// Get user from header
	restfulReq := &restful.Request{Request: r}
	bflUser := restfulReq.HeaderParameter("X-Bfl-User")
	if bflUser == "" {
		glog.V(3).Info("X-Bfl-User header is empty for fetch-signature callback")
		s.sendResponse(w, http.StatusBadRequest, false, "X-Bfl-User header is required", nil)
		return
	}

	// Delegate to payment module
	if err := paymentnew.HandleFetchSignatureCallback(req.JWS, signBodyStr, bflUser, req.Signed); err != nil {
		glog.Errorf("Failed to process fetch-signature callback: %v", err)
		s.sendResponse(w, http.StatusInternalServerError, false, "Failed to process fetch-signature callback", nil)
		return
	}

	s.sendResponse(w, http.StatusOK, true, "Fetch signature callback processed", map[string]interface{}{
		"status":      "processed",
		"user":        bflUser,
		"jws_present": req.JWS != "",
	})
}

// processSignatureSubmission calls payment module for business logic processing
func processSignatureSubmission(jws, signBody, user, xForwardedHost string) error {
	glog.V(2).Infof("Processing signature submission in payment module - JWS: %s, SignBody: %s, User: %s, X-Forwarded-Host: %s", jws, signBody, user, xForwardedHost)

	// Call payment module function for business logic processing
	return paymentnew.ProcessSignatureSubmission(jws, signBody, user, xForwardedHost)
}
