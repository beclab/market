package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"market/internal/v2/utils"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

// TaskStore manages persistence for task records
type TaskStore struct {
	db *sqlx.DB
}

// NewTaskStore initializes a new TaskStore instance with PostgreSQL backend
func NewTaskStore() (*TaskStore, error) {
	if utils.IsPublicEnvironment() {
		return nil, fmt.Errorf("task store is disabled in public environment")
	}

	dbHost := os.Getenv("POSTGRES_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}

	dbPort := os.Getenv("POSTGRES_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}

	dbName := os.Getenv("POSTGRES_DB")
	if dbName == "" {
		dbName = "history"
	}

	dbUser := os.Getenv("POSTGRES_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}

	dbPassword := os.Getenv("POSTGRES_PASSWORD")
	if dbPassword == "" {
		dbPassword = "password"
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sqlx.Connect("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	store := &TaskStore{db: db}
	if err := store.initSchema(); err != nil {
		return nil, err
	}

	return store, nil
}

// initSchema ensures the task_records table exists with required indexes
func (ts *TaskStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS task_records (
		id BIGSERIAL PRIMARY KEY,
		task_id VARCHAR(128) UNIQUE NOT NULL,
		type INTEGER NOT NULL,
		status INTEGER NOT NULL,
		app_name VARCHAR(255) NOT NULL,
		user_account VARCHAR(255) NOT NULL DEFAULT '',
		op_id VARCHAR(255) NOT NULL DEFAULT '',
		metadata TEXT NOT NULL DEFAULT '{}',
		result TEXT NOT NULL DEFAULT '',
		error_msg TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL,
		started_at TIMESTAMP NULL,
		completed_at TIMESTAMP NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := ts.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create task_records table: %w", err)
	}

	indexes := `
	CREATE INDEX IF NOT EXISTS idx_task_records_status ON task_records(status);
	CREATE INDEX IF NOT EXISTS idx_task_records_created_at ON task_records(created_at);
	CREATE INDEX IF NOT EXISTS idx_task_records_completed_at ON task_records(completed_at);
	`

	if _, err := ts.db.Exec(indexes); err != nil {
		return fmt.Errorf("failed to create task_records indexes: %w", err)
	}

	return nil
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

// Close releases the underlying database connection
func (ts *TaskStore) Close() error {
	if ts == nil || ts.db == nil {
		return nil
	}
	return ts.db.Close()
}
