package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

// UserInfo represents user information structure
type UserInfo struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email"`
}

const (
	AuthorizationTokenCookieKey = "auth_token"
	AuthorizationTokenKey       = "X-Authorization"
	AppServiceHostEnv           = "APP_SERVICE_SERVICE_HOST"
	AppServicePortEnv           = "APP_SERVICE_SERVICE_PORT"
	AppServiceUserInfoTempl     = "http://%s:%s/app-service/v1/user-info"
	isPublicEnvKey              = "isPublic"
)

func isPublicEnv() bool {
	return os.Getenv(isPublicEnvKey) == "true"
}

// GetUserInfoFromRequest extracts username from request
func GetUserInfoFromRequest(req *restful.Request) (string, error) {
	if IsPublicEnvironment() {
		return "admin", nil
	}

	// Check if it's development environment, return admin user directly
	if IsDevelopmentEnvironment() {
		glog.Infof("Development environment detected, returning admin username")
		return "admin", nil
	}

	if IsAccountFromHeader() {
		account := req.HeaderParameter("X-Bfl-User")
		return account, nil
	}

	// Get token from request
	token := GetTokenFromRequest(req)
	if token == "" {
		return "", errors.New("access token not found")
	}

	// Call internal user info service instead of appservice
	resBody, err := getUserInfo(token)
	if err != nil {
		glog.Errorf("Failed to get user info: %v", err)
		return "", err
	}

	// Parse JSON response to extract username
	var userInfo UserInfo
	err = json.Unmarshal([]byte(resBody), &userInfo)
	if err != nil {
		glog.Errorf("Failed to parse user info JSON: %v, response: %s", err, resBody)
		return "", fmt.Errorf("failed to parse user info response: %w", err)
	}

	if userInfo.Username == "" {
		glog.Warningf("Username is empty in response: %s", resBody)
		return "", errors.New("username not found in response")
	}

	return userInfo.Username, nil
}

// getUserInfo gets user information from app service (copied from appservice.GetUserInfo)
func getUserInfo(token string) (string, error) {
	appServiceHost, appServicePort := getAppServiceHostAndPort()
	url := fmt.Sprintf(AppServiceUserInfoTempl, appServiceHost, appServicePort)

	return sendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

// getAppServiceHostAndPort gets app service host and port (copied from constants.GetAppServiceHostAndPort)
func getAppServiceHostAndPort() (string, string) {
	return os.Getenv(AppServiceHostEnv), os.Getenv(AppServicePortEnv)
}

// sendHttpRequestWithToken sends HTTP request with token (copied from utils.SendHttpRequestWithToken)
func sendHttpRequestWithToken(method, url, token string, reader io.Reader) (string, error) {
	httpReq, err := http.NewRequest(method, url, reader)
	if err != nil {
		glog.Warningf("url:%s, err:%s", url, err.Error())
		return "", err
	}

	httpReq.Header.Set(AuthorizationTokenKey, token)
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Content-Type", "application/json")

	return sendHttpRequest(httpReq)
}

// sendHttpRequestWithBflUser sends HTTP request with bfl user (copied from utils.SendHttpRequestWithToken)
func sendHttpRequestWithBflUser(method, url, bflUser string, reader io.Reader) (string, error) {
	httpReq, err := http.NewRequest(method, url, reader)
	if err != nil {
		glog.Warningf("url:%s, err:%s", url, err.Error())
		return "", err
	}

	httpReq.Header.Set("X-Bfl-User", bflUser)
	httpReq.Header.Set("Accept", "*/*")
	httpReq.Header.Set("Content-Type", "application/json")

	return sendHttpRequest(httpReq)
}

// sendHttpRequest sends HTTP request (copied from utils.SendHttpRequest)
func sendHttpRequest(req *http.Request) (string, error) {
	client := &http.Client{
		Timeout: time.Duration(60) * time.Second,
	}

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		glog.Warningf("url:%s, method:%s, err:%v", req.URL, req.Method, err)
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		glog.Warningf("url:%s, method:%s, err:%v", req.URL, req.Method, err)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		glog.Warningf("url:%s, res:%s, resp.StatusCode:%d", req.URL, string(body), resp.StatusCode)
		if len(body) != 0 {
			return string(body), errors.New(string(body))
		}
		return string(body), fmt.Errorf("http status not 200 %d msg:%s", resp.StatusCode, string(body))
	}

	debugBody := string(body)
	if len(debugBody) > 256 {
		debugBody = debugBody[:256]
	}

	return string(body), nil
}

// GetTokenFromRequest extracts token from request (same logic as handler.go)
func GetTokenFromRequest(req *restful.Request) string {
	if isPublicEnv() {
		return "public"
	}

	// Try to get token from cookie first
	cookie, err := req.Request.Cookie(AuthorizationTokenCookieKey)
	if err != nil {
		// If cookie not found, try to get from header
		token := req.Request.Header.Get(AuthorizationTokenKey)
		if token == "" {
			glog.Warningf("req.Request.Cookie err:%s", err)
		}
		return token
	}

	return cookie.Value
}

// AdminUsernameResponse represents the response structure for admin username API
type AdminUsernameResponse struct {
	Code int `json:"code"`
	Data struct {
		Username string `json:"username"`
	} `json:"data"`
}

// GetAdminUsername retrieves the admin username from the app service
func GetAdminUsername(token string) (string, error) {
	// Check if running in development environment
	if IsDevelopmentEnvironment() {
		glog.Infof("Running in development environment, returning admin username: admin")
		return "admin", nil
	}

	// Get app service host and port from environment
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if appServiceHost == "" || appServicePort == "" {
		return "", fmt.Errorf("app service host or port not configured")
	}

	// Build admin username endpoint URL
	url := fmt.Sprintf("http://%s:%s/app-service/v1/users/admin/username", appServiceHost, appServicePort)

	// Create HTTP client with timeout
	client := &http.Client{Timeout: 30 * time.Second}

	// Create HTTP request
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create admin username request: %w", err)
	}

	// Add authorization token if provided
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch admin username from service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("admin username service returned status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read admin username response: %w", err)
	}

	glog.Infof("Admin username response: %s", string(body))

	// Parse response JSON
	var response AdminUsernameResponse
	if err := json.Unmarshal(body, &response); err != nil {
		glog.Warningf("Failed to unmarshal admin username response: %s, error: %v", string(body), err)
		return "", fmt.Errorf("failed to parse admin username response: %w", err)
	}

	return response.Data.Username, nil
}

// UserInfoFromAPI represents user information returned from app-service API
type UserInfoFromAPI struct {
	UID               string   `json:"uid"`
	Name              string   `json:"name"`
	DisplayName       string   `json:"display_name"`
	Description       string   `json:"description"`
	Email             string   `json:"email"`
	State             string   `json:"state"`
	LastLoginTime     *int64   `json:"last_login_time"`
	CreationTimestamp int64    `json:"creation_timestamp"`
	Avatar            string   `json:"avatar"`
	Zone              string   `json:"zone"`
	TerminusName      string   `json:"terminusName"`
	WizardComplete    bool     `json:"wizard_complete"`
	Roles             []string `json:"roles"`
	MemoryLimit       string   `json:"memory_limit"`
	CPULimit          string   `json:"cpu_limit"`
}

// UsersListResponse represents the response structure for users list API
type UsersListResponse struct {
	Code   int                `json:"code"`
	Data   []*UserInfoFromAPI `json:"data"`
	Totals int                `json:"totals"`
}

// GetUserZone retrieves the zone for a specific user from app-service
// It fetches the user list and returns the zone for users in "Created" state
func GetUserZone(username string) (string, error) {
	// Check if running in development environment
	if IsDevelopmentEnvironment() {
		glog.Infof("Running in development environment, returning empty zone for user: %s", username)
		return "", nil
	}

	// Get app service host and port from environment
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if appServiceHost == "" || appServicePort == "" {
		return "", fmt.Errorf("app service host or port not configured")
	}

	// Build users list endpoint URL
	url := fmt.Sprintf("http://%s:%s/app-service/v1/users", appServiceHost, appServicePort)

	// Create HTTP client with timeout
	client := &http.Client{Timeout: 30 * time.Second}

	// Create HTTP request
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create users list request: %w", err)
	}

	// Add X-Bfl-User header
	req.Header.Set("X-Bfl-User", username)
	glog.Infof("Requesting users list with X-Bfl-User header: %s", username)

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch users list from service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("users list service returned status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read users list response: %w", err)
	}

	glog.Infof("Users list response for user %s: %s", username, string(body))

	// Parse response JSON
	var response UsersListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		glog.Warningf("Failed to unmarshal users list response: %s, error: %v", string(body), err)
		return "", fmt.Errorf("failed to parse users list response: %w", err)
	}

	// Find user with matching name and state "Created"
	for _, user := range response.Data {
		if user.Name == username && user.State == "Created" {
			glog.Infof("Found zone for user %s: %s", username, user.Zone)
			return user.Zone, nil
		}
	}

	glog.Warningf("User %s not found or not in Created state", username)
	return "", fmt.Errorf("user %s not found or not in Created state", username)
}
