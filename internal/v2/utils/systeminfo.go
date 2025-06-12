package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/golang/glog"
)

// VersionInfo represents version information response
type VersionInfo struct {
	Version string `json:"version"`
}

// GetTerminusVersion retrieves the Terminus version with environment-aware logic
// 获取 Terminus 版本，支持环境感知逻辑
func GetTerminusVersion() (string, error) {
	// Check if running in development environment
	// 检查是否在开发环境中运行
	if isDevelopmentEnvironment() {
		glog.Infof("Running in development environment, returning fixed version: 1.12.0")
		return "1.12.0", nil
	}

	// For production environment, try to get version from service
	// 生产环境中，尝试从服务获取版本
	return getTerminusVersionFromService()
}

// GetTerminusVersionValue returns the parsed version string
// 返回解析后的版本字符串
func GetTerminusVersionValue() (string, error) {
	// Check public environment first
	// 首先检查公共环境
	if isPublicEnvironment() {
		version := os.Getenv("PUBLIC_VERSION")
		if version != "" {
			return version, nil
		}
	}

	// Check development environment
	// 检查开发环境
	if isDevelopmentEnvironment() {
		glog.Infof("Development environment detected, returning version: 1.12.0")
		return "1.12.0", nil
	}

	// Get version from service
	// 从服务获取版本
	versionResponse, err := getTerminusVersionFromService()
	if err != nil {
		glog.Errorf("Failed to get version from service: %v", err)
		return "", fmt.Errorf("failed to get version from service: %w", err)
	}

	glog.Infof("Version response: %s", versionResponse)

	var versionInfo VersionInfo
	err = json.Unmarshal([]byte(versionResponse), &versionInfo)
	if err != nil {
		glog.Errorf("Failed to parse version JSON: %v", err)
		return "", fmt.Errorf("failed to parse version response: %w", err)
	}

	return versionInfo.Version, nil
}

// isDevelopmentEnvironment checks if the application is running in development mode
// 检查应用是否在开发模式下运行
func isDevelopmentEnvironment() bool {
	// Check DEV_MODE environment variable
	// 检查 DEV_MODE 环境变量
	devMode := os.Getenv("DEV_MODE")
	if devMode == "true" {
		return true
	}

	// Check ENVIRONMENT environment variable
	// 检查 ENVIRONMENT 环境变量
	environment := os.Getenv("ENVIRONMENT")
	if environment == "development" {
		return true
	}

	// Check DEBUG_MODE environment variable
	// 检查 DEBUG_MODE 环境变量
	debugMode := os.Getenv("DEBUG_MODE")
	if debugMode == "true" {
		return true
	}

	return false
}

// isPublicEnvironment checks if the application is running in public mode
// 检查应用是否在公共模式下运行
func isPublicEnvironment() bool {
	isPublic := os.Getenv("isPublic")
	return isPublic == "true"
}

// getTerminusVersionFromService retrieves version from the app service
// 从应用服务获取版本信息
func getTerminusVersionFromService() (string, error) {
	// Get app service host and port from environment
	// 从环境变量获取应用服务主机和端口
	appServiceHost := os.Getenv("APP_SERVICE_SERVICE_HOST")
	appServicePort := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if appServiceHost == "" || appServicePort == "" {
		return "", fmt.Errorf("app service host or port not configured")
	}

	// Build version endpoint URL
	// 构建版本端点 URL
	url := fmt.Sprintf("http://%s:%s/app-service/v1/terminus/version", appServiceHost, appServicePort)

	// Create HTTP client with timeout
	// 创建带超时的 HTTP 客户端
	client := &http.Client{Timeout: 30 * time.Second}

	// Create HTTP request
	// 创建 HTTP 请求
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create version request: %w", err)
	}

	// Execute request
	// 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch version from service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version service returned status %d", resp.StatusCode)
	}

	// Read response body
	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read version response: %w", err)
	}

	return string(body), nil
}
