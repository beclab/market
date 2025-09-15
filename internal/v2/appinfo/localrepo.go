package appinfo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"market/internal/v2/task"
	"market/internal/v2/types"
)

const (
	AppCfgFileName = "OlaresManifest.yaml"
)

// App types
const (
	AppType        = "app"
	RecommendType  = "recommend"
	AgentType      = "agent"
	ModelType      = "model"
	MiddlewareType = "middleware"
)

var (
	ValidTypes = []string{AppType, RecommendType, AgentType, ModelType, MiddlewareType}
)

// SupportClient represents the support client configuration
type SupportClient struct {
	Web     bool `yaml:"web,omitempty"`
	Desktop bool `yaml:"desktop,omitempty"`
	Mobile  bool `yaml:"mobile,omitempty"`
	CLI     bool `yaml:"cli,omitempty"`
}

// Permission represents app permissions
type Permission struct {
	AppData  bool     `yaml:"appData,omitempty"`
	AppCache bool     `yaml:"appCache,omitempty"`
	UserData []string `yaml:"userData,omitempty"`
	Network  bool     `yaml:"network,omitempty"`
	GPU      bool     `yaml:"gpu,omitempty"`
	Storage  bool     `yaml:"storage,omitempty"`
}

// DatabaseConfig represents database configuration
type DatabaseConfig struct {
	Type     string `yaml:"type,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Database string `yaml:"database,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// QueueConfig represents queue configuration
type QueueConfig struct {
	Type     string `yaml:"type,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type      string `yaml:"type,omitempty"`
	Endpoint  string `yaml:"endpoint,omitempty"`
	Bucket    string `yaml:"bucket,omitempty"`
	Region    string `yaml:"region,omitempty"`
	AccessKey string `yaml:"accessKey,omitempty"`
	SecretKey string `yaml:"secretKey,omitempty"`
}

// Middleware represents middleware configuration
type Middleware struct {
	Database *DatabaseConfig    `yaml:"database,omitempty"`
	Cache    *types.CacheConfig `yaml:"cache,omitempty"`
	Queue    *QueueConfig       `yaml:"queue,omitempty"`
	Storage  *StorageConfig     `yaml:"storage,omitempty"`
}

// Dependency represents a dependency or conflict
type Dependency struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Version string `yaml:"version,omitempty"`
}

// Options represents app options and dependencies
type Options struct {
	Dependencies []Dependency      `yaml:"dependencies,omitempty"`
	Conflicts    []Dependency      `yaml:"conflicts,omitempty"`
	Settings     map[string]string `yaml:"settings,omitempty"`
}

// Entrance represents an app entrance point
type Entrance struct {
	Name        string `yaml:"name"`
	Port        int    `yaml:"port,omitempty"`
	Host        string `yaml:"host,omitempty"`
	Title       string `yaml:"title,omitempty"`
	Icon        string `yaml:"icon,omitempty"`
	Path        string `yaml:"path,omitempty"`
	Protocol    string `yaml:"protocol,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// License represents license information
type License struct {
	Name        string `yaml:"name,omitempty"`
	Type        string `yaml:"type,omitempty"`
	URL         string `yaml:"url,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// Legal represents legal information
type Legal struct {
	PrivacyPolicy  string `yaml:"privacyPolicy,omitempty"`
	TermsOfService string `yaml:"termsOfService,omitempty"`
	Disclaimer     string `yaml:"disclaimer,omitempty"`
}

// I18nMetadata represents internationalized metadata
type I18nMetadata struct {
	Title       string `yaml:"title,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// I18nSpec represents internationalized spec
type I18nSpec struct {
	FullDescription    string `yaml:"fullDescription,omitempty"`
	UpgradeDescription string `yaml:"upgradeDescription,omitempty"`
}

// I18nData represents internationalization data
type I18nData struct {
	Metadata *I18nMetadata `yaml:"metadata,omitempty"`
	Spec     *I18nSpec     `yaml:"spec,omitempty"`
}

// Chart represents the Chart.yaml structure
type Chart struct {
	APIVersion string `yaml:"apiVersion"`
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
}

// AppMetaData represents the metadata section in OlaresManifest.yaml
type AppMetaData struct {
	Name        string   `yaml:"name"`
	Icon        string   `yaml:"icon"`
	Description string   `yaml:"description"`
	AppID       string   `yaml:"appid"`
	Title       string   `yaml:"title"`
	Version     string   `yaml:"version"`
	Categories  []string `yaml:"categories"`
	Rating      float32  `yaml:"rating"`
	Target      string   `yaml:"target"`
}

type Spec struct {
	VersionName     string   `yaml:"versionName"`     // version name
	FullDescription string   `yaml:"fullDescription"` // full description
	Developer       string   `yaml:"developer"`       // developer
	Website         string   `yaml:"website"`         // website
	SourceCode      string   `yaml:"sourceCode"`      // source code
	Submitter       string   `yaml:"submitter"`       // submitter
	Locale          []string `yaml:"locale"`          // supported locales
	Doc             string   `yaml:"doc"`             // documentation url
	License         []struct {
		Text string `yaml:"text"`
		URL  string `yaml:"url"`
	} `yaml:"license"` // license info
	RequiredMemory string   `yaml:"requiredMemory"` // required memory
	LimitedMemory  string   `yaml:"limitedMemory"`  // limited memory
	RequiredDisk   string   `yaml:"requiredDisk"`   // required disk
	LimitedDisk    string   `yaml:"limitedDisk"`    // limited disk
	RequiredCpu    string   `yaml:"requiredCpu"`    // required cpu
	LimitedCpu     string   `yaml:"limitedCpu"`     // limited cpu
	RequiredGpu    string   `yaml:"requiredGpu"`    // required gpu
	LimitedGpu     string   `yaml:"limitedGpu"`     // limited gpu
	SupportArch    []string `yaml:"supportArch"`    // supported architectures
}

// AppConfiguration represents the OlaresManifest.yaml structure
type AppConfiguration struct {
	ConfigVersion string      `yaml:"olaresManifest.version"`
	ConfigType    string      `yaml:"olaresManifest.type"`
	Metadata      AppMetaData `yaml:"metadata"`
	Spec          Spec        `yaml:"spec"`
}

// RenderResponse represents the response from render manifest API
type RenderResponse struct {
	Code int `json:"code"`
	Data struct {
		Content string `json:"content"`
	} `json:"data"`
}

// LocalRepo manages local chart package operations
type LocalRepo struct {
	cacheManager *CacheManager
	taskModule   *task.TaskModule
}

// NewLocalRepo creates a new local repository manager
func NewLocalRepo(cacheManager *CacheManager) *LocalRepo {
	return &LocalRepo{
		cacheManager: cacheManager,
	}
}

// SetTaskModule sets the task module for checking installation status
func (lr *LocalRepo) SetTaskModule(taskModule *task.TaskModule) {
	lr.taskModule = taskModule
}

// UploadAppPackage processes an uploaded chart package by calling chart repo service API
func (lr *LocalRepo) UploadAppPackage(userID, sourceID string, fileBytes []byte, filename string, token string) (*types.ApplicationInfoEntry, error) {
	log.Printf("Processing uploaded chart package via chart repo service: %s for user: %s, source: %s", filename, userID, sourceID)

	// Get chart repo service host from environment variable
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		return nil, fmt.Errorf("CHART_REPO_SERVICE_HOST environment variable is not set")
	}

	// Create multipart form data
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add file field
	part, err := writer.CreateFormFile("chart", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, fmt.Errorf("failed to write file data: %w", err)
	}

	// Add source field
	if err := writer.WriteField("source", "upload"); err != nil {
		return nil, fmt.Errorf("failed to write source field: %w", err)
	}

	// Close the writer
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/apps/upload", chartRepoHost)
	req, err := http.NewRequest("POST", url, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Authorization", token)
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Bfl-User", userID)

	// Send request
	client := &http.Client{Timeout: 300 * time.Second} // 5 minutes timeout
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to chart repo service: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chart repo service returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			AppData interface{} `json:"app_data"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("chart repo service returned error: %s", response.Message)
	}

	log.Printf("Successfully processed chart package via chart repo service: %s", filename)

	// var appDataMap map[string]interface{}
	var latest types.AppInfoLatestData

	if response.Data.AppData != nil {
		b, _ := json.Marshal(response.Data.AppData)

		if err := json.Unmarshal(b, &latest); err == nil && latest.RawData != nil {

			// var m map[string]interface{}
			// if bb, err := json.Marshal(latest); err == nil {
			// 	_ = json.Unmarshal(bb, &m)
			// 	appDataMap = m
			// }
		}
	}

	// if appDataMap != nil {
	if err := lr.cacheManager.SetLocalAppData(userID, "upload", types.AppInfoLatestPending, latest); err != nil {
		log.Printf("Warning: Failed to add app data to cache: %v", err)
	}
	// }

	return latest.RawData, nil
}

func (lr *LocalRepo) DeleteApp(userID, appName, appVersion string, token string) error {
	log.Printf("Deleting app: %s, version: %s, user: %s", appName, appVersion, userID)

	// Check if the app is currently being installed
	if lr.taskModule != nil {
		taskType, source, found := lr.taskModule.GetLatestTaskByAppNameAndUser(appName, userID)
		if found && taskType == "install" && source == "upload" {
			log.Printf("Cannot delete app %s: app is currently being installed (task type: %s, source: %s)", appName, taskType, source)
			return fmt.Errorf("cannot delete app %s: app is currently being installed", appName)
		}
		log.Printf("App %s installation check passed: found=%v, taskType=%s, source=%s", appName, found, taskType, source)
	} else {
		log.Printf("Task module not available, skipping installation status check for app: %s", appName)
	}

	// Get chart repo service host from environment variable
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		return fmt.Errorf("CHART_REPO_SERVICE_HOST environment variable is not set")
	}

	// Construct request URL
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/local-apps/delete", chartRepoHost)

	// Prepare JSON body
	bodyMap := map[string]string{
		"app_name":    appName,
		"app_version": appVersion,
		"source_id":   "upload",
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("DELETE", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Authorization", token)
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Bfl-User", userID)

	// Send request
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to chart repo service: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chart repo service returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response JSON
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("failed to parse response JSON: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("chart repo service returned error: %s", response.Message)
	}

	log.Printf("Successfully deleted app %s version %s for user %s", appName, appVersion, userID)
	return nil
}
