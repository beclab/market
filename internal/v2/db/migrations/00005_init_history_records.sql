-- +goose Up
-- history_records: append-only log of action / system events used by the
-- history module. Schema is the union of the legacy CREATE TABLE plus the
-- account column the legacy code added at runtime via ALTER TABLE.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS history_records (
    id         BIGSERIAL    PRIMARY KEY,
    type       VARCHAR(100) NOT NULL,
    message    TEXT         NOT NULL,
    time       BIGINT       NOT NULL,
    app        VARCHAR(255) NOT NULL,
    account    VARCHAR(255) NOT NULL DEFAULT '',
    extended   TEXT                  DEFAULT '',
    created_at TIMESTAMPTZ             DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- For pre-existing deployments where the table was created without the
-- account column, the legacy code performed a runtime ALTER TABLE. Repeat
-- it idempotently here so the column is guaranteed present.
-- +goose StatementBegin
ALTER TABLE history_records
    ADD COLUMN IF NOT EXISTS account VARCHAR(255) NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_history_type       ON history_records (type);
CREATE INDEX IF NOT EXISTS idx_history_app        ON history_records (app);
CREATE INDEX IF NOT EXISTS idx_history_account    ON history_records (account);
CREATE INDEX IF NOT EXISTS idx_history_time       ON history_records (time);
CREATE INDEX IF NOT EXISTS idx_history_created_at ON history_records (created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS history_records;
-- +goose StatementEnd
