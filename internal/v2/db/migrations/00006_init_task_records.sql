-- +goose Up
-- task_records: persisted state of background tasks executed by the task
-- module. Schema mirrors the legacy CREATE TABLE in task/taskstore.go so the
-- migration is a no-op on databases the legacy code already initialised.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS task_records (
    id           BIGSERIAL    PRIMARY KEY,
    task_id      VARCHAR(128) UNIQUE NOT NULL,
    type         INTEGER      NOT NULL,
    status       INTEGER      NOT NULL,
    app_name     VARCHAR(255) NOT NULL,
    user_account VARCHAR(255) NOT NULL DEFAULT '',
    op_id        VARCHAR(255) NOT NULL DEFAULT '',
    metadata     TEXT         NOT NULL DEFAULT '{}',
    result       TEXT         NOT NULL DEFAULT '',
    error_msg    TEXT         NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ    NOT NULL,
    started_at   TIMESTAMPTZ    NULL,
    completed_at TIMESTAMPTZ    NULL,
    updated_at   TIMESTAMPTZ    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_task_records_status       ON task_records (status);
CREATE INDEX IF NOT EXISTS idx_task_records_created_at   ON task_records (created_at);
CREATE INDEX IF NOT EXISTS idx_task_records_completed_at ON task_records (completed_at);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_task_records_set_updated_at ON task_records;
CREATE TRIGGER trg_task_records_set_updated_at
BEFORE UPDATE ON task_records
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_task_records_set_updated_at ON task_records;
DROP TABLE IF EXISTS task_records;
-- +goose StatementEnd
