package history

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"market/internal/v2/db"
	"market/internal/v2/helper"

	"github.com/golang/glog"
	"github.com/jmoiron/sqlx"
)

// HistoryType represents the type of history record
type HistoryType string

const (
	// Action types
	TypeActionInstall   HistoryType = "ACTION_INSTALL"
	TypeActionUninstall HistoryType = "ACTION_UNINSTALL"
	TypeActionCancel    HistoryType = "ACTION_CANCEL"
	TypeActionUpgrade   HistoryType = "ACTION_UPGRADE"

	// System types
	TypeSystemInstallSucceed HistoryType = "SYSTEM_INSTALL_SUCCEED"
	TypeSystemInstallFailed  HistoryType = "SYSTEM_INSTALL_FAILED"

	// Generic types for extensibility
	TypeAction HistoryType = "action"
	TypeSystem HistoryType = "system"
)

// HistoryRecord represents a history record in the database
type HistoryRecord struct {
	ID       int64       `json:"id" db:"id"`
	Type     HistoryType `json:"type" db:"type"`
	Message  string      `json:"message" db:"message"`
	Time     int64       `json:"time" db:"time"`
	App      string      `json:"app" db:"app"`
	Account  string      `json:"account" db:"account"`
	Extended string      `json:"extended" db:"extended"`
}

// QueryCondition represents conditions for querying history records
type QueryCondition struct {
	Type      HistoryType `json:"type,omitempty"`
	App       string      `json:"app,omitempty"`
	Account   string      `json:"account,omitempty"`
	StartTime int64       `json:"start_time,omitempty"`
	EndTime   int64       `json:"end_time,omitempty"`
	Limit     int         `json:"limit,omitempty"`
	Offset    int         `json:"offset,omitempty"`
}

// HistoryModule manages history records
type HistoryModule struct {
	db            *sqlx.DB
	cleanupTicker *time.Ticker
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewHistoryModule creates a new history module instance backed by the
// shared PostgreSQL pool managed by package db. It does NOT open a new
// connection: the underlying *sql.DB is borrowed from db.Global() and
// wrapped in sqlx so the existing CRUD code paths continue to work
// unchanged.
//
// Schema (table + indexes) is owned by internal/v2/db migrations (see
// migrations/00005_init_history_records.sql) and is applied during
// application startup before this constructor runs.
func NewHistoryModule() (*HistoryModule, error) {
	if helper.IsPublicEnvironment() {
		return nil, fmt.Errorf("in public environment, no need to load history module")
	}

	gormDB := db.Global()
	if gormDB == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before NewHistoryModule")
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("acquire underlying *sql.DB: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	module := &HistoryModule{
		db:     sqlx.NewDb(sqlDB, "postgres"),
		ctx:    ctx,
		cancel: cancel,
	}
	module.startCleanupRoutine()
	return module, nil
}

// StoreRecord stores a new history record
func (hm *HistoryModule) StoreRecord(record *HistoryRecord) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	// Validate that account field is not empty
	if record.Account == "" {
		return fmt.Errorf("account field cannot be empty")
	}

	// Set current timestamp if not provided
	if record.Time == 0 {
		record.Time = time.Now().Unix()
	}

	// Validate extended field is valid JSON
	if record.Extended != "" {
		var temp interface{}
		if err := json.Unmarshal([]byte(record.Extended), &temp); err != nil {
			glog.Warningf("Invalid JSON in extended field, storing as plain text: %v", err)
		}
	}

	query := `
		INSERT INTO history_records (type, message, time, app, account, extended)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`

	err := hm.db.QueryRow(query, record.Type, record.Message, record.Time, record.App, record.Account, record.Extended).Scan(&record.ID)
	if err != nil {
		glog.Errorf("Failed to store history record: %v", err)
		return err
	}

	glog.Infof("Stored history record with ID: %d, type: %s, app: %s, account: %s", record.ID, record.Type, record.App, record.Account)
	return nil
}

// QueryRecords queries history records based on conditions
func (hm *HistoryModule) QueryRecords(condition *QueryCondition) ([]*HistoryRecord, error) {
	if condition == nil {
		condition = &QueryCondition{}
	}

	// Set default limit if not specified
	if condition.Limit <= 0 {
		condition.Limit = 100
	}

	// Build query
	query := "SELECT id, type, message, time, app, account, extended FROM history_records WHERE 1=1"
	args := []interface{}{}
	argIndex := 1

	// Add conditions
	if condition.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", argIndex)
		args = append(args, condition.Type)
		argIndex++
	}

	if condition.App != "" {
		query += fmt.Sprintf(" AND app = $%d", argIndex)
		args = append(args, condition.App)
		argIndex++
	}

	if condition.Account != "" {
		query += fmt.Sprintf(" AND account = $%d", argIndex)
		args = append(args, condition.Account)
		argIndex++
	}

	if condition.StartTime > 0 {
		query += fmt.Sprintf(" AND time >= $%d", argIndex)
		args = append(args, condition.StartTime)
		argIndex++
	}

	if condition.EndTime > 0 {
		query += fmt.Sprintf(" AND time <= $%d", argIndex)
		args = append(args, condition.EndTime)
		argIndex++
	}

	// Add ordering and pagination
	query += " ORDER BY time DESC"

	if condition.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, condition.Limit)
		argIndex++
	}

	if condition.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, condition.Offset)
		argIndex++
	}

	glog.V(3).Infof("Executing query: %s with args: %v", query, args)

	rows, err := hm.db.Query(query, args...)
	if err != nil {
		glog.Errorf("Failed to query history records: %v", err)
		return nil, err
	}
	defer rows.Close()

	var records []*HistoryRecord
	for rows.Next() {
		record := &HistoryRecord{}
		err := rows.Scan(&record.ID, &record.Type, &record.Message, &record.Time, &record.App, &record.Account, &record.Extended)
		if err != nil {
			glog.Errorf("Failed to scan history record: %v", err)
			continue
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		glog.Errorf("Error during rows iteration: %v", err)
		return nil, err
	}

	glog.V(2).Infof("Retrieved %d history records, args: %v", len(records), args)
	return records, nil
}

// startCleanupRoutine starts a routine to cleanup old records
func (hm *HistoryModule) startCleanupRoutine() {
	// Get cleanup interval from environment, default to 24 hours
	intervalStr := os.Getenv("HISTORY_CLEANUP_INTERVAL_HOURS")
	interval, err := strconv.Atoi(intervalStr)
	if err != nil || interval <= 0 {
		interval = 24 // Default to 24 hours
	}

	hm.cleanupTicker = time.NewTicker(time.Duration(interval) * time.Hour)

	go func() {
		glog.Infof("Starting history cleanup routine with interval: %d hours", interval)

		// Run initial cleanup
		hm.cleanupOldRecords()

		for {
			select {
			case <-hm.cleanupTicker.C:
				hm.cleanupOldRecords()
			case <-hm.ctx.Done():
				glog.Infof("History cleanup routine stopped")
				return
			}
		}
	}()
}

// cleanupOldRecords removes records older than 1 month
func (hm *HistoryModule) cleanupOldRecords() {
	// Calculate timestamp for 1 month ago
	oneMonthAgo := time.Now().AddDate(0, -1, 0).Unix()

	query := "DELETE FROM history_records WHERE time < $1"

	result, err := hm.db.Exec(query, oneMonthAgo)
	if err != nil {
		glog.Errorf("Failed to cleanup old history records: %v", err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		glog.Warningf("Failed to get rows affected count: %v", err)
	} else {
		glog.Infof("Cleaned up %d old history records (older than 1 month)", rowsAffected)
	}
}

// GetRecordCount returns the total count of records matching the condition
func (hm *HistoryModule) GetRecordCount(condition *QueryCondition) (int64, error) {
	if condition == nil {
		condition = &QueryCondition{}
	}

	// Build count query
	query := "SELECT COUNT(*) FROM history_records WHERE 1=1"
	args := []interface{}{}
	argIndex := 1

	// Add conditions (same as QueryRecords but without ordering and pagination)
	if condition.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", argIndex)
		args = append(args, condition.Type)
		argIndex++
	}

	if condition.App != "" {
		query += fmt.Sprintf(" AND app = $%d", argIndex)
		args = append(args, condition.App)
		argIndex++
	}

	if condition.Account != "" {
		query += fmt.Sprintf(" AND account = $%d", argIndex)
		args = append(args, condition.Account)
		argIndex++
	}

	if condition.StartTime > 0 {
		query += fmt.Sprintf(" AND time >= $%d", argIndex)
		args = append(args, condition.StartTime)
		argIndex++
	}

	if condition.EndTime > 0 {
		query += fmt.Sprintf(" AND time <= $%d", argIndex)
		args = append(args, condition.EndTime)
		argIndex++
	}

	var count int64
	err := hm.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		glog.Errorf("Failed to get record count: %v", err)
		return 0, err
	}

	return count, nil
}

// Close stops the cleanup goroutine and tears down the module's local state.
//
// It deliberately does NOT close hm.db: the underlying *sql.DB is owned by
// package db and closed once during graceful shutdown in main.go. Closing
// it here would tear down the pool shared with task and any other consumer.
func (hm *HistoryModule) Close() error {
	glog.Infof("Closing history module")
	if hm.cleanupTicker != nil {
		hm.cleanupTicker.Stop()
	}
	if hm.cancel != nil {
		hm.cancel()
	}
	return nil
}

// HealthCheck checks if the module is healthy
func (hm *HistoryModule) HealthCheck() error {
	if hm.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Test database connection
	if err := hm.db.Ping(); err != nil {
		return fmt.Errorf("database ping failed: %v", err)
	}

	return nil
}
