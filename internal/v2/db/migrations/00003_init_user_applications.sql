-- +goose Up
-- user_applications: per-user view of a rendered app manifest.
-- Refreshed whenever chart-repo successfully renders a manifest for this user.
-- Identity fields (app_id / app_raw_id, app_name / app_raw_name) encode whether
-- the row is a clone; columns mirror the OlaresManifest top-level structure.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_applications (
    id                          BIGSERIAL    PRIMARY KEY,
    source_id                   VARCHAR(50)  NOT NULL,
    user_id                     VARCHAR(120) NOT NULL,
    -- For clone apps app_id differs from app_raw_id; for plain apps they match.
    app_id                      VARCHAR(32)  NOT NULL,
    app_raw_id                  VARCHAR(32)  NOT NULL,
    app_name                    VARCHAR(100) NOT NULL,
    app_raw_name                VARCHAR(100) NOT NULL,

    -- olaresManifest.version / olaresManifest.type (plus optional apiVersion).
    manifest_version            VARCHAR(32)  NOT NULL,
    manifest_type               VARCHAR(32)  NOT NULL,
    api_version                 VARCHAR(32),

    -- Manifest top-level blocks. metadata = name/icon/title/categories/...,
    -- spec = developer/website/description/license/...,
    -- resources = required*/limited* compute limits.
    metadata                    JSONB,
    spec                        JSONB,
    resources                   JSONB,
    options                     JSONB,
    entrances                   JSONB,
    shared_entrances            JSONB,
    ports                       JSONB,
    tailscale                   JSONB,
    permission                  JSONB,
    middleware                  JSONB,
    envs                        JSONB,

    -- Pricing & purchase live alongside the manifest data because they are
    -- per-(user, app) too.
    price                       JSONB,
    purchase_info               JSONB,

    -- render_status reflects the result of the latest chart-repo render
    -- attempt for this user_application: 'pending' before the first attempt,
    -- 'success' once the render succeeded, 'failed' otherwise.
    render_status               VARCHAR(16)  NOT NULL DEFAULT 'pending',
    -- render_error stores the most recent failure message; it is cleared
    -- when render_status flips back to 'success'. The application layer
    -- truncates messages to fit the 200-char column.
    render_error                VARCHAR(200),
    -- render_consecutive_failures counts retries since the last success;
    -- it is reset to 0 whenever render_status becomes 'success'.
    render_consecutive_failures INTEGER      NOT NULL DEFAULT 0,

    is_upgrade                  BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_user_applications_user_app
        UNIQUE (user_id, app_id),
    CONSTRAINT ck_user_applications_price_object
        CHECK (price IS NULL OR jsonb_typeof(price) = 'object'),
    CONSTRAINT ck_user_applications_purchase_info_object
        CHECK (purchase_info IS NULL OR jsonb_typeof(purchase_info) = 'object'),
    CONSTRAINT ck_user_applications_render_status
        CHECK (render_status IN ('pending', 'success', 'failed'))
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_applications_user_id      ON user_applications (user_id);
CREATE INDEX IF NOT EXISTS idx_user_applications_app_id       ON user_applications (app_id);
CREATE INDEX IF NOT EXISTS idx_user_applications_app_raw_id   ON user_applications (app_raw_id);
CREATE INDEX IF NOT EXISTS idx_user_applications_app_raw_name ON user_applications (app_raw_name);
CREATE INDEX IF NOT EXISTS idx_user_applications_source_id    ON user_applications (source_id);
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
