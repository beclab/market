package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"market/internal/v2/history"
	"market/internal/v2/utils"

	"github.com/golang/glog"
	"github.com/nats-io/nats.go"
)

// UserStateMessage represents the message structure for user app state updates
type UserStateMessage struct {
	EventType string `json:"event_type"`
	Username  string `json:"username"`
	Timestamp string `json:"timestamp"`
}

// DataWatcherUser manages NATS connection and message processing for user app state updates
type DataWatcherUser struct {
	conn          *nats.Conn
	subject       string
	isDev         bool
	cancelFunc    context.CancelFunc
	historyModule *history.HistoryModule
}

// NewDataWatcherUser creates a new DataWatcherUser instance
func NewDataWatcherUser() *DataWatcherUser {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	isDev := env == "dev" || env == "development" || env == ""

	return &DataWatcherUser{
		subject: os.Getenv("NATS_SUBJECT_SYSTEM_USER_STATE"),
		isDev:   isDev,
	}
}

// SetHistoryModule sets the history module reference
func (dw *DataWatcherUser) SetHistoryModule(historyModule *history.HistoryModule) {
	dw.historyModule = historyModule
	glog.V(3).Infof("History module reference set in DataWatcherUser")
}

// Start initializes and starts the data watcher
func (dw *DataWatcherUser) Start(ctx context.Context) error {
	if utils.IsPublicEnvironment() {
		glog.V(3).Infof("Public environment detected, DataWatcherUser disabled")
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	dw.cancelFunc = cancel

	// Check if history module is available
	if dw.historyModule == nil {
		glog.V(3).Infof("Warning: History module not available in DataWatcherUser, some functionality may be limited")
	}

	if dw.isDev {
		glog.V(3).Infof("Running in development mode, NATS connection disabled")
		go dw.simulateMessages(ctx)
		return nil
	}

	if err := dw.connectToNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	if err := dw.subscribeToMessages(); err != nil {
		return fmt.Errorf("failed to subscribe to messages: %w", err)
	}

	glog.V(2).Infof("DataWatcherUser started successfully, listening on subject: %s", dw.subject)

	// Keep the connection alive until context is cancelled
	go func() {
		<-ctx.Done()
		dw.Stop()
	}()

	return nil
}

// Stop gracefully stops the data watcher
func (dw *DataWatcherUser) Stop() {
	if dw.cancelFunc != nil {
		dw.cancelFunc()
	}

	if dw.historyModule != nil {
		if err := dw.historyModule.Close(); err != nil {
			glog.Errorf("Error closing history module: %v", err)
		}
	}

	if dw.conn != nil {
		dw.conn.Close()
		glog.V(3).Infof("NATS connection closed")
	}
}

// connectToNATS establishes connection to NATS server using environment variables
func (dw *DataWatcherUser) connectToNATS() error {
	natsHost := os.Getenv("NATS_HOST")
	natsPort := os.Getenv("NATS_PORT")
	natsUsername := os.Getenv("NATS_USERNAME")
	natsPassword := os.Getenv("NATS_PASSWORD")

	if natsHost == "" {
		natsHost = "localhost"
	}
	if natsPort == "" {
		natsPort = "4222"
	}

	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort)

	// Prepare connection options
	opts := []nats.Option{
		nats.Name("DataWatcherUser"),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			glog.Errorf("[NATS] Disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			glog.V(3).Infof("[NATS] Reconnected to %v", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			glog.V(3).Infof("[NATS] Connection closed")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			glog.V(3).Infof("[NATS] Error: %v", err)
		}),
		nats.MaxReconnects(60),
		nats.ReconnectWait(2 * time.Second),
	}

	// Add authentication if provided
	if natsUsername != "" && natsPassword != "" {
		opts = append(opts, nats.UserInfo(natsUsername, natsPassword))
	}

	var err error
	dw.conn, err = nats.Connect(natsURL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS server at %s: %w", natsURL, err)
	}

	glog.V(2).Infof("Connected to NATS server at %s", natsURL)
	return nil
}

// subscribeToMessages subscribes to the NATS subject and processes incoming messages
func (dw *DataWatcherUser) subscribeToMessages() error {
	if dw.subject == "" {
		return fmt.Errorf("NATS_SUBJECT_SYSTEM_USER_STATE environment variable is not set")
	}

	_, err := dw.conn.Subscribe(dw.subject, func(msg *nats.Msg) {
		dw.processMessage(msg.Data)
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to subject %s: %w", dw.subject, err)
	}

	glog.V(2).Infof("Subscribed to NATS subject: %s", dw.subject)
	return nil
}

// processMessage processes incoming NATS messages
func (dw *DataWatcherUser) processMessage(data []byte) {
	glog.V(2).Infof("User - Received message from NATS subject %s: %s", string(data))
	var message UserStateMessage

	if err := json.Unmarshal(data, &message); err != nil {
		glog.Errorf("Error unmarshaling message: %v, raw data: %s", err, string(data))
		return
	}

	// Print the received message
	glog.V(3).Infof("Received app state message - EventType: %s, Username: %s, Timestamp: %s",
		message.EventType, message.Username, message.Timestamp)

	// Write to history
	if dw.historyModule != nil {
		historyRecord := &history.HistoryRecord{
			Type:     history.TypeSystem,
			Message:  "",
			Time:     time.Now().Unix(),
			App:      "",
			Account:  message.Username,
			Extended: string(data),
		}

		if err := dw.historyModule.StoreRecord(historyRecord); err != nil {
			glog.Errorf("Failed to store history record: %v", err)
		} else {
			glog.V(2).Infof("Successfully stored user state message to history for user: %s", message.Username)
		}
	}
}

// simulateMessages generates random messages every 30 seconds in development mode
func (dw *DataWatcherUser) simulateMessages(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventTypes := []string{"app_install", "app_uninstall", "app_start", "app_stop", "app_update"}
	usernames := []string{"admin", "user1"}

	// Generate initial message immediately
	dw.generateRandomMessage(eventTypes, usernames)

	for {
		select {
		case <-ctx.Done():
			glog.V(4).Info("Stopping message simulation")
			return
		case <-ticker.C:
			dw.generateRandomMessage(eventTypes, usernames)
		}
	}
}

// generateRandomMessage creates and processes a random message for development
func (dw *DataWatcherUser) generateRandomMessage(eventTypes, usernames []string) {
	message := UserStateMessage{
		EventType: eventTypes[rand.Intn(len(eventTypes))],
		Username:  usernames[rand.Intn(len(usernames))],
		Timestamp: time.Now().Format(time.RFC3339),
	}

	messageData, err := json.Marshal(message)
	if err != nil {
		glog.Errorf("Error marshaling simulated message: %v", err)
		return
	}

	glog.V(2).Info("Simulated message generated")
	dw.processMessage(messageData)
}

// IsHealthy checks if the data watcher is healthy
func (dw *DataWatcherUser) IsHealthy() bool {
	if dw.isDev {
		return true // Always healthy in dev mode
	}

	if dw.conn == nil {
		return false
	}

	return dw.conn.IsConnected()
}

// GetStatus returns the current status of the data watcher
func (dw *DataWatcherUser) GetStatus() map[string]interface{} {
	status := map[string]interface{}{
		"is_dev":  dw.isDev,
		"subject": dw.subject,
		"healthy": dw.IsHealthy(),
	}

	if !dw.isDev && dw.conn != nil {
		status["connected"] = dw.conn.IsConnected()
		status["server_url"] = dw.conn.ConnectedUrl()
		status["server_id"] = dw.conn.ConnectedServerId()
	}

	return status
}
