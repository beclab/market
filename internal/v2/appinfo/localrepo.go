package appinfo

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"market/internal/v2/appinfo/hydrationfn"
	"market/internal/v2/types"

	"gopkg.in/yaml.v3"
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

// AppConfiguration represents the OlaresManifest.yaml structure
type AppConfiguration struct {
	ConfigVersion string      `yaml:"olaresManifest.version"`
	ConfigType    string      `yaml:"olaresManifest.type"`
	Metadata      AppMetaData `yaml:"metadata"`
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
	hydrator     *Hydrator // Add hydrator reference for task creation
}

// NewLocalRepo creates a new local repository manager
func NewLocalRepo(cacheManager *CacheManager) *LocalRepo {
	return &LocalRepo{
		cacheManager: cacheManager,
		hydrator:     nil, // Will be set later via SetHydrator
	}
}

// SetHydrator sets the hydrator for task creation
func (lr *LocalRepo) SetHydrator(hydrator *Hydrator) {
	lr.hydrator = hydrator
}

// UploadAppPackage processes an uploaded chart package and stores it in the local repository
func (lr *LocalRepo) UploadAppPackage(userID, sourceID string, fileBytes []byte, filename string, token string) (*types.ApplicationInfoEntry, error) {
	log.Printf("Processing uploaded chart package: %s for user: %s, source: %s", filename, userID, sourceID)

	// Step 1: Create temporary directory for processing
	tempDir, err := lr.createTempDir(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Step 2: Write uploaded file to temp directory
	tempFilePath := filepath.Join(tempDir, filename)
	if err := os.WriteFile(tempFilePath, fileBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Step 3: Extract chart package
	extractDir := filepath.Join(tempDir, "extracted")
	if err := lr.unArchive(tempFilePath, extractDir); err != nil {
		return nil, fmt.Errorf("failed to extract chart package: %w", err)
	}

	// Step 4: Find and validate chart directory
	chartDir := lr.findChartPath(extractDir)
	if chartDir == "" {
		return nil, fmt.Errorf("no valid chart directory found in package")
	}

	// Step 5: Validate chart structure and configuration
	if err := lr.validateChart(chartDir, token); err != nil {
		return nil, fmt.Errorf("chart validation failed: %w", err)
	}

	// Step 6: Parse app information from chart
	appInfo, err := lr.parseAppInfo(chartDir, token)
	if err != nil {
		return nil, fmt.Errorf("failed to parse app info: %w", err)
	}

	// Step 7: Convert to AppInfoLatestData and store in cache
	if err := lr.storeAppInfo(userID, sourceID, appInfo, chartDir); err != nil {
		return nil, fmt.Errorf("failed to store app info: %w", err)
	}

	log.Printf("Successfully processed and stored chart package: %s", filename)
	return appInfo, nil
}

// createTempDir creates a temporary directory for processing
func (lr *LocalRepo) createTempDir(filename string) (string, error) {
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("chart_upload_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", err
	}
	return tempDir, nil
}

// findChartPath finds the chart directory within the extracted package
func (lr *LocalRepo) findChartPath(extractDir string) string {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		chartPath := filepath.Join(extractDir, entry.Name())
		appCfgPath := filepath.Join(chartPath, AppCfgFileName)

		if lr.pathExists(appCfgPath) {
			return chartPath
		}
	}

	return ""
}

// validateChart validates the chart structure and configuration
func (lr *LocalRepo) validateChart(chartDir string, token string) error {
	// First, parse app info to get cfgType for validation logic
	appInfo, err := lr.parseAppInfo(chartDir, token)
	if err != nil {
		return fmt.Errorf("failed to parse app info for validation: %w", err)
	}

	// Check cfgType to determine validation level
	switch appInfo.CfgType {
	case AppType:
		// For app type, perform full validation
		log.Printf("Performing full chart validation for app type: %s", appInfo.CfgType)
	case RecommendType, AgentType, ModelType, MiddlewareType:
		// For other types, skip chart validation
		log.Printf("Skipping chart validation for type: %s", appInfo.CfgType)
		return nil
	default:
		return fmt.Errorf("olaresManifest.type %s must be one of %v", appInfo.CfgType, ValidTypes)
	}

	// Perform chart folder validation
	if err := lr.checkChartFolder(chartDir, token); err != nil {
		return fmt.Errorf("chart folder validation failed: %w", err)
	}

	// Perform app configuration validation
	if err := lr.checkAppCfg(chartDir, token); err != nil {
		return fmt.Errorf("app configuration validation failed: %w", err)
	}

	// Perform service account role validation
	if err := lr.checkServiceAccountRole(chartDir, token); err != nil {
		return fmt.Errorf("service account role validation failed: %w", err)
	}

	return nil
}

// checkChartFolder validates the chart folder structure
func (lr *LocalRepo) checkChartFolder(folder string, token string) error {
	folderName := filepath.Base(folder)
	// if !lr.isValidFolderName(folderName) {
	// 	return fmt.Errorf("invalid folder name: '%s' must '^[a-z0-9]{1,30}$'", folder)
	// }

	if !lr.dirExists(folder) {
		return fmt.Errorf("folder does not exist: '%s'", folder)
	}

	chartFile := filepath.Join(folder, "Chart.yaml")
	if !lr.fileExists(chartFile) {
		return fmt.Errorf("missing Chart.yaml in folder: '%s'", folder)
	}

	chartContent, err := os.ReadFile(chartFile)
	if err != nil {
		return fmt.Errorf("failed to read Chart.yaml in folder '%s': %v", folder, err)
	}
	var chart Chart
	if err := yaml.Unmarshal(chartContent, &chart); err != nil {
		return fmt.Errorf("failed to parse Chart.yaml in folder '%s': %v", folder, err)
	}

	if err := lr.isValidChartFields(chart); err != nil {
		return err
	}

	valuesFile := filepath.Join(folder, "values.yaml")
	if !lr.fileExists(valuesFile) {
		return fmt.Errorf("missing values.yaml in folder: '%s'", folder)
	}

	templatesDir := filepath.Join(folder, "templates")
	if !lr.dirExists(templatesDir) {
		return fmt.Errorf("missing templates folder in folder: '%s'", folder)
	}

	appCfgFile := filepath.Join(folder, AppCfgFileName)
	if !lr.fileExists(appCfgFile) {
		return fmt.Errorf("missing %s in folder: '%s'", AppCfgFileName, folder)
	}

	appCfgContent, err := os.ReadFile(appCfgFile)
	if err != nil {
		return fmt.Errorf("failed to read %s in folder '%s': %v", AppCfgFileName, folder, err)
	}

	renderedContent, err := lr.renderManifest(string(appCfgContent), token)
	if err != nil {
		return fmt.Errorf("failed to render %s in folder '%s': %v", AppCfgFileName, folder, err)
	}

	var appConf AppConfiguration
	if err := yaml.Unmarshal([]byte(renderedContent), &appConf); err != nil {
		return fmt.Errorf("failed to parse %s in folder '%s': %v", AppCfgFileName, folder, err)
	}

	if err := lr.isValidMetadataFields(appConf.Metadata, chart, folderName); err != nil {
		return err
	}

	if lr.checkReservedWord(folderName) {
		return fmt.Errorf("foldername %s in reserved foldername list, invalid", folderName)
	}

	return nil
}

// checkAppCfg validates the app configuration
func (lr *LocalRepo) checkAppCfg(chartDir string, token string) error {
	// Basic validation - check if OlaresManifest.yaml exists and is valid
	appCfgFile := filepath.Join(chartDir, AppCfgFileName)
	if !lr.fileExists(appCfgFile) {
		return fmt.Errorf("missing %s in chart directory", AppCfgFileName)
	}

	content, err := os.ReadFile(appCfgFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", AppCfgFileName, err)
	}

	// Try to render and parse the manifest
	renderedContent, err := lr.renderManifest(string(content), token)
	if err != nil {
		return fmt.Errorf("failed to render manifest: %w", err)
	}

	var appCfg AppConfiguration
	if err := yaml.Unmarshal([]byte(renderedContent), &appCfg); err != nil {
		return fmt.Errorf("failed to parse %s: %w", AppCfgFileName, err)
	}

	// Validate required fields
	if appCfg.ConfigVersion == "" {
		return fmt.Errorf("olaresManifest.version is required")
	}
	if appCfg.ConfigType == "" {
		return fmt.Errorf("olaresManifest.type is required")
	}
	if appCfg.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if appCfg.Metadata.AppID == "" {
		return fmt.Errorf("metadata.appid is required")
	}
	if appCfg.Metadata.Version == "" {
		return fmt.Errorf("metadata.version is required")
	}

	return nil
}

// checkServiceAccountRole validates service account and role configuration
func (lr *LocalRepo) checkServiceAccountRole(chartDir string, token string) error {
	// For now, we'll do basic validation
	// In a real implementation, you might want to check for specific service account files
	// or validate against Kubernetes RBAC rules

	templatesDir := filepath.Join(chartDir, "templates")
	if !lr.dirExists(templatesDir) {
		return fmt.Errorf("templates directory does not exist")
	}

	// Check for common service account related files
	serviceAccountFiles := []string{
		filepath.Join(templatesDir, "serviceaccount.yaml"),
		filepath.Join(templatesDir, "rbac.yaml"),
		filepath.Join(templatesDir, "clusterrole.yaml"),
		filepath.Join(templatesDir, "clusterrolebinding.yaml"),
		filepath.Join(templatesDir, "role.yaml"),
		filepath.Join(templatesDir, "rolebinding.yaml"),
	}

	// At least one service account related file should exist
	found := false
	for _, file := range serviceAccountFiles {
		if lr.fileExists(file) {
			found = true
			break
		}
	}

	if !found {
		log.Printf("Warning: No service account related files found in templates directory")
		// This is a warning, not an error, as some apps might not need service accounts
	}

	return nil
}

// Helper functions for validation
func (lr *LocalRepo) checkReservedWord(str string) bool {
	reservedWords := []string{
		"user", "system", "space", "default", "os", "kubesphere", "kube",
		"kubekey", "kubernetes", "gpu", "tapr", "bfl", "bytetrade",
		"project", "pod",
	}

	for _, word := range reservedWords {
		if strings.EqualFold(str, word) {
			return true
		}
	}

	return false
}

func (lr *LocalRepo) isValidFolderName(name string) bool {
	match, _ := regexp.MatchString("^[a-z0-9]{1,30}$", name)
	return match
}

func (lr *LocalRepo) fileExists(path string) bool {
	info, err := os.Stat(path)
	return (err == nil || os.IsExist(err)) && !info.IsDir()
}

func (lr *LocalRepo) dirExists(path string) bool {
	info, err := os.Stat(path)
	return (err == nil || os.IsExist(err)) && info.IsDir()
}

func (lr *LocalRepo) pathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func (lr *LocalRepo) isValidChartFields(chart Chart) error {
	if chart.APIVersion == "" {
		return fmt.Errorf("apiVersion field empty in Chart.yaml")
	}

	if chart.Name == "" {
		return fmt.Errorf("name field empty in Chart.yaml")
	}

	if chart.Version == "" {
		return fmt.Errorf("version field empty in Chart.yaml")
	}

	return nil
}

func (lr *LocalRepo) isValidMetadataFields(metadata AppMetaData, chart Chart, folder string) error {
	if chart.Name != folder || metadata.Name != folder {
		return fmt.Errorf("name in Chart.yaml:%s, chartFolder:%s, name in %s:%s must same",
			chart.Name, folder, AppCfgFileName, metadata.Name)
	}

	if metadata.Version != chart.Version {
		return fmt.Errorf("version in %s:%s, version in Chart.yaml:%s must same",
			AppCfgFileName, metadata.Version, chart.Version)
	}

	return nil
}

// renderManifest renders the manifest using app service
func (lr *LocalRepo) renderManifest(content, token string) (string, error) {
	// For local development, we'll return the content as-is
	// In production, you would call the app service API
	log.Printf("Rendering manifest for local development")
	return content, nil
}

// unArchive extracts files from an archive (copied from utils.UnArchive)
func (lr *LocalRepo) unArchive(src, dstDir string) error {
	err := lr.checkDir(dstDir)
	if err != nil {
		log.Printf("Warning: %v", err)
		return err
	}

	// For now, we'll implement a simple tar.gz extraction
	// In production, you might want to use a more robust library like archiver
	return lr.extractTarGz(src, dstDir)
}

// checkDir ensures a directory exists (copied from utils.CheckDir)
func (lr *LocalRepo) checkDir(dirname string) error {
	fi, err := os.Stat(dirname)
	if (err == nil || os.IsExist(err)) && fi.IsDir() {
		return nil
	}
	if os.IsExist(err) {
		return err
	}

	err = os.MkdirAll(dirname, 0755)
	return err
}

// extractTarGz extracts a tar.gz file
func (lr *LocalRepo) extractTarGz(src, dstDir string) error {
	file, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		target := filepath.Join(dstDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := lr.extractFile(tr, target, header.FileInfo().Mode()); err != nil {
				return fmt.Errorf("failed to extract file: %w", err)
			}
		}
	}

	return nil
}

// extractFile extracts a single file from tar reader
func (lr *LocalRepo) extractFile(tr *tar.Reader, target string, mode os.FileMode) error {
	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}

	// Create the file
	f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	// Copy the file content
	_, err = io.Copy(f, tr)
	return err
}

// parseAppInfo parses app information from the chart
func (lr *LocalRepo) parseAppInfo(chartDir string, token string) (*types.ApplicationInfoEntry, error) {
	appCfgFile := filepath.Join(chartDir, AppCfgFileName)

	// Read the configuration file
	content, err := os.ReadFile(appCfgFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", AppCfgFileName, err)
	}

	// Render the manifest
	renderedContent, err := lr.renderManifest(string(content), token)
	if err != nil {
		return nil, fmt.Errorf("failed to render manifest: %w", err)
	}

	// Parse the rendered configuration - using the correct structure that matches OlaresManifest.yaml
	var appCfg struct {
		ConfigVersion string `yaml:"olaresManifest.version"`
		ConfigType    string `yaml:"olaresManifest.type"`
		Metadata      struct {
			Name        string   `yaml:"name"`
			Icon        string   `yaml:"icon"`
			Description string   `yaml:"description"`
			AppID       string   `yaml:"appid"`
			Title       string   `yaml:"title"`
			Version     string   `yaml:"version"`
			Categories  []string `yaml:"categories"`
			Rating      float32  `yaml:"rating"`
			Target      string   `yaml:"target"`
		} `yaml:"metadata"`
		Spec struct {
			VersionName        string                 `yaml:"versionName"`
			FullDescription    string                 `yaml:"fullDescription"`
			UpgradeDescription string                 `yaml:"upgradeDescription"`
			PromoteImage       []string               `yaml:"promoteImage"`
			PromoteVideo       string                 `yaml:"promoteVideo"`
			SubCategory        string                 `yaml:"subCategory"`
			Developer          string                 `yaml:"developer"`
			RequiredMemory     string                 `yaml:"requiredMemory"`
			RequiredDisk       string                 `yaml:"requiredDisk"`
			SupportClient      map[string]interface{} `yaml:"supportClient"`
			SupportArch        []string               `yaml:"supportArch"`
			RequiredGPU        string                 `yaml:"requiredGPU"`
			RequiredCPU        string                 `yaml:"requiredCPU"`
			Locale             []string               `yaml:"locale"`
			Submitter          string                 `yaml:"submitter"`
			Doc                string                 `yaml:"doc"`
			Website            string                 `yaml:"website"`
			FeatureImage       string                 `yaml:"featuredImage"`
			SourceCode         string                 `yaml:"sourceCode"`
			ModelSize          string                 `yaml:"modelSize"`
			Namespace          string                 `yaml:"namespace"`
			OnlyAdmin          bool                   `yaml:"onlyAdmin"`
		} `yaml:"spec"`
		Permission map[string]interface{}   `yaml:"permission"`
		Middleware map[string]interface{}   `yaml:"middleware"`
		Options    map[string]interface{}   `yaml:"options"`
		Entrances  []map[string]interface{} `yaml:"entrances"`
	}

	if err := yaml.Unmarshal([]byte(renderedContent), &appCfg); err != nil {
		return nil, fmt.Errorf("failed to parse rendered %s: %w", AppCfgFileName, err)
	}

	// Validate required fields
	if appCfg.Metadata.Name == "" {
		return nil, fmt.Errorf("metadata.name is required")
	}
	if appCfg.Metadata.AppID == "" {
		return nil, fmt.Errorf("metadata.appid is required")
	}
	if appCfg.Metadata.Version == "" {
		return nil, fmt.Errorf("metadata.version is required")
	}

	// Create ApplicationInfoEntry with proper field mapping
	appInfo := &types.ApplicationInfoEntry{
		ID:                 appCfg.Metadata.AppID, // Use AppID as the primary ID
		AppID:              appCfg.Metadata.AppID,
		Name:               appCfg.Metadata.Name,
		CfgType:            appCfg.ConfigType,
		ChartName:          appCfg.Metadata.Name,
		Icon:               appCfg.Metadata.Icon,
		Description:        map[string]string{"en-US": appCfg.Metadata.Description},
		Title:              map[string]string{"en-US": appCfg.Metadata.Title},
		Version:            appCfg.Metadata.Version,
		Categories:         appCfg.Metadata.Categories,
		VersionName:        appCfg.Spec.VersionName,
		FullDescription:    map[string]string{"en-US": appCfg.Spec.FullDescription},
		UpgradeDescription: map[string]string{"en-US": appCfg.Spec.UpgradeDescription},
		PromoteImage:       appCfg.Spec.PromoteImage,
		PromoteVideo:       appCfg.Spec.PromoteVideo,
		SubCategory:        appCfg.Spec.SubCategory,
		Developer:          appCfg.Spec.Developer,
		RequiredMemory:     appCfg.Spec.RequiredMemory,
		RequiredDisk:       appCfg.Spec.RequiredDisk,
		SupportArch:        appCfg.Spec.SupportArch,
		RequiredGPU:        appCfg.Spec.RequiredGPU,
		RequiredCPU:        appCfg.Spec.RequiredCPU,
		Rating:             appCfg.Metadata.Rating,
		Target:             appCfg.Metadata.Target,
		Locale:             appCfg.Spec.Locale,
		Submitter:          appCfg.Spec.Submitter,
		Doc:                appCfg.Spec.Doc,
		Website:            appCfg.Spec.Website,
		FeaturedImage:      appCfg.Spec.FeatureImage,
		SourceCode:         appCfg.Spec.SourceCode,
		ModelSize:          appCfg.Spec.ModelSize,
		Namespace:          appCfg.Spec.Namespace,
		OnlyAdmin:          appCfg.Spec.OnlyAdmin,
		CreateTime:         time.Now().Unix(),
		UpdateTime:         time.Now().Unix(),
		Metadata:           make(map[string]interface{}),
	}

	// Store only essential metadata to avoid circular references
	appInfo.Metadata["config_version"] = appCfg.ConfigVersion
	appInfo.Metadata["config_type"] = appCfg.ConfigType
	appInfo.Metadata["parsed_at"] = time.Now().Unix()

	// Safely handle map[string]interface{} fields to avoid circular references
	if appCfg.Spec.SupportClient != nil {
		log.Printf("DEBUG: Processing SupportClient field, length: %d", len(appCfg.Spec.SupportClient))
		// Create a safe copy of SupportClient
		supportClientCopy := make(map[string]interface{})
		for k, v := range appCfg.Spec.SupportClient {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: SupportClient[%s] = %v (type: %T)", k, v, v)
					supportClientCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					supportClientCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping SupportClient[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping SupportClient[%s] to avoid circular reference", k)
			}
		}
		appInfo.SupportClient = supportClientCopy
		log.Printf("DEBUG: SupportClient copy completed, length: %d", len(supportClientCopy))
	}

	if appCfg.Permission != nil {
		log.Printf("DEBUG: Processing Permission field, length: %d", len(appCfg.Permission))
		// Create a safe copy of Permission
		permissionCopy := make(map[string]interface{})
		for k, v := range appCfg.Permission {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Permission[%s] = %v (type: %T)", k, v, v)
					permissionCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					permissionCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Permission[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Permission[%s] to avoid circular reference", k)
			}
		}
		appInfo.Permission = permissionCopy
		log.Printf("DEBUG: Permission copy completed, length: %d", len(permissionCopy))
	}

	if appCfg.Middleware != nil {
		log.Printf("DEBUG: Processing Middleware field, length: %d", len(appCfg.Middleware))
		// Create a safe copy of Middleware
		middlewareCopy := make(map[string]interface{})
		for k, v := range appCfg.Middleware {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Middleware[%s] = %v (type: %T)", k, v, v)
					middlewareCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					middlewareCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Middleware[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Middleware[%s] to avoid circular reference", k)
			}
		}
		appInfo.Middleware = middlewareCopy
		log.Printf("DEBUG: Middleware copy completed, length: %d", len(middlewareCopy))
	}

	if appCfg.Options != nil {
		log.Printf("DEBUG: Processing Options field, length: %d", len(appCfg.Options))
		// Create a safe copy of Options
		optionsCopy := make(map[string]interface{})
		for k, v := range appCfg.Options {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Options[%s] = %v (type: %T)", k, v, v)
					optionsCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					optionsCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Options[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Options[%s] to avoid circular reference", k)
			}
		}
		appInfo.Options = optionsCopy
		log.Printf("DEBUG: Options copy completed, length: %d", len(optionsCopy))
	}

	if appCfg.Entrances != nil {
		log.Printf("DEBUG: Processing Entrances field, length: %d", len(appCfg.Entrances))
		// Create a safe copy of Entrances
		entrancesCopy := make([]map[string]interface{}, len(appCfg.Entrances))
		for i, entrance := range appCfg.Entrances {
			if entrance != nil {
				log.Printf("DEBUG: Processing entrance[%d], length: %d", i, len(entrance))
				entranceCopy := make(map[string]interface{})
				for k, v := range entrance {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							log.Printf("DEBUG: Entrance[%d][%s] = %v (type: %T)", i, k, v, v)
							entranceCopy[k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							entranceCopy[k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping Entrance[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping Entrance[%d][%s] to avoid circular reference", i, k)
					}
				}
				entrancesCopy[i] = entranceCopy
				log.Printf("DEBUG: Entrance[%d] copy completed, length: %d", i, len(entranceCopy))
			}
		}
		appInfo.Entrances = entrancesCopy
		log.Printf("DEBUG: Entrances copy completed, total entrances: %d", len(entrancesCopy))
	}

	// Load i18n information if available
	if err := lr.loadI18nInfo(appInfo, chartDir); err != nil {
		log.Printf("Warning: failed to load i18n info: %v", err)
	}

	return appInfo, nil
}

// loadI18nInfo loads internationalization information
func (lr *LocalRepo) loadI18nInfo(appInfo *types.ApplicationInfoEntry, chartDir string) error {
	if len(appInfo.Locale) == 0 {
		log.Printf("DEBUG: No locale specified, skipping i18n loading")
		return nil
	}

	log.Printf("DEBUG: Loading i18n info for locales: %v", appInfo.Locale)
	i18nMap := make(map[string]interface{})
	for _, lang := range appInfo.Locale {
		i18nPath := filepath.Join(chartDir, "i18n", lang, AppCfgFileName)
		log.Printf("DEBUG: Checking i18n path: %s", i18nPath)
		if !lr.pathExists(i18nPath) {
			log.Printf("DEBUG: i18n file not found: %s", i18nPath)
			continue
		}

		content, err := os.ReadFile(i18nPath)
		if err != nil {
			log.Printf("Warning: failed to read i18n file %s: %v", i18nPath, err)
			continue
		}

		var i18nData map[string]interface{}
		if err := yaml.Unmarshal(content, &i18nData); err != nil {
			log.Printf("Warning: failed to parse i18n file %s: %v", i18nPath, err)
			continue
		}

		log.Printf("DEBUG: Successfully parsed i18n data for %s, length: %d", lang, len(i18nData))
		// Create a safe copy of i18n data to avoid circular references
		safeI18nData := make(map[string]interface{})
		for k, v := range i18nData {
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: i18n[%s][%s] = %v (type: %T)", lang, k, v, v)
					safeI18nData[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					safeI18nData[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping i18n[%s][%s] with complex type %T", lang, k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping i18n[%s][%s] to avoid circular reference", lang, k)
			}
		}
		i18nMap[lang] = safeI18nData
		log.Printf("DEBUG: Safe i18n data for %s, length: %d", lang, len(safeI18nData))
	}

	if len(i18nMap) > 0 {
		log.Printf("DEBUG: Setting i18n data, total languages: %d", len(i18nMap))
		appInfo.I18n = i18nMap
	} else {
		log.Printf("DEBUG: No i18n data to set")
	}

	return nil
}

// storeAppInfo stores the app information in the cache
func (lr *LocalRepo) storeAppInfo(userID, sourceID string, appInfo *types.ApplicationInfoEntry, chartDir string) error {
	// Step 1: Create chart package and store it in CHART_ROOT
	chartPackagePath, err := lr.createChartPackage(appInfo, chartDir, sourceID)
	if err != nil {
		return fmt.Errorf("failed to create chart package: %w", err)
	}

	// Step 2: Create a completely safe copy of ApplicationInfoEntry to avoid any circular references
	safeAppInfo := lr.createSafeApplicationInfoEntryCopy(appInfo)

	// Step 3: Convert ApplicationInfoEntry to map for cache storage
	appDataMap := lr.convertApplicationInfoEntryToMap(safeAppInfo)

	// Add the chart package path to the data
	appDataMap["raw_package"] = chartPackagePath
	appDataMap["rendered_package"] = chartDir

	// Step 4: Store in cache - pass the app data directly, not wrapped in app_info
	if err := lr.cacheManager.SetAppData(userID, sourceID, types.AppInfoLatestPending, appDataMap); err != nil {
		return fmt.Errorf("failed to store app data in cache: %w", err)
	}

	// Step 5: Create hydration task if hydrator is available
	if lr.hydrator != nil {
		if err := lr.createHydrationTask(userID, sourceID, safeAppInfo); err != nil {
			log.Printf("Warning: failed to create hydration task for app %s: %v", appInfo.Name, err)
			// Don't return error here as the main operation (storing app info) was successful
		}
	} else {
		log.Printf("Warning: hydrator not set, skipping hydration task creation for app %s", appInfo.Name)
	}

	log.Printf("Successfully stored app info for %s in cache (user: %s, source: %s)", appInfo.Name, userID, sourceID)
	return nil
}

// createChartPackage creates a chart package and stores it in CHART_ROOT/sourceID/appName-appVersion.tgz
func (lr *LocalRepo) createChartPackage(appInfo *types.ApplicationInfoEntry, chartDir string, sourceID string) (string, error) {
	// Get CHART_ROOT environment variable
	chartRoot := os.Getenv("CHART_ROOT")
	if chartRoot == "" {
		return "", fmt.Errorf("CHART_ROOT environment variable is not set")
	}

	// Create source directory path: CHART_ROOT/sourceID/
	sourceDir := filepath.Join(chartRoot, sourceID)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create source directory: %w", err)
	}

	// Build chart package filename: appName-appVersion.tgz
	chartFileName := fmt.Sprintf("%s-%s.tgz", appInfo.Name, appInfo.Version)
	chartPackagePath := filepath.Join(sourceDir, chartFileName)

	// Check if chart package already exists
	if lr.pathExists(chartPackagePath) {
		log.Printf("Chart package already exists: %s", chartPackagePath)
		return chartPackagePath, nil
	}

	// Create temporary directory for packaging
	tempDir, err := lr.createTempDir("chart_package")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory for packaging: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create the chart package by archiving the chart directory
	if err := lr.archiveChartDirectory(chartDir, chartPackagePath); err != nil {
		return "", fmt.Errorf("failed to create chart package: %w", err)
	}

	log.Printf("Created chart package: %s", chartPackagePath)
	return chartPackagePath, nil
}

// archiveChartDirectory creates a tar.gz archive from the chart directory
func (lr *LocalRepo) archiveChartDirectory(chartDir string, outputPath string) error {
	// Get the chart directory name (should be the app name)
	chartDirName := filepath.Base(chartDir)

	// Create a temporary directory to structure the archive properly
	tempDir, err := lr.createTempDir("archive_temp")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Create the chart directory structure in temp
	tempChartDir := filepath.Join(tempDir, chartDirName)
	if err := os.MkdirAll(tempChartDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp chart directory: %w", err)
	}

	// Copy all files from chartDir to tempChartDir
	if err := lr.copyDirectory(chartDir, tempChartDir); err != nil {
		return fmt.Errorf("failed to copy chart directory: %w", err)
	}

	// Create the tar.gz archive
	if err := lr.createTarGzArchive(tempDir, outputPath); err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}

	return nil
}

// createTarGzArchive creates a tar.gz archive from a directory
func (lr *LocalRepo) createTarGzArchive(sourceDir string, outputPath string) error {
	// Create the output file
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(outputFile)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Walk through the source directory and add files to the archive
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get the relative path for the archive
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, relPath)
		if err != nil {
			return fmt.Errorf("failed to create tar header: %w", err)
		}

		// Set the name to the relative path
		header.Name = relPath

		// Write the header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header: %w", err)
		}

		// If it's a regular file, write the content
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", path, err)
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("failed to copy file content: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	return nil
}

// copyDirectory copies all files and subdirectories from src to dst
func (lr *LocalRepo) copyDirectory(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Create directory and copy contents
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := lr.copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Copy file
			if err := lr.copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single file from src to dst
func (lr *LocalRepo) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// createHydrationTask creates a hydration task for the uploaded app
func (lr *LocalRepo) createHydrationTask(userID, sourceID string, appInfo *types.ApplicationInfoEntry) error {
	// Convert ApplicationInfoEntry to map for task creation
	appDataMap := lr.convertApplicationInfoEntryToMap(appInfo)

	// Create hydration task
	task := hydrationfn.NewHydrationTask(
		userID, sourceID, appInfo.AppID,
		appDataMap, lr.cacheManager.cache, nil, // settingsManager will be nil for local tasks
	)

	// Enqueue the task
	if err := lr.hydrator.EnqueueTask(task); err != nil {
		return fmt.Errorf("failed to enqueue hydration task: %w", err)
	}

	log.Printf("Successfully created hydration task for app %s (user: %s, source: %s)", appInfo.Name, userID, sourceID)
	return nil
}

// convertApplicationInfoEntryToMap converts ApplicationInfoEntry to map for task creation
func (lr *LocalRepo) convertApplicationInfoEntryToMap(entry *types.ApplicationInfoEntry) map[string]interface{} {
	if entry == nil {
		log.Printf("DEBUG: convertApplicationInfoEntryToMap called with nil entry")
		return make(map[string]interface{})
	}

	log.Printf("DEBUG: Converting ApplicationInfoEntry to map for app: %s", entry.Name)

	// Create a safe map that avoids potential circular references
	result := map[string]interface{}{
		"id":          entry.ID,
		"name":        entry.Name,
		"cfgType":     entry.CfgType,
		"chartName":   entry.ChartName,
		"icon":        entry.Icon,
		"appID":       entry.AppID,
		"version":     entry.Version,
		"categories":  entry.Categories,
		"versionName": entry.VersionName,

		"promoteImage":   entry.PromoteImage,
		"promoteVideo":   entry.PromoteVideo,
		"subCategory":    entry.SubCategory,
		"locale":         entry.Locale,
		"developer":      entry.Developer,
		"requiredMemory": entry.RequiredMemory,
		"requiredDisk":   entry.RequiredDisk,
		"supportArch":    entry.SupportArch,
		"requiredGPU":    entry.RequiredGPU,
		"requiredCPU":    entry.RequiredCPU,
		"rating":         entry.Rating,
		"target":         entry.Target,

		"submitter":     entry.Submitter,
		"doc":           entry.Doc,
		"website":       entry.Website,
		"featuredImage": entry.FeaturedImage,
		"sourceCode":    entry.SourceCode,

		"modelSize": entry.ModelSize,
		"namespace": entry.Namespace,
		"onlyAdmin": entry.OnlyAdmin,

		"lastCommitHash": entry.LastCommitHash,
		"createTime":     entry.CreateTime,
		"updateTime":     entry.UpdateTime,
		"appLabels":      entry.AppLabels,

		"screenshots": entry.Screenshots,
		"tags":        entry.Tags,
		"updated_at":  entry.UpdatedAt,
	}

	log.Printf("DEBUG: Basic fields converted, processing map fields...")

	// Safely handle map fields that might contain circular references
	if entry.Description != nil {
		// Create a copy of the description map
		descCopy := make(map[string]string)
		for k, v := range entry.Description {
			descCopy[k] = v
		}
		result["description"] = descCopy
	}

	if entry.Title != nil {
		// Create a copy of the title map
		titleCopy := make(map[string]string)
		for k, v := range entry.Title {
			titleCopy[k] = v
		}
		result["title"] = titleCopy
	}

	if entry.FullDescription != nil {
		// Create a copy of the full description map
		fullDescCopy := make(map[string]string)
		for k, v := range entry.FullDescription {
			fullDescCopy[k] = v
		}
		result["fullDescription"] = fullDescCopy
	}

	if entry.UpgradeDescription != nil {
		// Create a copy of the upgrade description map
		upgradeDescCopy := make(map[string]string)
		for k, v := range entry.UpgradeDescription {
			upgradeDescCopy[k] = v
		}
		result["upgradeDescription"] = upgradeDescCopy
	}

	// Handle interface{} fields safely
	if entry.SupportClient != nil {
		// Create a safe copy of SupportClient to avoid circular references
		supportClientCopy := make(map[string]interface{})
		for k, v := range entry.SupportClient {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: SupportClient[%s] = %v (type: %T)", k, v, v)
					supportClientCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					supportClientCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping SupportClient[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping SupportClient[%s] to avoid circular reference", k)
			}
		}
		result["supportClient"] = supportClientCopy
		log.Printf("DEBUG: SupportClient copy completed, length: %d", len(supportClientCopy))
	}

	if entry.Permission != nil {
		// Create a safe copy of Permission to avoid circular references
		permissionCopy := make(map[string]interface{})
		for k, v := range entry.Permission {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Permission[%s] = %v (type: %T)", k, v, v)
					permissionCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					permissionCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Permission[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Permission[%s] to avoid circular reference", k)
			}
		}
		result["permission"] = permissionCopy
		log.Printf("DEBUG: Permission copy completed, length: %d", len(permissionCopy))
	}

	if entry.Middleware != nil {
		// Create a safe copy of Middleware to avoid circular references
		middlewareCopy := make(map[string]interface{})
		for k, v := range entry.Middleware {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Middleware[%s] = %v (type: %T)", k, v, v)
					middlewareCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					middlewareCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Middleware[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Middleware[%s] to avoid circular reference", k)
			}
		}
		result["middleware"] = middlewareCopy
		log.Printf("DEBUG: Middleware copy completed, length: %d", len(middlewareCopy))
	}

	if entry.Options != nil {
		// Create a safe copy of Options to avoid circular references
		log.Printf("DEBUG: Processing Options field, length: %d", len(entry.Options))
		optionsCopy := make(map[string]interface{})
		for k, v := range entry.Options {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Options[%s] = %v (type: %T)", k, v, v)
					optionsCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					optionsCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Options[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Options[%s] to avoid circular reference", k)
			}
		}
		result["options"] = optionsCopy
		log.Printf("DEBUG: Options copy completed, length: %d", len(optionsCopy))
	}

	if entry.Entrances != nil {
		// Create a safe copy of Entrances to avoid circular references
		entrancesCopy := make([]map[string]interface{}, len(entry.Entrances))
		for i, entrance := range entry.Entrances {
			if entrance != nil {
				log.Printf("DEBUG: Processing entrance[%d], length: %d", i, len(entrance))
				entranceCopy := make(map[string]interface{})
				for k, v := range entrance {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							log.Printf("DEBUG: Entrance[%d][%s] = %v (type: %T)", i, k, v, v)
							entranceCopy[k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							entranceCopy[k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping Entrance[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping Entrance[%d][%s] to avoid circular reference", i, k)
					}
				}
				entrancesCopy[i] = entranceCopy
				log.Printf("DEBUG: Entrance[%d] copy completed, length: %d", i, len(entranceCopy))
			}
		}
		result["entrances"] = entrancesCopy
		log.Printf("DEBUG: Entrances copy completed, total entrances: %d", len(entrancesCopy))
	}

	if entry.License != nil {
		// Create a safe copy of License to avoid circular references
		licenseCopy := make([]map[string]interface{}, len(entry.License))
		for i, license := range entry.License {
			if license != nil {
				log.Printf("DEBUG: Processing license[%d], length: %d", i, len(license))
				licenseItemCopy := make(map[string]interface{})
				for k, v := range license {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							log.Printf("DEBUG: License[%d][%s] = %v (type: %T)", i, k, v, v)
							licenseItemCopy[k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							licenseItemCopy[k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping License[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping License[%d][%s] to avoid circular reference", i, k)
					}
				}
				licenseCopy[i] = licenseItemCopy
				log.Printf("DEBUG: License[%d] copy completed, length: %d", i, len(licenseItemCopy))
			}
		}
		result["license"] = licenseCopy
		log.Printf("DEBUG: License copy completed, total licenses: %d", len(licenseCopy))
	}

	if entry.Legal != nil {
		// Create a safe copy of Legal to avoid circular references
		legalCopy := make([]map[string]interface{}, len(entry.Legal))
		for i, legal := range entry.Legal {
			if legal != nil {
				log.Printf("DEBUG: Processing legal[%d], length: %d", i, len(legal))
				legalItemCopy := make(map[string]interface{})
				for k, v := range legal {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							log.Printf("DEBUG: Legal[%d][%s] = %v (type: %T)", i, k, v, v)
							legalItemCopy[k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							legalItemCopy[k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping Legal[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping Legal[%d][%s] to avoid circular reference", i, k)
					}
				}
				legalCopy[i] = legalItemCopy
				log.Printf("DEBUG: Legal[%d] copy completed, length: %d", i, len(legalItemCopy))
			}
		}
		result["legal"] = legalCopy
		log.Printf("DEBUG: Legal copy completed, total legal items: %d", len(legalCopy))
	}

	if entry.I18n != nil {
		// Create a safe copy of I18n to avoid circular references
		i18nCopy := make(map[string]interface{})
		for k, v := range entry.I18n {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Create a deep copy of the value to avoid circular references
				if mapValue, ok := v.(map[string]interface{}); ok {
					log.Printf("DEBUG: Processing i18n[%s], length: %d", k, len(mapValue))
					safeMap := make(map[string]interface{})
					for mk, mv := range mapValue {
						if mk != "source_data" && mk != "raw_data" && mk != "app_info" {
							// Only copy simple types to avoid circular references
							switch val := mv.(type) {
							case string, int, int64, float64, bool, []string, []interface{}:
								log.Printf("DEBUG: i18n[%s][%s] = %v (type: %T)", k, mk, mv, mv)
								safeMap[mk] = mv
							case map[string]interface{}:
								// Create a shallow copy of nested maps, excluding problematic keys
								nestedCopy := make(map[string]interface{})
								for nk, nv := range val {
									if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
										nestedCopy[nk] = nv
									}
								}
								safeMap[mk] = nestedCopy
							default:
								log.Printf("DEBUG: Skipping i18n[%s][%s] with complex type %T", k, mk, mv)
							}
						} else {
							log.Printf("DEBUG: Skipping i18n[%s][%s] to avoid circular reference", k, mk)
						}
					}
					i18nCopy[k] = safeMap
					log.Printf("DEBUG: i18n[%s] copy completed, length: %d", k, len(safeMap))
				} else {
					log.Printf("DEBUG: i18n[%s] is not a map, copying as-is", k)
					i18nCopy[k] = v
				}
			} else {
				log.Printf("DEBUG: Skipping i18n[%s] to avoid circular reference", k)
			}
		}
		result["i18n"] = i18nCopy
		log.Printf("DEBUG: i18n copy completed, total languages: %d", len(i18nCopy))
	}

	if entry.Count != nil {
		// Create a safe copy of Count to avoid circular references
		if countMap, ok := entry.Count.(map[string]interface{}); ok {
			log.Printf("DEBUG: Processing Count field, length: %d", len(countMap))
			countCopy := make(map[string]interface{})
			for k, v := range countMap {
				// Skip any potential circular references and complex nested structures
				if k != "source_data" && k != "raw_data" && k != "app_info" {
					// Only copy simple types to avoid circular references
					switch val := v.(type) {
					case string, int, int64, float64, bool, []string, []interface{}:
						log.Printf("DEBUG: Count[%s] = %v (type: %T)", k, v, v)
						countCopy[k] = v
					case map[string]interface{}:
						// Create a shallow copy of nested maps, excluding problematic keys
						nestedCopy := make(map[string]interface{})
						for nk, nv := range val {
							if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
								nestedCopy[nk] = nv
							}
						}
						countCopy[k] = nestedCopy
					default:
						log.Printf("DEBUG: Skipping Count[%s] with complex type %T", k, v)
					}
				} else {
					log.Printf("DEBUG: Skipping Count[%s] to avoid circular reference", k)
				}
			}
			result["count"] = countCopy
			log.Printf("DEBUG: Count copy completed, length: %d", len(countCopy))
		} else {
			log.Printf("DEBUG: Count is not a map, copying as-is")
			result["count"] = entry.Count
		}
	}

	// Handle metadata field safely - create a copy to avoid circular references
	if entry.Metadata != nil {
		log.Printf("DEBUG: Processing Metadata field, length: %d", len(entry.Metadata))
		metadataCopy := make(map[string]interface{})
		for k, v := range entry.Metadata {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					log.Printf("DEBUG: Metadata[%s] = %v (type: %T)", k, v, v)
					metadataCopy[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					metadataCopy[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Metadata[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Metadata[%s] to avoid circular reference", k)
			}
		}
		result["metadata"] = metadataCopy
		log.Printf("DEBUG: Metadata copy completed, length: %d", len(metadataCopy))
	}

	return result
}

// createSafeApplicationInfoEntryCopy creates a completely safe copy of ApplicationInfoEntry to avoid any circular references
func (lr *LocalRepo) createSafeApplicationInfoEntryCopy(entry *types.ApplicationInfoEntry) *types.ApplicationInfoEntry {
	if entry == nil {
		return nil
	}

	log.Printf("DEBUG: Creating safe copy of ApplicationInfoEntry for app: %s", entry.Name)

	// Create a new ApplicationInfoEntry with the same fields
	safeAppInfo := &types.ApplicationInfoEntry{
		ID:                 entry.ID,
		AppID:              entry.AppID,
		Name:               entry.Name,
		CfgType:            entry.CfgType,
		ChartName:          entry.ChartName,
		Icon:               entry.Icon,
		Description:        make(map[string]string),
		Title:              make(map[string]string),
		Version:            entry.Version,
		Categories:         append([]string{}, entry.Categories...),
		VersionName:        entry.VersionName,
		FullDescription:    make(map[string]string),
		UpgradeDescription: make(map[string]string),
		PromoteImage:       append([]string{}, entry.PromoteImage...),
		PromoteVideo:       entry.PromoteVideo,
		SubCategory:        entry.SubCategory,
		Developer:          entry.Developer,
		RequiredMemory:     entry.RequiredMemory,
		RequiredDisk:       entry.RequiredDisk,
		SupportArch:        append([]string{}, entry.SupportArch...),
		RequiredGPU:        entry.RequiredGPU,
		RequiredCPU:        entry.RequiredCPU,
		Rating:             entry.Rating,
		Target:             entry.Target,
		Locale:             append([]string{}, entry.Locale...),
		Submitter:          entry.Submitter,
		Doc:                entry.Doc,
		Website:            entry.Website,
		FeaturedImage:      entry.FeaturedImage,
		SourceCode:         entry.SourceCode,
		ModelSize:          entry.ModelSize,
		Namespace:          entry.Namespace,
		OnlyAdmin:          entry.OnlyAdmin,
		CreateTime:         entry.CreateTime,
		UpdateTime:         entry.UpdateTime,
		LastCommitHash:     entry.LastCommitHash,
		AppLabels:          append([]string{}, entry.AppLabels...),
		Screenshots:        append([]string{}, entry.Screenshots...),
		Tags:               append([]string{}, entry.Tags...),
		UpdatedAt:          entry.UpdatedAt,
		Metadata:           make(map[string]interface{}),
	}

	// Copy all fields from the original entry to the new one
	safeAppInfo.Metadata["config_version"] = entry.Metadata["config_version"]
	safeAppInfo.Metadata["config_type"] = entry.Metadata["config_type"]
	safeAppInfo.Metadata["parsed_at"] = entry.Metadata["parsed_at"]

	// Safely handle map fields that might contain circular references
	if entry.Description != nil {
		for k, v := range entry.Description {
			safeAppInfo.Description[k] = v
		}
	}

	if entry.Title != nil {
		for k, v := range entry.Title {
			safeAppInfo.Title[k] = v
		}
	}

	if entry.FullDescription != nil {
		for k, v := range entry.FullDescription {
			safeAppInfo.FullDescription[k] = v
		}
	}

	if entry.UpgradeDescription != nil {
		for k, v := range entry.UpgradeDescription {
			safeAppInfo.UpgradeDescription[k] = v
		}
	}

	// Handle interface{} fields safely
	if entry.SupportClient != nil {
		safeAppInfo.SupportClient = make(map[string]interface{})
		for k, v := range entry.SupportClient {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					safeAppInfo.SupportClient[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					safeAppInfo.SupportClient[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping SupportClient[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping SupportClient[%s] to avoid circular reference", k)
			}
		}
	}

	if entry.Permission != nil {
		safeAppInfo.Permission = make(map[string]interface{})
		for k, v := range entry.Permission {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					safeAppInfo.Permission[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					safeAppInfo.Permission[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Permission[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Permission[%s] to avoid circular reference", k)
			}
		}
	}

	if entry.Middleware != nil {
		safeAppInfo.Middleware = make(map[string]interface{})
		for k, v := range entry.Middleware {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					safeAppInfo.Middleware[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					safeAppInfo.Middleware[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Middleware[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Middleware[%s] to avoid circular reference", k)
			}
		}
	}

	if entry.Options != nil {
		safeAppInfo.Options = make(map[string]interface{})
		for k, v := range entry.Options {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					safeAppInfo.Options[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					safeAppInfo.Options[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Options[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Options[%s] to avoid circular reference", k)
			}
		}
	}

	if entry.Entrances != nil {
		safeAppInfo.Entrances = make([]map[string]interface{}, len(entry.Entrances))
		for i, entrance := range entry.Entrances {
			if entrance != nil {
				safeAppInfo.Entrances[i] = make(map[string]interface{})
				for k, v := range entrance {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							safeAppInfo.Entrances[i][k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							safeAppInfo.Entrances[i][k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping Entrance[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping Entrance[%d][%s] to avoid circular reference", i, k)
					}
				}
			}
		}
	}

	if entry.License != nil {
		safeAppInfo.License = make([]map[string]interface{}, len(entry.License))
		for i, license := range entry.License {
			if license != nil {
				safeAppInfo.License[i] = make(map[string]interface{})
				for k, v := range license {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							safeAppInfo.License[i][k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							safeAppInfo.License[i][k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping License[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping License[%d][%s] to avoid circular reference", i, k)
					}
				}
			}
		}
	}

	if entry.Legal != nil {
		safeAppInfo.Legal = make([]map[string]interface{}, len(entry.Legal))
		for i, legal := range entry.Legal {
			if legal != nil {
				safeAppInfo.Legal[i] = make(map[string]interface{})
				for k, v := range legal {
					// Skip any potential circular references and complex nested structures
					if k != "source_data" && k != "raw_data" && k != "app_info" {
						// Only copy simple types to avoid circular references
						switch val := v.(type) {
						case string, int, int64, float64, bool, []string, []interface{}:
							safeAppInfo.Legal[i][k] = v
						case map[string]interface{}:
							// Create a shallow copy of nested maps, excluding problematic keys
							nestedCopy := make(map[string]interface{})
							for nk, nv := range val {
								if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
									nestedCopy[nk] = nv
								}
							}
							safeAppInfo.Legal[i][k] = nestedCopy
						default:
							log.Printf("DEBUG: Skipping Legal[%d][%s] with complex type %T", i, k, v)
						}
					} else {
						log.Printf("DEBUG: Skipping Legal[%d][%s] to avoid circular reference", i, k)
					}
				}
			}
		}
	}

	if entry.I18n != nil {
		safeAppInfo.I18n = make(map[string]interface{})
		for k, v := range entry.I18n {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Create a deep copy of the value to avoid circular references
				if mapValue, ok := v.(map[string]interface{}); ok {
					safeMap := make(map[string]interface{})
					for mk, mv := range mapValue {
						if mk != "source_data" && mk != "raw_data" && mk != "app_info" {
							// Only copy simple types to avoid circular references
							switch val := mv.(type) {
							case string, int, int64, float64, bool, []string, []interface{}:
								safeMap[mk] = mv
							case map[string]interface{}:
								// Create a shallow copy of nested maps, excluding problematic keys
								nestedCopy := make(map[string]interface{})
								for nk, nv := range val {
									if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
										nestedCopy[nk] = nv
									}
								}
								safeMap[mk] = nestedCopy
							default:
								log.Printf("DEBUG: Skipping i18n[%s][%s] with complex type %T", k, mk, mv)
							}
						} else {
							log.Printf("DEBUG: Skipping i18n[%s][%s] to avoid circular reference", k, mk)
						}
					}
					safeAppInfo.I18n[k] = safeMap
				} else {
					log.Printf("DEBUG: i18n[%s] is not a map, copying as-is", k)
					safeAppInfo.I18n[k] = v
				}
			} else {
				log.Printf("DEBUG: Skipping i18n[%s] to avoid circular reference", k)
			}
		}
	}

	if entry.Count != nil {
		safeAppInfo.Count = entry.Count
	}

	if entry.VersionHistory != nil {
		safeAppInfo.VersionHistory = make([]*types.VersionInfo, len(entry.VersionHistory))
		for i, versionInfo := range entry.VersionHistory {
			if versionInfo != nil {
				safeAppInfo.VersionHistory[i] = &types.VersionInfo{
					ID:                 versionInfo.ID,
					AppName:            versionInfo.AppName,
					Version:            versionInfo.Version,
					VersionName:        versionInfo.VersionName,
					MergedAt:           versionInfo.MergedAt,
					UpgradeDescription: versionInfo.UpgradeDescription,
				}
			}
		}
	}

	if entry.Metadata != nil {
		safeAppInfo.Metadata = make(map[string]interface{})
		for k, v := range entry.Metadata {
			// Skip any potential circular references and complex nested structures
			if k != "source_data" && k != "raw_data" && k != "app_info" {
				// Only copy simple types to avoid circular references
				switch val := v.(type) {
				case string, int, int64, float64, bool, []string, []interface{}:
					safeAppInfo.Metadata[k] = v
				case map[string]interface{}:
					// Create a shallow copy of nested maps, excluding problematic keys
					nestedCopy := make(map[string]interface{})
					for nk, nv := range val {
						if nk != "source_data" && nk != "raw_data" && nk != "app_info" {
							nestedCopy[nk] = nv
						}
					}
					safeAppInfo.Metadata[k] = nestedCopy
				default:
					log.Printf("DEBUG: Skipping Metadata[%s] with complex type %T", k, v)
				}
			} else {
				log.Printf("DEBUG: Skipping Metadata[%s] to avoid circular reference", k)
			}
		}
	}

	return safeAppInfo
}
