-- +goose Up
-- user_application_states: per-user installation runtime.
-- One row per user_application. installed_version reflects what is actually
-- running; target_version is set during installing/upgrading/cancelling and
-- cleared otherwise. The state / reason / message / progress fields mirror
-- the NATS status messages produced by app-service verbatim; their values
-- are not constrained at the database level (the application layer is the
-- source of truth for valid state transitions).

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_application_states (
    id                  BIGSERIAL    PRIMARY KEY,
    user_application_id BIGINT       NOT NULL UNIQUE
                                     REFERENCES user_applications(id) ON DELETE CASCADE,

    installed_version   VARCHAR(32),
    target_version      VARCHAR(32),
    is_sys_app          BOOLEAN      NOT NULL DEFAULT FALSE,

    state               VARCHAR(64)  NOT NULL DEFAULT '',
    reason              VARCHAR(200) NOT NULL DEFAULT '',
    message             TEXT         NOT NULL DEFAULT '',
    progress            VARCHAR(10)  NOT NULL DEFAULT '',

    -- Runtime data delivered by app-service callbacks (with assigned URLs).
    entrances           JSONB,
    shared_entrances    JSONB,
    -- status.entranceStatuses from app-service: per-entrance running status.
    status_entrances    JSONB,

    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT ck_user_application_states_entrances_array
        CHECK (entrances IS NULL OR jsonb_typeof(entrances) = 'array'),
    CONSTRAINT ck_user_application_states_shared_entrances_array
        CHECK (shared_entrances IS NULL OR jsonb_typeof(shared_entrances) = 'array'),
    CONSTRAINT ck_user_application_states_status_entrances_object
        CHECK (status_entrances IS NULL OR jsonb_typeof(status_entrances) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_application_states_state ON user_application_states (state);
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_user_application_states_set_updated_at ON user_application_states;
CREATE TRIGGER trg_user_application_states_set_updated_at
BEFORE UPDATE ON user_application_states
FOR EACH ROW
EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_user_application_states_set_updated_at ON user_application_states;
DROP TABLE IF EXISTS user_application_states;
-- +goose StatementEnd
