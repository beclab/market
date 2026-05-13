// Package helper holds dependency-free building blocks shared across the v2
// codebase. Anything that lives here MUST NOT import another internal/v2/*
// package, so it stays at the bottom of the dependency graph and can safely
// be pulled in by every other package (db, task, history, utils, appinfo,
// settings, ...).
package helper

import (
	"os"
	"strconv"
	"time"
)

// GetEnvOrDefault returns the environment variable value for key, or
// defaultValue when the variable is unset or empty.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvIntOrDefault parses the environment variable as int and returns
// defaultValue when the variable is unset or fails to parse.
func GetEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return defaultValue
}

// GetEnvDurationOrDefault parses the environment variable as a time.Duration
// (using time.ParseDuration) and returns defaultValue when the variable is
// unset or fails to parse.
func GetEnvDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
