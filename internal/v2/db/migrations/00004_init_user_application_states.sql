-- +goose Up
-- user_application_states: per-user installation runtime.
-- One row per user_application. installed_version reflects what is actually
-- running; target_version is set during installing/upgrading/cancelling and
-- cleared otherwise. The state / reason / message / progress fields mirror
-- the NATS status messages produced by app-service verbatim; their values
-- are not constrained at the database level (the application layer is the
-- source of truth for valid state transitions).
--
-- The op_id / op_type / event_create_time columns are populated by the
-- new task → state pipeline (see internal/v2/appinfo/state.go +
-- internal/v2/store/userappstate.go). Two write paths feed this table:
--   - Market-initiated operations: API handlers write a pending row, the
--     task executor links op_id after app-service responds, and NATS
--     events update state / progress / entrances thereafter.
--   - Externally-initiated operations (other services calling app-service
--     directly): no API handler / no task; state.go is the FIRST writer
--     on the first NATS event for that (user, source, app). op_id and
--     op_type may legitimately remain empty in this case.
-- event_create_time provides monotonic ordering against out-of-order NATS
-- delivery in both paths.

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

    -- The op_id of the operation whose progress this row currently reflects.
    -- May legitimately be empty:
    --   - transiently between API-handler pending-row creation and the task
    --     executor receiving an op_id from app-service (Market-initiated path);
    --   - persistently for operations initiated outside Market (e.g. another
    --     service called app-service directly) where no Market task exists
    --     and the originating service did not include an op_id in NATS events.
    -- Sources of writes (later writers win, gated by event_create_time):
    --   1. NATS state events (msg.OpID) — primary source for both
    --      Market-initiated and externally-initiated operations;
    --   2. Task executor's LinkOpID call after app-service HTTP response
    --      (Market-initiated path only).
    -- For audit / debug correlation with task_records.op_id when present;
    -- NOT used as a lookup key. Lookup is by user_application_id; source
    -- resolution is by NATS msg.MarketSource — see internal/v2/appinfo/state.go.
    op_id               VARCHAR(64)  NOT NULL DEFAULT '',

    -- The kind of operation currently in flight (install / uninstall /
    -- upgrade / clone / cancel / stop / resume). Sources of writes:
    --   1. API handler at pending-row creation (Market-initiated operations);
    --   2. NATS state events (msg.OpType) — may overwrite the API value, and
    --      is the only source for externally-initiated operations.
    -- Empty string means unknown — typical for NATS events from non-Market
    -- services that don't classify operations, or for app-service events
    -- that simply omit the field. For diagnostic / SCC reasoning only.
    op_type             VARCHAR(32)  NOT NULL DEFAULT '',

    -- Last applied event's createTime (NATS message createTime, parsed).
    -- Used as the monotonic guard against out-of-order NATS delivery in
    -- DAO upserts: an incoming event whose createTime is older than this
    -- column is skipped. Initialised to NOW() at API handler write so any
    -- legitimate later NATS event passes; subsequent updates set it to the
    -- incoming msg.CreateTime.
    event_create_time   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Runtime data delivered by app-service callbacks (with assigned URLs).
    entrances           JSONB,
    shared_entrances    JSONB,
    -- status.entranceStatuses from app-service: per-entrance running status,
    -- delivered as a JSON array of objects (one element per entrance).
    status_entrances    JSONB,

    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT ck_user_application_states_entrances_array
        CHECK (entrances IS NULL OR jsonb_typeof(entrances) = 'array'),
    CONSTRAINT ck_user_application_states_shared_entrances_array
        CHECK (shared_entrances IS NULL OR jsonb_typeof(shared_entrances) = 'array'),
    CONSTRAINT ck_user_application_states_status_entrances_array
        CHECK (status_entrances IS NULL OR jsonb_typeof(status_entrances) = 'array')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_application_states_state ON user_application_states (state);
CREATE INDEX IF NOT EXISTS idx_user_application_states_op_id ON user_application_states (op_id) WHERE op_id <> '';
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
