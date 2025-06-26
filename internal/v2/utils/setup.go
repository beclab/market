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
var userAppStateData map[string][]*types.AppStateLatestData

// SetupAppServiceData fetches app data from app-service or reads from local file in development
func SetupAppServiceData() error {
	log.Println("Starting app service data setup...")

	// Initialize user app state data map
	userAppStateData = make(map[string][]*types.AppStateLatestData)

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
	return extractedUsers
}

// GetUserAppStateData returns the app state data for a specific user
func GetUserAppStateData(userID string) []*types.AppStateLatestData {
	if data, exists := userAppStateData[userID]; exists {
		return data
	}
	return []*types.AppStateLatestData{}
}

// GetAllUserAppStateData returns all user app state data
func GetAllUserAppStateData() map[string][]*types.AppStateLatestData {
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

		// Create AppStateLatestData for this app
		appStateData := createAppStateLatestData(app)

		// Add to user's app state data only if creation was successful
		if appStateData != nil {
			if userAppStateData[user] == nil {
				userAppStateData[user] = make([]*types.AppStateLatestData, 0)
			}
			userAppStateData[user] = append(userAppStateData[user], appStateData)
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
	for user, appStates := range userAppStateData {
		log.Printf("  User %s: %d app states", user, len(appStates))
		for _, appState := range appStates {
			log.Printf("    - App Status: %s", appState.Status.State)
		}
	}

	log.Println("=== End App Service Data Summary ===")

	return nil
}

// FetchAppEntranceUrls fetches entrance URLs for a specific app from app-service
func FetchAppEntranceUrls(appName string) (map[string]string, error) {
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
		if app.Spec.Name == appName {
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
func createAppStateLatestData(app AppServiceResponse) *types.AppStateLatestData {
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
	hasEmptyUrl := false
	for _, entranceStatus := range app.Status.EntranceStatuses {
		if url, exists := entranceUrls[entranceStatus.Name]; !exists || url == "" {
			hasEmptyUrl = true
			break
		}
	}

	// If any entrance has empty URL, ignore the entire app state data
	if hasEmptyUrl {
		log.Printf("Skipping app %s due to empty URL in entrance statuses", app.Spec.Name)
		return nil
	}

	// Combine entrance statuses with URLs from spec.entrances
	entrances := make([]interface{}, 0, len(app.Status.EntranceStatuses))
	for _, entranceStatus := range app.Status.EntranceStatuses {
		if url, exists := entranceUrls[entranceStatus.Name]; exists && url != "" {
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
		} else {
			log.Printf("Skipping entrance %s for app %s due to missing URL", entranceStatus.Name, app.Spec.Name)
		}
	}
	data["entranceStatuses"] = entrances

	return types.NewAppStateLatestData(data, app.Spec.Owner, GetAppVersionFromDownloadRecord)
}
