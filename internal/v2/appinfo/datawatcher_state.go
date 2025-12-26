package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// SharedEntrance represents a shared entrance configuration from app-service
type SharedEntrance struct {
	Name            string `yaml:"name" json:"name"`
	Host            string `yaml:"host" json:"host"`
	Port            int32  `yaml:"port" json:"port"`
	Icon            string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Title           string `yaml:"title" json:"title,omitempty"`
	AuthLevel       string `yaml:"authLevel,omitempty" json:"authLevel,omitempty"`
	Invisible       bool   `yaml:"invisible,omitempty" json:"invisible,omitempty"`
	URL             string `yaml:"url,omitempty" json:"url,omitempty"`
	OpenMethod      string `yaml:"openMethod,omitempty" json:"openMethod,omitempty"`
	WindowPushState bool   `yaml:"windowPushState,omitempty" json:"windowPushState,omitempty"`
	Skip            bool   `yaml:"skip,omitempty" json:"skip,omitempty"`
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
	SharedEntrances  []SharedEntrance `json:"sharedEntrances,omitempty"`
}

// DelayedMessage represents a message that needs to be processed later
type DelayedMessage struct {
	msg        *nats.Msg
	appStateMsg AppStateMessage
	retryCount int
	nextRetry  time.Time
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
	// Cache for app-service API responses to avoid repeated calls
	appServiceCache      map[string]map[string]bool // key: "userID:appName", value: map[entranceName]invisible
	appServiceCacheMutex sync.RWMutex
	// Delayed message queue for pending state messages
	delayedMessages      []*DelayedMessage
	delayedMessagesMutex sync.Mutex
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
// If neither upstream nor cache has the value, it will try to fetch from app-service API.
func (dw *DataWatcherState) resolveInvisibleFlag(raw *bool, entranceName, appName, userID string, existing map[string]bool) bool {
	if raw != nil {
		return *raw
	}

	if val, ok := existing[entranceName]; ok {
		return val
	}

	// If not found in cache, try to fetch from app-service API (only once per entrance to avoid performance issues)
	// This matches the behavior during startup where we get invisible from spec.entrances
	if dw != nil && appName != "" && userID != "" {
		if specInvisible, err := dw.fetchInvisibleFromAppService(appName, userID, entranceName); err == nil {
			log.Printf("DEBUG: resolveInvisibleFlag - fetched invisible=%t for entrance %s (app=%s, user=%s) from app-service API",
				specInvisible, entranceName, appName, userID)
			return specInvisible
		} else {
			log.Printf("DEBUG: resolveInvisibleFlag - failed to fetch invisible for entrance %s (app=%s, user=%s) from app-service: %v",
				entranceName, appName, userID, err)
		}
	}

	return false
}

// fetchInvisibleFromAppService fetches invisible flag from app-service API's spec.entrances
// Uses caching to avoid repeated API calls for the same app
func (dw *DataWatcherState) fetchInvisibleFromAppService(appName, userID, entranceName string) (bool, error) {
	// Check cache first (using TryRLock to avoid blocking)
	cacheKey := fmt.Sprintf("%s:%s", userID, appName)
	if dw.appServiceCacheMutex.TryRLock() {
		if appCache, exists := dw.appServiceCache[cacheKey]; exists {
			if invisible, found := appCache[entranceName]; found {
				dw.appServiceCacheMutex.RUnlock()
				log.Printf("DEBUG: fetchInvisibleFromAppService - using cached invisible=%t for entrance %s (app=%s, user=%s)",
					invisible, entranceName, appName, userID)
				return invisible, nil
			}
		}
		dw.appServiceCacheMutex.RUnlock()
	} else {
		log.Printf("DEBUG: fetchInvisibleFromAppService - read lock not available, skipping cache check for entrance %s (app=%s, user=%s)",
			entranceName, appName, userID)
	}

	// Fetch from API
	host := getEnvOrDefault("APP_SERVICE_SERVICE_HOST", "localhost")
	port := getEnvOrDefault("APP_SERVICE_SERVICE_PORT", "80")
	url := fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", host, port)

	client := &http.Client{
		Timeout: 5 * time.Second, // Short timeout to avoid blocking
	}

	resp, err := client.Get(url)
	if err != nil {
		return false, fmt.Errorf("failed to fetch from app-service: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("app-service returned status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response body: %v", err)
	}

	var apps []utils.AppServiceResponse
	if err := json.Unmarshal(data, &apps); err != nil {
		return false, fmt.Errorf("failed to parse app-service response: %v", err)
	}

	// Find the app matching appName and userID
	for _, app := range apps {
		if app.Spec.Name == appName && app.Spec.Owner == userID {
			// Find the entrance in spec.entrances first
			var foundInvisible bool
			var invisibleValue bool
			for _, specEntrance := range app.Spec.Entrances {
				if specEntrance.Name == entranceName {
					foundInvisible = true
					invisibleValue = specEntrance.Invisible
					break
				}
			}

			if !foundInvisible {
				return false, fmt.Errorf("entrance %s not found in spec.entrances for app %s", entranceName, appName)
			}

			// Cache all entrances for this app to avoid future API calls (using TryLock to avoid blocking)
			if dw.appServiceCacheMutex.TryLock() {
				if dw.appServiceCache[cacheKey] == nil {
					dw.appServiceCache[cacheKey] = make(map[string]bool)
				}
				for _, specEntrance := range app.Spec.Entrances {
					dw.appServiceCache[cacheKey][specEntrance.Name] = specEntrance.Invisible
				}
				dw.appServiceCacheMutex.Unlock()
				log.Printf("DEBUG: fetchInvisibleFromAppService - fetched and cached invisible=%t for entrance %s (app=%s, user=%s)",
					invisibleValue, entranceName, appName, userID)
			} else {
				log.Printf("DEBUG: fetchInvisibleFromAppService - write lock not available, skipping cache update for entrance %s (app=%s, user=%s)",
					entranceName, appName, userID)
			}

			return invisibleValue, nil
		}
	}

	return false, fmt.Errorf("app %s not found for user %s", appName, userID)
}

// NewDataWatcherState creates a new DataWatcherState instance
func NewDataWatcherState(cacheManager *CacheManager, taskModule *task.TaskModule, historyModule *history.HistoryModule, dataWatcher *DataWatcher) *DataWatcherState {
	ctx, cancel := context.WithCancel(context.Background())

	dw := &DataWatcherState{
		ctx:             ctx,
		cancel:          cancel,
		isDev:           isDevEnvironment(),
		natsHost:        getEnvOrDefault("NATS_HOST", "localhost"),
		natsPort:        getEnvOrDefault("NATS_PORT", "4222"),
		natsUser:        getEnvOrDefault("NATS_USERNAME", ""),
		natsPass:        getEnvOrDefault("NATS_PASSWORD", ""),
		subject:         getEnvOrDefault("NATS_SUBJECT_SYSTEM_APP_STATE", "os.application.*"),
		historyModule:   historyModule,
		cacheManager:    cacheManager, // Set cache manager reference
		taskModule:      taskModule,   // Set task module reference
		dataWatcher:     dataWatcher,  // Set data watcher reference
		appServiceCache: make(map[string]map[string]bool),
		delayedMessages: make([]*DelayedMessage, 0),
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
	// Start delayed message processor
	go dw.processDelayedMessages()
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

	// Special handling for pending state: delay processing if there are pending/running install tasks
	// Or if task is not found in memory and we need to wait for database persistence
	if appStateMsg.OpType == "install" && appStateMsg.State == "pending" {
		if dw.taskModule != nil {
			hasPendingTask, lockAcquired := dw.taskModule.HasPendingOrRunningInstallTask(appStateMsg.Name, appStateMsg.User)
			if !lockAcquired {
				// If we can't acquire the lock, delay processing to be safe
				// This avoids blocking NATS message processing
				log.Printf("Delaying pending state message for app=%s, user=%s, opID=%s - failed to acquire lock",
					appStateMsg.Name, appStateMsg.User, appStateMsg.OpID)
				dw.addDelayedMessage(msg, appStateMsg)
				return
			}
			if hasPendingTask {
				// If there are pending/running install tasks, delay processing this message
				// This avoids matching to the wrong source when a new install starts
				log.Printf("Delaying pending state message for app=%s, user=%s, opID=%s - found pending/running install task",
					appStateMsg.Name, appStateMsg.User, appStateMsg.OpID)
				dw.addDelayedMessage(msg, appStateMsg)
				return
			}
		}
		// If we have OpID but no pending/running tasks, check if task exists in database
		// This handles the case where task completed very quickly before pending message arrived
		if appStateMsg.OpID != "" {
			db, err := utils.GetTaskStoreForQuery()
			if err == nil && db != nil {
				query := `
				SELECT metadata, type, status, created_at
				FROM task_records
				WHERE op_id = $1
					AND app_name = $2
					AND user_account = $3
					AND type IN ($4, $5)
				ORDER BY created_at DESC
				LIMIT 1
				`
				var metadataStr string
				var taskType int
				var taskStatus int
				var taskCreatedAt time.Time
				err = db.QueryRow(query, appStateMsg.OpID, appStateMsg.Name, appStateMsg.User, 1, 5).Scan(&metadataStr, &taskType, &taskStatus, &taskCreatedAt)
				if err != nil {
					// Task not found in database, delay to wait for task to be persisted
					log.Printf("Delaying pending state message for app=%s, user=%s, opID=%s - task not found in DB, waiting for persistence",
						appStateMsg.Name, appStateMsg.User, appStateMsg.OpID)
					dw.addDelayedMessage(msg, appStateMsg)
					return
				}
				// Task found in database, can proceed (storeStateToCache will use OpID to query from DB)
				log.Printf("Found task with OpID=%s in database for pending state message (app=%s, user=%s), proceeding with processing",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
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

	// Process the message
	dw.processMessageInternal(msg, appStateMsg)
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

// isFailedOrCanceledState checks if the state represents a failed or canceled operation
// These states should not be used to determine sourceID from cache, as they may be stale
func isFailedOrCanceledState(state string) bool {
	failedStates := []string{
		"installFailed",
		"downloadFailed",
		"installCancelFailed",
		"downloadCancelFailed",
		"pendingCanceled",
		"installCanceled",
		"installingCanceled",
		"downloadingCanceled",  // Canceled during downloading phase
		"downloadingCanceling", // In the process of canceling download
		"uninstallFailed",
	}
	for _, failedState := range failedStates {
		if state == failedState {
			return true
		}
	}
	return false
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
	if len(existingInvisible) > 0 {
		log.Printf("DEBUG: storeStateToCache - found %d existing invisible flags in cache for app %s (user=%s)",
			len(existingInvisible), msg.Name, userID)
		for name, val := range existingInvisible {
			log.Printf("DEBUG: storeStateToCache - cached invisible[%s]=%t", name, val)
		}
	} else {
		log.Printf("DEBUG: storeStateToCache - no existing invisible flags found in cache for app %s (user=%s)",
			msg.Name, userID)
	}

	// Add debug logging for entranceStatuses
	log.Printf("DEBUG: storeStateToCache - entranceStatuses count: %d", len(msg.EntranceStatuses))
	for i, entrance := range msg.EntranceStatuses {
		upstreamValue := "nil"
		if entrance.Invisible != nil {
			upstreamValue = fmt.Sprintf("%t", *entrance.Invisible)
		}
		invisible := dw.resolveInvisibleFlag(entrance.Invisible, entrance.Name, msg.Name, userID, existingInvisible)
		log.Printf("DEBUG: storeStateToCache - entrance[%d]: ID=%s, Name=%s, State=%s, URL=%s, UpstreamInvisible=%s, ResolvedInvisible=%t",
			i, entrance.ID, entrance.Name, entrance.State, entrance.Url, upstreamValue, invisible)
	}

	entranceStatuses := make([]interface{}, len(msg.EntranceStatuses))
	for i, v := range msg.EntranceStatuses {
		invisible := dw.resolveInvisibleFlag(v.Invisible, v.Name, msg.Name, userID, existingInvisible)
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

	// Convert SharedEntrances to interface slice
	sharedEntrances := make([]interface{}, len(msg.SharedEntrances))
	for i, v := range msg.SharedEntrances {
		sharedEntrances[i] = map[string]interface{}{
			"name":            v.Name,
			"host":            v.Host,
			"port":            v.Port,
			"icon":            v.Icon,
			"title":           v.Title,
			"authLevel":       v.AuthLevel,
			"invisible":       v.Invisible,
			"url":             v.URL,
			"openMethod":      v.OpenMethod,
			"windowPushState": v.WindowPushState,
			"skip":            v.Skip,
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
		"opType":             msg.OpType,     // Add operation type from message
	}

	// Add SharedEntrances if present
	if len(msg.SharedEntrances) > 0 {
		stateData["sharedEntrances"] = sharedEntrances
	}

	// Add debug logging for stateData
	log.Printf("DEBUG: storeStateToCache - stateData keys: %v", getMapKeys(stateData))
	if entranceStatusesVal, ok := stateData["entranceStatuses"]; ok {
		log.Printf("DEBUG: storeStateToCache - entranceStatuses type: %T, value: %+v", entranceStatusesVal, entranceStatusesVal)
	}

	sourceID := ""

	// If current message is in failed/canceled state, skip cache lookup to avoid using stale source
	// and query from task module or database instead
	skipCacheLookup := isFailedOrCanceledState(msg.State)

	// For install operations, prioritize OpID-based lookup to ensure accurate source matching
	// This is especially important when tasks complete quickly before state messages arrive
	isInstallOp := msg.OpType == "install"
	shouldPrioritizeOpID := isInstallOp && msg.OpID != ""

	if shouldPrioritizeOpID {
		// Try OpID lookup first (from memory or database)
		if dw.taskModule != nil {
			_, src, found, _ := dw.taskModule.GetTaskByOpID(msg.OpID)
			if found && src != "" {
				sourceID = src
				log.Printf("Found task with source=%s by OpID=%s for app=%s, user=%s (prioritized OpID lookup)",
					src, msg.OpID, msg.Name, userID)
			} else {
				// Task not found in memory, try to query from database (task might be completed)
				db, err := utils.GetTaskStoreForQuery()
				if err == nil && db != nil {
					query := `
					SELECT metadata, type, status, created_at
					FROM task_records
					WHERE op_id = $1
						AND app_name = $2
						AND user_account = $3
						AND type IN ($4, $5)
					ORDER BY created_at DESC
					LIMIT 1
					`
					var metadataStr string
					var taskType int
					var taskStatus int
					var taskCreatedAt time.Time
					err = db.QueryRow(query, msg.OpID, msg.Name, userID, 1, 5).Scan(&metadataStr, &taskType, &taskStatus, &taskCreatedAt)
					if err == nil && metadataStr != "" {
						var metadataMap map[string]interface{}
						if err := json.Unmarshal([]byte(metadataStr), &metadataMap); err == nil {
							if s, ok := metadataMap["source"].(string); ok && s != "" {
								sourceID = s
								log.Printf("Found task with source=%s by OpID=%s from database (status=%d, created_at=%s) for app=%s, user=%s (prioritized OpID lookup)",
									sourceID, msg.OpID, taskStatus, taskCreatedAt.Format(time.RFC3339), msg.Name, userID)
							}
						}
					}
				}
			}
		}
	}

	// If OpID lookup didn't find source, fall back to cache lookup (for non-install ops or when OpID is missing)
	if sourceID == "" && !skipCacheLookup {
		userData := dw.cacheManager.GetUserData(userID)
		if userData != nil {
			// Collect all matching app states from all sources
			type candidateRecord struct {
				sourceID   string
				appState   *AppStateLatestData
				statusTime time.Time
			}
			var candidates []candidateRecord

			for srcID, srcData := range userData.Sources {
				if srcData == nil || srcData.AppStateLatest == nil {
					continue
				}
				for _, appState := range srcData.AppStateLatest {
					// Exclude uninstalled and failed/canceled states when looking up sourceID
					if appState != nil &&
						appState.Status.State != "uninstalled" &&
						!isFailedOrCanceledState(appState.Status.State) &&
						appState.Status.Name == msg.Name {
						// Parse statusTime for sorting
						var statusTime time.Time
						if appState.Status.StatusTime != "" {
							if parsedTime, err := time.Parse("2006-01-02T15:04:05.000000000Z", appState.Status.StatusTime); err == nil {
								statusTime = parsedTime
							} else {
								// If parsing fails, use zero time (will be sorted to the end)
								log.Printf("Failed to parse StatusTime for app %s in source %s: %v", msg.Name, srcID, err)
								statusTime = time.Time{}
							}
						}
						candidates = append(candidates, candidateRecord{
							sourceID:   srcID,
							appState:   appState,
							statusTime: statusTime,
						})
					}
				}
			}

			// Sort by statusTime in descending order (newest first)
			if len(candidates) > 0 {
				// Log all candidates before sorting for debugging
				if len(candidates) > 1 {
					log.Printf("Found %d candidate records for app=%s, user=%s:", len(candidates), msg.Name, userID)
					for i, cand := range candidates {
						log.Printf("  Candidate[%d]: sourceID=%s, state=%s, statusTime=%s",
							i, cand.sourceID, cand.appState.Status.State, cand.statusTime.Format(time.RFC3339))
					}
				}
				// Sort candidates by statusTime descending (newest first)
				sort.Slice(candidates, func(i, j int) bool {
					return candidates[i].statusTime.After(candidates[j].statusTime)
				})
				// Use the newest record
				sourceID = candidates[0].sourceID
				if len(candidates) > 1 {
					log.Printf("Found sourceID=%s from cache (newest of %d records) for app=%s, user=%s, state=%s, statusTime=%s",
						sourceID, len(candidates), msg.Name, userID, candidates[0].appState.Status.State, candidates[0].statusTime.Format(time.RFC3339))
				} else {
					log.Printf("Found sourceID=%s from cache for app=%s, user=%s, state=%s, statusTime=%s",
						sourceID, msg.Name, userID, candidates[0].appState.Status.State, candidates[0].statusTime.Format(time.RFC3339))
				}
			}
		}
	} else {
		log.Printf("Skipping cache lookup for sourceID due to failed/canceled state: %s for app=%s, user=%s",
			msg.State, msg.Name, userID)
	}

	// If OpID lookup was not prioritized (non-install ops or missing OpID), try fallback methods
	if sourceID == "" && dw.taskModule != nil {
		// For non-install ops or when OpID is missing, try OpID lookup as fallback
		if msg.OpID != "" && !shouldPrioritizeOpID {
			_, src, found, _ := dw.taskModule.GetTaskByOpID(msg.OpID)
			if found && src != "" {
				sourceID = src
				log.Printf("Found task with source=%s by OpID=%s for app=%s, user=%s", src, msg.OpID, msg.Name, userID)
			} else {
				// Task not found in memory, try to query from database (task might be completed)
				db, err := utils.GetTaskStoreForQuery()
				if err == nil && db != nil {
					query := `
					SELECT metadata, type, status, created_at
					FROM task_records
					WHERE op_id = $1
						AND app_name = $2
						AND user_account = $3
						AND type IN ($4, $5)
					ORDER BY created_at DESC
					LIMIT 1
					`
					var metadataStr string
					var taskType int
					var taskStatus int
					var taskCreatedAt time.Time
					err = db.QueryRow(query, msg.OpID, msg.Name, userID, 1, 5).Scan(&metadataStr, &taskType, &taskStatus, &taskCreatedAt)
					if err == nil && metadataStr != "" {
						var metadataMap map[string]interface{}
						if err := json.Unmarshal([]byte(metadataStr), &metadataMap); err == nil {
							if s, ok := metadataMap["source"].(string); ok && s != "" {
								sourceID = s
								log.Printf("Found task with source=%s by OpID=%s from database (status=%d, created_at=%s) for app=%s, user=%s",
									sourceID, msg.OpID, taskStatus, taskCreatedAt.Format(time.RFC3339), msg.Name, userID)
							}
						}
					}
				}
			}
		}
		// If OpID match failed, try to find by appName and user
		if sourceID == "" {
			_, src, found, _ := dw.taskModule.GetLatestTaskByAppNameAndUser(msg.Name, userID)
			if found && src != "" {
				sourceID = src
				log.Printf("Found task with source=%s for app=%s, user=%s", src, msg.Name, userID)
			}
		}
	}

	// If still not found, try to query from task store database (all statuses, ordered by time)
	if sourceID == "" {
		db, err := utils.GetTaskStoreForQuery()
		if err == nil && db != nil {
			// Query for latest task (InstallApp or CloneApp) with all statuses
			// Priority: Running (2) > Pending (1) > Completed (3) > Failed (4) > Canceled (5)
			// Within same priority, order by created_at descending (newest first)
			query := `
		SELECT metadata, type, status, created_at
		FROM task_records
		WHERE app_name = $1
			AND user_account = $2
			AND type IN ($3, $4)
		ORDER BY 
			CASE status
				WHEN 2 THEN 1  -- Running: highest priority
				WHEN 1 THEN 2  -- Pending: second priority
				WHEN 3 THEN 3  -- Completed: third priority
				WHEN 4 THEN 4  -- Failed: fourth priority
				WHEN 5 THEN 5  -- Canceled: lowest priority
				ELSE 6
			END,
			created_at DESC
		LIMIT 1
		`
			// Task types: InstallApp = 1, CloneApp = 5
			var metadataStr string
			var taskType int
			var taskStatus int
			var taskCreatedAt time.Time
			err = db.QueryRow(query, msg.Name, userID, 1, 5).Scan(&metadataStr, &taskType, &taskStatus, &taskCreatedAt)
			if err == nil && metadataStr != "" {
				var metadataMap map[string]interface{}
				if err := json.Unmarshal([]byte(metadataStr), &metadataMap); err == nil {
					if s, ok := metadataMap["source"].(string); ok && s != "" {
						sourceID = s
						log.Printf("Found source=%s from task database (status=%d, created_at=%s) for app=%s, user=%s",
							sourceID, taskStatus, taskCreatedAt.Format(time.RFC3339), msg.Name, userID)
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
	if len(msg.SharedEntrances) > 0 {
		log.Printf("Shared Entrances:")
		for i, entrance := range msg.SharedEntrances {
			log.Printf("  [%d] Name: %s, Host: %s, Port: %d, URL: %s, Invisible: %t",
				i, entrance.Name, entrance.Host, entrance.Port, entrance.URL, entrance.Invisible)
		}
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

// addDelayedMessage adds a message to the delayed processing queue
func (dw *DataWatcherState) addDelayedMessage(msg *nats.Msg, appStateMsg AppStateMessage) {
	dw.delayedMessagesMutex.Lock()
	defer dw.delayedMessagesMutex.Unlock()

	delayedMsg := &DelayedMessage{
		msg:        msg,
		appStateMsg: appStateMsg,
		retryCount: 0,
		nextRetry:  time.Now().Add(2 * time.Second), // First retry after 2 seconds
	}

	dw.delayedMessages = append(dw.delayedMessages, delayedMsg)
	log.Printf("Added delayed message to queue: app=%s, user=%s, opID=%s, queue_size=%d",
		appStateMsg.Name, appStateMsg.User, appStateMsg.OpID, len(dw.delayedMessages))
}

// processDelayedMessages processes delayed messages in the background
func (dw *DataWatcherState) processDelayedMessages() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dw.ctx.Done():
			return
		case <-ticker.C:
			dw.processDelayedMessagesBatch()
		}
	}
}

// processDelayedMessagesBatch processes a batch of delayed messages that are ready to be retried
func (dw *DataWatcherState) processDelayedMessagesBatch() {
	dw.delayedMessagesMutex.Lock()
	defer dw.delayedMessagesMutex.Unlock()

	now := time.Now()
	var remaining []*DelayedMessage
	maxRetries := 10 // Maximum 10 retries (about 20 seconds total)

	for _, delayedMsg := range dw.delayedMessages {
		if now.Before(delayedMsg.nextRetry) {
			// Not ready to retry yet
			remaining = append(remaining, delayedMsg)
			continue
		}

		if delayedMsg.retryCount >= maxRetries {
			// Max retries reached, process anyway
			log.Printf("Max retries reached for delayed message: app=%s, user=%s, opID=%s, processing anyway",
				delayedMsg.appStateMsg.Name, delayedMsg.appStateMsg.User, delayedMsg.appStateMsg.OpID)
			dw.processMessageInternal(delayedMsg.msg, delayedMsg.appStateMsg)
			continue
		}

		// Check if there are still pending/running install tasks
		if dw.taskModule != nil {
			hasPendingTask, lockAcquired := dw.taskModule.HasPendingOrRunningInstallTask(delayedMsg.appStateMsg.Name, delayedMsg.appStateMsg.User)
			if !lockAcquired {
				// If we can't acquire the lock, retry later
				delayedMsg.retryCount++
				delayedMsg.nextRetry = now.Add(2 * time.Second)
				remaining = append(remaining, delayedMsg)
				log.Printf("Retrying delayed message later (lock not acquired): app=%s, user=%s, opID=%s, retry_count=%d",
					delayedMsg.appStateMsg.Name, delayedMsg.appStateMsg.User, delayedMsg.appStateMsg.OpID, delayedMsg.retryCount)
				continue
			}
			if hasPendingTask {
				// Still has pending tasks, retry later
				delayedMsg.retryCount++
				delayedMsg.nextRetry = now.Add(2 * time.Second)
				remaining = append(remaining, delayedMsg)
				log.Printf("Retrying delayed message later: app=%s, user=%s, opID=%s, retry_count=%d",
					delayedMsg.appStateMsg.Name, delayedMsg.appStateMsg.User, delayedMsg.appStateMsg.OpID, delayedMsg.retryCount)
				continue
			}
		}

		// No pending tasks, process the message
		log.Printf("Processing delayed message: app=%s, user=%s, opID=%s, retry_count=%d",
			delayedMsg.appStateMsg.Name, delayedMsg.appStateMsg.User, delayedMsg.appStateMsg.OpID, delayedMsg.retryCount)
		dw.processMessageInternal(delayedMsg.msg, delayedMsg.appStateMsg)
	}

	dw.delayedMessages = remaining
}

// processMessageInternal processes a message (extracted from handleMessage for reuse)
func (dw *DataWatcherState) processMessageInternal(msg *nats.Msg, appStateMsg AppStateMessage) {
	// Store as history record
	dw.storeHistoryRecord(appStateMsg, string(msg.Data))

	// Store state data to cache
	dw.storeStateToCache(appStateMsg)

	// Print the parsed message
	dw.printAppStateMessage(appStateMsg)
}
