package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"market/internal/v2/history" // Import history module with correct path
	"market/internal/v2/task"

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
	Invisible  bool   `json:"invisible"`
}

// AppStateMessage represents the message structure from NATS
type AppStateMessage struct {
	EventID          string           `json:"eventID"`
	CreateTime       string           `json:"createTime"`
	Name             string           `json:"name"`
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

// Start starts the data watcher
func (dw *DataWatcherState) Start() error {
	if dw.isDev {
		log.Println("Starting data watcher in development mode")
		go dw.startDevMockData()
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
			if err := dw.taskModule.InstallTaskSucceed(appStateMsg.OpID); err != nil {
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
	if appStateMsg.OpType == "install" && (appStateMsg.State == "installFailed" || appStateMsg.State == "downloadFailed") {
		log.Printf("Detected install operation with %s state for opID: %s, app: %s, user: %s",
			appStateMsg.State, appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		// Call InstallTaskFailed if task module is available
		if dw.taskModule != nil {
			errorMsg := fmt.Sprintf("Installation failed for app %s", appStateMsg.Name)
			if err := dw.taskModule.InstallTaskFailed(appStateMsg.OpID, errorMsg); err != nil {
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
	if appStateMsg.OpType == "install" && appStateMsg.State == "installCanceled" {
		log.Printf("Detected install operation with installCanceled state for opID: %s, app: %s, user: %s",
			appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)

		// Call CancelInstallTaskSucceed if task module is available
		if dw.taskModule != nil {
			if err := dw.taskModule.CancelInstallTaskSucceed(appStateMsg.OpID); err != nil {
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
			if err := dw.taskModule.CancelInstallTaskFailed(appStateMsg.OpID, errorMsg); err != nil {
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
			if err := dw.taskModule.UninstallTaskSucceed(appStateMsg.OpID); err != nil {
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
			if err := dw.taskModule.UninstallTaskFailed(appStateMsg.OpID, errorMsg); err != nil {
				log.Printf("Failed to mark uninstall task as failed for opID %s: %v", appStateMsg.OpID, err)
			} else {
				log.Printf("Successfully marked uninstall task as failed for opID: %s, app: %s, user: %s",
					appStateMsg.OpID, appStateMsg.Name, appStateMsg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark uninstall task as failed for opID: %s", appStateMsg.OpID)
		}
	}

	// Store as history record
	dw.storeHistoryRecord(appStateMsg, string(msg.Data))

	// Store state data to cache
	dw.storeStateToCache(appStateMsg)

	// Print the parsed message
	dw.printAppStateMessage(appStateMsg)
}

// startDevMockData generates mock data every 30 seconds in development mode
func (dw *DataWatcherState) startDevMockData() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("Development mode: generating mock data every 30 seconds")

	// Generate first message immediately
	dw.generateMockMessage()

	for {
		select {
		case <-ticker.C:
			dw.generateMockMessage()
		case <-dw.ctx.Done():
			log.Println("Development mock data generator stopped")
			return
		}
	}
}

// generateMockMessage creates and processes a random mock message
func (dw *DataWatcherState) generateMockMessage() {
	appIds := []string{"app001", "app002", "app003", "app004", "app005"}
	states := []string{"running", "stopped", "starting", "stopping", "error", "downloading", "installFailed", "downloadFailed"}
	opTypes := []string{"install", "uninstall", "update", "restart", "start", "stop", "cancel"}
	users := []string{"admin", "user1", "user2", "olaresid"}
	progressValues := []string{"0%", "25%", "50%", "75%", "100%", "downloading...", "installing...", "updating..."}

	// Create sample entrance statuses
	entranceStatuses := []EntranceStatus{
		{
			ID:         "http", // ID extracted from "http://localhost:8080/aa"
			Name:       "aa",
			State:      "",
			StatusTime: "",
			Reason:     "",
			Url:        "http://localhost:8080/aa",
			Invisible:  false,
		},
		{
			ID:         "http", // ID extracted from "http://localhost:8080/bb"
			Name:       "bb",
			State:      "downloading",
			StatusTime: time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
			Reason:     "",
			Url:        "http://localhost:8080/bb",
			Invisible:  false,
		},
	}

	msg := AppStateMessage{
		EventID:          fmt.Sprintf("event_%d_%d", time.Now().Unix(), rand.Intn(1000)),
		CreateTime:       time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		Name:             appIds[rand.Intn(len(appIds))],
		OpID:             fmt.Sprintf("task_%d", rand.Intn(10000)),
		OpType:           opTypes[rand.Intn(len(opTypes))],
		State:            states[rand.Intn(len(states))],
		User:             users[rand.Intn(len(users))],
		Progress:         progressValues[rand.Intn(len(progressValues))],
		EntranceStatuses: entranceStatuses,
	}

	jsonData, _ := json.Marshal(msg)
	log.Printf("Generated mock message: %s", string(jsonData))

	// Check if this is an install operation with running state
	if msg.OpType == "install" && msg.State == "running" {
		log.Printf("Detected install operation with running state for opID: %s, app: %s, user: %s",
			msg.OpID, msg.Name, msg.User)

		// Call InstallTaskSucceed if task module is available
		if dw.taskModule != nil {
			if err := dw.taskModule.InstallTaskSucceed(msg.OpID); err != nil {
				log.Printf("Failed to mark install task as succeeded for opID %s: %v", msg.OpID, err)
			} else {
				log.Printf("Successfully marked install task as succeeded for opID: %s, app: %s, user: %s",
					msg.OpID, msg.Name, msg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark install task as succeeded for opID: %s", msg.OpID)
		}
	}

	// Check if this is an install operation with installFailed or downloadFailed state
	if msg.OpType == "install" && (msg.State == "installFailed" || msg.State == "downloadFailed") {
		log.Printf("Detected install operation with %s state for opID: %s, app: %s, user: %s",
			msg.State, msg.OpID, msg.Name, msg.User)

		// Call InstallTaskFailed if task module is available
		if dw.taskModule != nil {
			errorMsg := fmt.Sprintf("Installation failed for app %s", msg.Name)
			if err := dw.taskModule.InstallTaskFailed(msg.OpID, errorMsg); err != nil {
				log.Printf("Failed to mark install task as failed for opID %s: %v", msg.OpID, err)
			} else {
				log.Printf("Successfully marked install task as failed for opID: %s, app: %s, user: %s",
					msg.OpID, msg.Name, msg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark install task as failed for opID: %s", msg.OpID)
		}
	}

	// Check if this is an install operation with installCanceled state
	if msg.OpType == "install" && msg.State == "installCanceled" {
		log.Printf("Detected install operation with installCanceled state for opID: %s, app: %s, user: %s",
			msg.OpID, msg.Name, msg.User)

		// Call CancelInstallTaskSucceed if task module is available
		if dw.taskModule != nil {
			if err := dw.taskModule.CancelInstallTaskSucceed(msg.OpID); err != nil {
				log.Printf("Failed to mark cancel install task as succeeded for opID %s: %v", msg.OpID, err)
			} else {
				log.Printf("Successfully marked cancel install task as succeeded for opID: %s, app: %s, user: %s",
					msg.OpID, msg.Name, msg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark cancel install task as succeeded for opID: %s", msg.OpID)
		}
	}

	// Check if this is an install operation with installCancelFailed or downloadCancelFailed state
	if msg.OpType == "install" && (msg.State == "installCancelFailed" || msg.State == "downloadCancelFailed") {
		log.Printf("Detected install operation with %s state for opID: %s, app: %s, user: %s",
			msg.State, msg.OpID, msg.Name, msg.User)

		// Call CancelInstallTaskFailed if task module is available
		if dw.taskModule != nil {
			errorMsg := fmt.Sprintf("Cancel installation failed for app %s", msg.Name)
			if err := dw.taskModule.CancelInstallTaskFailed(msg.OpID, errorMsg); err != nil {
				log.Printf("Failed to mark cancel install task as failed for opID %s: %v", msg.OpID, err)
			} else {
				log.Printf("Successfully marked cancel install task as failed for opID: %s, app: %s, user: %s",
					msg.OpID, msg.Name, msg.User)
			}
		} else {
			log.Printf("Task module not available, cannot mark cancel install task as failed for opID: %s", msg.OpID)
		}
	}

	// Store as history record
	dw.storeHistoryRecord(msg, string(jsonData))

	// Store state data to cache
	dw.storeStateToCache(msg)

	dw.printAppStateMessage(msg)
}

// storeHistoryRecord stores the app state message as a history record
func (dw *DataWatcherState) storeHistoryRecord(msg AppStateMessage, rawMessage string) {
	if dw.historyModule == nil {
		log.Printf("History module not available, skipping record storage")
		return
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

	// Add debug logging for entranceStatuses
	log.Printf("DEBUG: storeStateToCache - entranceStatuses count: %d", len(msg.EntranceStatuses))
	for i, entrance := range msg.EntranceStatuses {
		log.Printf("DEBUG: storeStateToCache - entrance[%d]: ID=%s, Name=%s, State=%s, URL=%s, Invisible=%t",
			i, entrance.ID, entrance.Name, entrance.State, entrance.Url, entrance.Invisible)
	}

	entranceStatuses := make([]interface{}, len(msg.EntranceStatuses))
	for i, v := range msg.EntranceStatuses {
		entranceStatuses[i] = map[string]interface{}{
			"id":         v.ID,
			"name":       v.Name,
			"state":      v.State,
			"statusTime": v.StatusTime,
			"reason":     v.Reason,
			"url":        v.Url,
			"invisible":  v.Invisible,
		}
	}

	stateData := map[string]interface{}{
		"state":              msg.State,
		"updateTime":         "",
		"statusTime":         msg.CreateTime,
		"lastTransitionTime": "",
		"progress":           msg.Progress,
		"entranceStatuses":   entranceStatuses,
		"name":               msg.Name, // Add app name for state monitoring
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
				if appState != nil && appState.Status.Name == msg.Name {
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
		_, src, found := dw.taskModule.GetLatestTaskByAppNameAndUser(msg.Name, userID)
		if found && src != "" {
			sourceID = src
			log.Printf("Found task with source=%s for app=%s, user=%s", src, msg.Name, userID)
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
