package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// CacheVersionGetter is an interface for getting app version from cache state
type CacheVersionGetter interface {
	GetAppVersionFromState(userID, sourceID, appName string) (version string, found bool)
}

var (
	cacheVersionGetter CacheVersionGetter
	cacheGetterMutex   sync.RWMutex
)

// SetCacheVersionGetter sets the cache version getter interface
func SetCacheVersionGetter(getter CacheVersionGetter) {
	cacheGetterMutex.Lock()
	defer cacheGetterMutex.Unlock()
	cacheVersionGetter = getter
}

// getVersionFromCacheState gets app version from cache state if available
func getVersionFromCacheState(userID, sourceID, appName string) (version string, found bool) {
	cacheGetterMutex.RLock()
	defer cacheGetterMutex.RUnlock()
	if cacheVersionGetter != nil {
		return cacheVersionGetter.GetAppVersionFromState(userID, sourceID, appName)
	}
	return "", false
}

// GetAppInfoFromDownloadRecord fetches app version and source from chart-repo service
func GetAppInfoFromDownloadRecord(userID, appName string) (string, string, error) {

	// Get chart repo service host from env, fallback to default
	host := os.Getenv("CHART_REPO_SERVICE_HOST")
	if host == "" {
		return "", "", fmt.Errorf("CHART_REPO_SERVICE_HOST env not set")
	}
	url := fmt.Sprintf("http://%s/chart-repo/api/v2/app/version-for-download-history?user=%s&app_name=%s", host, userID, appName)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("failed to request chart-repo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("chart-repo returned status: %d", resp.StatusCode)
	}

	var result struct {
		Success bool        `json:"success"`
		Message string      `json:"message"`
		Data    interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	if !result.Success {
		return "", "", fmt.Errorf("chart-repo error: %s", result.Message)
	}

	// parse data field
	dataMap, ok := result.Data.(map[string]interface{})
	if !ok {

		// if data is map[string]string, return version and source
		if dataStrMap, ok2 := result.Data.(map[string]string); ok2 {
			return dataStrMap["version"], dataStrMap["source"], nil
		}
		return "", "", fmt.Errorf("invalid data format in response")
	}

	version, _ := dataMap["version"].(string)
	source, _ := dataMap["source"].(string)
	if version == "" || source == "" {
		return "", "", fmt.Errorf("version or source not found in response")
	}

	return version, source, nil
}

var (
	taskStoreOnce sync.Once
	taskStore     *sqlx.DB
	taskStoreErr  error
)

// GetTaskStoreForQuery returns a singleton database connection for querying task records
// This is exported so other packages can query task database
func GetTaskStoreForQuery() (*sqlx.DB, error) {
	return getTaskStore()
}

// getTaskStore returns a singleton database connection for querying task records
func getTaskStore() (*sqlx.DB, error) {
	taskStoreOnce.Do(func() {
		if IsPublicEnvironment() {
			taskStoreErr = fmt.Errorf("task store is disabled in public environment")
			return
		}

		dbHost := os.Getenv("POSTGRES_HOST")
		if dbHost == "" {
			dbHost = "localhost"
		}

		dbPort := os.Getenv("POSTGRES_PORT")
		if dbPort == "" {
			dbPort = "5432"
		}

		dbName := os.Getenv("POSTGRES_DB")
		if dbName == "" {
			dbName = "history"
		}

		dbUser := os.Getenv("POSTGRES_USER")
		if dbUser == "" {
			dbUser = "postgres"
		}

		dbPassword := os.Getenv("POSTGRES_PASSWORD")
		if dbPassword == "" {
			dbPassword = "password"
		}

		connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			dbHost, dbPort, dbUser, dbPassword, dbName)

		db, err := sqlx.Connect("postgres", connStr)
		if err != nil {
			taskStoreErr = fmt.Errorf("failed to connect to PostgreSQL: %w", err)
			return
		}

		if err := db.Ping(); err != nil {
			taskStoreErr = fmt.Errorf("failed to ping PostgreSQL: %w", err)
			return
		}

		taskStore = db
	})

	if taskStoreErr != nil {
		return nil, taskStoreErr
	}
	return taskStore, nil
}

// GetAppInfoLastInstalled fetches app version and source by analyzing task records and download history
// It first tries to get information from completed task records, then falls back to download history
// For CloneApp, it queries both the cloned app name (rawAppName + title) and the original app name (rawAppName)
func GetAppInfoLastInstalled(userID, appName string) (string, string, error) {
	// Try to get version and source from task records first
	db, err := getTaskStore()
	if err == nil && db != nil {
		// Query for latest completed task (InstallApp, UpgradeApp, or CloneApp)
		query := `
		SELECT metadata, type
		FROM task_records
		WHERE app_name = $1 
			AND user_account = $2 
			AND status = $3
			AND type IN ($4, $5, $6)
		ORDER BY completed_at DESC NULLS LAST, created_at DESC
		LIMIT 1
		`

		// Task status: Completed = 3, Task types: InstallApp = 1, UpgradeApp = 4, CloneApp = 5
		var metadataStr string
		var taskType int
		err = db.QueryRow(query, appName, userID, 3, 1, 4, 5).Scan(&metadataStr, &taskType)
		if err == nil {
			// Parse metadata JSON
			var metadataMap map[string]interface{}
			if metadataStr != "" {
				if err := json.Unmarshal([]byte(metadataStr), &metadataMap); err == nil {
					// Extract version and source from metadata
					version := ""
					source := ""
					if v, ok := metadataMap["version"].(string); ok && v != "" {
						version = v
					}
					if s, ok := metadataMap["source"].(string); ok && s != "" {
						source = s
					}

					// For CloneApp, get version from the installed original app's state
					// Clone app should use the same version as the installed original app
					if taskType == 5 { // CloneApp = 5
						if rawAppName, ok := metadataMap["rawAppName"].(string); ok && rawAppName != "" {
							// Try to get version from original app's state in cache first
							// Use the source from metadata if available, otherwise skip cache lookup
							if source != "" {
								stateVersion, found := getVersionFromCacheState(userID, source, rawAppName)
								if found && stateVersion != "" {
									// Use version from state (installed original app's version)
									version = stateVersion
								}
							}

							// If version is still empty, try to get from original app's task record
							if version == "" || source == "" {
								originalQuery := `
								SELECT metadata
								FROM task_records
								WHERE app_name = $1 
									AND user_account = $2 
									AND status = $3
									AND type IN ($4, $5, $6)
								ORDER BY completed_at DESC NULLS LAST, created_at DESC
								LIMIT 1
								`
								var originalMetadataStr string
								originalErr := db.QueryRow(originalQuery, rawAppName, userID, 3, 1, 4, 5).Scan(&originalMetadataStr)
								if originalErr == nil && originalMetadataStr != "" {
									var originalMetadataMap map[string]interface{}
									if err := json.Unmarshal([]byte(originalMetadataStr), &originalMetadataMap); err == nil {
										if version == "" {
											if v, ok := originalMetadataMap["version"].(string); ok && v != "" {
												version = v
											}
										}
										if source == "" {
											if s, ok := originalMetadataMap["source"].(string); ok && s != "" {
												source = s
											}
										}
									}
								}
							}
						}
					}

					// If we have both version and source, return them
					if version != "" && source != "" {
						return version, source, nil
					}
					// If we have at least one, we can use it and try to get the other from download record
					if version != "" || source != "" {
						// Try to get missing info from download record
						// For CloneApp, try original app name first
						downloadAppName := appName
						if taskType == 5 { // CloneApp = 5
							if rawAppName, ok := metadataMap["rawAppName"].(string); ok && rawAppName != "" {
								downloadAppName = rawAppName
							}
						}
						downloadVersion, downloadSource, err := GetAppInfoFromDownloadRecord(userID, downloadAppName)
						if err == nil {
							// Use task version/source if available, otherwise use download record
							if version == "" {
								version = downloadVersion
							}
							if source == "" {
								source = downloadSource
							}
							if version != "" && source != "" {
								return version, source, nil
							}
						}
						// If we have at least one from task, return what we have
						if version != "" || source != "" {
							return version, source, nil
						}
					}
				}
			}
		}
		// If task query failed (e.g., no rows), continue to fallback
	}

	// Fallback to download record
	return GetAppInfoFromDownloadRecord(userID, appName)
}
