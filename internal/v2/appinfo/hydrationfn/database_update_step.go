package hydrationfn

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
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

	// Chart data - create a safe copy to avoid circular references
	safeChartData := s.createSafeChartDataCopy(task.ChartData)
	updateData["chart_data"] = safeChartData

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
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Get the specific pending data entry for this task
	pendingDataRef, err := s.findPendingDataForTaskLocked(task)
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

	// Log pending data after package update to check for cycles
	s.logPendingDataAfterUpdate(pendingDataRef, "after package update")

	return nil
}

// updateAppInfoWithImages updates AppInfo with image analysis results
func (s *DatabaseUpdateStep) updateAppInfoWithImages(task *HydrationTask, imageAnalysis *types.AppImageAnalysis, metadata map[string]interface{}) error {
	if task.Cache == nil {
		return fmt.Errorf("cache reference is nil")
	}

	// Step 1: Perform I/O-intensive operations (reading i18n data) outside the lock.
	// We read the original RawData locale settings before locking.
	locale := []string{"en-US", "zh-CN"} // Default languages
	i18nData, err := s.readI18nData(task, locale)
	if err != nil {
		log.Printf("Warning: Failed to read i18n data for app %s: %v", task.AppID, err)
		// Continue with other updates even if i18n update fails
	}

	// Step 2: Acquire the lock to safely update the shared cache data.
	task.Cache.Mutex.Lock()
	defer task.Cache.Mutex.Unlock()

	// Get the specific pending data entry for this task
	pendingDataRef, err := s.findPendingDataForTaskLocked(task)
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

	// Step 3: Apply the i18n data that was read outside the lock.
	if i18nData != nil {
		s.applyI18nData(pendingDataRef, i18nData)
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

	// Log pending data after AppInfo update to check for cycles
	s.logPendingDataAfterUpdate(pendingDataRef, "after AppInfo update")

	return nil
}

// logPendingDataAfterUpdate logs pending data after update to check for cycles
func (s *DatabaseUpdateStep) logPendingDataAfterUpdate(pendingDataRef *types.AppInfoLatestPendingData, context string) {
	log.Printf("DEBUG: Pending data structure check - %s", context)

	if pendingDataRef == nil {
		log.Printf("DEBUG: Pending data is nil")
		return
	}

	// Create a safe copy of pending data for JSON marshaling to avoid cycles
	safePendingData := s.createSafePendingDataCopy(pendingDataRef)

	// Try to JSON marshal the safe copy of pending data
	if jsonData, err := json.Marshal(safePendingData); err != nil {
		log.Printf("ERROR: JSON marshal failed for pending data - %s: %v", context, err)
		log.Printf("ERROR: Pending data structure: RawData=%v, AppInfo=%v, RawPackage=%s, RenderedPackage=%s",
			pendingDataRef.RawData != nil, pendingDataRef.AppInfo != nil, pendingDataRef.RawPackage, pendingDataRef.RenderedPackage)

		// Try to marshal individual components to isolate the problem
		if pendingDataRef.RawData != nil {
			if _, err := json.Marshal(pendingDataRef.RawData); err != nil {
				log.Printf("ERROR: JSON marshal failed for RawData - %s: %v", context, err)
			}
		}

		if pendingDataRef.AppInfo != nil {
			if _, err := json.Marshal(pendingDataRef.AppInfo); err != nil {
				log.Printf("ERROR: JSON marshal failed for AppInfo - %s: %v", context, err)
			}
		}
	} else {
		log.Printf("DEBUG: Pending data JSON length - %s: %d bytes", context, len(jsonData))
	}
}

// readI18nData reads i18n data from files and returns it without modifying any state.
func (s *DatabaseUpdateStep) readI18nData(task *HydrationTask, supportedLanguages []string) (map[string]map[string]string, error) {
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
			return nil, nil
		}
		chartDir = task.RenderedChartURL
		if strings.HasPrefix(chartDir, "file://") {
			chartDir = strings.TrimPrefix(chartDir, "file://")
		}
	}

	log.Printf("I18n read: Chart directory for app %s: %s", task.AppID, chartDir)

	// Check if the directory exists
	if _, err := os.Stat(chartDir); os.IsNotExist(err) {
		log.Printf("Chart directory does not exist: %s", chartDir)
		return nil, nil
	}

	i18nDir := s.findI18nDirectory(task, chartDir)
	if i18nDir == "" {
		log.Printf("No i18n directory found for app: %s", task.AppID)
		return nil, nil
	}

	allI18nData := make(map[string]map[string]string)

	// Read i18n data for each supported language
	for _, lang := range supportedLanguages {
		log.Printf("I18n read: Processing language: %s for app: %s", lang, task.AppID)
		langDir := filepath.Join(i18nDir, lang)
		if _, err := os.Stat(langDir); os.IsNotExist(err) {
			log.Printf("Language directory does not exist: %s", langDir)
			continue
		}

		log.Printf("I18n read: Found language directory: %s", langDir)

		parsedData := s.parseI18nConfigFile(langDir)
		if parsedData == nil {
			log.Printf("No valid i18n config found for language: %s in directory: %s", lang, langDir)
			continue
		}

		s.extractMultilingualFields(parsedData, lang, allI18nData)
	}

	log.Printf("Completed i18n data read for app: %s", task.AppID)
	return allI18nData, nil
}

// findI18nDirectory searches for the i18n directory in several possible locations.
func (s *DatabaseUpdateStep) findI18nDirectory(task *HydrationTask, chartDir string) string {
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
			log.Printf("I18n update: Found i18n directory: %s", path)
			return path
		}
	}

	log.Printf("No i18n directory found in any of the expected locations for app: %s", task.AppID)
	log.Printf("Searched paths: %v", possibleI18nPaths)
	return ""
}

// parseI18nConfigFile finds and parses an i18n configuration file in a given directory.
func (s *DatabaseUpdateStep) parseI18nConfigFile(langDir string) map[string]interface{} {
	configFiles := []string{
		"OlaresManifest.yaml", "OlaresManifest.yml", "app.cfg", "app.yaml", "app.yml",
		"config.yaml", "config.yml", "manifest.yaml", "manifest.yml",
	}
	var i18nData map[string]interface{}

	for _, configFile := range configFiles {
		configPath := filepath.Join(langDir, configFile)
		if _, err := os.Stat(configPath); err == nil {
			log.Printf("I18n read: Found config file: %s", configPath)
			data, err := ioutil.ReadFile(configPath)
			if err != nil {
				log.Printf("Failed to read i18n config file %s: %v", configPath, err)
				continue
			}

			if err = yaml.Unmarshal(data, &i18nData); err == nil {
				log.Printf("I18n read: Successfully parsed YAML config file: %s", configPath)
				return i18nData
			}
			if err = json.Unmarshal(data, &i18nData); err == nil {
				log.Printf("I18n read: Successfully parsed JSON config file: %s", configPath)
				return i18nData
			}
			log.Printf("Failed to parse i18n config file %s as YAML or JSON", configPath)
		}
	}
	return nil
}

// extractMultilingualFields extracts internationalized strings from parsed data and puts them into a map.
func (s *DatabaseUpdateStep) extractMultilingualFields(i18nData map[string]interface{}, lang string, allI18nData map[string]map[string]string) {
	fields := []string{"title", "description", "fullDescription", "upgradeDescription"}

	for _, field := range fields {
		if allI18nData[field] == nil {
			allI18nData[field] = make(map[string]string)
		}

		var value string
		// Try to get from metadata section
		if metadata, ok := i18nData["metadata"].(map[string]interface{}); ok {
			if v, ok := metadata[field].(string); ok && v != "" {
				value = v
			}
		}
		// Try to get from spec section
		if value == "" {
			if spec, ok := i18nData["spec"].(map[string]interface{}); ok {
				if v, ok := spec[field].(string); ok && v != "" {
					value = v
				}
			}
		}
		// Try to get from top level (backward compatibility)
		if value == "" {
			if v, ok := i18nData[field].(string); ok && v != "" {
				value = v
			}
		}

		if value != "" {
			allI18nData[field][lang] = value
			log.Printf("Extracted i18n field '%s' for lang '%s'", field, lang)
		}
	}
}

// applyI18nData takes the pre-read i18n data and applies it to the pending data entry.
func (s *DatabaseUpdateStep) applyI18nData(pendingDataRef *types.AppInfoLatestPendingData, i18nData map[string]map[string]string) {
	if pendingDataRef.RawData == nil || pendingDataRef.AppInfo == nil || pendingDataRef.AppInfo.AppEntry == nil {
		log.Printf("Cannot apply i18n data, RawData or AppEntry is nil")
		return
	}

	// Initialize multilingual fields if they don't exist
	if pendingDataRef.RawData.Title == nil {
		pendingDataRef.RawData.Title = make(map[string]string)
		pendingDataRef.AppInfo.AppEntry.Title = make(map[string]string)
	}
	if pendingDataRef.RawData.Description == nil {
		pendingDataRef.RawData.Description = make(map[string]string)
		pendingDataRef.AppInfo.AppEntry.Description = make(map[string]string)
	}
	if pendingDataRef.RawData.FullDescription == nil {
		pendingDataRef.RawData.FullDescription = make(map[string]string)
		pendingDataRef.AppInfo.AppEntry.FullDescription = make(map[string]string)
	}
	if pendingDataRef.RawData.UpgradeDescription == nil {
		pendingDataRef.RawData.UpgradeDescription = make(map[string]string)
		pendingDataRef.AppInfo.AppEntry.UpgradeDescription = make(map[string]string)
	}

	// Apply the extracted data
	if titles, ok := i18nData["title"]; ok {
		for lang, text := range titles {
			pendingDataRef.RawData.Title[lang] = text
			pendingDataRef.AppInfo.AppEntry.Title[lang] = text
		}
	}
	if descriptions, ok := i18nData["description"]; ok {
		for lang, text := range descriptions {
			pendingDataRef.RawData.Description[lang] = text
			pendingDataRef.AppInfo.AppEntry.Description[lang] = text
		}
	}
	if fullDescriptions, ok := i18nData["fullDescription"]; ok {
		for lang, text := range fullDescriptions {
			pendingDataRef.RawData.FullDescription[lang] = text
			pendingDataRef.AppInfo.AppEntry.FullDescription[lang] = text
		}
	}
	if upgradeDescriptions, ok := i18nData["upgradeDescription"]; ok {
		for lang, text := range upgradeDescriptions {
			pendingDataRef.RawData.UpgradeDescription[lang] = text
			pendingDataRef.AppInfo.AppEntry.UpgradeDescription[lang] = text
		}
	}

	log.Printf("Successfully applied i18n data for app: %s", pendingDataRef.AppInfo.AppEntry.AppID)
}

// updateI18nData reads i18n data from i18n directory and updates the app info
func (s *DatabaseUpdateStep) updateI18nData(task *HydrationTask, pendingDataRef *types.AppInfoLatestPendingData) error {
	// THIS FUNCTION IS NOW DEPRECATED and its logic has been moved to
	// readI18nData and applyI18nData to fix concurrency issues.
	// It is kept here to avoid breaking any potential call sites, but it does nothing.
	// Consider removing it in a future refactoring.
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

// findPendingDataForTask finds the pending data entry for a specific task using a read lock.
func (s *DatabaseUpdateStep) findPendingDataForTask(task *HydrationTask) (*types.AppInfoLatestPendingData, error) {
	if task.Cache == nil {
		return nil, fmt.Errorf("cache reference is nil")
	}

	// Access cache data using global read lock
	task.Cache.Mutex.RLock()
	defer task.Cache.Mutex.RUnlock()

	return s.findPendingDataForTaskLocked(task)
}

// findPendingDataForTaskLocked finds the pending data entry for a specific task without acquiring locks.
// It assumes the caller is responsible for locking.
func (s *DatabaseUpdateStep) findPendingDataForTaskLocked(task *HydrationTask) (*types.AppInfoLatestPendingData, error) {
	if task.Cache == nil {
		return nil, fmt.Errorf("cache reference is nil")
	}

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

		// Standard checks for data
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

// createSafeChartDataCopy creates a safe copy of chart data to avoid circular references
func (s *DatabaseUpdateStep) createSafeChartDataCopy(chartData map[string]interface{}) map[string]interface{} {
	if chartData == nil {
		return make(map[string]interface{})
	}

	safeChartData := make(map[string]interface{})
	visited := make(map[uintptr]bool)

	for key, value := range chartData {
		// Skip potential circular reference keys or large data structures
		if key == "template_data" || key == "template_values" || key == "entrances" ||
			key == "domain" || key == "chart_files" || key == "rendered_manifest" ||
			key == "rendered_chart" || key == "source_data" || key == "raw_data" ||
			key == "app_info" || key == "parent" || key == "self" || key == "circular_ref" ||
			key == "back_ref" || key == "loop" || key == "templateData" || key == "templateData.Values" {
			continue
		}

		// For values that might contain circular references, create a safe copy
		if key == "rendered_manifest" || key == "rendered_chart" {
			// Only store essential information, not the full content
			if strVal, ok := value.(string); ok {
				safeChartData[key] = strVal
			}
			continue
		}

		safeChartData[key] = s.deepCopyChartDataValue(value, visited)
	}

	return safeChartData
}

// deepCopyChartDataValue performs a deep copy of a chart data value while avoiding circular references
func (s *DatabaseUpdateStep) deepCopyChartDataValue(value interface{}, visited map[uintptr]bool) interface{} {
	if value == nil {
		return nil
	}

	// Only check for circular references for pointer types
	if reflect.ValueOf(value).Kind() == reflect.Ptr {
		ptr := reflect.ValueOf(value).Pointer()
		if visited[ptr] {
			return nil // Return nil for circular references
		}
		visited[ptr] = true
		defer delete(visited, ptr)
	}

	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			// Skip potential circular reference keys
			if key == "template_data" || key == "template_values" || key == "entrances" ||
				key == "domain" || key == "chart_files" || key == "rendered_manifest" ||
				key == "rendered_chart" || key == "source_data" || key == "raw_data" ||
				key == "app_info" || key == "parent" || key == "self" || key == "circular_ref" ||
				key == "back_ref" || key == "loop" || key == "templateData" || key == "templateData.Values" {
				continue
			}
			result[key] = s.deepCopyChartDataValue(val, visited)
		}
		return result
	case []interface{}:
		result := make([]interface{}, 0, len(v))
		for _, val := range v {
			// Only copy simple types to avoid circular references
			switch val.(type) {
			case string, int, int64, float64, bool:
				result = append(result, val)
			default:
				// Skip complex types to avoid cycles
			}
		}
		return result
	case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return v // Return primitive types as-is
	default:
		// For other types, try to convert to string or return nil
		if str, ok := v.(fmt.Stringer); ok {
			return str.String()
		}
		return fmt.Sprintf("%v", v)
	}
}

// createSafePendingDataCopy creates a safe copy of pending data to avoid circular references
func (s *DatabaseUpdateStep) createSafePendingDataCopy(pendingData *types.AppInfoLatestPendingData) map[string]interface{} {
	if pendingData == nil {
		return nil
	}

	safeCopy := map[string]interface{}{
		"type":             pendingData.Type,
		"timestamp":        pendingData.Timestamp,
		"version":          pendingData.Version,
		"raw_package":      pendingData.RawPackage,
		"rendered_package": pendingData.RenderedPackage,
	}

	// Only include basic information from RawData to avoid cycles
	if pendingData.RawData != nil {
		rawDataMap := map[string]interface{}{
			"id":     pendingData.RawData.ID,
			"name":   pendingData.RawData.Name,
			"app_id": pendingData.RawData.AppID,
		}
		// Marshal each field separately to locate cycle
		fields := []struct {
			name string
			val  interface{}
		}{
			{"options", pendingData.RawData.Options},
			{"supportClient", pendingData.RawData.SupportClient},
			{"permission", pendingData.RawData.Permission},
			{"middleware", pendingData.RawData.Middleware},
			{"i18n", pendingData.RawData.I18n},
			{"metadata", pendingData.RawData.Metadata},
		}
		for _, f := range fields {
			if f.val != nil {
				if _, err := json.Marshal(f.val); err != nil {
					log.Printf("DEBUG: JSON marshal failed for RawData.%s: %v", f.name, err)
				} else {
					log.Printf("DEBUG: JSON marshal success for RawData.%s", f.name)
				}
			}
			rawDataMap[f.name] = convertToStringMapDBUSWithLog(f.val, "RawData."+f.name)
		}
		safeCopy["raw_data"] = rawDataMap
	}

	// Only include basic information from AppInfo to avoid cycles
	if pendingData.AppInfo != nil && pendingData.AppInfo.AppEntry != nil {
		appEntryMap := map[string]interface{}{
			"id":     pendingData.AppInfo.AppEntry.ID,
			"name":   pendingData.AppInfo.AppEntry.Name,
			"app_id": pendingData.AppInfo.AppEntry.AppID,
		}
		fields := []struct {
			name string
			val  interface{}
		}{
			{"options", pendingData.AppInfo.AppEntry.Options},
			{"supportClient", pendingData.AppInfo.AppEntry.SupportClient},
			{"permission", pendingData.AppInfo.AppEntry.Permission},
			{"middleware", pendingData.AppInfo.AppEntry.Middleware},
			{"i18n", pendingData.AppInfo.AppEntry.I18n},
			{"metadata", pendingData.AppInfo.AppEntry.Metadata},
		}
		for _, f := range fields {
			if f.val != nil {
				if _, err := json.Marshal(f.val); err != nil {
					log.Printf("DEBUG: JSON marshal failed for AppEntry.%s: %v", f.name, err)
				} else {
					log.Printf("DEBUG: JSON marshal success for AppEntry.%s", f.name)
				}
			}
			appEntryMap[f.name] = convertToStringMapDBUSWithLog(f.val, "AppEntry."+f.name)
		}
		appInfoMap := map[string]interface{}{
			"app_entry": appEntryMap,
		}
		safeCopy["app_info"] = appInfoMap
	}

	// Include Values if they exist
	if pendingData.Values != nil {
		safeCopy["values"] = pendingData.Values
	}

	return safeCopy
}

func convertToStringMapDBUSWithLog(val interface{}, field string) map[string]interface{} {
	if val != nil {
		if _, err := json.Marshal(val); err != nil {
			log.Printf("DEBUG: JSON marshal failed inside convertToStringMapDBUS for %s: %v", field, err)
		} else {
			log.Printf("DEBUG: JSON marshal success inside convertToStringMapDBUS for %s", field)
		}
	}
	return convertToStringMapDBUS(val)
}

func convertToStringMapDBUS(val interface{}) map[string]interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		return v
	case map[interface{}]interface{}:
		converted := make(map[string]interface{})
		for k, v2 := range v {
			if ks, ok := k.(string); ok {
				converted[ks] = v2
			}
		}
		return converted
	case nil:
		return nil
	default:
		return nil
	}
}
