-- +goose Up
-- user_applications: rendered apps per user. Inserted/updated after rendering
-- completes, including for cloned apps.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_applications (
    id                          BIGSERIAL     PRIMARY KEY,
    user_id                     VARCHAR(120)  NOT NULL,
    application_id              BIGINT        NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    app_raw_data                JSONB,
    app_image_analysis          JSONB,
    price                       JSONB,
    purchase_info               JSONB,
    -- render_status reflects the result of the latest chart-repo render
    -- attempt for this user_application: 'pending' before the first
    -- attempt, 'success' once the render succeeded, 'failed' otherwise.
    render_status               VARCHAR(16)   NOT NULL DEFAULT 'pending',
    -- render_error stores the most recent failure message; it is cleared
    -- when render_status flips back to 'success'. The application layer
    -- truncates messages to fit the 200-char column.
    render_error                VARCHAR(200),
    -- render_consecutive_failures counts retries since the last success;
    -- it is reset to 0 whenever render_status becomes 'success'.
    render_consecutive_failures INTEGER       NOT NULL DEFAULT 0,
    is_upgrade                  BOOLEAN       NOT NULL DEFAULT FALSE,
    created_at                  TIMESTAMP     NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMP     NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_applications_user_app
        UNIQUE (user_id, application_id),
    CONSTRAINT ck_user_applications_app_raw_object
        CHECK (app_raw_data IS NULL OR jsonb_typeof(app_raw_data) = 'object'),
    CONSTRAINT ck_user_applications_app_image_analysis_object
        CHECK (app_image_analysis IS NULL OR jsonb_typeof(app_image_analysis) = 'object'),
    CONSTRAINT ck_user_applications_price_object
        CHECK (price IS NULL OR jsonb_typeof(price) = 'object'),
    CONSTRAINT ck_user_applications_purchase_info_object
        CHECK (purchase_info IS NULL OR jsonb_typeof(purchase_info) = 'object'),
    CONSTRAINT ck_user_applications_render_status
        CHECK (render_status IN ('pending', 'success', 'failed'))
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
