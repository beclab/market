-- +goose Up
-- Add settings + tailscale JSONB columns to user_application_states.
--
-- Rationale: app-service exposes these two blocks per app via the
-- /app-service/v1/all/apps endpoint, which Market polls periodically as
-- part of the StatusCorrectionChecker (SCC) reconciliation loop. Both
-- blocks describe per-(user, app) runtime state: tailscale carries the
-- live tailscale ACL/network membership the running pod negotiated;
-- settings carries the per-instance UI/runtime configuration that may
-- diverge from the manifest (user-edited title, app-service-derived
-- runtime overrides, etc.). They are NOT delivered through the NATS
-- state pipeline (the AppStateMessage payload omits both fields) and
-- they are NOT static manifest data (user_applications already carries
-- tailscale as a manifest column), so they belong on the
-- user_application_states row alongside other runtime-driven JSONB
-- blocks (entrances / shared_entrances / status_entrances).
--
-- Population path (intentionally NOT wired in this migration):
--   - The SCC reconciliation loop will add a store-level helper in a
--     follow-up commit to write these columns from the /all/apps
--     response. Until that helper lands, the columns stay NULL.
--   - The NATS pipeline (UpsertStateFromNATS) does not touch these
--     columns: they are out of NATS scope, and the COALESCE-on-nil
--     UPSERT pattern the DAO already uses for other JSONB columns
--     means the NATS path will preserve whatever the SCC writer
--     leaves there.
--
-- jsonb_typeof = 'object' constraints mirror the style 00004 uses for
-- the manifest-derived JSONB blocks on user_applications: tailscale is
-- a map of network parameters, settings is a map of key→value
-- overrides — neither should ever be a JSON array or scalar.

-- +goose StatementBegin
ALTER TABLE user_application_states
    ADD COLUMN IF NOT EXISTS settings  JSONB,
    ADD COLUMN IF NOT EXISTS tailscale JSONB;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_application_states
    DROP CONSTRAINT IF EXISTS ck_user_application_states_settings_object;
ALTER TABLE user_application_states
    ADD CONSTRAINT ck_user_application_states_settings_object
        CHECK (settings IS NULL OR jsonb_typeof(settings) = 'object');
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_application_states
    DROP CONSTRAINT IF EXISTS ck_user_application_states_tailscale_object;
ALTER TABLE user_application_states
    ADD CONSTRAINT ck_user_application_states_tailscale_object
        CHECK (tailscale IS NULL OR jsonb_typeof(tailscale) = 'object');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_application_states
    DROP CONSTRAINT IF EXISTS ck_user_application_states_settings_object;
ALTER TABLE user_application_states
    DROP CONSTRAINT IF EXISTS ck_user_application_states_tailscale_object;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_application_states
    DROP COLUMN IF EXISTS settings,
    DROP COLUMN IF EXISTS tailscale;
-- +goose StatementEnd
