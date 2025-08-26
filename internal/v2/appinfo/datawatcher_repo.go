package appinfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"market/internal/v2/types"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// StateChange represents a single state change record from the API
type StateChange struct {
	ID        int64                 `json:"id"`                   // Auto-increment ID
	Type      string                `json:"type"`                 // Type of state change
	AppData   *StateChangeAppData   `json:"app_data,omitempty"`   // App upload data
	ImageData *StateChangeImageData `json:"image_data,omitempty"` // Image update data
	Timestamp time.Time             `json:"timestamp"`            // When the change occurred
}

// StateChangeAppData represents data for app upload completed state
type StateChangeAppData struct {
	Source  string `json:"source"`   // Data source
	AppName string `json:"app_name"` // Application name
	UserID  string `json:"user_id"`  // User ID
}

// StateChangeImageData represents data for image info updated state
type StateChangeImageData struct {
	ImageName string `json:"image_name"` // Image name
}

// StateChangesResponse represents the response from /state-changes API
type StateChangesResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    *StateChangesData `json:"data,omitempty"`
}

// StateChangesData represents the data field in the response
type StateChangesData struct {
	AfterID        int64          `json:"after_id"`
	Limit          int            `json:"limit"`
	TypeFilter     string         `json:"type_filter"`
	Count          int            `json:"count"`
	TotalAvailable int            `json:"total_available"`
	StateChanges   []*StateChange `json:"state_changes"`
}

// DataWatcherRepo represents the data watcher repository
type DataWatcherRepo struct {
	redisClient     *RedisClient // Change from *redis.Client to *RedisClient
	lastProcessedID int64
	apiBaseURL      string
	cacheManager    *CacheManager // Add cache manager reference
	dataWatcher     *DataWatcher  // Add DataWatcher reference for hash calculation
	mu              sync.RWMutex
	ticker          *time.Ticker
	stopChannel     chan bool
	isRunning       bool
}

// NewDataWatcherRepo creates a new data watcher repository instance
func NewDataWatcherRepo(redisClient *RedisClient, cacheManager *CacheManager, dataWatcher *DataWatcher) *DataWatcherRepo {
	// Get API base URL from environment variable
	apiBaseURL := os.Getenv("CHART_REPO_SERVICE_HOST")
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:8080" // Default fallback
		log.Printf("CHART_REPO_SERVICE_HOST not set, using default: %s", apiBaseURL)
	}

	repo := &DataWatcherRepo{
		redisClient:  redisClient,
		apiBaseURL:   apiBaseURL,
		cacheManager: cacheManager,
		dataWatcher:  dataWatcher,
		stopChannel:  make(chan bool),
	}

	// Initialize last processed ID from Redis
	repo.initializeLastProcessedID()

	return repo
}

// initializeLastProcessedID retrieves the last processed ID from Redis
func (dwr *DataWatcherRepo) initializeLastProcessedID() error {
	ctx := context.Background()

	// Get the last processed ID from Redis
	lastIDStr, err := dwr.redisClient.client.Get(ctx, "datawatcher:last_processed_id").Result()
	if err != nil {
		if err == redis.Nil {
			// No record found, start from 0
			dwr.lastProcessedID = 0
			log.Printf("No previous state changes found, starting from ID 0")
			return nil
		}
		log.Printf("Error retrieving last processed ID from Redis: %v", err)
		return err
	}

	lastID, err := strconv.ParseInt(lastIDStr, 10, 64)
	if err != nil {
		log.Printf("Error parsing last processed ID from Redis: %v", err)
		dwr.lastProcessedID = 0
		return nil
	}

	dwr.lastProcessedID = lastID
	log.Printf("Initialized last processed ID from Redis: %d", dwr.lastProcessedID)
	return nil
}

// Start begins the periodic state checking process
func (dwr *DataWatcherRepo) Start() error {
	dwr.mu.Lock()
	defer dwr.mu.Unlock()

	if dwr.isRunning {
		return fmt.Errorf("data watcher is already running")
	}

	// Create ticker for 2-minute intervals
	dwr.ticker = time.NewTicker(2 * time.Minute)
	dwr.isRunning = true

	log.Printf("Starting data watcher with 2-minute intervals")

	// Start the monitoring goroutine
	go dwr.monitorStateChanges()

	return nil
}

// Stop stops the periodic state checking process
func (dwr *DataWatcherRepo) Stop() error {
	dwr.mu.Lock()
	defer dwr.mu.Unlock()

	if !dwr.isRunning {
		return fmt.Errorf("data watcher is not running")
	}

	// Stop the ticker
	if dwr.ticker != nil {
		dwr.ticker.Stop()
	}

	// Signal the monitoring goroutine to stop
	close(dwr.stopChannel)
	dwr.isRunning = false

	log.Printf("Data watcher stopped")
	return nil
}

// IsRunning returns whether the data watcher is currently running
func (dwr *DataWatcherRepo) IsRunning() bool {
	dwr.mu.RLock()
	defer dwr.mu.RUnlock()
	return dwr.isRunning
}

// monitorStateChanges runs the main monitoring loop
func (dwr *DataWatcherRepo) monitorStateChanges() {
	log.Printf("State change monitoring started")

	// Process immediately on start
	if err := dwr.processStateChanges(); err != nil {
		log.Printf("Error processing state changes on startup: %v", err)
	}

	for {
		select {
		case <-dwr.ticker.C:
			if err := dwr.processStateChanges(); err != nil {
				log.Printf("Error processing state changes: %v", err)
			}
		case <-dwr.stopChannel:
			log.Printf("State change monitoring stopped")
			return
		}
	}
}

// processStateChanges fetches and processes new state changes
func (dwr *DataWatcherRepo) processStateChanges() error {
	log.Printf("Processing state changes after ID: %d", dwr.lastProcessedID)

	// Fetch new state changes from API
	stateChanges, err := dwr.fetchStateChanges(dwr.lastProcessedID)
	if err != nil {
		return fmt.Errorf("failed to fetch state changes: %w", err)
	}

	if len(stateChanges) == 0 {
		log.Printf("No new state changes found")
		return nil
	}

	log.Printf("Found %d new state changes", len(stateChanges))

	// Sort state changes by ID to ensure proper order
	sort.Slice(stateChanges, func(i, j int) bool {
		return stateChanges[i].ID < stateChanges[j].ID
	})

	log.Printf("State changes sorted by ID, processing in order...")

	// Process state changes in order by ID
	var lastProcessedID int64
	for _, change := range stateChanges {
		if err := dwr.processStateChange(change); err != nil {
			log.Printf("Error processing state change ID %d: %v", change.ID, err)
			continue
		}

		lastProcessedID = change.ID
	}

	// Update the last processed ID in Redis
	ctx := context.Background()
	err = dwr.redisClient.client.Set(ctx, "datawatcher:last_processed_id", strconv.FormatInt(lastProcessedID, 10), 0).Err()
	if err != nil {
		log.Printf("Failed to update last processed ID in Redis: %v", err)
	}

	return nil
}

// fetchStateChanges calls the /state-changes API to get new state changes
func (dwr *DataWatcherRepo) fetchStateChanges(afterID int64) ([]*StateChange, error) {
	url := fmt.Sprintf("http://s%s/chart-repo/api/v2/state-changes?after_id=%d&limit=1000", dwr.apiBaseURL, afterID)

	log.Printf("Fetching state changes from: %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var apiResponse StateChangesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	if !apiResponse.Success {
		return nil, fmt.Errorf("API request failed: %s", apiResponse.Message)
	}

	if apiResponse.Data == nil {
		return nil, fmt.Errorf("API response data is nil")
	}

	log.Printf("Successfully fetched %d state changes", apiResponse.Data.Count)
	return apiResponse.Data.StateChanges, nil
}

// processStateChange processes a single state change based on its type
func (dwr *DataWatcherRepo) processStateChange(change *StateChange) error {
	log.Printf("Processing state change ID %d, type: %s", change.ID, change.Type)

	switch change.Type {
	case "app_upload_completed":
		return dwr.handleAppUploadCompleted(change)
	case "image_info_updated":
		return dwr.handleImageInfoUpdated(change)
	default:
		log.Printf("Unknown state change type: %s, skipping", change.Type)
		return nil
	}
}

// handleAppUploadCompleted handles app upload completed state changes
func (dwr *DataWatcherRepo) handleAppUploadCompleted(change *StateChange) error {
	log.Printf("Handling app upload completed for app: %s, source: %s, user: %s",
		change.AppData.AppName, change.AppData.Source, change.AppData.UserID)

	// Check if cache manager is available
	if dwr.cacheManager == nil {
		log.Printf("Cache manager is not available, skipping app upload completed handling")
		return fmt.Errorf("cache manager not available")
	}

	// Step 1: Fetch app information from API directly
	log.Printf("Fetching app info from API for app: %s, user: %s, source: %s",
		change.AppData.AppName, change.AppData.UserID, change.AppData.Source)

	appInfo, err := dwr.fetchAppInfoFromAPI(change.AppData.UserID, change.AppData.Source, change.AppData.AppName)
	if err != nil {
		log.Printf("Failed to fetch app info from API: %v", err)
		return fmt.Errorf("failed to fetch app info from API: %w", err)
	}

	// Step 2: Check if the app exists in cache and compare versions
	shouldUpdate := dwr.shouldUpdateAppInCache(change.AppData.UserID, change.AppData.Source, change.AppData.AppName, appInfo)

	if !shouldUpdate {
		log.Printf("App %s already exists in cache with same or newer version for user %s, source %s",
			change.AppData.AppName, change.AppData.UserID, change.AppData.Source)
		return nil
	}

	log.Printf("App %s needs update in cache for user %s, source %s",
		change.AppData.AppName, change.AppData.UserID, change.AppData.Source)

	// Step 3: Update cache with the fetched app information
	err = dwr.updateCacheWithAppInfo(change.AppData.UserID, change.AppData.Source, appInfo)
	if err != nil {
		log.Printf("Failed to update cache with app info: %v", err)
		return fmt.Errorf("failed to update cache with app info: %w", err)
	}

	log.Printf("Successfully updated cache with app info for app %s, user %s, source %s",
		change.AppData.AppName, change.AppData.UserID, change.AppData.Source)

	return nil
}

// handleImageInfoUpdated handles image info updated state changes
func (dwr *DataWatcherRepo) handleImageInfoUpdated(change *StateChange) error {
	log.Printf("Handling image info updated for image: %s", change.ImageData.ImageName)

	if dwr.cacheManager == nil {
		log.Printf("Cache manager is not available, skipping image info updated handling")
		return fmt.Errorf("cache manager not available")
	}

	// Step 1: Fetch updated image information from API
	imageName := change.ImageData.ImageName
	updatedImageInfo, err := dwr.fetchImageInfoFromAPI(imageName)
	if err != nil {
		log.Printf("Failed to fetch image info from API for image %s: %v", imageName, err)
		return fmt.Errorf("failed to fetch image info from API: %w", err)
	}

	log.Printf("Successfully fetched updated image info for %s", imageName)

	// Step 2: Update image information in all cache data
	updatedCount := dwr.updateImageInfoInCache(imageName, updatedImageInfo)
	log.Printf("Updated image info for %s in %d cache entries", imageName, updatedCount)

	// Step 3: Trigger hash calculation for all users
	if dwr.dataWatcher != nil {
		if err := dwr.dataWatcher.ForceCalculateAllUsersHash(); err != nil {
			log.Printf("Failed to trigger hash calculation for all users: %v", err)
			return fmt.Errorf("failed to trigger hash calculation: %w", err)
		}
		log.Printf("Successfully triggered hash calculation for all users after image update")
	} else {
		log.Printf("DataWatcher not available, skipping hash calculation")
	}

	log.Printf("Successfully handled image info updated for image: %s", imageName)
	return nil
}

// checkAppInCache checks if an app exists in the cache
func (dwr *DataWatcherRepo) checkAppInCache(userID, sourceID, appName string) bool {
	if dwr.cacheManager == nil {
		log.Printf("Cache manager is nil, cannot check cache for app: %s", appName)
		return false
	}

	// Get user data from cache
	userData := dwr.cacheManager.GetUserData(userID)
	if userData == nil {
		log.Printf("User data not found in cache for user: %s", userID)
		return false
	}

	// Check if source exists
	sourceData, exists := userData.Sources[sourceID]
	if !exists {
		log.Printf("Source %s not found in cache for user: %s", sourceID, userID)
		return false
	}

	// Check in AppInfoLatest
	for _, appInfo := range sourceData.AppInfoLatest {
		if appInfo != nil && appInfo.RawData != nil {
			if appInfo.RawData.Name == appName || appInfo.RawData.AppID == appName || appInfo.RawData.ID == appName {
				log.Printf("App %s found in AppInfoLatest cache", appName)
				return true
			}
		}
	}

	// Check in AppStateLatest
	for _, appState := range sourceData.AppStateLatest {
		if appState != nil && appState.Status.Name == appName {
			log.Printf("App %s found in AppStateLatest cache", appName)
			return true
		}
	}

	// Check in AppInfoLatestPending
	for _, appPending := range sourceData.AppInfoLatestPending {
		if appPending != nil && appPending.RawData != nil {
			if appPending.RawData.Name == appName || appPending.RawData.AppID == appName || appPending.RawData.ID == appName {
				log.Printf("App %s found in AppInfoLatestPending cache", appName)
				return true
			}
		}
	}

	log.Printf("App %s not found in any cache data for user: %s, source: %s", appName, userID, sourceID)
	return false
}

// fetchAppInfoFromAPI fetches app information from the /apps API endpoint
func (dwr *DataWatcherRepo) fetchAppInfoFromAPI(userID, sourceID, appName string) (map[string]interface{}, error) {
	// Prepare request payload
	requestPayload := map[string]interface{}{
		"apps": []map[string]string{
			{
				"appid":          appName,
				"sourceDataName": sourceID,
			},
		},
	}

	// Convert to JSON
	jsonData, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	// Make HTTP request to /apps endpoint
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/apps", dwr.apiBaseURL)
	log.Printf("Fetching app info from API: %s for app: %s, user: %s, source: %s", url, appName, userID, sourceID)

	// Create HTTP request with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Note: In a real implementation, you might need to add authentication headers here

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d for app: %s", resp.StatusCode, appName)
	}

	// Parse response
	var apiResponse struct {
		Success bool                   `json:"success"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response for app %s: %w", appName, err)
	}

	if !apiResponse.Success {
		return nil, fmt.Errorf("API request failed for app %s: %s", appName, apiResponse.Message)
	}

	// Extract apps data from response
	if appsData, ok := apiResponse.Data["apps"]; ok {
		if apps, ok := appsData.([]interface{}); ok && len(apps) > 0 {
			if appInfo, ok := apps[0].(map[string]interface{}); ok {
				log.Printf("Successfully fetched app info from API for app: %s", appName)
				return appInfo, nil
			}
		}
	}

	return nil, fmt.Errorf("no app data found in API response for app: %s", appName)
}

// updateCacheWithAppInfo updates the cache with the fetched app information
func (dwr *DataWatcherRepo) updateCacheWithAppInfo(userID, sourceID string, appInfo map[string]interface{}) error {
	if dwr.cacheManager == nil {
		return fmt.Errorf("cache manager not available")
	}

	log.Printf("Updating cache with app info for user: %s, source: %s", userID, sourceID)

	// Convert the app info to the appropriate data type and update cache
	// We'll use AppInfoLatestPending as the data type for newly uploaded apps
	err := dwr.cacheManager.SetAppData(userID, sourceID, AppInfoLatestPending, appInfo)
	if err != nil {
		log.Printf("Failed to set app data in cache: %v", err)
		return fmt.Errorf("failed to set app data in cache: %w", err)
	}

	log.Printf("Successfully updated cache with app info for user: %s, source: %s", userID, sourceID)
	return nil
}

// SetCacheManager sets the cache manager for the data watcher repository
func (dwr *DataWatcherRepo) SetCacheManager(cacheManager *CacheManager) {
	dwr.mu.Lock()
	defer dwr.mu.Unlock()
	dwr.cacheManager = cacheManager
	log.Printf("Cache manager set for data watcher repository")
}

// GetCacheManager returns the current cache manager
func (dwr *DataWatcherRepo) GetCacheManager() *CacheManager {
	dwr.mu.RLock()
	defer dwr.mu.RUnlock()
	return dwr.cacheManager
}

// SetDataWatcher sets the DataWatcher reference for hash calculation
func (dwr *DataWatcherRepo) SetDataWatcher(dataWatcher *DataWatcher) {
	dwr.dataWatcher = dataWatcher
}

// GetDataWatcher returns the DataWatcher reference
func (dwr *DataWatcherRepo) GetDataWatcher() *DataWatcher {
	return dwr.dataWatcher
}

// GetLastProcessedID returns the last processed ID
func (dwr *DataWatcherRepo) GetLastProcessedID() int64 {
	dwr.mu.RLock()
	defer dwr.mu.RUnlock()
	return dwr.lastProcessedID
}

// GetApiBaseURL returns the current API base URL
func (dwr *DataWatcherRepo) GetApiBaseURL() string {
	return dwr.apiBaseURL
}

// SetApiBaseURL updates the API base URL
func (dwr *DataWatcherRepo) SetApiBaseURL(url string) {
	dwr.mu.Lock()
	defer dwr.mu.Unlock()
	dwr.apiBaseURL = url
	log.Printf("API base URL updated to: %s", url)
}

// shouldUpdateAppInCache checks if an app should be updated in cache based on existence and version comparison
func (dwr *DataWatcherRepo) shouldUpdateAppInCache(userID, sourceID, appName string, newAppInfo map[string]interface{}) bool {
	if dwr.cacheManager == nil {
		log.Printf("Cache manager is nil, should update app: %s", appName)
		return true
	}

	// Get user data from cache
	userData := dwr.cacheManager.GetUserData(userID)
	if userData == nil {
		log.Printf("User data not found in cache for user: %s, should update app: %s", userID, appName)
		return true
	}

	// Check if source exists
	sourceData, exists := userData.Sources[sourceID]
	if !exists {
		log.Printf("Source %s not found in cache for user: %s, should update app: %s", sourceID, userID, appName)
		return true
	}

	// Extract version from new app info
	newVersion := dwr.extractVersionFromAppInfo(newAppInfo)
	if newVersion == "" {
		log.Printf("Could not extract version from new app info for app: %s, should update", appName)
		return true
	}

	log.Printf("New app version: %s for app: %s", newVersion, appName)

	// Check in AppInfoLatest
	for _, appInfo := range sourceData.AppInfoLatest {
		if appInfo != nil && appInfo.RawData != nil {
			if dwr.isSameApp(appInfo.RawData, appName) {
				existingVersion := dwr.extractVersionFromAppInfoLatest(appInfo)
				log.Printf("Found app in AppInfoLatest with version: %s", existingVersion)
				return dwr.shouldUpdateVersion(existingVersion, newVersion)
			}
		}
	}

	// Check in AppStateLatest
	for _, appState := range sourceData.AppStateLatest {
		if appState != nil && appState.Status.Name == appName {
			existingVersion := dwr.extractVersionFromAppState(appState)
			log.Printf("Found app in AppStateLatest with version: %s", existingVersion)
			return dwr.shouldUpdateVersion(existingVersion, newVersion)
		}
	}

	// Check in AppInfoLatestPending
	for _, appPending := range sourceData.AppInfoLatestPending {
		if appPending != nil && appPending.RawData != nil {
			if dwr.isSameApp(appPending.RawData, appName) {
				existingVersion := dwr.extractVersionFromAppInfoLatest(appPending)
				log.Printf("Found app in AppInfoLatestPending with version: %s", existingVersion)
				return dwr.shouldUpdateVersion(existingVersion, newVersion)
			}
		}
	}

	log.Printf("App %s not found in any cache data, should update", appName)
	return true
}

// isSameApp checks if the app data represents the same app
func (dwr *DataWatcherRepo) isSameApp(rawData interface{}, appName string) bool {
	// Try to convert to map to access fields
	if appData, ok := rawData.(map[string]interface{}); ok {
		// Check multiple possible ID fields
		if name, exists := appData["name"].(string); exists && name == appName {
			return true
		}
		if appID, exists := appData["appID"].(string); exists && appID == appName {
			return true
		}
		if id, exists := appData["id"].(string); exists && id == appName {
			return true
		}
	}
	return false
}

// extractVersionFromAppInfo extracts version from app info map
func (dwr *DataWatcherRepo) extractVersionFromAppInfo(appInfo map[string]interface{}) string {
	// Try to extract version from different possible locations
	if version, ok := appInfo["version"].(string); ok && version != "" {
		return version
	}

	// Check in nested structures
	if rawData, ok := appInfo["raw_data"].(map[string]interface{}); ok {
		if version, ok := rawData["version"].(string); ok && version != "" {
			return version
		}
	}

	if appInfoData, ok := appInfo["app_info"].(map[string]interface{}); ok {
		if version, ok := appInfoData["version"].(string); ok && version != "" {
			return version
		}
	}

	return ""
}

// extractVersionFromAppInfoLatest extracts version from AppInfoLatestData
func (dwr *DataWatcherRepo) extractVersionFromAppInfoLatest(appInfo interface{}) string {
	// Try to convert to map to access fields
	if appData, ok := appInfo.(map[string]interface{}); ok {
		if version, ok := appData["version"].(string); ok && version != "" {
			return version
		}
	}
	return ""
}

// extractVersionFromAppState extracts version from AppStateLatestData
func (dwr *DataWatcherRepo) extractVersionFromAppState(appState interface{}) string {
	// Try to convert to map to access fields
	if stateData, ok := appState.(map[string]interface{}); ok {
		if version, ok := stateData["version"].(string); ok && version != "" {
			return version
		}
	}
	return ""
}

// shouldUpdateVersion compares two version strings and determines if an update is needed
func (dwr *DataWatcherRepo) shouldUpdateVersion(existingVersion, newVersion string) bool {
	if existingVersion == "" {
		log.Printf("Existing version is empty, should update to: %s", newVersion)
		return true
	}

	if newVersion == "" {
		log.Printf("New version is empty, should not update from: %s", existingVersion)
		return false
	}

	// Simple string comparison for now
	// In a production environment, you might want to use semantic versioning comparison
	if existingVersion != newVersion {
		log.Printf("Version mismatch: existing=%s, new=%s, should update", existingVersion, newVersion)
		return true
	}

	log.Printf("Version match: existing=%s, new=%s, no update needed", existingVersion, newVersion)
	return false
}

// fetchImageInfoFromAPI fetches image information from the /images/{imageName} API endpoint
func (dwr *DataWatcherRepo) fetchImageInfoFromAPI(imageName string) (map[string]interface{}, error) {
	// Make HTTP request to /images/{imageName} endpoint
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/images/%s", dwr.apiBaseURL, imageName)
	log.Printf("Fetching image info from API: %s for image: %s", url, imageName)

	// Create HTTP request with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var apiResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	// Check if the response indicates success
	if success, ok := apiResponse["success"].(bool); !ok || !success {
		message := "unknown error"
		if msg, ok := apiResponse["message"].(string); ok {
			message = msg
		}
		return nil, fmt.Errorf("API request failed: %s", message)
	}

	// Extract the image info data
	if data, ok := apiResponse["data"].(map[string]interface{}); ok {
		if imageInfo, ok := data["image_info"].(map[string]interface{}); ok {
			return imageInfo, nil
		}
	}

	return nil, fmt.Errorf("invalid API response format: missing image_info data")
}

// updateImageInfoInCache updates image information in all cache data for the specified image
func (dwr *DataWatcherRepo) updateImageInfoInCache(imageName string, updatedImageInfo map[string]interface{}) int {
	updatedCount := 0

	// Get all users data from cache
	allUsersData := dwr.cacheManager.GetAllUsersData()
	if len(allUsersData) == 0 {
		log.Printf("No users found in cache, skipping image info update")
		return 0
	}

	// Iterate through all users and sources
	for userID, userData := range allUsersData {
		if userData == nil {
			continue
		}

		for sourceID, sourceData := range userData.Sources {
			if sourceData == nil {
				continue
			}

			// Update image info in AppInfoLatest
			updatedCount += dwr.updateImageInfoInAppInfoLatest(userID, sourceID, sourceData, imageName, updatedImageInfo)

			// Update image info in AppInfoLatestPending
			updatedCount += dwr.updateImageInfoInAppInfoLatestPending(userID, sourceID, sourceData, imageName, updatedImageInfo)
		}
	}

	return updatedCount
}

// updateImageInfoInAppInfoLatest updates image information in AppInfoLatest list
func (dwr *DataWatcherRepo) updateImageInfoInAppInfoLatest(userID, sourceID string, sourceData *types.SourceData, imageName string, updatedImageInfo map[string]interface{}) int {
	updatedCount := 0

	for _, appInfo := range sourceData.AppInfoLatest {
		if appInfo == nil || appInfo.AppInfo == nil || appInfo.AppInfo.ImageAnalysis == nil {
			continue
		}

		// Check if this app contains the target image
		if appInfo.AppInfo.ImageAnalysis.Images != nil {
			if imageInfo, exists := appInfo.AppInfo.ImageAnalysis.Images[imageName]; exists {
				// Update the image info with new data
				if err := dwr.updateSingleImageInfo(imageInfo, updatedImageInfo); err != nil {
					log.Printf("Failed to update image info for app %s in AppInfoLatest: %v",
						appInfo.RawData.Name, err)
					continue
				}
				updatedCount++
				log.Printf("Updated image info for %s in AppInfoLatest for app %s (user: %s, source: %s)",
					imageName, appInfo.RawData.Name, userID, sourceID)
			}
		}
	}

	return updatedCount
}

// updateImageInfoInAppInfoLatestPending updates image information in AppInfoLatestPending list
func (dwr *DataWatcherRepo) updateImageInfoInAppInfoLatestPending(userID, sourceID string, sourceData *types.SourceData, imageName string, updatedImageInfo map[string]interface{}) int {
	updatedCount := 0

	for _, pendingApp := range sourceData.AppInfoLatestPending {
		if pendingApp == nil || pendingApp.AppInfo == nil || pendingApp.AppInfo.ImageAnalysis == nil {
			continue
		}

		// Check if this app contains the target image
		if pendingApp.AppInfo.ImageAnalysis.Images != nil {
			if imageInfo, exists := pendingApp.AppInfo.ImageAnalysis.Images[imageName]; exists {
				// Update the image info with new data
				if err := dwr.updateSingleImageInfo(imageInfo, updatedImageInfo); err != nil {
					log.Printf("Failed to update image info for app %s in AppInfoLatestPending: %v",
						pendingApp.RawData.Name, err)
					continue
				}
				updatedCount++
				log.Printf("Updated image info for %s in AppInfoLatestPending for app %s (user: %s, source: %s)",
					imageName, pendingApp.RawData.Name, userID, sourceID)
			}
		}
	}

	return updatedCount
}

// updateSingleImageInfo updates a single ImageInfo struct with new data
func (dwr *DataWatcherRepo) updateSingleImageInfo(imageInfo *types.ImageInfo, updatedData map[string]interface{}) error {
	if imageInfo == nil {
		return fmt.Errorf("imageInfo is nil")
	}

	// Update basic fields if present in updated data
	if tag, ok := updatedData["tag"].(string); ok {
		imageInfo.Tag = tag
	}
	if architecture, ok := updatedData["architecture"].(string); ok {
		imageInfo.Architecture = architecture
	}
	if totalSize, ok := updatedData["total_size"].(float64); ok {
		imageInfo.TotalSize = int64(totalSize)
	}
	if downloadedSize, ok := updatedData["downloaded_size"].(float64); ok {
		imageInfo.DownloadedSize = int64(downloadedSize)
	}
	if downloadProgress, ok := updatedData["download_progress"].(float64); ok {
		imageInfo.DownloadProgress = downloadProgress
	}
	if layerCount, ok := updatedData["layer_count"].(float64); ok {
		imageInfo.LayerCount = int(layerCount)
	}
	if downloadedLayers, ok := updatedData["downloaded_layers"].(float64); ok {
		imageInfo.DownloadedLayers = int(downloadedLayers)
	}
	if status, ok := updatedData["status"].(string); ok {
		imageInfo.Status = status
	}
	if errorMessage, ok := updatedData["error_message"].(string); ok {
		imageInfo.ErrorMessage = errorMessage
	}

	// Update timestamp
	imageInfo.AnalyzedAt = time.Now()

	// Update nodes if present
	if nodesData, ok := updatedData["nodes"].([]interface{}); ok {
		imageInfo.Nodes = dwr.convertNodesData(nodesData)
	}

	return nil
}

// convertNodesData converts nodes data from API response to NodeInfo slice
func (dwr *DataWatcherRepo) convertNodesData(nodesData []interface{}) []*types.NodeInfo {
	nodes := make([]*types.NodeInfo, 0, len(nodesData))

	for _, nodeData := range nodesData {
		if nodeMap, ok := nodeData.(map[string]interface{}); ok {
			nodeInfo := &types.NodeInfo{}

			// Extract basic node information
			if nodeName, ok := nodeMap["node_name"].(string); ok {
				nodeInfo.NodeName = nodeName
			}
			if architecture, ok := nodeMap["architecture"].(string); ok {
				nodeInfo.Architecture = architecture
			}
			if variant, ok := nodeMap["variant"].(string); ok {
				nodeInfo.Variant = variant
			}
			if os, ok := nodeMap["os"].(string); ok {
				nodeInfo.OS = os
			}
			if totalSize, ok := nodeMap["total_size"].(float64); ok {
				nodeInfo.TotalSize = int64(totalSize)
			}
			if layerCount, ok := nodeMap["layer_count"].(float64); ok {
				nodeInfo.LayerCount = int(layerCount)
			}

			// Extract layers information
			if layersData, ok := nodeMap["layers"].([]interface{}); ok {
				nodeInfo.Layers = dwr.convertLayersData(layersData)
			}

			nodes = append(nodes, nodeInfo)
		}
	}

	return nodes
}

// convertLayersData converts layers data from API response to LayerInfo slice
func (dwr *DataWatcherRepo) convertLayersData(layersData []interface{}) []*types.LayerInfo {
	layers := make([]*types.LayerInfo, 0, len(layersData))

	for _, layerData := range layersData {
		if layerMap, ok := layerData.(map[string]interface{}); ok {
			layerInfo := &types.LayerInfo{}

			// Extract basic layer information
			if digest, ok := layerMap["digest"].(string); ok {
				layerInfo.Digest = digest
			}
			if size, ok := layerMap["size"].(float64); ok {
				layerInfo.Size = int64(size)
			}
			if mediaType, ok := layerMap["media_type"].(string); ok {
				layerInfo.MediaType = mediaType
			}
			if offset, ok := layerMap["offset"].(float64); ok {
				layerInfo.Offset = int64(offset)
			}
			if downloaded, ok := layerMap["downloaded"].(bool); ok {
				layerInfo.Downloaded = downloaded
			}
			if progress, ok := layerMap["progress"].(float64); ok {
				layerInfo.Progress = int(progress)
			}
			if localPath, ok := layerMap["local_path"].(string); ok {
				layerInfo.LocalPath = localPath
			}

			layers = append(layers, layerInfo)
		}
	}

	return layers
}
