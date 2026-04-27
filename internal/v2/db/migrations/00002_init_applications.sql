-- +goose Up
-- applications: per-source app catalog. One row per (source_id, app_id, app_version).
-- Cloned-app semantics live in user_applications (app_id vs app_raw_id), not here.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS applications (
    id                 BIGSERIAL    PRIMARY KEY,
    source_id          VARCHAR(50)  NOT NULL,
    app_id             VARCHAR(32)  NOT NULL,
    app_name           VARCHAR(100) NOT NULL,
    app_version        VARCHAR(16)  NOT NULL,
    -- app_type indicates "app" or "middleware".
    app_type           VARCHAR(32)  NOT NULL,
    app_entry          JSONB,
    price              JSONB,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_applications_source_app_version
        UNIQUE (source_id, app_id, app_version),
    CONSTRAINT ck_applications_app_entry_object
        CHECK (app_entry IS NULL OR jsonb_typeof(app_entry) = 'object'),
    CONSTRAINT ck_applications_price_object
        CHECK (price IS NULL OR jsonb_typeof(price) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_applications_app_id    ON applications (app_id);
CREATE INDEX IF NOT EXISTS idx_applications_source_id ON applications (source_id);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_applications_set_updated_at ON applications;
CREATE TRIGGER trg_applications_set_updated_at
BEFORE UPDATE ON applications
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_applications_set_updated_at ON applications;
DROP TABLE IF EXISTS applications;
-- +goose StatementEnd
