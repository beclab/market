package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"market/internal/v2/types"

	"github.com/Masterminds/semver/v3"
)

// AppServiceResponse represents the response structure from app-service
type AppServiceResponse struct {
	Metadata struct {
		Name      string `json:"name"`
		UID       string `json:"uid"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Name      string `json:"name"`
		AppID     string `json:"appid"`
		Owner     string `json:"owner"`
		Icon      string `json:"icon"`
		Title     string `json:"title"`
		Source    string `json:"source"`
		Entrances []struct {
			Name      string `json:"name"`
			Url       string `json:"url"`
			Invisible bool   `json:"invisible"`
		} `json:"entrances"`
	} `json:"spec"`
	Status struct {
		State              string `json:"state"`
		UpdateTime         string `json:"updateTime"`
		StatusTime         string `json:"statusTime"`
		LastTransitionTime string `json:"lastTransitionTime"`
		EntranceStatuses   []struct {
			ID         string `json:"id"` // ID extracted from URL's first segment after splitting by "."
			Name       string `json:"name"`
			State      string `json:"state"`
			StatusTime string `json:"statusTime"`
			Reason     string `json:"reason"`
			Url        string `json:"url"`
		} `json:"entranceStatuses"`
	} `json:"status"`
}

// AppInfo represents the extracted app information
type AppInfo struct {
	User   string `json:"user"`
	App    string `json:"app"`
	Status struct {
		State              string `json:"state"`
		UpdateTime         string `json:"updateTime"`
		StatusTime         string `json:"statusTime"`
		LastTransitionTime string `json:"lastTransitionTime"`
		EntranceStatuses   []struct {
			ID         string `json:"id"` // ID extracted from URL's first segment after splitting by "."
			Name       string `json:"name"`
			State      string `json:"state"`
			StatusTime string `json:"statusTime"`
			Reason     string `json:"reason"`
			Url        string `json:"url"`
		} `json:"entranceStatuses"`
	} `json:"status"`
}

// Global variable to store extracted users
var extractedUsers []string

// Global variable to store app state data for each user
var userAppStateData map[string]map[string][]*types.AppStateLatestData

// SetupAppServiceData fetches app data from app-service or reads from local file in development
func SetupAppServiceData() error {

	if IsPublicEnvironment() {
		return nil
	}

	log.Println("Starting app service data setup...")

	// Initialize user app state data map
	userAppStateData = make(map[string]map[string][]*types.AppStateLatestData)

	// Check if we're in development environment
	if isDevelopmentEnvironment() {
		log.Println("Development environment detected, reading from local app-state.json")
		return readLocalAppState()
	}

	// Production environment - fetch from app-service
	log.Println("Production environment detected, fetching from app-service")
	return fetchFromAppService()
}

// GetExtractedUsers returns the list of users extracted from app service data
func GetExtractedUsers() []string {
	if IsPublicEnvironment() {
		return []string{"admin"}
	}
	return extractedUsers
}

// GetUserAppStateData returns the app state data for a specific user
func GetUserAppStateData(userID string) map[string][]*types.AppStateLatestData {
	if data, exists := userAppStateData[userID]; exists {
		return data
	}
	return make(map[string][]*types.AppStateLatestData)
}

// GetAllUserAppStateData returns all user app state data
func GetAllUserAppStateData() map[string]map[string][]*types.AppStateLatestData {
	if IsPublicEnvironment() {
		return map[string]map[string][]*types.AppStateLatestData{
			"admin": {
				"app": {},
			},
		}
	}
	return userAppStateData
}

// isDevelopmentEnvironment checks if we're in development environment
func isDevelopmentEnvironment() bool {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	return env == "dev" || env == "development" || env == ""
}

// readLocalAppState reads and processes the local app-state.json file
func readLocalAppState() error {
	log.Println("Reading local app-state.json file...")

	file, err := os.Open("app-state.json")
	if err != nil {
		return fmt.Errorf("failed to open app-state.json: %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read app-state.json: %v", err)
	}

	var apps []AppServiceResponse
	if err := json.Unmarshal(data, &apps); err != nil {
		return fmt.Errorf("failed to parse app-state.json: %v", err)
	}

	return processAppData(apps)
}

// fetchFromAppService fetches app data from the app-service
func fetchFromAppService() error {
	// Fetch apps data
	if err := fetchAppsFromAppService(); err != nil {
		return fmt.Errorf("failed to fetch apps from app-service: %v", err)
	}

	// Fetch middlewares data
	if err := fetchMiddlewaresFromAppService(); err != nil {
		return fmt.Errorf("failed to fetch middlewares from app-service: %v", err)
	}

	return nil
}

// fetchAppsFromAppService fetches app data from the app-service
func fetchAppsFromAppService() error {
	host := os.Getenv("APP_SERVICE_SERVICE_HOST")
	port := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if host == "" {
		host = "localhost" // Default fallback
	}
	if port == "" {
		port = "80" // Default fallback
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", host, port)
	log.Printf("Fetching app data from: %s", url)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch from app-service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("app-service returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	var apps []AppServiceResponse
	if err := json.Unmarshal(data, &apps); err != nil {
		return fmt.Errorf("failed to parse app-service response: %v", err)
	}

	return processAppData(apps)
}

// processAppData processes the app data and prints the extracted information
func processAppData(apps []AppServiceResponse) error {
	log.Println("Processing app data...")
	log.Printf("Found %d applications", len(apps))

	var appInfos []AppInfo
	userSet := make(map[string]bool) // Use map to avoid duplicates

	for i, app := range apps {
		// Extract user (owner), app (name), and status
		user := app.Spec.Owner
		appName := app.Spec.Name

		// Add user to set to avoid duplicates
		userSet[user] = true

		appInfo := AppInfo{
			User:   user,
			App:    appName,
			Status: app.Status,
		}

		appInfos = append(appInfos, appInfo)

		// Print individual app info with full status structure
		log.Printf("App %d: User=%s, App=%s, Status=%+v", i+1, user, appName, app.Status)

		// Also print entrance statuses if available
		if len(app.Status.EntranceStatuses) > 0 {
			for j, entrance := range app.Status.EntranceStatuses {
				log.Printf("  Entrance %d: Name=%s, State=%s", j+1, entrance.Name, entrance.State)
			}
		}

		// Create AppStateLatestData for this app (startup process)
		appStateData, sourceID := createAppStateLatestData(app, true)

		// Add to user's app state data only if creation was successful
		if appStateData != nil {
			if userAppStateData[user] == nil {
				userAppStateData[user] = make(map[string][]*types.AppStateLatestData)
			}
			if userAppStateData[user][sourceID] == nil {
				userAppStateData[user][sourceID] = make([]*types.AppStateLatestData, 0)
			}
			userAppStateData[user][sourceID] = append(userAppStateData[user][sourceID], appStateData)
		}
	}

	// Convert user set to slice
	extractedUsers = make([]string, 0, len(userSet))
	for user := range userSet {
		extractedUsers = append(extractedUsers, user)
	}

	// Print summary
	log.Println("=== App Service Data Summary ===")
	log.Printf("Total applications: %d", len(appInfos))
	log.Printf("Extracted users: %v", extractedUsers)

	// Group by user
	userStats := make(map[string]int)
	for _, info := range appInfos {
		userStats[info.User]++
	}

	log.Println("Applications per user:")
	for user, count := range userStats {
		log.Printf("  %s: %d apps", user, count)
	}

	// Group by status
	statusStats := make(map[string]int)
	for _, info := range appInfos {
		statusStats[info.Status.State]++
	}

	log.Println("Applications by status:")
	for status, count := range statusStats {
		log.Printf("  %s: %d apps", status, count)
	}

	// Print app state data summary
	log.Println("App state data created:")
	for user, sourceData := range userAppStateData {
		for sourceID, appStates := range sourceData {
			log.Printf("  User %s: Source %s: %d app states", user, sourceID, len(appStates))
			for _, appState := range appStates {
				log.Printf("    - App Status: %s", appState.Status.State)
			}
		}
	}

	log.Println("=== End App Service Data Summary ===")

	return nil
}

// MiddlewareResponse represents the response structure from middleware status endpoint
type MiddlewareResponse struct {
	Code int `json:"code"`
	Data []struct {
		UUID           string `json:"uuid"`
		Namespace      string `json:"namespace"`
		User           string `json:"user"`
		ResourceStatus string `json:"resourceStatus"`
		ResourceType   string `json:"resourceType"`
		CreateTime     string `json:"createTime"`
		UpdateTime     string `json:"updateTime"`
		Metadata       struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Version string `json:"version"`
		Title   string `json:"title"`
	} `json:"data"`
}

// fetchMiddlewaresFromAppService fetches middleware data from the app-service
func fetchMiddlewaresFromAppService() error {
	host := os.Getenv("APP_SERVICE_SERVICE_HOST")
	port := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if host == "" {
		host = "localhost" // Default fallback
	}
	if port == "" {
		port = "80" // Default fallback
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/middlewares/status", host, port)
	log.Printf("Fetching middleware data from: %s", url)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch middlewares from app-service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("app-service middleware endpoint returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read middleware response body: %v", err)
	}

	var middlewareResp MiddlewareResponse
	if err := json.Unmarshal(data, &middlewareResp); err != nil {
		return fmt.Errorf("failed to parse middleware response: %v", err)
	}

	return processMiddlewareData(middlewareResp.Data)
}

// processMiddlewareData processes the middleware data and creates AppStateLatestData
func processMiddlewareData(middlewares []struct {
	UUID           string `json:"uuid"`
	Namespace      string `json:"namespace"`
	User           string `json:"user"`
	ResourceStatus string `json:"resourceStatus"`
	ResourceType   string `json:"resourceType"`
	CreateTime     string `json:"createTime"`
	UpdateTime     string `json:"updateTime"`
	Metadata       struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Version string `json:"version"`
	Title   string `json:"title"`
}) error {
	log.Println("Processing middleware data...")
	log.Printf("Found %d middlewares", len(middlewares))

	userSet := make(map[string]bool) // Use map to avoid duplicates

	for i, middleware := range middlewares {
		// Extract user and middleware name
		user := middleware.User
		middlewareName := middleware.Metadata.Name

		// Add user to set to avoid duplicates
		userSet[user] = true

		// Print individual middleware info
		log.Printf("Middleware %d: User=%s, Name=%s, Status=%s, Version=%s",
			i+1, user, middlewareName, middleware.ResourceStatus, middleware.Version)

		// Create AppStateLatestData for this middleware (startup process)
		middlewareStateData, sourceID := createMiddlewareStateLatestData(middleware, true)

		// Add to user's app state data only if creation was successful
		if middlewareStateData != nil {
			if userAppStateData[user] == nil {
				userAppStateData[user] = make(map[string][]*types.AppStateLatestData)
			}
			if userAppStateData[user][sourceID] == nil {
				userAppStateData[user][sourceID] = make([]*types.AppStateLatestData, 0)
			}
			userAppStateData[user][sourceID] = append(userAppStateData[user][sourceID], middlewareStateData)
		}
	}

	// Update extracted users with middleware users
	for user := range userSet {
		// Check if user already exists in extractedUsers
		exists := false
		for _, existingUser := range extractedUsers {
			if existingUser == user {
				exists = true
				break
			}
		}
		if !exists {
			extractedUsers = append(extractedUsers, user)
		}
	}

	// Print summary
	log.Println("=== Middleware Data Summary ===")
	log.Printf("Total middlewares: %d", len(middlewares))

	// Group by user
	userStats := make(map[string]int)
	for _, middleware := range middlewares {
		userStats[middleware.User]++
	}

	log.Println("Middlewares per user:")
	for user, count := range userStats {
		log.Printf("  %s: %d middlewares", user, count)
	}

	// Group by status
	statusStats := make(map[string]int)
	for _, middleware := range middlewares {
		statusStats[middleware.ResourceStatus]++
	}

	log.Println("Middlewares by status:")
	for status, count := range statusStats {
		log.Printf("  %s: %d middlewares", status, count)
	}

	log.Println("=== End Middleware Data Summary ===")

	return nil
}

// createMiddlewareStateLatestData creates AppStateLatestData from middleware data
// isStartupProcess indicates whether this is called during startup process
func createMiddlewareStateLatestData(middleware struct {
	UUID           string `json:"uuid"`
	Namespace      string `json:"namespace"`
	User           string `json:"user"`
	ResourceStatus string `json:"resourceStatus"`
	ResourceType   string `json:"resourceType"`
	CreateTime     string `json:"createTime"`
	UpdateTime     string `json:"updateTime"`
	Metadata       struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Version string `json:"version"`
	Title   string `json:"title"`
}, isStartupProcess bool) (*types.AppStateLatestData, string) {
	data := map[string]interface{}{
		"name":               middleware.Metadata.Name,
		"state":              middleware.ResourceStatus,
		"updateTime":         middleware.UpdateTime,
		"statusTime":         middleware.UpdateTime, // Use UpdateTime as StatusTime for middlewares
		"lastTransitionTime": middleware.UpdateTime, // Use UpdateTime as LastTransitionTime for middlewares
		"version":            middleware.Version,
	}

	// // Create entrance statuses for middleware (simplified structure)
	// entrances := make([]interface{}, 0, 1)
	// entrances = append(entrances, map[string]interface{}{
	// 	"id":         middleware.UUID, // Use UUID as ID
	// 	"name":       middleware.Metadata.Name,
	// 	"state":      middleware.ResourceStatus,
	// 	"statusTime": middleware.UpdateTime,
	// 	"reason":     "",
	// 	"url":        "", // Middlewares don't have URLs like apps
	// 	"invisible":  false,
	// })
	// data["entranceStatuses"] = entrances

	return types.NewAppStateLatestData(data, middleware.User, GetAppInfoFromDownloadRecord)
}

// FetchAppEntranceUrls fetches entrance URLs for a specific app from app-service
func FetchAppEntranceUrls(appName string, user string) (map[string]string, error) {
	host := os.Getenv("APP_SERVICE_SERVICE_HOST")
	port := os.Getenv("APP_SERVICE_SERVICE_PORT")

	if host == "" {
		host = "localhost" // Default fallback
	}
	if port == "" {
		port = "80" // Default fallback
	}

	url := fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", host, port)
	log.Printf("Fetching app entrance URLs from: %s for app: %s", url, appName)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from app-service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("app-service returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var apps []AppServiceResponse
	if err := json.Unmarshal(data, &apps); err != nil {
		return nil, fmt.Errorf("failed to parse app-service response: %v", err)
	}

	// Find the specific app and extract entrance URLs
	entranceUrls := make(map[string]string)
	for _, app := range apps {
		if app.Spec.Name == appName && app.Spec.Owner == user {
			for _, entrance := range app.Spec.Entrances {
				if entrance.Url != "" {
					entranceUrls[entrance.Name] = entrance.Url
				}
			}
			break
		}
	}

	log.Printf("Found %d entrance URLs for app %s", len(entranceUrls), appName)
	return entranceUrls, nil
}

// createAppStateLatestData creates AppStateLatestData from AppServiceResponse
// isStartupProcess indicates whether this is called during startup process
func createAppStateLatestData(app AppServiceResponse, isStartupProcess bool) (*types.AppStateLatestData, string) {
	data := map[string]interface{}{
		"name":               app.Spec.Name,
		"state":              app.Status.State,
		"updateTime":         app.Status.UpdateTime,
		"statusTime":         app.Status.StatusTime,
		"lastTransitionTime": app.Status.LastTransitionTime,
	}

	// Create a map of entrance URLs and invisible flags from spec.entrances
	entranceUrls := make(map[string]string)
	entranceInvisible := make(map[string]bool)
	for _, entrance := range app.Spec.Entrances {
		entranceUrls[entrance.Name] = entrance.Url
		entranceInvisible[entrance.Name] = entrance.Invisible
	}

	// Check if any entrance status has empty URL
	// During startup process, we don't enforce URL requirement
	hasEmptyUrl := false
	if !isStartupProcess {
		for _, entranceStatus := range app.Status.EntranceStatuses {
			if url, exists := entranceUrls[entranceStatus.Name]; !exists || url == "" {
				hasEmptyUrl = true
				break
			}
		}

		// If any entrance has empty URL, ignore the entire app state data
		if hasEmptyUrl {
			log.Printf("Skipping app %s due to empty URL in entrance statuses", app.Spec.Name)
			return nil, ""
		}
	} else {
		log.Printf("Startup process detected - skipping URL validation for app %s", app.Spec.Name)
	}

	// Combine entrance statuses with URLs from spec.entrances
	entrances := make([]interface{}, 0, len(app.Status.EntranceStatuses))
	for _, entranceStatus := range app.Status.EntranceStatuses {
		url, exists := entranceUrls[entranceStatus.Name]

		// In startup process, allow empty URLs; otherwise, skip entrances without URLs
		if !isStartupProcess && (!exists || url == "") {
			log.Printf("Skipping entrance %s for app %s due to missing URL", entranceStatus.Name, app.Spec.Name)
			continue
		}

		// Extract ID from URL: split by "." and take the first segment
		id := ""
		if url != "" {
			segments := strings.Split(url, ".")
			if len(segments) > 0 {
				id = segments[0]
			}
		}

		// Get invisible flag, default to false if not found
		invisible := false
		if invisibleFlag, exists := entranceInvisible[entranceStatus.Name]; exists {
			invisible = invisibleFlag
		}

		entrances = append(entrances, map[string]interface{}{
			"id":         id, // ID extracted from URL's first segment after splitting by "."
			"name":       entranceStatus.Name,
			"state":      entranceStatus.State,
			"statusTime": entranceStatus.StatusTime,
			"reason":     entranceStatus.Reason,
			"url":        url,
			"invisible":  invisible,
		})
	}
	data["entranceStatuses"] = entrances

	return types.NewAppStateLatestData(data, app.Spec.Owner, GetAppInfoFromDownloadRecord)
}

// DependencyServiceResponse represents the response structure from dependency service
type DependencyServiceResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Version   string `json:"version"`
		BuildTime string `json:"build_time"`
		GitCommit string `json:"git_commit"`
		Timestamp int64  `json:"timestamp"`
	} `json:"data"`
}

// checkDependencyService checks if the dependency service is available and has correct version
func checkDependencyService() error {
	// Get chart repo service host from environment
	chartRepoHost := os.Getenv("CHART_REPO_SERVICE_HOST")
	if chartRepoHost == "" {
		chartRepoHost = "http://localhost:8080" // Default fallback
		log.Printf("CHART_REPO_SERVICE_HOST not set, using default: %s", chartRepoHost)
	}

	// Build the API endpoint URL
	apiURL := "http://" + chartRepoHost + "/chart-repo/api/v2/version"
	log.Printf("Checking dependency service at: %s", apiURL)

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Print request details
	log.Printf("Making GET request to: %s", apiURL)
	log.Printf("Request timeout: 10 seconds")

	// Make GET request to the dependency service
	resp, err := client.Get(apiURL)
	if err != nil {
		log.Printf("Request failed with error: %v", err)
		return fmt.Errorf("failed to connect to dependency service: %w", err)
	}
	defer resp.Body.Close()

	// Print response details
	log.Printf("Response status code: %d", resp.StatusCode)
	log.Printf("Response headers: %v", resp.Header)

	// Check if response is successful
	if resp.StatusCode != http.StatusOK {
		log.Printf("Non-OK status code received: %d", resp.StatusCode)
		return fmt.Errorf("dependency service returned status %d", resp.StatusCode)
	}

	// Read response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response body: %v", err)
		return fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("Raw response body: %s", string(bodyBytes))

	// Parse response body
	var responseData DependencyServiceResponse
	if err := json.Unmarshal(bodyBytes, &responseData); err != nil {
		log.Printf("Failed to parse JSON response: %v", err)
		log.Printf("Response was: %s", string(bodyBytes))
		return fmt.Errorf("failed to parse dependency service response: %w", err)
	}

	log.Printf("Parsed response data: %+v", responseData)
	log.Printf("Dependency service response: success=%t, message=%s", responseData.Success, responseData.Message)
	log.Printf("Version data: version=%s, build_time=%s, git_commit=%s, timestamp=%d",
		responseData.Data.Version, responseData.Data.BuildTime, responseData.Data.GitCommit, responseData.Data.Timestamp)

	// Check if the response indicates success
	if !responseData.Success {
		return fmt.Errorf("dependency service returned success=false, message: %s", responseData.Message)
	}

	// Check version constraints: >= 0.2.0 and < 0.3.0
	if err := validateDependencyVersion(responseData.Data.Version); err != nil {
		log.Printf("Version validation failed: %v", err)
		return fmt.Errorf("version validation failed: %w", err)
	}

	log.Printf("Dependency service check passed: version %s is valid", responseData.Data.Version)
	return nil
}

// validateDependencyVersion validates that the version is >= 0.2.0 and < 0.3.0
func validateDependencyVersion(version string) error {
	if version == "" {
		return fmt.Errorf("version is empty")
	}

	// Parse the version using semver
	v, err := semver.NewVersion(version)
	if err != nil {
		return fmt.Errorf("invalid version format '%s': %w", version, err)
	}

	// Define version constraints
	minVersion, err := semver.NewVersion("0.2.0")
	if err != nil {
		return fmt.Errorf("failed to parse min version: %w", err)
	}

	maxVersion, err := semver.NewVersion("0.3.0")
	if err != nil {
		return fmt.Errorf("failed to parse max version: %w", err)
	}

	// Check if version is >= 0.2.0
	if v.LessThan(minVersion) {
		return fmt.Errorf("version %s is less than required minimum 0.2.0", version)
	}

	// Check if version is < 0.3.0
	if !v.LessThan(maxVersion) {
		return fmt.Errorf("version %s is not less than 0.3.0", version)
	}

	return nil
}

// WaitForDependencyService waits for dependency service to be available with retry logic
func WaitForDependencyService() {
	log.Println("=== Checking dependency service availability ===")
	log.Println("WaitForDependencyService function called successfully")

	retryCount := 0
	for {
		retryCount++
		log.Printf("Starting dependency service check iteration #%d...", retryCount)

		if err := checkDependencyService(); err != nil {
			log.Printf("Dependency service check failed (attempt #%d): %v", retryCount, err)
			log.Println("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("Dependency service is available and version is valid (succeeded on attempt #%d)", retryCount)
		break
	}
	log.Println("=== End dependency service check ===")
}
