-- +goose Up
-- Shared trigger function: stamp NEW.updated_at on every UPDATE.
-- Defined here (the first migration) and reused by every table's
-- BEFORE UPDATE trigger created in subsequent migrations.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS market_source (
    id           BIGSERIAL    PRIMARY KEY,
    source_id    VARCHAR(50)  NOT NULL,
    source_title VARCHAR(50)  NOT NULL,
    source_url   TEXT         NOT NULL,
    source_type  VARCHAR(16)  NOT NULL CHECK (source_type IN ('local', 'remote')),
    description  TEXT         NOT NULL DEFAULT '',
    priority     INTEGER      NOT NULL DEFAULT 100 CHECK (priority >= 0),
    others       JSONB,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_market_source_source_id UNIQUE (source_id),
    CONSTRAINT ck_market_source_others_object
        CHECK (others IS NULL OR jsonb_typeof(others) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_market_source_source_id ON market_source (source_id);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_market_source_set_updated_at ON market_source;
CREATE TRIGGER trg_market_source_set_updated_at
BEFORE UPDATE ON market_source
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_market_source_set_updated_at ON market_source;
DROP TABLE IF EXISTS market_source;
DROP FUNCTION IF EXISTS set_updated_at();
-- +goose StatementEnd
