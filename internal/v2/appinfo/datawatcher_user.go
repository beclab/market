package appinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

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
	conn       *nats.Conn
	subject    string
	isDev      bool
	cancelFunc context.CancelFunc
}

// NewDataWatcherUser creates a new DataWatcherUser instance
func NewDataWatcherUser() *DataWatcherUser {
	isDev := os.Getenv("ENVIRONMENT") == "development" || os.Getenv("DEV_MODE") == "true"

	return &DataWatcherUser{
		subject: os.Getenv("NATS_SUBJECT_SYSTEM_USER_STATE"),
		isDev:   isDev,
	}
}

// Start initializes and starts the data watcher
func (dw *DataWatcherUser) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	dw.cancelFunc = cancel

	if dw.isDev {
		log.Printf("Running in development mode, NATS connection disabled")
		go dw.simulateMessages(ctx)
		return nil
	}

	if err := dw.connectToNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	if err := dw.subscribeToMessages(); err != nil {
		return fmt.Errorf("failed to subscribe to messages: %w", err)
	}

	log.Printf("DataWatcherUser started successfully, listening on subject: %s", dw.subject)

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

	if dw.conn != nil {
		dw.conn.Close()
		log.Printf("NATS connection closed")
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
			log.Printf("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS reconnected to %v", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Printf("NATS connection closed")
		}),
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

	log.Printf("Connected to NATS server at %s", natsURL)
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

	log.Printf("Subscribed to NATS subject: %s", dw.subject)
	return nil
}

// processMessage processes incoming NATS messages
func (dw *DataWatcherUser) processMessage(data []byte) {
	var message UserStateMessage

	if err := json.Unmarshal(data, &message); err != nil {
		log.Printf("Error unmarshaling message: %v, raw data: %s", err, string(data))
		return
	}

	// Print the received message
	log.Printf("Received app state message - EventType: %s, Username: %s, Timestamp: %s",
		message.EventType, message.Username, message.Timestamp)
}

// simulateMessages generates random messages every 30 seconds in development mode
func (dw *DataWatcherUser) simulateMessages(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	eventTypes := []string{"app_install", "app_uninstall", "app_start", "app_stop", "app_update"}
	usernames := []string{"admin", "user1", "user2", "testuser", "developer"}

	// Generate initial message immediately
	dw.generateRandomMessage(eventTypes, usernames)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stopping message simulation")
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
		log.Printf("Error marshaling simulated message: %v", err)
		return
	}

	log.Printf("Simulated message generated")
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
