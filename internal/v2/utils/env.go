package utils

import (
	"os"
	"time"
)

// GetEnvOrDefault gets environment variable or returns default value.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvDurationOrDefault parses an environment variable as a time.Duration
// (using time.ParseDuration). It returns the default when the variable is
// unset or fails to parse.
func GetEnvDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
