-- +goose Up
-- applications: base catalog of apps coming from cloud. Local clones are also
-- inserted here.
--
-- Naming conventions for cloned vs. plain apps:
--   plain app:   app_id       = app_raw_id     (e.g. md5(testapp1))
--                app_name     = app_raw_name   (e.g. testapp1)
--   cloned app:  app_id       = md5(testapp1XXXXXX)
--                app_name     = testapp1XXXXXX
--                app_raw_id   = md5(testapp1)
--                app_raw_name = testapp1

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS applications (
    id                 BIGSERIAL     PRIMARY KEY,
    source_id          VARCHAR(50)   NOT NULL,
    app_id             VARCHAR(32)   NOT NULL,
    app_raw_id         VARCHAR(32)   NOT NULL,
    app_name           VARCHAR(100)  NOT NULL,
    app_raw_name       VARCHAR(100)  NOT NULL,
    app_version        VARCHAR(16)   NOT NULL,
    -- app_type indicates "app" or "middleware".
    app_type           VARCHAR(32)   NOT NULL,
    app_entry          JSONB,
    app_image_analysis JSONB,
    -- installed_type captures install mode: full / server / client.
    installed_type     VARCHAR(10)   NOT NULL,
    created_at         TIMESTAMP     NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMP     NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_applications_source_app_version
        UNIQUE (source_id, app_id, app_version),
    CONSTRAINT ck_applications_app_entry_object
        CHECK (app_entry IS NULL OR jsonb_typeof(app_entry) = 'object'),
    CONSTRAINT ck_applications_app_image_analysis_object
        CHECK (app_image_analysis IS NULL OR jsonb_typeof(app_image_analysis) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_applications_app_id       ON applications (app_id);
CREATE INDEX IF NOT EXISTS idx_applications_app_raw_id   ON applications (app_raw_id);
CREATE INDEX IF NOT EXISTS idx_applications_app_raw_name ON applications (app_raw_name);
CREATE INDEX IF NOT EXISTS idx_applications_source_id    ON applications (source_id);
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
