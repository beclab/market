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
// 从请求中获取用户名
func GetUserInfoFromRequest(req *restful.Request) (string, error) {
	// Check if it's development environment, return admin user directly
	// 检查是否为开发环境，直接返回 admin 用户
	if isDevelopmentEnvironment() {
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
	// 解析 JSON 响应提取用户名
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
// 从 app service 获取用户信息（从 appservice.GetUserInfo 复制而来）
func getUserInfo(token string) (string, error) {
	appServiceHost, appServicePort := getAppServiceHostAndPort()
	url := fmt.Sprintf(constants.AppServiceUserInfoTempl, appServiceHost, appServicePort)

	return sendHttpRequestWithToken(http.MethodGet, url, token, nil)
}

// getAppServiceHostAndPort gets app service host and port (copied from constants.GetAppServiceHostAndPort)
// 获取 app service 主机和端口（从 constants.GetAppServiceHostAndPort 复制而来）
func getAppServiceHostAndPort() (string, string) {
	return os.Getenv(constants.AppServiceHostEnv), os.Getenv(constants.AppServicePortEnv)
}

// sendHttpRequestWithToken sends HTTP request with token (copied from utils.SendHttpRequestWithToken)
// 发送带 token 的 HTTP 请求（从 utils.SendHttpRequestWithToken 复制而来）
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
// 发送 HTTP 请求（从 utils.SendHttpRequest 复制而来）
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
// 从请求中提取 token
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
// 管理员用户名 API 响应结构
type AdminUsernameResponse struct {
	Code int `json:"code"`
	Data struct {
		Username string `json:"username"`
	} `json:"data"`
}

// GetAdminUsername retrieves the admin username from the app service
// 从应用服务获取管理员用户名
func GetAdminUsername(token string) (string, error) {
	// Check if running in development environment
	// 检查是否在开发环境中运行
	if isDevelopmentEnvironment() {
		glog.Infof("Running in development environment, returning admin username: admin")
		return "admin", nil
	}

	// Get app service host and port from environment
	// 从环境变量获取应用服务主机和端口
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if appServiceHost == "" || appServicePort == "" {
		return "", fmt.Errorf("app service host or port not configured")
	}

	// Build admin username endpoint URL
	// 构建管理员用户名端点 URL
	url := fmt.Sprintf("http://%s:%s/app-service/v1/admin/username", appServiceHost, appServicePort)

	// Create HTTP client with timeout
	// 创建带超时的 HTTP 客户端
	client := &http.Client{Timeout: 30 * time.Second}

	// Create HTTP request
	// 创建 HTTP 请求
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create admin username request: %w", err)
	}

	// Add authorization token if provided
	// 如果提供了令牌，添加授权头
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Execute request
	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch admin username from service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("admin username service returned status %d", resp.StatusCode)
	}

	// Read response body
	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read admin username response: %w", err)
	}

	glog.Infof("Admin username response: %s", string(body))

	// Parse response JSON
	// 解析响应 JSON
	var response AdminUsernameResponse
	if err := json.Unmarshal(body, &response); err != nil {
		glog.Warningf("Failed to unmarshal admin username response: %s, error: %v", string(body), err)
		return "", fmt.Errorf("failed to parse admin username response: %w", err)
	}

	return response.Data.Username, nil
}
