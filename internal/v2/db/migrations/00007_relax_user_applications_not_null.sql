-- +goose Up
-- Hydration writes a placeholder user_applications row on the first
-- chart-repo render failure, before any manifest data is available.
-- manifest_version / manifest_type are NOT NULL with no default in 00003,
-- which would reject those failure-only inserts. Give them an empty-string
-- default so the failure path can omit the columns entirely; identity
-- columns (app_raw_id / app_raw_name / app_name / source_id / user_id /
-- app_id) remain NOT NULL because the caller can always fill them from the
-- applications row that drove the candidate.
--
-- Also add a (user_id, render_status) composite index so the hydration
-- candidate query (LEFT JOIN applications ↔ user_applications, filtered by
-- 'pending' / 'failed' / version-mismatched) can use an index scan instead
-- of falling back to a full table scan as user_applications grows.

-- +goose StatementBegin
ALTER TABLE user_applications
    ALTER COLUMN manifest_version SET DEFAULT '',
    ALTER COLUMN manifest_type    SET DEFAULT '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_user_applications_user_status
    ON user_applications (user_id, render_status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_user_applications_user_status;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_applications
    ALTER COLUMN manifest_version DROP DEFAULT,
    ALTER COLUMN manifest_type    DROP DEFAULT;
-- +goose StatementEnd
