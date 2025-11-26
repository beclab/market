package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"market/internal/v2/history" // Import history module with correct path
	"market/internal/v2/task"
	"market/internal/v2/utils"

	"github.com/nats-io/nats.go"
)

// EntranceStatus represents the status of an entrance
type EntranceStatus struct {
	ID         string `json:"id"` // ID extracted from URL's first segment after splitting by "."
	Name       string `json:"name"`
	State      string `json:"state"`
	StatusTime string `json:"statusTime"`
	Reason     string `json:"reason"`
	Url        string `json:"url"`
	Invisible  *bool  `json:"invisible,omitempty"`
}

// AppStateMessage represents the message structure from NATS
type AppStateMessage struct {
	EventID          string           `json:"eventID"`
	CreateTime       string           `json:"createTime"`
	Name             string           `json:"name"`
	RawAppName       string           `json:"rawAppName"`
	Title            string           `json:"title"`
	OpID             string           `json:"opID"`
	OpType           string           `json:"opType"`
	State            string           `json:"state"`
	User             string           `json:"user"`
	Progress         string           `json:"progress"`
	EntranceStatuses []EntranceStatus `json:"entranceStatuses"`
}

// DataWatcherState handles app state messages from NATS
type DataWatcherState struct {
	nc            *nats.Conn
	sub           *nats.Subscription
	ctx           context.Context
	cancel        context.CancelFunc
	isDev         bool
	natsHost      string
	natsPort      string
	natsUser      string
	natsPass      string
	subject       string
	historyModule *history.HistoryModule // Add history module
	cacheManager  *CacheManager          // Add cache manager reference
	taskModule    *task.TaskModule       // Add task module reference
	dataWatcher   *DataWatcher           // Add data watcher reference for hash calculation
}

// getExistingEntranceInvisibleMap collects cached invisible flags for the specified app.
func (dw *DataWatcherState) getExistingEntranceInvisibleMap(userID, appName string) map[string]bool {
	result := make(map[string]bool)
	if dw == nil || dw.cacheManager == nil || userID == "" || appName == "" {
		return result
	}

	userData := dw.cacheManager.GetUserData(userID)
	if userData == nil {
		return result
	}

	for _, srcData := range userData.Sources {
		if srcData == nil {
			continue
		}
		for _, appState := range srcData.AppStateLatest {
			if appState == nil || appState.Status.Name != appName {
				continue
			}

			for _, entrance := range appState.Status.EntranceStatuses {
				result[entrance.Name] = entrance.Invisible
			}
			return result
		}
	}

	return result
}

// resolveInvisibleFlag returns upstream value when available, otherwise falls back to cached data.
func resolveInvisibleFlag(raw *bool, entranceName string, existing map[string]bool) bool {
	if raw != nil {
		return *raw
	}

	if val, ok := existing[entranceName]; ok {
		return val
	}

	return false
}

// NewDataWatcherState creates a new DataWatcherState instance
func NewDataWatcherState(cacheManager *CacheManager, taskModule *task.TaskModule, historyModule *history.HistoryModule, dataWatcher *DataWatcher) *DataWatcherState {
	ctx, cancel := context.WithCancel(context.Background())

	dw := &DataWatcherState{
		ctx:           ctx,
		cancel:        cancel,
		isDev:         isDevEnvironment(),
		natsHost:      getEnvOrDefault("NATS_HOST", "localhost"),
		natsPort:      getEnvOrDefault("NATS_PORT", "4222"),
		natsUser:      getEnvOrDefault("NATS_USERNAME", ""),
		natsPass:      getEnvOrDefault("NATS_PASSWORD", ""),
		subject:       getEnvOrDefault("NATS_SUBJECT_SYSTEM_APP_STATE", "os.application.*"),
		historyModule: historyModule,
		cacheManager:  cacheManager, // Set cache manager reference
		taskModule:    taskModule,   // Set task module reference
		dataWatcher:   dataWatcher,  // Set data watcher reference
	}

	return dw
}

// SetTaskModule updates the task module reference without restarting subscriptions.
func (dw *DataWatcherState) SetTaskModule(taskModule *task.TaskModule) {
	dw.taskModule = taskModule
	log.Println("DataWatcherState task module updated")
}

// SetHistoryModule updates the history module reference without restarting subscriptions.
func (dw *DataWatcherState) SetHistoryModule(historyModule *history.HistoryModule) {
	dw.historyModule = historyModule
	log.Println("DataWatcherState history module updated")
}

// Start starts the data watcher
func (dw *DataWatcherState) Start() error {

	if utils.IsPublicEnvironment() {
		log.Println("Public environment detected, DataWatcherState disabled")
		return nil
	}

	log.Println("Starting data watcher in production mode")
	// Start NATS connection in a goroutine
	go func() {
		if err := dw.startNatsConnection(); err != nil {
			log.Printf("Error in NATS connection: %v", err)
		}
	}()
	return nil
}

// Stop stops the data watcher
func (dw *DataWatcherState) Stop() error {
	dw.cancel()

	if dw.sub != nil {
		if err := dw.sub.Unsubscribe(); err != nil {
			log.Printf("Error unsubscribing from NATS: %v", err)
		}
	}

	if dw.nc != nil {
		dw.nc.Close()
	}

	// Close history module
	if dw.historyModule != nil {
		if err := dw.historyModule.Close(); err != nil {
			log.Printf("Error closing history module: %v", err)
		}
	}

	log.Println("Data watcher stopped")
	return nil
}

// startNatsConnection establishes connection to NATS and subscribes to the subject
func (dw *DataWatcherState) startNatsConnection() error {
	natsURL := fmt.Sprintf("nats://%s:%s", dw.natsHost, dw.natsPort)

	var opts []nats.Option
	if dw.natsUser != "" && dw.natsPass != "" {
		opts = append(opts, nats.UserInfo(dw.natsUser, dw.natsPass))
	}

	opts = append(opts,
		nats.DisconnectHandler(func(nc *nats.Conn) {
			log.Printf("[NATS] Disconnected from %s", nc.ConnectedUrl())
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("[NATS] Reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Printf("[NATS] Connection closed: %v", nc.LastError())
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			log.Printf("[NATS] Error: %v", err)
		}),
		nats.MaxReconnects(60),
		nats.ReconnectWait(2*time.Second),
	)

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS at %s: %w", natsURL, err)
	}

	dw.nc = nc
	log.Printf("Connected to NATS at %s", natsURL)

	// Subscribe to the subject
	sub, err := nc.Subscribe(dw.subject, dw.handleMessage)
	if err != nil {
		return fmt.Errorf("failed to subscribe to subject %s: %w", dw.subject, err)
	}

	dw.sub = sub
	log.Printf("Subscribed to NATS subject: %s", dw.subject)

	// Wait for context cancellation
	<-dw.ctx.Done()
	return nil
}

// handleMessage processes incoming NATS messages
func (dw *DataWatcherState) handleMessage(msg *nats.Msg) {
	log.Printf("Received message from NATS subject %s: %s", msg.Subject, string(msg.Data))

	var appStateMsg AppStateMessage
	if err := json.Unmarshal(msg.Data, &appStateMsg); err != nil {
		log.Printf("Error parsing JSON message: %v", err)
		return
	}

	// Check if this is an install operation with running state
	if appStateMsg.OpType == "install" && appStateMsg.State == "running" {
		log.Printf("Detected install operation with running state for opID: %s, app: %s, user: %s",
			appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		// Call InstallTaskSucceed if task module is available
		if dw.taskModule != nil {
			if err := dw.taskModule.InstallTaskSucceed(appStateMsg.OpID, appStateMsg.Name, appStateMsg.User); err != nil {
				log.Printf("Failed to mark install task as succeeded for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked install task as succeeded for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark install task as succeeded for opID: %s", appStateMsg.OpID)
		}
	}

	// Check if this is an install operation with installFailed or downloadFailed state
	if appStateMsg.OpType == "install" && (appStateMsg.State == "installFailed" || appStateMsg.State == "downloadFailed" || appStateMsg.State == "installCancelFailed" || appStateMsg.State == "downloadCancelFailed") {
		log.Printf("Detected install operation with %s state for opID: %s, app: %s, user: %s",
			appStateMsg.State, appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		// Call InstallTaskFailed if task module is available
		if dw.taskModule != nil {
			errorMsg := fmt.Sprintf("Installation failed for app %s", appStateMsg.Name)
			if err := dw.taskModule.InstallTaskFailed(appStateMsg.OpID, appStateMsg.Name, appStateMsg.User, errorMsg); err != nil {
				log.Printf("Failed to mark install task as failed for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked install task as failed for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark install task as failed for opID: %s", appStateMsg.OpID)
		}
	}

	// Check if this is an install operation with installCanceled state
	if appStateMsg.OpType == "install" && (appStateMsg.State == "installCanceled" || appStateMsg.State == "installingCanceled") {
		log.Printf("Detected install operation with installCanceled state for opID: %s, app: %s, user: %s",
			appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		// Call CancelInstallTaskSucceed if task module is available
		if dw.taskModule != nil {
			if err := dw.taskModule.CancelInstallTaskSucceed(appStateMsg.OpID, appStateMsg.Name, appStateMsg.User); err != nil {
				log.Printf("Failed to mark cancel install task as succeeded for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked cancel install task as succeeded for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark cancel install task as succeeded for opID: %s", appStateMsg.OpID)
		}
	}

	// Check if this is an install operation with installCancelFailed or downloadCancelFailed state
	if appStateMsg.OpType == "install" && (appStateMsg.State == "installCancelFailed" || appStateMsg.State == "downloadCancelFailed") {
		log.Printf("Detected install operation with %s state for opID: %s, app: %s, user: %s",
			appStateMsg.State, appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		// Call CancelInstallTaskFailed if task module is available
		if dw.taskModule != nil {
			errorMsg := fmt.Sprintf("Cancel installation failed for app %s", appStateMsg.Name)
			if err := dw.taskModule.CancelInstallTaskFailed(appStateMsg.OpID, appStateMsg.Name, appStateMsg.User, errorMsg); err != nil {
				log.Printf("Failed to mark cancel install task as failed for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked cancel install task as failed for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark cancel install task as failed for opID: %s", appStateMsg.OpID)
		}
	}

	// Check if this is an uninstall operation with uninstalled state
	if appStateMsg.OpType == "uninstall" && appStateMsg.State == "uninstalled" {
		log.Printf("Detected uninstall operation with uninstalled state for opID: %s, app: %s, user: %s",
			appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		if dw.taskModule != nil {
			if err := dw.taskModule.UninstallTaskSucceed(appStateMsg.OpID, appStateMsg.Name, appStateMsg.User); err != nil {
				log.Printf("Failed to mark uninstall task as succeeded for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked uninstall task as succeeded for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark uninstall task as succeeded for opID: %s", appStateMsg.OpID)
		}
	}

	// Check if this is an uninstall operation with uninstallFailed state
	if appStateMsg.OpType == "uninstall" && appStateMsg.State == "uninstallFailed" {
		log.Printf("Detected uninstall operation with uninstallFailed state for opID: %s, app: %s, user: %s",
			appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		if dw.taskModule != nil {
			errorMsg := fmt.Sprintf("Uninstallation failed for app %s", appStateMsg.Name)
			if err := dw.taskModule.UninstallTaskFailed(appStateMsg.OpID, appStateMsg.Name, appStateMsg.User, errorMsg); err != nil {
				log.Printf("Failed to mark uninstall task as failed for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked uninstall task as failed for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark uninstall task as failed for opID: %s", appStateMsg.OpID)
		}
	}

	userData := dw.cacheManager.getUserData(appStateMsg.User)
	if userData == nil {
		log.Printf("User data not found for user %s", appStateMsg.User)
		return
	}

	for _, sourceData := range userData.Sources {
		for _, appState := range sourceData.AppStateLatest {
			if appState.Status.Name == appStateMsg.Name &&
				appState.Status.State == appStateMsg.State {

				if (appStateMsg.EntranceStatuses == nil || len(appStateMsg.EntranceStatuses) == 0) && appState.Status.Progress == appStateMsg.Progress {
					log.Printf("App state message is the same as the cached app state message for app %s, user %s, source %s",
						appStateMsg.Name, appStateMsg.User, appStateMsg.OpID)
					return
				}

				// Compare timestamps properly by parsing them
				if appState.Status.StatusTime != "" && appStateMsg.CreateTime != "" {
					statusTime, err1 := time.Parse("2006-01-02T15:04:05.000000000Z", appState.Status.StatusTime)
					createTime, err2 := time.Parse("2006-01-02T15:04:05.000000000Z", appStateMsg.CreateTime)

					if err1 == nil && err2 == nil {
						if statusTime.After(createTime) {
							log.Printf("Cached app state is newer than incoming message for app %s, user %s, source %s. Skipping update.",
								appStateMsg.Name, appStateMsg.User, appStateMsg.OpID)
							return
						}
					} else {
						log.Printf("Failed to parse timestamps for comparison: StatusTime=%s, CreateTime=%s, err1=%v, err2=%v",
							appState.Status.StatusTime, appStateMsg.CreateTime, err1, err2)
					}
				}
			}
		}
	}

	// Store as history record
	dw.storeHistoryRecord(appStateMsg, string(msg.Data))

	// Store state data to cache
	dw.storeStateToCache(appStateMsg)

	// Print the parsed message
	dw.printAppStateMessage(appStateMsg)
}

// storeHistoryRecord stores the app state message as a history record
func (dw *DataWatcherState) storeHistoryRecord(msg AppStateMessage, rawMessage string) {
	if dw.historyModule == nil {
		log.Printf("History module not available, skipping record storage")
		return
	}

	// Special handling for downloading state - check progress difference
	if msg.State == "downloading" {
		if dw.shouldSkipDownloadingMessage(msg) {
			log.Printf("Skipping downloading message for app %s, user %s - progress difference is within 10", msg.Name, msg.User)
			return
		}
	}

	record := &history.HistoryRecord{
		Type:     history.TypeSystem, // Use system type
		Message:  "",                 // Leave message empty as requested
		Time:     time.Now().Unix(),  // Current timestamp
		App:      msg.Name,           // App field from message
		Account:  msg.User,           // User field from message
		Extended: rawMessage,         // Store complete message in Extended field
	}

	if err := dw.historyModule.StoreRecord(record); err != nil {
		log.Printf("Failed to store history record: %v", err)
	} else {
		log.Printf("Stored history record for app %s with user %s and ID %d", msg.Name, msg.User, record.ID)
	}
}

// storeStateToCache stores the app state message to cache as AppStateLatestData
func (dw *DataWatcherState) storeStateToCache(msg AppStateMessage) {
	if dw.cacheManager == nil {
		log.Printf("Cache manager not available, skipping cache storage")
		return
	}

	userID := msg.User
	if userID == "" {
		log.Printf("User ID is empty, skipping cache storage for app %s", msg.Name)
		return
	}

	// Preload existing invisible flags to avoid overwriting when upstream omits the field.
	existingInvisible := dw.getExistingEntranceInvisibleMap(userID, msg.Name)

	// Add debug logging for entranceStatuses
	log.Printf("DEBUG: storeStateToCache - entranceStatuses count: %d", len(msg.EntranceStatuses))
	for i, entrance := range msg.EntranceStatuses {
		invisible := resolveInvisibleFlag(entrance.Invisible, entrance.Name, existingInvisible)
		log.Printf("DEBUG: storeStateToCache - entrance[%d]: ID=%s, Name=%s, State=%s, URL=%s, Invisible=%t",
			i, entrance.ID, entrance.Name, entrance.State, entrance.Url, invisible)
	}

	entranceStatuses := make([]interface{}, len(msg.EntranceStatuses))
	for i, v := range msg.EntranceStatuses {
		invisible := resolveInvisibleFlag(v.Invisible, v.Name, existingInvisible)
		entranceStatuses[i] = map[string]interface{}{
			"id":         v.ID,
			"name":       v.Name,
			"state":      v.State,
			"statusTime": v.StatusTime,
			"reason":     v.Reason,
			"url":        v.Url,
			"invisible":  invisible,
		}
	}

	stateData := map[string]interface{}{
		"state":              msg.State,
		"updateTime":         "",
		"statusTime":         msg.CreateTime,
		"lastTransitionTime": "",
		"progress":           msg.Progress,
		"entranceStatuses":   entranceStatuses,
		"name":               msg.Name,       // Add app name for state monitoring
		"rawAppName":         msg.RawAppName, // Add raw app name for clone app support
		"title":              msg.Title,      // Add title from message
	}

	// Add debug logging for stateData
	log.Printf("DEBUG: storeStateToCache - stateData keys: %v", getMapKeys(stateData))
	if entranceStatusesVal, ok := stateData["entranceStatuses"]; ok {
		log.Printf("DEBUG: storeStateToCache - entranceStatuses type: %T, value: %+v", entranceStatusesVal, entranceStatusesVal)
	}

	sourceID := ""

	userData := dw.cacheManager.GetUserData(userID)
	if userData != nil {
		for srcID, srcData := range userData.Sources {
			if srcData == nil || srcData.AppStateLatest == nil {
				continue
			}
			for _, appState := range srcData.AppStateLatest {
				if appState != nil && appState.Status.State != "uninstalled" && appState.Status.Name == msg.Name {
					sourceID = srcID
					break
				}
			}
			if sourceID != "" {
				break
			}
		}
	}

	if sourceID == "" && dw.taskModule != nil {
		_, src, found, _ := dw.taskModule.GetLatestTaskByAppNameAndUser(msg.Name, userID)
		if found && src != "" {
			sourceID = src
			log.Printf("Found task with source=%s for app=%s, user=%s", src, msg.Name, userID)
		}
	}

	// If still not found, try to query from task store database (for completed tasks)
	if sourceID == "" {
		db, err := utils.GetTaskStoreForQuery()
		if err == nil && db != nil {
			// Query for latest completed task (InstallApp or CloneApp)
			query := `
			SELECT metadata, type
			FROM task_records
			WHERE app_name = $1
				AND user_account = $2
				AND status = $3
				AND type IN ($4, $5)
			ORDER BY completed_at DESC NULLS LAST, created_at DESC
			LIMIT 1
			`
			// Task status: Completed = 3, Task types: InstallApp = 1, CloneApp = 5
			var metadataStr string
			var taskType int
			err = db.QueryRow(query, msg.Name, userID, 3, 1, 5).Scan(&metadataStr, &taskType)
			if err == nil && metadataStr != "" {
				var metadataMap map[string]interface{}
				if err := json.Unmarshal([]byte(metadataStr), &metadataMap); err == nil {
					if s, ok := metadataMap["source"].(string); ok && s != "" {
						sourceID = s
						log.Printf("Found source=%s from completed task database for app=%s, user=%s", sourceID, msg.Name, userID)
					}
				}
			}
		}
	}

	if sourceID == "" {
		log.Printf("[ERROR] Cannot determine sourceID for app state: user=%s, app=%s, state=%s. Ignore this message.", userID, msg.Name, msg.State)
		return
	}

	if err := dw.cacheManager.SetAppData(userID, sourceID, AppStateLatest, stateData); err != nil {
		log.Printf("Failed to store app state to cache: %v", err)
	} else {
		log.Printf("Successfully stored app state to cache for user=%s, source=%s, app=%s, state=%s",
			userID, sourceID, msg.Name, msg.State)

		// Call ForceCalculateAllUsersHash for hash calculation after successful cache update
		if dw.dataWatcher != nil {
			log.Printf("Triggering hash recalculation for all users after cache update")
			if err := dw.dataWatcher.ForceCalculateAllUsersHash(); err != nil {
				log.Printf("Failed to force calculate all users hash: %v", err)
			} else {
				log.Printf("Successfully triggered hash recalculation for all users")
			}
		} else {
			log.Printf("DataWatcher not available, skipping hash recalculation")
		}
	}
}

// Helper function to get map keys for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// printAppStateMessage prints the app state message details
func (dw *DataWatcherState) printAppStateMessage(msg AppStateMessage) {
	log.Printf("=== App State Message ===")
	log.Printf("Event ID: %s", msg.EventID)
	log.Printf("Create Time: %s", msg.CreateTime)
	log.Printf("Name: %s", msg.Name)
	log.Printf("Raw App Name: %s", msg.RawAppName)
	log.Printf("Title: %s", msg.Title)
	log.Printf("State: %s", msg.State)
	log.Printf("Progress: %s", msg.Progress)
	log.Printf("Operation Type: %s", msg.OpType)
	log.Printf("Operation ID: %s", msg.OpID)
	log.Printf("User: %s", msg.User)
	log.Printf("Entrance Statuses:")
	for i, status := range msg.EntranceStatuses {
		log.Printf("  [%d] Name: %s, State: %s, Status Time: %s, Reason: %s",
			i, status.Name, status.State, status.StatusTime, status.Reason)
	}
	log.Printf("========================")
}

// isDevEnvironment checks if we're running in development environment
func isDevEnvironment() bool {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	return env == "dev" || env == "development" || env == ""
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// shouldSkipDownloadingMessage checks if downloading message should be skipped based on progress difference
func (dw *DataWatcherState) shouldSkipDownloadingMessage(msg AppStateMessage) bool {
	userID := msg.User
	appName := msg.Name

	if dw.historyModule == nil {
		log.Printf("History module not available, cannot compare with DB, proceeding with message storage")
		return false
	}

	// Query recent records for this app/account from DB (type=system)
	cond := &history.QueryCondition{
		Type:    history.TypeSystem,
		App:     appName,
		Account: userID,
		Limit:   50,
		Offset:  0,
	}

	records, err := dw.historyModule.QueryRecords(cond)
	if err != nil {
		log.Printf("Failed to query history records for app %s, user %s: %v", appName, userID, err)
		return false
	}

	// Find the most recent downloading record and compare progress
	for _, rec := range records {
		if rec == nil || rec.Extended == "" {
			continue
		}
		var lastMsg AppStateMessage
		if err := json.Unmarshal([]byte(rec.Extended), &lastMsg); err != nil {
			log.Printf("Failed to unmarshal extended history for record %d: %v", rec.ID, err)
			continue
		}
		if lastMsg.Name != appName || lastMsg.User != userID {
			continue
		}
		if lastMsg.State != "downloading" {
			// keep searching older records until we find a downloading one
			continue
		}

		cachedProgress := lastMsg.Progress
		newProgress := msg.Progress

		cachedProgressFloat, err1 := strconv.ParseFloat(cachedProgress, 64)
		newProgressFloat, err2 := strconv.ParseFloat(newProgress, 64)
		if err1 != nil || err2 != nil {
			log.Printf("Failed to parse progress values: cached=%s, new=%s, err1=%v, err2=%v", cachedProgress, newProgress, err1, err2)
			return false
		}

		progressDiff := newProgressFloat - cachedProgressFloat
		if progressDiff < 0 {
			progressDiff = -progressDiff
		}

		if progressDiff <= 10.0 {
			log.Printf("Progress difference is %.2f (within 10.0), skipping message for app %s", progressDiff, appName)
			return true
		}

		log.Printf("Progress difference is %.2f (greater than 10.0), proceeding with message storage for app %s", progressDiff, appName)
		return false
	}

	// No previous downloading record found in DB
	log.Printf("No previous downloading record found in DB for app %s, user %s, proceeding with message storage", appName, userID)
	return false
}
