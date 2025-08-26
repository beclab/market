package appinfo

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"market/internal/v2/types"
	"market/internal/v2/utils"

	"github.com/nats-io/nats.go"
)

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

	if utils.IsPublicEnvironment() {
		log.Println("Public environment detected, NATS data sender disabled")
		return &DataSender{enabled: false}, nil
	}

	config := loadConfig()

	// Build NATS connection URL
	natsURL := fmt.Sprintf("nats://%s:%s@%s:%s",
		config.Username, config.Password, config.Host, config.Port)

	// Connect to NATS
	conn, err := nats.Connect(natsURL,
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1), // -1 means unlimited reconnection attempts
		nats.ReconnectJitter(100*time.Millisecond, 1*time.Second),
		nats.Timeout(10*time.Second),
		nats.PingInterval(30*time.Second),
		nats.MaxPingsOutstanding(5),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("NATS disconnected: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("NATS reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Printf("NATS connection closed")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			log.Printf("NATS error: %v", err)
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
		Host:     utils.GetEnvOrDefault("NATS_HOST", "localhost"),
		Port:     utils.GetEnvOrDefault("NATS_PORT", "4222"),
		Username: utils.GetEnvOrDefault("NATS_USERNAME", ""),
		Password: utils.GetEnvOrDefault("NATS_PASSWORD", ""),
		Subject:  utils.GetEnvOrDefault("NATS_SUBJECT_SYSTEM_MARKET_STATE", "os.market"),
	}
}

// SendAppInfoUpdate sends app info update to NATS
func (ds *DataSender) SendAppInfoUpdate(update types.AppInfoUpdate) error {
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

	subject := fmt.Sprintf("%s.%s", ds.subject, update.User)

	// Log before sending
	log.Printf("Sending app info update to NATS subject '%s': %s", subject, string(data))

	// Send message to NATS
	err = ds.conn.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish message to NATS: %w", err)
	}

	log.Printf("Successfully sent app info update to NATS")
	return nil
}

// SendMarketSystemUpdate sends market system update to NATS
func (ds *DataSender) SendMarketSystemUpdate(update types.MarketSystemUpdate) error {
	if !ds.enabled {
		log.Println("NATS data sender is disabled, skipping market system update message send")
		return nil
	}

	if ds.conn == nil {
		return fmt.Errorf("NATS connection is not initialized")
	}

	// Convert data to JSON
	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal market system update: %w", err)
	}

	// Use system subject for market system updates
	subject := fmt.Sprintf("%s.%s", ds.subject, update.User)

	// Log before sending
	log.Printf("Sending market system update to NATS subject '%s': %s", subject, string(data))

	// Send message to NATS
	err = ds.conn.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish market system update message to NATS: %w", err)
	}

	log.Printf("Successfully sent market system update to NATS")
	return nil
}

// SendImageInfoUpdate sends image info update to NATS
func (ds *DataSender) SendImageInfoUpdate(update types.ImageInfoUpdate) error {
	if !ds.enabled {
		log.Println("NATS data sender is disabled, skipping image info update message send")
		return nil
	}

	if ds.conn == nil {
		return fmt.Errorf("NATS connection is not initialized")
	}

	// Convert data to JSON
	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal image info update: %w", err)
	}

	// Use system subject for image info updates
	subject := fmt.Sprintf("%s.%s", ds.subject, update.User)

	// Log before sending
	log.Printf("Sending image info update to NATS subject '%s': %s", subject, string(data))

	// Send message to NATS
	err = ds.conn.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish image info update message to NATS: %w", err)
	}

	log.Printf("Successfully sent image info update to NATS")
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
