-- +goose Up
-- user_application_states: installed-app runtime state, queried by
-- (user_id, app_id). Updated on install/upgrade/uninstall transitions.

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_application_states (
    id                  BIGSERIAL     PRIMARY KEY,
    user_application_id BIGINT        NOT NULL REFERENCES user_applications(id) ON DELETE CASCADE,
    app_version         VARCHAR(16)   NOT NULL,
    state               VARCHAR(64)   NOT NULL DEFAULT '',
    reason              VARCHAR(200)  NOT NULL DEFAULT '',
    message             VARCHAR(200)  NOT NULL DEFAULT '',
    progress            VARCHAR(10)   NOT NULL DEFAULT '',
    spec                JSONB,
    status              JSONB,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_application_states_ua
        UNIQUE (user_application_id),
    CONSTRAINT ck_user_application_states_spec_object
        CHECK (spec IS NULL OR jsonb_typeof(spec) = 'object'),
    CONSTRAINT ck_user_application_states_status_object
        CHECK (status IS NULL OR jsonb_typeof(status) = 'object')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_application_states_ua    ON user_application_states (user_application_id);
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
