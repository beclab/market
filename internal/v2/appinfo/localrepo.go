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
func (lr *LocalRepo) UploadAppPackage(userID, sourceID string, fileBytes []byte, filename string, token string) error {
	log.Printf("Processing uploaded chart package: %s for user: %s, source: %s", filename, userID, sourceID)

	// Step 1: Create temporary directory for processing
	tempDir, err := lr.createTempDir(filename)
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Step 2: Write uploaded file to temp directory
	tempFilePath := filepath.Join(tempDir, filename)
	if err := os.WriteFile(tempFilePath, fileBytes, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Step 3: Extract chart package
	extractDir := filepath.Join(tempDir, "extracted")
	if err := lr.unArchive(tempFilePath, extractDir); err != nil {
		return fmt.Errorf("failed to extract chart package: %w", err)
	}

	// Step 4: Find and validate chart directory
	chartDir := lr.findChartPath(extractDir)
	if chartDir == "" {
		return fmt.Errorf("no valid chart directory found in package")
	}

	// Step 5: Validate chart structure and configuration
	if err := lr.validateChart(chartDir, token); err != nil {
		return fmt.Errorf("chart validation failed: %w", err)
	}

	// Step 6: Parse app information from chart
	appInfo, err := lr.parseAppInfo(chartDir, token)
	if err != nil {
		return fmt.Errorf("failed to parse app info: %w", err)
	}

	// Step 7: Convert to AppInfoLatestData and store in cache
	if err := lr.storeAppInfo(userID, sourceID, appInfo, chartDir); err != nil {
		return fmt.Errorf("failed to store app info: %w", err)
	}

	log.Printf("Successfully processed and stored chart package: %s", filename)
	return nil
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
	if !lr.isValidFolderName(folderName) {
		return fmt.Errorf("invalid folder name: '%s' must '^[a-z0-9]{1,30}$'", folder)
	}

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
		SupportClient:      appCfg.Spec.SupportClient,
		SupportArch:        appCfg.Spec.SupportArch,
		RequiredGPU:        appCfg.Spec.RequiredGPU,
		RequiredCPU:        appCfg.Spec.RequiredCPU,
		Rating:             appCfg.Metadata.Rating,
		Target:             appCfg.Metadata.Target,
		Permission:         appCfg.Permission,
		Middleware:         appCfg.Middleware,
		Options:            appCfg.Options,
		Entrances:          appCfg.Entrances,
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

	// Load i18n information if available
	if err := lr.loadI18nInfo(appInfo, chartDir); err != nil {
		log.Printf("Warning: failed to load i18n info: %v", err)
	}

	return appInfo, nil
}

// loadI18nInfo loads internationalization information
func (lr *LocalRepo) loadI18nInfo(appInfo *types.ApplicationInfoEntry, chartDir string) error {
	if len(appInfo.Locale) == 0 {
		return nil
	}

	i18nMap := make(map[string]interface{})
	for _, lang := range appInfo.Locale {
		i18nPath := filepath.Join(chartDir, "i18n", lang, AppCfgFileName)
		if !lr.pathExists(i18nPath) {
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

		i18nMap[lang] = i18nData
	}

	if len(i18nMap) > 0 {
		appInfo.I18n = i18nMap
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

	// Step 2: Convert ApplicationInfoEntry to map for cache storage
	appDataMap := lr.convertApplicationInfoEntryToMap(appInfo)

	// Add the chart package path to the data
	appDataMap["raw_package"] = chartPackagePath
	appDataMap["rendered_package"] = chartDir

	// Step 3: Store in cache - pass the app data directly, not wrapped in app_info
	if err := lr.cacheManager.SetAppData(userID, sourceID, types.AppInfoLatestPending, appDataMap); err != nil {
		return fmt.Errorf("failed to store app data in cache: %w", err)
	}

	// Step 4: Create hydration task if hydrator is available
	if lr.hydrator != nil {
		if err := lr.createHydrationTask(userID, sourceID, appInfo); err != nil {
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
		return make(map[string]interface{})
	}

	return map[string]interface{}{
		"id":          entry.ID,
		"name":        entry.Name,
		"cfgType":     entry.CfgType,
		"chartName":   entry.ChartName,
		"icon":        entry.Icon,
		"description": entry.Description,
		"appID":       entry.AppID,
		"title":       entry.Title,
		"version":     entry.Version,
		"categories":  entry.Categories,
		"versionName": entry.VersionName,

		"fullDescription":    entry.FullDescription,
		"upgradeDescription": entry.UpgradeDescription,
		"promoteImage":       entry.PromoteImage,
		"promoteVideo":       entry.PromoteVideo,
		"subCategory":        entry.SubCategory,
		"locale":             entry.Locale,
		"developer":          entry.Developer,
		"requiredMemory":     entry.RequiredMemory,
		"requiredDisk":       entry.RequiredDisk,
		"supportClient":      entry.SupportClient,
		"supportArch":        entry.SupportArch,
		"requiredGPU":        entry.RequiredGPU,
		"requiredCPU":        entry.RequiredCPU,
		"rating":             entry.Rating,
		"target":             entry.Target,
		"permission":         entry.Permission,
		"entrances":          entry.Entrances,
		"middleware":         entry.Middleware,
		"options":            entry.Options,

		"submitter":     entry.Submitter,
		"doc":           entry.Doc,
		"website":       entry.Website,
		"featuredImage": entry.FeaturedImage,
		"sourceCode":    entry.SourceCode,
		"license":       entry.License,
		"legal":         entry.Legal,
		"i18n":          entry.I18n,

		"modelSize": entry.ModelSize,
		"namespace": entry.Namespace,
		"onlyAdmin": entry.OnlyAdmin,

		"lastCommitHash": entry.LastCommitHash,
		"createTime":     entry.CreateTime,
		"updateTime":     entry.UpdateTime,
		"appLabels":      entry.AppLabels,
		"count":          entry.Count,
		"variants":       entry.Variants,

		"screenshots": entry.Screenshots,
		"tags":        entry.Tags,
		"metadata":    entry.Metadata,
		"updated_at":  entry.UpdatedAt,
	}
}
