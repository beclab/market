package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"market/internal/bk/conf"
	"market/internal/bk/constants"

	"github.com/emicklei/go-restful/v3"
	"github.com/golang/glog"
)

// UserInfo represents user information structure
type UserInfo struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email"`
}

// GetUserInfoFromRequest extracts username from request
func GetUserInfoFromRequest(req *restful.Request) (string, error) {
	// Check if it's development environment, return admin user directly
	if IsDevelopmentEnvironment() {
		glog.Infof("Development environment detected, returning admin username")
		return "admin", nil
	}

	// Get token from request
	token := getTokenFromRequest(req)
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
	url := fmt.Sprintf(constants.AppServiceUserInfoTempl, appServiceHost, appServicePort)

	return sendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

// getAppServiceHostAndPort gets app service host and port (copied from constants.GetAppServiceHostAndPort)
func getAppServiceHostAndPort() (string, string) {
	return os.Getenv(constants.AppServiceHostEnv), os.Getenv(constants.AppServicePortEnv)
}

// sendHttpRequestWithToken sends HTTP request with token (copied from utils.SendHttpRequestWithToken)
func sendHttpRequestWithToken(method, url, token string, reader io.Reader) (string, error) {
	httpReq, err := http.NewRequest(method, url, reader)
	if err != nil {
		glog.Warningf("url:%s, err:%s", url, err.Error())
		return "", err
	}

	httpReq.Header.Set(constants.AuthorizationTokenKey, token)
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

// getTokenFromRequest extracts token from request (same logic as handler.go)
func getTokenFromRequest(req *restful.Request) string {
	if conf.GetIsPublic() {
		return "public"
	}

	// Try to get token from cookie first
	cookie, err := req.Request.Cookie(constants.AuthorizationTokenCookieKey)
	if err != nil {
		// If cookie not found, try to get from header
		token := req.Request.Header.Get(constants.AuthorizationTokenKey)
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
