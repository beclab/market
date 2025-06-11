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

	"github.com/nats-io/nats.go"
)

// AppStateMessage represents the message structure from NATS
type AppStateMessage struct {
	AppId    string `json:"appId"`
	State    string `json:"state"`
	OpType   string `json:"opType"`
	OpTaskId string `json:"opTaskId"`
	EventId  string `json:"eventId"`
}

// DataWatcherState handles app state messages from NATS
type DataWatcherState struct {
	nc       *nats.Conn
	sub      *nats.Subscription
	ctx      context.Context
	cancel   context.CancelFunc
	isDev    bool
	natsHost string
	natsPort string
	natsUser string
	natsPass string
	subject  string
}

// NewDataWatcherState creates a new DataWatcherState instance
func NewDataWatcherState() *DataWatcherState {
	ctx, cancel := context.WithCancel(context.Background())

	dw := &DataWatcherState{
		ctx:      ctx,
		cancel:   cancel,
		isDev:    isDevEnvironment(),
		natsHost: getEnvOrDefault("NATS_HOST", "localhost"),
		natsPort: getEnvOrDefault("NATS_PORT", "4222"),
		natsUser: getEnvOrDefault("NATS_USERNAME", ""),
		natsPass: getEnvOrDefault("NATS_PASSWORD", ""),
		subject:  getEnvOrDefault("NATS_SUBJECT_SYSTEM_APP_STATE", "system.app.state"),
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
	return dw.startNatsConnection()
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
	states := []string{"running", "stopped", "starting", "stopping", "error"}
	opTypes := []string{"install", "uninstall", "update", "restart", "start", "stop"}

	msg := AppStateMessage{
		AppId:    appIds[rand.Intn(len(appIds))],
		State:    states[rand.Intn(len(states))],
		OpType:   opTypes[rand.Intn(len(opTypes))],
		OpTaskId: fmt.Sprintf("task_%d", rand.Intn(10000)),
		EventId:  fmt.Sprintf("event_%d_%d", time.Now().Unix(), rand.Intn(1000)),
	}

	jsonData, _ := json.Marshal(msg)
	log.Printf("Generated mock message: %s", string(jsonData))

	dw.printAppStateMessage(msg)
}

// printAppStateMessage prints the app state message details
func (dw *DataWatcherState) printAppStateMessage(msg AppStateMessage) {
	log.Printf("=== App State Message ===")
	log.Printf("App ID: %s", msg.AppId)
	log.Printf("State: %s", msg.State)
	log.Printf("Operation Type: %s", msg.OpType)
	log.Printf("Operation Task ID: %s", msg.OpTaskId)
	log.Printf("Event ID: %s", msg.EventId)
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
