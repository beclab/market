package appinfo

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

// AppInfoUpdate represents the data structure for app info updates
type AppInfoUpdate struct {
	// Fields to be added as needed
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
		Subject:  getEnvOrDefault("NATS_SUBJECT_SYSTEM_MARKET_STATE", "system.market.state"),
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
