-- +goose Up
-- user_applications: rendered apps per user. Inserted/updated after rendering
-- completes, including for cloned apps.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_applications (
    id                 BIGSERIAL     PRIMARY KEY,
    user_id            VARCHAR(120)  NOT NULL,
    application_id     BIGINT        NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    app_raw_data       JSONB,
    app_image_analysis JSONB,
    is_upgrade         BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMP     NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMP     NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_applications_user_app
        UNIQUE (user_id, application_id),
    CONSTRAINT ck_user_applications_app_raw_object
        CHECK (app_raw_data IS NULL OR jsonb_typeof(app_raw_data) = 'object'),
    CONSTRAINT ck_user_applications_app_image_analysis_object
        CHECK (app_image_analysis IS NULL OR jsonb_typeof(app_image_analysis) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_applications_user_id        ON user_applications (user_id);
CREATE INDEX IF NOT EXISTS idx_user_applications_application_id ON user_applications (application_id);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_user_applications_set_updated_at ON user_applications;
CREATE TRIGGER trg_user_applications_set_updated_at
BEFORE UPDATE ON user_applications
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_user_applications_set_updated_at ON user_applications;
DROP TABLE IF EXISTS user_applications;
-- +goose StatementEnd
