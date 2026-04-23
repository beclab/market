package utils

import (
	"os"
	"strconv"
	"time"
)

// GetEnvOrDefault gets environment variable or returns default value.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvIntOrDefault parses an environment variable as an int (using
// strconv.Atoi). It returns the default when the variable is unset or fails
// to parse.
func GetEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
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
