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

    -- i18n carries chart-repo's localised metadata bundle keyed by
    -- locale ("zh-CN" / "en-US" / ...). Each value is itself a flat
    -- {field -> string} object (title / description / ...). Sourced
    -- directly from the chart-repo sync-app response, not derived
    -- from raw_data_ex.
    i18n                        JSONB,
    -- version_history is the per-app changelog as emitted by chart-repo
    -- (one entry per release: version / versionName / mergedAt /
    -- upgradeDescription). Sourced directly from sync-app's
    -- top-level version_history field, NOT spliced into spec.
    version_history             JSONB,
    -- image_analysis is chart-repo's per-app docker image analysis
    -- result (types.ImageAnalysisResult: app_id / user_id / source_id /
    -- analyzed_at / total_images / images map keyed by image name).
    -- Sourced directly from the sync-app response top-level field
    -- (sibling of raw_data_ex), NOT derived from raw_data_ex. Kept
    -- nullable so the failure / never-rendered placeholder rows can
    -- be inserted without a value, and so that legacy rows persisted
    -- before this column existed surface as NULL until their next
    -- successful render refreshes them.
    image_analysis              JSONB,

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

    -- A given user can see the same app_id from multiple sources, so the
    -- catalog uniqueness key includes source_id. Cross-source single-install
    -- (one running app per user_id+app_id regardless of source) is enforced
    -- by the application layer at install time, not at the database level.
    CONSTRAINT uq_user_applications_user_source_app
        UNIQUE (user_id, source_id, app_id),
    CONSTRAINT ck_user_applications_metadata_object
        CHECK (metadata IS NULL OR jsonb_typeof(metadata) = 'object'),
    CONSTRAINT ck_user_applications_spec_object
        CHECK (spec IS NULL OR jsonb_typeof(spec) = 'object'),
    CONSTRAINT ck_user_applications_resources_object
        CHECK (resources IS NULL OR jsonb_typeof(resources) = 'object'),
    CONSTRAINT ck_user_applications_options_object
        CHECK (options IS NULL OR jsonb_typeof(options) = 'object'),
    CONSTRAINT ck_user_applications_entrances_array
        CHECK (entrances IS NULL OR jsonb_typeof(entrances) = 'array'),
    CONSTRAINT ck_user_applications_shared_entrances_array
        CHECK (shared_entrances IS NULL OR jsonb_typeof(shared_entrances) = 'array'),
    CONSTRAINT ck_user_applications_ports_array
        CHECK (ports IS NULL OR jsonb_typeof(ports) = 'array'),
    CONSTRAINT ck_user_applications_tailscale_object
        CHECK (tailscale IS NULL OR jsonb_typeof(tailscale) = 'object'),
    CONSTRAINT ck_user_applications_permission_object
        CHECK (permission IS NULL OR jsonb_typeof(permission) = 'object'),
    CONSTRAINT ck_user_applications_middleware_object
        CHECK (middleware IS NULL OR jsonb_typeof(middleware) = 'object'),
    CONSTRAINT ck_user_applications_envs_array
        CHECK (envs IS NULL OR jsonb_typeof(envs) = 'array'),
    CONSTRAINT ck_user_applications_price_object
        CHECK (price IS NULL OR jsonb_typeof(price) = 'object'),
    CONSTRAINT ck_user_applications_purchase_info_object
        CHECK (purchase_info IS NULL OR jsonb_typeof(purchase_info) = 'object'),
    CONSTRAINT ck_user_applications_i18n_object
        CHECK (i18n IS NULL OR jsonb_typeof(i18n) = 'object'),
    CONSTRAINT ck_user_applications_version_history_array
        CHECK (version_history IS NULL OR jsonb_typeof(version_history) = 'array'),
    CONSTRAINT ck_user_applications_image_analysis_object
        CHECK (image_analysis IS NULL OR jsonb_typeof(image_analysis) = 'object'),
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
-- Composite (user_id, app_id) accelerates the application-level
-- "is this app already installed from any source" lookup.
CREATE INDEX IF NOT EXISTS idx_user_applications_user_app     ON user_applications (user_id, app_id);
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
