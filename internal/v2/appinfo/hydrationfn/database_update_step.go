package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"market/internal/v2/types"
)

// DatabaseUpdateStep represents the step to update memory cache and database
type DatabaseUpdateStep struct {
}

// NewDatabaseUpdateStep creates a new database update step
func NewDatabaseUpdateStep() *DatabaseUpdateStep {
	return &DatabaseUpdateStep{}
}

// GetStepName returns the name of this step
func (s *DatabaseUpdateStep) GetStepName() string {
	return "Database and Cache Update"
}

// CanSkip determines if this step can be skipped
func (s *DatabaseUpdateStep) CanSkip(ctx context.Context, task *HydrationTask) bool {
	// This step should rarely be skipped as it's the final step
	return false
}

// Execute performs the database and cache update
func (s *DatabaseUpdateStep) Execute(ctx context.Context, task *HydrationTask) error {
	log.Printf("Executing database update step for app: %s (user: %s, source: %s)",
		task.AppID, task.UserID, task.SourceID)

	var err error
	// Defer cleanup of rendered directory in case of any failure in this step
	defer func() {
		// Only cleanup if the step failed
		if err != nil {
			s.cleanupRenderedDirectory(task)
		}
	}()

	// Validate that previous steps completed successfully
	if err = s.validatePreviousSteps(task); err != nil {
		return fmt.Errorf("previous steps validation failed: %w", err)
	}

	// Prepare update data with image analysis integration
	var updateData map[string]interface{}
	var imageAnalysis *types.AppImageAnalysis
	updateData, imageAnalysis, err = s.prepareUpdateDataWithImages(task)
	if err != nil {
		return fmt.Errorf("failed to prepare update data: %w", err)
	}

	// Update memory cache with image analysis
	if err = s.updateMemoryCacheWithImages(task, updateData, imageAnalysis); err != nil {
		return fmt.Errorf("failed to update memory cache: %w", err)
	}

	// Store update information in task
	task.DatabaseUpdateData = updateData

	log.Printf("Database and cache update completed for app: %s with %d Docker images analyzed",
		task.AppID, s.getImageCount(imageAnalysis))
	return nil
}

// validatePreviousSteps validates that all previous steps completed successfully
func (s *DatabaseUpdateStep) validatePreviousSteps(task *HydrationTask) error {
	// Check if source chart URL is available
	if task.SourceChartURL == "" {
		return fmt.Errorf("source chart URL is missing")
	}

	// Check if rendered chart URL is available
	if task.RenderedChartURL == "" {
		return fmt.Errorf("rendered chart URL is missing")
	}

	// Check if chart data is available
	if len(task.ChartData) == 0 {
		return fmt.Errorf("chart data is missing")
	}

	return nil
}

// prepareUpdateDataWithImages prepares the data to be updated including image analysis
func (s *DatabaseUpdateStep) prepareUpdateDataWithImages(task *HydrationTask) (map[string]interface{}, *types.AppImageAnalysis, error) {
	updateData := make(map[string]interface{})

	// Basic app information
	updateData["app_id"] = task.AppID
	updateData["user_id"] = task.UserID
	updateData["source_id"] = task.SourceID
	updateData["app_name"] = task.AppName
	updateData["app_version"] = task.AppVersion

	// Chart URLs
	updateData["source_chart_url"] = task.SourceChartURL
	updateData["rendered_chart_url"] = task.RenderedChartURL

	// Chart data
	updateData["chart_data"] = task.ChartData

	// Extract and integrate image analysis data
	var imageAnalysis *types.AppImageAnalysis
	if analysisData, exists := task.ChartData["image_analysis"]; exists {
		if analysis, ok := analysisData.(*types.ImageAnalysisResult); ok {
			// Convert ImageAnalysisResult to AppImageAnalysis
			imageAnalysis = s.convertToAppImageAnalysis(task, analysis)

			// Add image analysis to update data
			updateData["docker_images"] = imageAnalysis.Images
			updateData["total_docker_images"] = imageAnalysis.TotalImages
			updateData["image_analysis_status"] = imageAnalysis.Status
			updateData["image_analysis_timestamp"] = imageAnalysis.AnalyzedAt.Unix()

			log.Printf("Integrated %d Docker images for app: %s", imageAnalysis.TotalImages, task.AppID)
		}
	}

	// Original app data (selective)
	if task.AppData != nil {
		// Copy important fields from original app data
		importantFields := []string{
			"description", "icon", "keywords", "maintainers",
			"home", "sources", "dependencies", "tags",
			"category", "subcategory", "license", "screenshots",
		}

		for _, field := range importantFields {
			if value, exists := task.AppData[field]; exists {
				updateData[field] = value
			}
		}
	}

	// Hydration metadata
	updateData["hydration_status"] = "completed"
	updateData["hydration_timestamp"] = time.Now().Unix()
	updateData["hydration_task_id"] = task.ID

	// Processing metrics
	updateData["processing_time"] = time.Since(task.CreatedAt).Seconds()
	updateData["retry_count"] = task.RetryCount

	return updateData, imageAnalysis, nil
}

// convertToAppImageAnalysis converts ImageAnalysisResult to AppImageAnalysis
func (s *DatabaseUpdateStep) convertToAppImageAnalysis(task *HydrationTask, result *types.ImageAnalysisResult) *types.AppImageAnalysis {
	return &types.AppImageAnalysis{
		AppID:            task.AppID,
		AnalyzedAt:       result.AnalyzedAt,
		TotalImages:      result.TotalImages,
		Images:           result.Images, // Now both use the same *types.ImageInfo type
		Status:           s.determineAnalysisStatus(result),
		SourceChartURL:   task.SourceChartURL,
		RenderedChartURL: task.RenderedChartURL,
	}
}

// determineAnalysisStatus determines the overall analysis status
func (s *DatabaseUpdateStep) determineAnalysisStatus(result *types.ImageAnalysisResult) string {
	if result.TotalImages == 0 {
		return "no_images"
	}

	successCount := 0
	analysisAttemptedCount := 0
	for _, img := range result.Images {
		analysisAttemptedCount++
		// Consider private registry images as successfully analyzed
		if img.Status == "fully_downloaded" || img.Status == "partially_downloaded" || img.Status == "private_registry" {
			successCount++
		}
	}

	// If all images were successfully analyzed (including private registry detection)
	if successCount == result.TotalImages && analysisAttemptedCount == result.TotalImages {
		return "completed"
	} else if successCount > 0 {
		return "partial"
	} else {
		return "failed"
	}
}

// getImageCount safely gets image count from analysis
func (s *DatabaseUpdateStep) getImageCount(analysis *types.AppImageAnalysis) int {
	if analysis == nil {
		return 0
	}
	return analysis.TotalImages
}

// updateMemoryCacheWithImages updates the in-memory cache with hydrated app data including images
func (s *DatabaseUpdateStep) updateMemoryCacheWithImages(task *HydrationTask, updateData map[string]interface{}, imageAnalysis *types.AppImageAnalysis) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Step 1: Prepare all data outside of any locks
	log.Printf("Preparing update data for app: %s (user: %s, source: %s)", task.AppID, task.UserID, task.SourceID)

	// Prepare metadata for hydration completion
	hydrationMetadata := map[string]interface{}{
		"hydration_status":    "completed",
		"hydration_timestamp": time.Now().Unix(),
		"processing_time":     time.Since(task.CreatedAt).Seconds(),
		"retry_count":         task.RetryCount,
		"hydration_task_id":   task.ID,
		"source_chart_url":    task.SourceChartURL,
		"rendered_chart_url":  task.RenderedChartURL,
		"validation_status":   "validated",
		"data_source":         "hydration_task",
	}

	// Step 2: Update RawPackage and RenderedPackage fields in pending data
	log.Printf("Updating package information for app: %s", task.AppID)
	if err := s.updatePackageInformation(task); err != nil {
		log.Printf("Warning: Failed to update package information for app %s: %v", task.AppID, err)
		return fmt.Errorf("failed to update package information: %w", err)
	}

	// Step 3: Update AppInfo with image analysis
	log.Printf("Updating AppInfo with image analysis for app: %s", task.AppID)
	if err := s.updateAppInfoWithImages(task, imageAnalysis, hydrationMetadata); err != nil {
		return fmt.Errorf("failed to update AppInfo with images: %w", err)
	}

	log.Printf("Cache update completed for app: %s with %d Docker images analyzed",
		task.AppID, s.getImageCount(imageAnalysis))
	return nil
}

// updatePackageInformation updates RawPackage and RenderedPackage fields
func (s *DatabaseUpdateStep) updatePackageInformation(task *HydrationTask) error {
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Get the specific pending data entry for this task
	pendingDataRef, err := s.findPendingDataForTask(task)
	if err != nil {
		return fmt.Errorf("failed to find pending data: %w", err)
	}

	if pendingDataRef == nil {
		log.Printf("No pending data found for task %s, skipping package update", task.ID)
		return nil
	}

	// Update package fields directly (these are simple string assignments)
	if task.SourceChartURL != "" {
		pendingDataRef.RawPackage = task.SourceChartURL
	}

	// RenderedPackage should only contain local file paths, not remote URLs
	if renderedChartDir, exists := task.ChartData["rendered_chart_dir"].(string); exists && renderedChartDir != "" {
		pendingDataRef.RenderedPackage = renderedChartDir
		log.Printf("Updated RenderedPackage with local path: %s", renderedChartDir)
	} else {
		log.Printf("No local rendered chart directory found, keeping existing RenderedPackage value")
	}

	log.Printf("Updated package information for app: %s (RawPackage: %s, RenderedPackage: %s)",
		task.AppID, pendingDataRef.RawPackage, pendingDataRef.RenderedPackage)
	return nil
}

// updateAppInfoWithImages updates AppInfo with image analysis results
func (s *DatabaseUpdateStep) updateAppInfoWithImages(task *HydrationTask, imageAnalysis *types.AppImageAnalysis, metadata map[string]interface{}) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Get the specific pending data entry for this task
	pendingDataRef, err := s.findPendingDataForTask(task)
	if err != nil {
		return fmt.Errorf("failed to find pending data: %w", err)
	}

	if pendingDataRef == nil {
		log.Printf("No pending data found for task %s, skipping AppInfo update", task.ID)
		return nil
	}

	// Initialize AppInfo if it doesn't exist
	if pendingDataRef.AppInfo == nil {
		pendingDataRef.AppInfo = &types.AppInfo{}
	}

	// Set AppEntry to RawData if not already set
	if pendingDataRef.AppInfo.AppEntry == nil && pendingDataRef.RawData != nil {
		pendingDataRef.AppInfo.AppEntry = pendingDataRef.RawData
	}

	// Update i18n data from i18n directory
	if err := s.updateI18nData(task, pendingDataRef); err != nil {
		log.Printf("Warning: Failed to update i18n data for app %s: %v", task.AppID, err)
		// Continue with other updates even if i18n update fails
	}

	// Update image analysis in AppInfo
	if imageAnalysis != nil {
		pendingDataRef.AppInfo.ImageAnalysis = &types.ImageAnalysisResult{
			AnalyzedAt:  imageAnalysis.AnalyzedAt,
			TotalImages: imageAnalysis.TotalImages,
			Images:      imageAnalysis.Images,
		}
		log.Printf("Updated image analysis for app: %s (%d images)", task.AppID, imageAnalysis.TotalImages)
	}

	// Update metadata in RawData if available
	if pendingDataRef.RawData != nil {
		if pendingDataRef.RawData.Metadata == nil {
			pendingDataRef.RawData.Metadata = make(map[string]interface{})
		}
		// Copy hydration metadata
		for key, value := range metadata {
			pendingDataRef.RawData.Metadata[key] = value
		}
	}

	// Update timestamp to reflect processing completion
	pendingDataRef.Timestamp = time.Now().Unix()

	log.Printf("Updated AppInfo for app: %s with hydration completion metadata", task.AppID)
	return nil
}

// updateI18nData reads i18n data from i18n directory and updates the app info
func (s *DatabaseUpdateStep) updateI18nData(task *HydrationTask, pendingDataRef *types.AppInfoLatestPendingData) error {
	// Get the rendered chart directory path from task data
	var chartDir string
	if renderedChartDir, exists := task.ChartData["rendered_chart_dir"]; exists {
		if dir, ok := renderedChartDir.(string); ok {
			chartDir = dir
		}
	}

	// Fallback to using RenderedChartURL if no local directory is found
	if chartDir == "" {
		if task.RenderedChartURL == "" {
			log.Printf("No rendered chart directory or URL available for app: %s, skipping i18n update", task.AppID)
			return nil
		}
		chartDir = task.RenderedChartURL
		if strings.HasPrefix(chartDir, "file://") {
			chartDir = strings.TrimPrefix(chartDir, "file://")
		}
	}

	log.Printf("I18n update: Chart directory for app %s: %s", task.AppID, chartDir)

	// Check if the directory exists
	if _, err := os.Stat(chartDir); os.IsNotExist(err) {
		log.Printf("Chart directory does not exist: %s", chartDir)
		return nil
	}

	// Look for i18n directory in multiple possible locations
	var i18nDir string
	possibleI18nPaths := []string{
		filepath.Join(chartDir, "i18n"),               // Direct i18n directory
		filepath.Join(chartDir, task.AppName, "i18n"), // App-specific subdirectory
	}

	// Also try to find i18n directory by scanning subdirectories
	if entries, err := os.ReadDir(chartDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				// Check if this subdirectory contains an i18n folder
				subI18nPath := filepath.Join(chartDir, entry.Name(), "i18n")
				possibleI18nPaths = append(possibleI18nPaths, subI18nPath)
			}
		}
	}

	for _, path := range possibleI18nPaths {
		if _, err := os.Stat(path); err == nil {
			i18nDir = path
			log.Printf("I18n update: Found i18n directory: %s", i18nDir)
			break
		}
	}

	if i18nDir == "" {
		log.Printf("No i18n directory found in any of the expected locations for app: %s", task.AppID)
		log.Printf("Searched paths: %v", possibleI18nPaths)
		return nil
	}

	// Get supported languages from app locale or use default languages
	supportedLanguages := []string{"en-US", "zh-CN"} // Default supported languages
	if pendingDataRef.RawData != nil && len(pendingDataRef.RawData.Locale) > 0 {
		supportedLanguages = pendingDataRef.RawData.Locale
	}

	// Initialize multilingual fields if they don't exist for both RawData and AppInfo.AppEntry
	if pendingDataRef.RawData != nil {
		if pendingDataRef.RawData.Title == nil {
			pendingDataRef.RawData.Title = make(map[string]string)
		}
		if pendingDataRef.RawData.Description == nil {
			pendingDataRef.RawData.Description = make(map[string]string)
		}
		if pendingDataRef.RawData.FullDescription == nil {
			pendingDataRef.RawData.FullDescription = make(map[string]string)
		}
		if pendingDataRef.RawData.UpgradeDescription == nil {
			pendingDataRef.RawData.UpgradeDescription = make(map[string]string)
		}
	}

	// Ensure AppInfo.AppEntry exists and initialize multilingual fields
	if pendingDataRef.AppInfo == nil {
		pendingDataRef.AppInfo = &types.AppInfo{}
	}
	if pendingDataRef.AppInfo.AppEntry == nil {
		pendingDataRef.AppInfo.AppEntry = &types.ApplicationInfoEntry{}
	}
	if pendingDataRef.AppInfo.AppEntry.Title == nil {
		pendingDataRef.AppInfo.AppEntry.Title = make(map[string]string)
	}
	if pendingDataRef.AppInfo.AppEntry.Description == nil {
		pendingDataRef.AppInfo.AppEntry.Description = make(map[string]string)
	}
	if pendingDataRef.AppInfo.AppEntry.FullDescription == nil {
		pendingDataRef.AppInfo.AppEntry.FullDescription = make(map[string]string)
	}
	if pendingDataRef.AppInfo.AppEntry.UpgradeDescription == nil {
		pendingDataRef.AppInfo.AppEntry.UpgradeDescription = make(map[string]string)
	}

	// Read i18n data for each supported language
	for _, lang := range supportedLanguages {
		log.Printf("I18n update: Processing language: %s for app: %s", lang, task.AppID)

		langDir := filepath.Join(i18nDir, lang)
		if _, err := os.Stat(langDir); os.IsNotExist(err) {
			log.Printf("Language directory does not exist: %s", langDir)
			continue
		}

		log.Printf("I18n update: Found language directory: %s", langDir)

		// Try to read i18n config file (usually app.cfg or similar)
		configFiles := []string{
			"OlaresManifest.yaml", // Olares manifest file
			"OlaresManifest.yml",
			"app.cfg",
			"app.yaml",
			"app.yml",
			"config.yaml",
			"config.yml",
			"manifest.yaml",
			"manifest.yml",
		}
		var i18nData map[string]interface{}

		for _, configFile := range configFiles {
			configPath := filepath.Join(langDir, configFile)
			if _, err := os.Stat(configPath); err == nil {
				log.Printf("I18n update: Found config file: %s", configPath)

				data, err := ioutil.ReadFile(configPath)
				if err != nil {
					log.Printf("Failed to read i18n config file %s: %v", configPath, err)
					continue
				}

				// Try to parse as YAML first, then JSON
				err = yaml.Unmarshal(data, &i18nData)
				if err != nil {
					err = json.Unmarshal(data, &i18nData)
					if err != nil {
						log.Printf("Failed to parse i18n config file %s: %v", configPath, err)
						continue
					}
				}
				log.Printf("I18n update: Successfully parsed config file: %s", configPath)
				break
			}
		}

		if i18nData == nil {
			log.Printf("No valid i18n config found for language: %s in directory: %s", lang, langDir)
			continue
		}

		// Extract and update multilingual fields in both RawData and AppInfo.AppEntry
		s.updateMultilingualFields(i18nData, lang, pendingDataRef.RawData, "RawData")
		s.updateMultilingualFields(i18nData, lang, pendingDataRef.AppInfo.AppEntry, "AppInfo.AppEntry")
	}

	log.Printf("Completed i18n data update for app: %s", task.AppID)
	return nil
}

// updateMultilingualFields updates multilingual fields in the given ApplicationInfoEntry
func (s *DatabaseUpdateStep) updateMultilingualFields(i18nData map[string]interface{}, lang string, entry *types.ApplicationInfoEntry, entryType string) {
	if entry == nil {
		return
	}

	// Update title from metadata or spec
	if metadata, ok := i18nData["metadata"].(map[string]interface{}); ok {
		if title, ok := metadata["title"].(string); ok && title != "" {
			entry.Title[lang] = title
			log.Printf("Updated title for language %s in %s: %s", lang, entryType, title)
		}
	}

	// Update description from metadata
	if metadata, ok := i18nData["metadata"].(map[string]interface{}); ok {
		if description, ok := metadata["description"].(string); ok && description != "" {
			entry.Description[lang] = description
			log.Printf("Updated description for language %s in %s", lang, entryType)
		}
	}

	// Update fullDescription and upgradeDescription from spec
	if spec, ok := i18nData["spec"].(map[string]interface{}); ok {
		if fullDescription, ok := spec["fullDescription"].(string); ok && fullDescription != "" {
			entry.FullDescription[lang] = fullDescription
			log.Printf("Updated fullDescription for language %s in %s", lang, entryType)
		}
		if upgradeDescription, ok := spec["upgradeDescription"].(string); ok && upgradeDescription != "" {
			entry.UpgradeDescription[lang] = upgradeDescription
			log.Printf("Updated upgradeDescription for language %s in %s", lang, entryType)
		}
	}

	// Direct field access for backward compatibility
	if title, ok := i18nData["title"].(string); ok && title != "" {
		entry.Title[lang] = title
		log.Printf("Updated title (direct) for language %s in %s: %s", lang, entryType, title)
	}
	if description, ok := i18nData["description"].(string); ok && description != "" {
		entry.Description[lang] = description
		log.Printf("Updated description (direct) for language %s in %s", lang, entryType)
	}
	if fullDescription, ok := i18nData["fullDescription"].(string); ok && fullDescription != "" {
		entry.FullDescription[lang] = fullDescription
		log.Printf("Updated fullDescription (direct) for language %s in %s", lang, entryType)
	}
	if upgradeDescription, ok := i18nData["upgradeDescription"].(string); ok && upgradeDescription != "" {
		entry.UpgradeDescription[lang] = upgradeDescription
		log.Printf("Updated upgradeDescription (direct) for language %s in %s", lang, entryType)
	}
}

// findPendingDataForTask finds the pending data entry for a specific task
func (s *DatabaseUpdateStep) findPendingDataForTask(task *HydrationTask) (*types.AppInfoLatestPendingData, error) {
	if task.Cache == nil {
		return nil, fmt.Errorf("cache reference is nil")
	}

	// Access cache data using global lock
	task.Cache.Mutex.RLock()
	defer task.Cache.Mutex.RUnlock()

	userData, userExists := task.Cache.Users[task.UserID]
	if !userExists {
		return nil, fmt.Errorf("user %s not found in cache", task.UserID)
	}

	sourceData, sourceExists := userData.Sources[task.SourceID]
	if !sourceExists {
		return nil, fmt.Errorf("source %s not found for user %s", task.SourceID, task.UserID)
	}

	// Find the matching pending data
	for _, pendingData := range sourceData.AppInfoLatestPending {
		if s.isTaskForPendingData(task, pendingData) {
			return pendingData, nil
		}
	}

	return nil, nil // Not found, but not an error
}

// isTaskForPendingData checks if the current task corresponds to the pending data
func (s *DatabaseUpdateStep) isTaskForPendingData(task *HydrationTask, pendingData *types.AppInfoLatestPendingData) bool {
	if pendingData == nil {
		log.Printf("isTaskForPendingData: pendingData is nil")
		return false
	}

	taskAppID := task.AppID
	log.Printf("isTaskForPendingData: Looking for task appID: %s", taskAppID)

	// Check if the task AppID matches the pending data's RawData
	if pendingData.RawData != nil {
		log.Printf("isTaskForPendingData: Checking RawData - ID: %s, AppID: %s, Name: %s",
			pendingData.RawData.ID, pendingData.RawData.AppID, pendingData.RawData.Name)

		// Check if this is legacy data by looking at metadata
		if pendingData.RawData.Metadata != nil {
			// Check for legacy_data in metadata - this contains multiple apps
			if legacyData, hasLegacyData := pendingData.RawData.Metadata["legacy_data"]; hasLegacyData {
				if legacyDataMap, ok := legacyData.(map[string]interface{}); ok {
					// Check if task app ID exists in the legacy data apps
					if dataSection, hasDataSection := legacyDataMap["data"].(map[string]interface{}); hasDataSection {
						if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
							if _, appExists := appsData[taskAppID]; appExists {
								log.Printf("isTaskForPendingData: Found task appID %s in legacy_data apps", taskAppID)
								return true
							}
						}
					}
				}
			}

			// Check for legacy_raw_data in metadata
			if legacyRawData, hasLegacyRawData := pendingData.RawData.Metadata["legacy_raw_data"]; hasLegacyRawData {
				if legacyRawDataMap, ok := legacyRawData.(map[string]interface{}); ok {
					// Similar check for legacy raw data format
					if dataSection, hasDataSection := legacyRawDataMap["data"].(map[string]interface{}); hasDataSection {
						if appsData, hasApps := dataSection["apps"].(map[string]interface{}); hasApps {
							if _, appExists := appsData[taskAppID]; appExists {
								log.Printf("isTaskForPendingData: Found task appID %s in legacy_raw_data apps", taskAppID)
								return true
							}
						}
					}
				}
			}

			// Check representative_app_id for legacy summary data
			if repAppID, hasRepAppID := pendingData.RawData.Metadata["representative_app_id"].(string); hasRepAppID {
				if repAppID == taskAppID {
					log.Printf("isTaskForPendingData: Found task appID %s as representative_app_id", taskAppID)
					return true
				}
			}

			// Check data_type to identify legacy data types
			if dataType, hasDataType := pendingData.RawData.Metadata["data_type"].(string); hasDataType {
				log.Printf("isTaskForPendingData: Pending data type: %s", dataType)

				// For legacy data types, we need to check the stored legacy data
				if dataType == "legacy_complete_data" || dataType == "legacy_unstructured_data" {
					// Already checked above, but log for debugging
					log.Printf("isTaskForPendingData: This is legacy data type: %s", dataType)
				}
			}
		}

		// Standard checks for non-legacy data
		// Match by ID, AppID or Name
		if pendingData.RawData.ID == taskAppID ||
			pendingData.RawData.AppID == taskAppID ||
			pendingData.RawData.Name == taskAppID {
			log.Printf("isTaskForPendingData: Found match in RawData")
			return true
		}

		// Try partial matches for debugging
		if pendingData.RawData.ID != "" && strings.Contains(taskAppID, pendingData.RawData.ID) {
			log.Printf("isTaskForPendingData: Partial match found - task contains RawData.ID")
		}
		if pendingData.RawData.AppID != "" && strings.Contains(taskAppID, pendingData.RawData.AppID) {
			log.Printf("isTaskForPendingData: Partial match found - task contains RawData.AppID")
		}
		if pendingData.RawData.Name != "" && strings.Contains(taskAppID, pendingData.RawData.Name) {
			log.Printf("isTaskForPendingData: Partial match found - task contains RawData.Name")
		}
	}

	// Check AppInfo if available
	if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
		log.Printf("isTaskForPendingData: Checking AppInfo.AppEntry - ID: %s, AppID: %s, Name: %s",
			pendingData.AppInfo.AppEntry.ID, pendingData.AppInfo.AppEntry.AppID, pendingData.AppInfo.AppEntry.Name)

		// Match by ID, AppID or Name in AppInfo
		if pendingData.AppInfo.AppEntry.ID == taskAppID ||
			pendingData.AppInfo.AppEntry.AppID == taskAppID ||
			pendingData.AppInfo.AppEntry.Name == taskAppID {
			log.Printf("isTaskForPendingData: Found match in AppInfo.AppEntry")
			return true
		}
	}

	log.Printf("isTaskForPendingData: No match found for task appID: %s", taskAppID)
	return false
}

// updateDatabase would update the persistent database (not implemented in this example)
func (s *DatabaseUpdateStep) updateDatabase(task *HydrationTask, updateData map[string]interface{}) error {
	// This would typically involve:
	// 1. Connect to database (PostgreSQL, MySQL, etc.)
	// 2. Prepare SQL statements or use ORM
	// 3. Insert or update records
	// 4. Handle transactions

	// For now, we'll just log the operation
	dataJSON, _ := json.MarshalIndent(updateData, "", "  ")
	log.Printf("Database update (simulated) for app: %s, data: %s", task.AppID, string(dataJSON))

	return nil
}

// cleanupRenderedDirectory removes the rendered chart directory if it exists
func (s *DatabaseUpdateStep) cleanupRenderedDirectory(task *HydrationTask) {
	if renderedDir, exists := task.ChartData["rendered_chart_dir"].(string); exists && renderedDir != "" {
		log.Printf("Executing cleanup, removing rendered chart directory: %s", renderedDir)
		if err := os.RemoveAll(renderedDir); err != nil {
			log.Printf("Warning: Failed to clean up rendered chart directory %s: %v", renderedDir, err)
		}
	}
}
