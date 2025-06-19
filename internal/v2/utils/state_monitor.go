package utils

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"market/internal/v2/types"

	"github.com/nats-io/nats.go"
)

// AppInfoUpdate represents the data structure for app info updates
type AppInfoUpdate struct {
	AppStateLatest *types.AppStateLatestData `json:"app_state_latest"`
	AppInfoLatest  *types.AppInfoLatestData  `json:"app_info_latest"`
	Timestamp      int64                     `json:"timestamp"`
	User           string                    `json:"user"`
}

// DataSenderInterface defines the interface for sending app info updates
type DataSenderInterface interface {
	SendAppInfoUpdate(update AppInfoUpdate) error
	IsConnected() bool
	Close()
}

// StateMonitor handles app state monitoring and change detection
type StateMonitor struct {
	dataSender DataSenderInterface
}

// NewStateMonitor creates a new StateMonitor instance
func NewStateMonitor() (*StateMonitor, error) {
	dataSender, err := NewDataSender()
	if err != nil {
		return nil, err
	}

	return &StateMonitor{
		dataSender: dataSender,
	}, nil
}

// CheckAndNotifyStateChange checks if app state has changed and sends notification if needed
func (sm *StateMonitor) CheckAndNotifyStateChange(
	userID, sourceID, appName string,
	newStateData *types.AppStateLatestData,
	existingStateData []*types.AppStateLatestData,
	appInfoLatestData []*types.AppInfoLatestData,
) error {
	// Check if state has changed
	hasChanged, changeReason := sm.hasStateChanged(appName, newStateData, existingStateData)

	if !hasChanged {
		log.Printf("No state change detected for app %s (user=%s, source=%s), reason: %s",
			appName, userID, sourceID, changeReason)
		return nil
	}

	log.Printf("State change detected for app %s (user=%s, source=%s), reason: %s",
		appName, userID, sourceID, changeReason)

	// Find corresponding AppInfoLatestData
	var appInfoLatest *types.AppInfoLatestData
	for _, appInfo := range appInfoLatestData {
		if appInfo != nil && appInfo.RawData != nil && appInfo.RawData.Name == appName {
			appInfoLatest = appInfo
			break
		}
	}

	// Create and send update
	update := AppInfoUpdate{
		AppStateLatest: newStateData,
		AppInfoLatest:  appInfoLatest,
		Timestamp:      time.Now().Unix(),
		User:           userID,
	}

	return sm.dataSender.SendAppInfoUpdate(update)
}

// hasStateChanged checks if the app state has changed compared to existing state
func (sm *StateMonitor) hasStateChanged(
	appName string,
	newStateData *types.AppStateLatestData,
	existingStateData []*types.AppStateLatestData,
) (bool, string) {
	if newStateData == nil {
		return false, "new state data is nil"
	}

	// Find existing state for this app
	// Since AppStateLatestData doesn't have a direct app name field,
	// we'll need to rely on the context or metadata
	// For now, we'll check if there are any existing states
	// This is a simplified approach - in a real implementation,
	// you might want to store app identification in metadata or use a different approach

	var existingState *types.AppStateLatestData
	if len(existingStateData) > 0 {
		// For now, we'll compare with the first existing state
		// This assumes that the state data is for the same app
		// In a more sophisticated implementation, you might want to:
		// 1. Store app name in metadata
		// 2. Use a different data structure that includes app identification
		// 3. Use external context to identify which app the state belongs to
		existingState = existingStateData[0]
	}

	// If no existing state found, this is a new app state
	if existingState == nil {
		return true, "new app state (no existing state found)"
	}

	// Compare main state
	if newStateData.Status.State != existingState.Status.State {
		return true, "main state changed: " + existingState.Status.State + " -> " + newStateData.Status.State
	}

	// Compare entrance statuses
	if !sm.compareEntranceStatuses(newStateData.Status.EntranceStatuses, existingState.Status.EntranceStatuses) {
		return true, "entrance statuses changed"
	}

	return false, "no changes detected"
}

// compareEntranceStatuses compares two arrays of entrance statuses
func (sm *StateMonitor) compareEntranceStatuses(
	newStatuses []struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
	},
	existingStatuses []struct {
		Name       string `json:"name"`
		State      string `json:"state"`
		StatusTime string `json:"statusTime"`
		Reason     string `json:"reason"`
	},
) bool {
	// If lengths are different, statuses have changed
	if len(newStatuses) != len(existingStatuses) {
		return false
	}

	// Create maps for easier comparison
	newStatusMap := make(map[string]string)
	existingStatusMap := make(map[string]string)

	for _, status := range newStatuses {
		newStatusMap[status.Name] = status.State
	}

	for _, status := range existingStatuses {
		existingStatusMap[status.Name] = status.State
	}

	// Compare each entrance status
	for name, newState := range newStatusMap {
		if existingState, exists := existingStatusMap[name]; !exists || existingState != newState {
			return false
		}
	}

	// Check for any entrances that exist in old but not in new
	for name := range existingStatusMap {
		if _, exists := newStatusMap[name]; !exists {
			return false
		}
	}

	return true
}

// Close closes the state monitor and its data sender
func (sm *StateMonitor) Close() {
	if sm.dataSender != nil {
		sm.dataSender.Close()
	}
}

// IsConnected checks if the data sender is connected
func (sm *StateMonitor) IsConnected() bool {
	if sm.dataSender == nil {
		return false
	}
	return sm.dataSender.IsConnected()
}

// DataSender handles NATS communication for app info updates
type DataSender struct {
	conn    *nats.Conn
	subject string
	enabled bool
}

// Config holds NATS configuration
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	Subject  string
}

// NewDataSender creates a new DataSender instance
func NewDataSender() (*DataSender, error) {
	// Check if we're in development environment
	env := os.Getenv("GO_ENV")
	if env == "development" || env == "dev" {
		log.Println("Development environment detected, NATS data sender disabled")
		return &DataSender{enabled: false}, nil
	}

	config := loadConfig()

	// Build NATS connection URL
	natsURL := fmt.Sprintf("nats://%s:%s@%s:%s",
		config.Username, config.Password, config.Host, config.Port)

	// Connect to NATS
	conn, err := nats.Connect(natsURL,
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(10),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS reconnected to %s", nc.ConnectedUrl())
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	log.Printf("Connected to NATS server at %s:%s", config.Host, config.Port)

	return &DataSender{
		conn:    conn,
		subject: config.Subject,
		enabled: true,
	}, nil
}

// loadConfig loads NATS configuration from environment variables
func loadConfig() Config {
	return Config{
		Host:     getEnvOrDefault("NATS_HOST", "localhost"),
		Port:     getEnvOrDefault("NATS_PORT", "4222"),
		Username: getEnvOrDefault("NATS_USERNAME", ""),
		Password: getEnvOrDefault("NATS_PASSWORD", ""),
		Subject:  getEnvOrDefault("NATS_SUBJECT_SYSTEM_MARKET_STATE", "os.market"),
	}
}

// SendAppInfoUpdate sends app info update to NATS
func (ds *DataSender) SendAppInfoUpdate(update AppInfoUpdate) error {
	if !ds.enabled {
		log.Println("NATS data sender is disabled, skipping message send")
		return nil
	}

	if ds.conn == nil {
		return fmt.Errorf("NATS connection is not initialized")
	}

	// Convert data to JSON
	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal app info update: %w", err)
	}

	// Log before sending
	log.Printf("Sending app info update to NATS subject '%s': %s", ds.subject, string(data))

	// Send message to NATS
	err = ds.conn.Publish(ds.subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish message to NATS: %w", err)
	}

	log.Printf("Successfully sent app info update to NATS")
	return nil
}

// Close closes the NATS connection
func (ds *DataSender) Close() {
	if ds.conn != nil && ds.enabled {
		ds.conn.Close()
		log.Println("NATS connection closed")
	}
}

// IsConnected checks if NATS connection is active
func (ds *DataSender) IsConnected() bool {
	if !ds.enabled || ds.conn == nil {
		return false
	}
	return ds.conn.IsConnected()
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
