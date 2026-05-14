package models

import "time"

// HistoryRecord mirrors the history_records table.
//
// IMPORTANT: this struct is currently a *schema mirror only*. The runtime
// CRUD for history_records still lives in internal/v2/history and uses
// sqlx + raw SQL. Until that code is migrated to gorm, any column changes
// must be applied in three places to stay in sync:
//
//  1. internal/v2/db/migrations/00005_init_history_records.sql (and any
//     subsequent migration that alters the table)
//  2. internal/v2/history/historymodule.go (raw SQL strings)
//  3. this file
//
// Field types intentionally match the legacy schema verbatim:
//   - Time is a Unix-second BIGINT (not a TIMESTAMPTZ column).
//   - Type is stored as VARCHAR(100); the typed history.HistoryType alias
//     lives in the history package and is not used here to avoid an import
//     cycle (history → db/models → history).
//   - Extended is a TEXT column holding a JSON string, not a jsonb column,
//     so it is modelled as plain string.
type HistoryRecord struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Type      string    `gorm:"column:type;size:100;not null;index:idx_history_type"`
	Message   string    `gorm:"column:message;type:text;not null"`
	Time      int64     `gorm:"column:time;not null;index:idx_history_time"`
	App       string    `gorm:"column:app;size:255;not null;index:idx_history_app"`
	Account   string    `gorm:"column:account;size:255;not null;default:'';index:idx_history_account"`
	Extended  string    `gorm:"column:extended;type:text;default:''"`
	CreatedAt time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP;index:idx_history_created_at"`
}

// TableName pins the table name; we use plural naming for all tables.
func (HistoryRecord) TableName() string { return "history_records" }
