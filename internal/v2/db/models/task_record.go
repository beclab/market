package models

import "time"

// TaskRecord mirrors the task_records table.
//
// IMPORTANT: this struct is currently a *schema mirror only*. The runtime
// CRUD for task_records still lives in internal/v2/task and uses sqlx + raw
// SQL. Until that code is migrated to gorm, any column changes must be
// applied in three places to stay in sync:
//
//  1. internal/v2/db/migrations/00006_init_task_records.sql (and any
//     subsequent migration that alters the table)
//  2. internal/v2/task/taskstore.go (raw SQL strings)
//  3. this file
//
// Field types intentionally match the legacy schema verbatim:
//   - Type and Status are stored as INTEGER; the typed task.TaskType /
//     task.TaskStatus aliases live in the task package and are not used
//     here to avoid an import cycle (task → db/models → task).
//   - Metadata / Result / ErrorMsg are TEXT columns holding plain or JSON
//     strings, not jsonb columns, so they are modelled as plain string.
//   - StartedAt / CompletedAt are nullable in the database and are modelled
//     as *time.Time.
type TaskRecord struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	TaskID      string     `gorm:"column:task_id;size:128;not null;uniqueIndex"`
	Type        int        `gorm:"column:type;not null"`
	Status      int        `gorm:"column:status;not null;index:idx_task_records_status"`
	AppName     string     `gorm:"column:app_name;size:255;not null"`
	UserAccount string     `gorm:"column:user_account;size:255;not null;default:''"`
	OpID        string     `gorm:"column:op_id;size:255;not null;default:''"`
	Metadata    string     `gorm:"column:metadata;type:text;not null;default:'{}'"`
	Result      string     `gorm:"column:result;type:text;not null;default:''"`
	ErrorMsg    string     `gorm:"column:error_msg;type:text;not null;default:''"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null;index:idx_task_records_created_at"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	CompletedAt *time.Time `gorm:"column:completed_at;index:idx_task_records_completed_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP"`
}

// TableName pins the table name; we use plural naming for all tables.
func (TaskRecord) TableName() string { return "task_records" }
