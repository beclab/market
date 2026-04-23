-- +goose Up
-- user_application: rendered apps per user. Inserted/updated after rendering
-- completes, including for cloned apps.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_application (
    id                 BIGSERIAL     PRIMARY KEY,
    user_id            VARCHAR(120)  NOT NULL,
    application_id     BIGINT        NOT NULL REFERENCES application(id) ON DELETE CASCADE,
    app_raw_data       JSONB,
    app_image_analysis JSONB,
    is_upgrade         BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_application_user_app
        UNIQUE (user_id, application_id),
    CONSTRAINT ck_user_application_app_raw_object
        CHECK (app_raw_data IS NULL OR jsonb_typeof(app_raw_data) = 'object'),
    CONSTRAINT ck_user_application_app_image_analysis_object
        CHECK (app_image_analysis IS NULL OR jsonb_typeof(app_image_analysis) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_application_user_id        ON user_application (user_id);
CREATE INDEX IF NOT EXISTS idx_user_application_application_id ON user_application (application_id);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_user_application_set_updated_at ON user_application;
CREATE TRIGGER trg_user_application_set_updated_at
BEFORE UPDATE ON user_application
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_user_application_set_updated_at ON user_application;
DROP TABLE IF EXISTS user_application;
-- +goose StatementEnd
