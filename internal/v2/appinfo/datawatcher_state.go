package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"market/internal/v2/helper"
	"market/internal/v2/history" // Import history module with correct path
	"market/internal/v2/task"
	"market/internal/v2/utils"

	"github.com/golang/glog"
	"github.com/nats-io/nats.go"
)

var nanoTimeLayout = "2006-01-02T15:04:05.999999999Z" // 2006-01-02T15:04:05.000000000Z

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
	MarketSource     string           `json:"marketSource"`
	Reason           string           `json:"reason"`
	Message          string           `json:"message"`
	EntranceStatuses []EntranceStatus `json:"entranceStatuses"`
	SharedEntrances  []SharedEntrance `json:"sharedEntrances,omitempty"`
}

// DelayedMessage represents a message that needs to be processed later
type DelayedMessage struct {
	msg         *nats.Msg
	appStateMsg AppStateMessage
	retryCount  int
	nextRetry   time.Time
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
	historyModule atomic.Pointer[history.HistoryModule]
	cacheManager  *CacheManager
	taskModule    atomic.Pointer[task.TaskModule]
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
			glog.V(3).Infof("resolveInvisibleFlag - fetched invisible=%t for entrance %s (app=%s, user=%s) from app-service API",
				specInvisible, entranceName, appName, userID)
			return specInvisible
		} else {
			glog.Errorf("resolveInvisibleFlag - failed to fetch invisible for entrance %s (app=%s, user=%s) from app-service: %v",
				entranceName, appName, userID, err)
		}
	}

	return false
}

// fetchInvisibleFromAppService fetches invisible flag from app-service API's spec.entrances
// Uses caching to avoid repeated API calls for the same app
func (dw *DataWatcherState) fetchInvisibleFromAppService(appName, userID, entranceName string) (bool, error) {
	cacheKey := fmt.Sprintf("%s:%s", userID, appName)

	// Check cache first (short read lock)
	dw.appServiceCacheMutex.RLock()
	if appCache, exists := dw.appServiceCache[cacheKey]; exists {
		if invisible, found := appCache[entranceName]; found {
			dw.appServiceCacheMutex.RUnlock()
			glog.V(3).Infof("fetchInvisibleFromAppService - cached invisible=%t for entrance %s (app=%s, user=%s)",
				invisible, entranceName, appName, userID)
			return invisible, nil
		}
	}
	dw.appServiceCacheMutex.RUnlock()

	// Fetch from API (no lock held)
	host := helper.GetEnvOrDefault("APP_SERVICE_SERVICE_HOST", "localhost")
	port := helper.GetEnvOrDefault("APP_SERVICE_SERVICE_PORT", "80")
	url := fmt.Sprintf("http://%s:%s/app-service/v1/all/apps", host, port)

	client := &http.Client{
		Timeout: 5 * time.Second,
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

	for _, app := range apps {
		if app.Spec.Name == appName && app.Spec.Owner == userID {
			var foundInvisible bool
			var invisibleValue bool
			for _, specEntrance := range app.Spec.EntranceStatuses {
				if specEntrance.Name == entranceName {
					foundInvisible = true
					invisibleValue = specEntrance.Invisible
					break
				}
			}

			if !foundInvisible {
				return false, fmt.Errorf("entrance %s not found in spec.entrances for app %s", entranceName, appName)
			}

			// Write cache (separate write lock, no read lock held)
			dw.appServiceCacheMutex.Lock()
			if dw.appServiceCache[cacheKey] == nil {
				dw.appServiceCache[cacheKey] = make(map[string]bool)
			}
			for _, specEntrance := range app.Spec.EntranceStatuses {
				dw.appServiceCache[cacheKey][specEntrance.Name] = specEntrance.Invisible
			}
			dw.appServiceCacheMutex.Unlock()

			glog.V(3).Infof("fetchInvisibleFromAppService - fetched and cached invisible=%t for entrance %s (app=%s, user=%s)",
				invisibleValue, entranceName, appName, userID)
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
		isDev:           false,
		natsHost:        helper.GetEnvOrDefault("NATS_HOST", "localhost"),
		natsPort:        helper.GetEnvOrDefault("NATS_PORT", "4222"),
		natsUser:        helper.GetEnvOrDefault("NATS_USERNAME", ""),
		natsPass:        helper.GetEnvOrDefault("NATS_PASSWORD", ""),
		subject:         helper.GetEnvOrDefault("NATS_SUBJECT_SYSTEM_APP_STATE", "os.application.*"),
		cacheManager:    cacheManager,
		dataWatcher:     dataWatcher,
		appServiceCache: make(map[string]map[string]bool),
		delayedMessages: make([]*DelayedMessage, 0),
	}
	if taskModule != nil {
		dw.taskModule.Store(taskModule)
	}
	if historyModule != nil {
		dw.historyModule.Store(historyModule)
	}

	return dw
}

// SetTaskModule updates the task module reference without restarting subscriptions.
func (dw *DataWatcherState) SetTaskModule(taskModule *task.TaskModule) {
	dw.taskModule.Store(taskModule)
	glog.V(3).Info("DataWatcherState task module updated")
}

// getTaskModule returns the current task module pointer (thread-safe).
func (dw *DataWatcherState) getTaskModule() *task.TaskModule {
	return dw.taskModule.Load()
}

// SetHistoryModule updates the history module reference without restarting subscriptions.
func (dw *DataWatcherState) SetHistoryModule(historyModule *history.HistoryModule) {
	dw.historyModule.Store(historyModule)
	glog.V(3).Info("DataWatcherState history module updated")
}

// getHistoryModule returns the current history module pointer (thread-safe).
func (dw *DataWatcherState) getHistoryModule() *history.HistoryModule {
	return dw.historyModule.Load()
}

// Start starts the data watcher
func (dw *DataWatcherState) Start() error {

	if helper.IsPublicEnvironment() {
		glog.V(3).Info("Public environment detected, DataWatcherState disabled")
		return nil
	}

	glog.V(2).Info("Starting data watcher in production mode")
	// Start NATS connection in a goroutine
	go func() {
		if err := dw.startNatsConnection(); err != nil {
			glog.Errorf("Error in NATS connection: %v", err)
		}
	}()
	
	return nil
}

// Stop stops the data watcher
func (dw *DataWatcherState) Stop() error {
	dw.cancel()

	if dw.sub != nil {
		if err := dw.sub.Unsubscribe(); err != nil {
			glog.Errorf("Error unsubscribing from NATS: %v", err)
		}
	}

	if dw.nc != nil {
		dw.nc.Close()
	}

	// Close history module
	if hm := dw.getHistoryModule(); hm != nil {
		if err := hm.Close(); err != nil {
			glog.Errorf("Error closing history module: %v", err)
		}
	}

	glog.V(3).Info("Data watcher stopped")
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
			glog.V(2).Infof("[NATS] Disconnected from %s", nc.ConnectedUrl())
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			glog.V(2).Infof("[NATS] Reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			glog.V(2).Infof("[NATS] Connection closed: %v", nc.LastError())
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			glog.Errorf("[NATS] Error: %v", err)
		}),
		nats.MaxReconnects(60),
		nats.ReconnectWait(2*time.Second),
	)

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS at %s: %w", natsURL, err)
	}

	dw.nc = nc
	glog.V(3).Infof("Connected to NATS at %s", natsURL)

	// Subscribe to the subject
	sub, err := nc.Subscribe(dw.subject, dw.handleMessage)
	if err != nil {
		return fmt.Errorf("failed to subscribe to subject %s: %w", dw.subject, err)
	}

	dw.sub = sub
	glog.V(2).Infof("Subscribed to NATS subject: %s", dw.subject)

	// Wait for context cancellation
	<-dw.ctx.Done()
	return nil
}

// handleMessage processes incoming NATS messages
func (dw *DataWatcherState) handleMessage(msg *nats.Msg) {
}


func isFailedOrCanceledState(state string) bool {
	// failedStates := []string{
	// 	"installFailed",
	// 	"downloadFailed",
	// 	"installCancelFailed",
	// 	"downloadCancelFailed",
	// 	"pendingCanceled",
	// 	"installCanceled",
	// 	"installingCanceled",
	// 	"downloadingCanceled",  // Canceled during downloading phase
	// 	"downloadingCanceling", // In the process of canceling download
	// 	"uninstallFailed",
	// }
	// for _, failedState := range failedStates {
	// 	if state == failedState {
	// 		return true
	// 	}
	// }
	return false
}

func (dw *DataWatcherState) printAppStateMessage(msg AppStateMessage) {
	// glog.V(2).Info("=== App State Message ===")
	// glog.V(2).Infof("Event ID: %s", msg.EventID)
	// glog.V(2).Infof("Create Time: %s", msg.CreateTime)
	// glog.V(2).Infof("Name: %s", msg.Name)
	// glog.V(2).Infof("Raw App Name: %s", msg.RawAppName)
	// glog.V(2).Infof("Title: %s", msg.Title)
	// glog.V(2).Infof("Source: %s", msg.MarketSource)
	// glog.V(2).Infof("State: %s", msg.State)
	// glog.V(2).Infof("Progress: %s", msg.Progress)
	// glog.V(2).Infof("Operation Type: %s", msg.OpType)
	// glog.V(2).Infof("Operation ID: %s", msg.OpID)
	// glog.V(2).Infof("User: %s", msg.User)
	// glog.V(2).Infof("Reason: %s", msg.Reason)
	// glog.V(2).Infof("Message: %s", msg.Message)
	// glog.V(2).Info("Entrance Statuses:")
	// for i, status := range msg.EntranceStatuses {
	// 	glog.V(2).Infof("  [%d] Name: %s, State: %s, Status Time: %s, Reason: %s",
	// 		i, status.Name, status.State, status.StatusTime, status.Reason)
	// }
	// if len(msg.SharedEntrances) > 0 {
	// 	glog.V(2).Info("Shared Entrances:")
	// 	for i, entrance := range msg.SharedEntrances {
	// 		glog.V(2).Infof("  [%d] Name: %s, Host: %s, Port: %d, URL: %s, Invisible: %t",
	// 			i, entrance.Name, entrance.Host, entrance.Port, entrance.URL, entrance.Invisible)
	// 	}
	// }
}

func (dw *DataWatcherState) shouldProcessStateMessage(appStateMsg AppStateMessage) bool {
	return false
	// shouldUpdate := true
	// checker := func(appState *AppStateLatestData) {
	// 	switch {
	// 	case appStateMsg.State == "running" && appState.Status.State == "installing":
	// 	case appStateMsg.State == "running" && appState.Status.State == "initializing":
	// 	case appStateMsg.State == "uninstalled" && appState.Status.State == "running":
	// 	case appStateMsg.State == "uninstalled" && appState.Status.State == "stopped":
	// 	case appStateMsg.State == "uninstalled" && appState.Status.State == "uninstalling":
	// 	case appStateMsg.State == "uninstalled" && appState.Status.State == "installingCanceling":
	// 	case appStateMsg.State == "uninstalled" && appState.Status.State == "installingCancelFailed":
	// 	case appStateMsg.State == "pendingCanceled" && appState.Status.State == "pending":
	// 	case appStateMsg.State == "stopped" && appState.Status.State == "pending":
	// 	case appStateMsg.State == "downloadingCanceled" && appState.Status.State == "downloadingCanceling":
	// 	case appStateMsg.State == "downloadingCanceled" && appState.Status.State == "pending":
	// 	case appStateMsg.State == "installingCanceled" && appState.Status.State == "installing":
	// 	case appStateMsg.State == "installingCanceled" && appState.Status.State == "installingCanceling":
	// 	case appStateMsg.State == "running" && appState.Status.State == "resuming":
	// 	case appStateMsg.State == "stopped" && appState.Status.State == "resuming":
	// 	case appStateMsg.State == "installingCanceled" && appState.Status.State == "resuming":
	// 	case appStateMsg.State == "stopped" && appState.Status.State == "stopping":
	// 	default:
	// 		if len(appStateMsg.EntranceStatuses) == 0 && appState.Status.Progress == appStateMsg.Progress {
	// 			glog.V(2).Infof("App state message is the same as the cached app state message for app %s, user %s, source %s, appState: %s, msgState: %s",
	// 				appStateMsg.Name, appStateMsg.User, appStateMsg.OpID, appState.Status.State, appStateMsg.State)
	// 			shouldUpdate = false
	// 			return
	// 		}
	// 	}

	// 	if appState.Status.StatusTime != "" && appStateMsg.CreateTime != "" {
	// 		statusTime, err1 := time.Parse(nanoTimeLayout, appState.Status.StatusTime)
	// 		createTime, err2 := time.Parse(nanoTimeLayout, appStateMsg.CreateTime)

	// 		if err1 == nil && err2 == nil {
	// 			if statusTime.After(createTime) {
	// 				glog.V(2).Infof("Cached app state is newer than incoming message for app %s, user %s, source %s, appTime: %s, msgTime: %s. Skipping update.",
	// 					appStateMsg.Name, appStateMsg.User, appStateMsg.OpID, statusTime.String(), createTime.String())
	// 				shouldUpdate = false
	// 				return
	// 			}
	// 		} else {
	// 			glog.Errorf("Failed to parse timestamps for comparison: StatusTime=%s, CreateTime=%s, err1=%v, err2=%v",
	// 				appState.Status.StatusTime, appStateMsg.CreateTime, err1, err2)
	// 		}
	// 	}
	// }

	// dw.cacheManager.CompareAppStateMsg(appStateMsg.User, appStateMsg.MarketSource, appStateMsg.Name, checker)
	// return shouldUpdate
}
