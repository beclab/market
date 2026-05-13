package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"market/internal/v2/db"
	"market/internal/v2/helper"

	"github.com/jmoiron/sqlx"
)

// TaskStore manages persistence for task records.
type TaskStore struct {
	db *sqlx.DB
}

// NewTaskStore returns a TaskStore that runs against the shared PostgreSQL
// pool managed by package db. It does NOT open a new connection: the
// underlying *sql.DB is borrowed from db.GlobalSqlxDB() so the existing CRUD
// code paths continue to work unchanged.
//
// Schema (table + indexes) is owned by internal/v2/db migrations (see
// migrations/00006_init_task_records.sql) and is applied during application
// startup before this constructor runs.
func NewTaskStore() (*TaskStore, error) {
	if helper.IsPublicEnvironment() {
		return nil, fmt.Errorf("task store is disabled in public environment")
	}
	xdb := db.GlobalSqlxDB()
	if xdb == nil {
		return nil, fmt.Errorf("postgres not initialised; db.Open must run before NewTaskStore")
	}
	return &TaskStore{db: xdb}, nil
}

// UpsertTask stores or updates a task record in the database
func (ts *TaskStore) UpsertTask(task *Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}

	metadataJSON := "{}"
	if task.Metadata != nil {
		data, err := json.Marshal(task.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal task metadata: %w", err)
		}
		metadataJSON = string(data)
	}

	var startedAt interface{}
	if task.StartedAt != nil {
		startedAt = *task.StartedAt
	}

	var completedAt interface{}
	if task.CompletedAt != nil {
		completedAt = *task.CompletedAt
	}

	query := `
	INSERT INTO task_records (
		task_id, type, status, app_name, user_account, op_id, metadata, result, error_msg,
		created_at, started_at, completed_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9,
		$10, $11, $12, NOW()
	)
	ON CONFLICT (task_id) DO UPDATE SET
		type = EXCLUDED.type,
		status = EXCLUDED.status,
		app_name = EXCLUDED.app_name,
		user_account = EXCLUDED.user_account,
		op_id = EXCLUDED.op_id,
		metadata = EXCLUDED.metadata,
		result = EXCLUDED.result,
		error_msg = EXCLUDED.error_msg,
		created_at = EXCLUDED.created_at,
		started_at = EXCLUDED.started_at,
		completed_at = EXCLUDED.completed_at,
		updated_at = NOW();
	`

	if _, err := ts.db.Exec(query,
		task.ID,
		int(task.Type),
		int(task.Status),
		task.AppName,
		task.User,
		task.OpID,
		metadataJSON,
		task.Result,
		task.ErrorMsg,
		task.CreatedAt,
		startedAt,
		completedAt,
	); err != nil {
		return fmt.Errorf("failed to upsert task record: %w", err)
	}

	return nil
}

// LoadRecentTasks loads recent tasks (including completed and failed) from the database
// limit specifies the maximum number of tasks to return (default: 100)
func (ts *TaskStore) LoadRecentTasks(limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
	SELECT task_id, type, status, app_name, user_account, op_id, metadata, result, error_msg,
		created_at, started_at, completed_at
	FROM task_records
	ORDER BY COALESCE(completed_at, started_at, created_at) DESC, created_at DESC
	LIMIT $1
	`

	rows, err := ts.db.Queryx(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to load recent tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var (
			taskID      string
			taskType    int
			status      int
			appName     string
			user        string
			opID        string
			metadata    string
			result      string
			errorMsg    string
			createdAt   time.Time
			startedAt   sql.NullTime
			completedAt sql.NullTime
		)

		if err := rows.Scan(
			&taskID,
			&taskType,
			&status,
			&appName,
			&user,
			&opID,
			&metadata,
			&result,
			&errorMsg,
			&createdAt,
			&startedAt,
			&completedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan task record: %w", err)
		}

		metadataMap := make(map[string]interface{})
		if metadata != "" {
			if err := json.Unmarshal([]byte(metadata), &metadataMap); err != nil {
				return nil, fmt.Errorf("failed to unmarshal task metadata for %s: %w", taskID, err)
			}
		}

		var startedPtr *time.Time
		if startedAt.Valid {
			value := startedAt.Time
			startedPtr = &value
		}

		var completedPtr *time.Time
		if completedAt.Valid {
			value := completedAt.Time
			completedPtr = &value
		}

		task := &Task{
			ID:          taskID,
			Type:        TaskType(taskType),
			Status:      TaskStatus(status),
			AppName:     appName,
			User:        user,
			OpID:        opID,
			CreatedAt:   createdAt,
			StartedAt:   startedPtr,
			CompletedAt: completedPtr,
			Result:      result,
			ErrorMsg:    errorMsg,
			Metadata:    metadataMap,
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during task record iteration: %w", err)
	}

	return tasks, nil
}

// LoadActiveTasks restores pending and running tasks from the database
func (ts *TaskStore) LoadActiveTasks() ([]*Task, error) {
	query := `
	SELECT task_id, type, status, app_name, user_account, op_id, metadata, result, error_msg,
		created_at, started_at, completed_at
	FROM task_records
	WHERE status IN ($1, $2)
	ORDER BY created_at ASC
	`

	rows, err := ts.db.Queryx(query, int(Pending), int(Running))
	if err != nil {
		return nil, fmt.Errorf("failed to load active tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var (
			taskID      string
			taskType    int
			status      int
			appName     string
			user        string
			opID        string
			metadata    string
			result      string
			errorMsg    string
			createdAt   time.Time
			startedAt   sql.NullTime
			completedAt sql.NullTime
		)

		if err := rows.Scan(
			&taskID,
			&taskType,
			&status,
			&appName,
			&user,
			&opID,
			&metadata,
			&result,
			&errorMsg,
			&createdAt,
			&startedAt,
			&completedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan task record: %w", err)
		}

		metadataMap := make(map[string]interface{})
		if metadata != "" {
			if err := json.Unmarshal([]byte(metadata), &metadataMap); err != nil {
				return nil, fmt.Errorf("failed to unmarshal task metadata for %s: %w", taskID, err)
			}
		}

		var startedPtr *time.Time
		if startedAt.Valid {
			value := startedAt.Time
			startedPtr = &value
		}

		var completedPtr *time.Time
		if completedAt.Valid {
			value := completedAt.Time
			completedPtr = &value
		}

		task := &Task{
			ID:          taskID,
			Type:        TaskType(taskType),
			Status:      TaskStatus(status),
			AppName:     appName,
			User:        user,
			OpID:        opID,
			CreatedAt:   createdAt,
			StartedAt:   startedPtr,
			CompletedAt: completedPtr,
			Result:      result,
			ErrorMsg:    errorMsg,
			Metadata:    metadataMap,
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during task record iteration: %w", err)
	}

	return tasks, nil
}

// TrimCompletedTasks keeps at most the latest limit completed tasks
func (ts *TaskStore) TrimCompletedTasks(limit int) error {
	if limit <= 0 {
		return nil
	}

	query := `
	WITH ordered AS (
		SELECT task_id
		FROM task_records
		WHERE status IN ($1, $2, $3)
		ORDER BY completed_at DESC NULLS LAST, id DESC
		OFFSET $4
	)
	DELETE FROM task_records
	WHERE task_id IN (SELECT task_id FROM ordered);
	`

	if _, err := ts.db.Exec(query, int(Completed), int(Failed), int(Canceled), limit); err != nil {
		return fmt.Errorf("failed to trim completed tasks: %w", err)
	}

	return nil
}

// GetLatestCompletedTaskByAppNameAndUser gets the latest completed task for a specific app and user
// Returns version and source from task metadata if found
func (ts *TaskStore) GetLatestCompletedTaskByAppNameAndUser(appName, user string) (version, source string, found bool, err error) {
	if ts == nil || ts.db == nil {
		return "", "", false, fmt.Errorf("task store is not initialized")
	}

	query := `
	SELECT metadata
	FROM task_records
	WHERE app_name = $1 
		AND user_account = $2 
		AND status = $3
		AND type IN ($4, $5, $6)
	ORDER BY completed_at DESC NULLS LAST, created_at DESC
	LIMIT 1
	`

	var metadataStr string
	err = ts.db.QueryRow(query, appName, user, int(Completed), int(InstallApp), int(UpgradeApp), int(CloneApp)).Scan(&metadataStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("failed to query task: %w", err)
	}

	// Parse metadata JSON
	var metadataMap map[string]interface{}
	if metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadataMap); err != nil {
			return "", "", false, fmt.Errorf("failed to unmarshal task metadata: %w", err)
		}
	}

	// Extract version and source from metadata
	if v, ok := metadataMap["version"].(string); ok && v != "" {
		version = v
	}
	if s, ok := metadataMap["source"].(string); ok && s != "" {
		source = s
	}

	// Return found=true only if we have at least version or source
	if version != "" || source != "" {
		return version, source, true, nil
	}

	return "", "", false, nil
}

// Close is a no-op: the underlying *sql.DB is owned by package db and
// closed once during graceful shutdown in main.go. Closing it here would
// tear down the pool shared with history and any other consumer.
func (ts *TaskStore) Close() error { return nil }
