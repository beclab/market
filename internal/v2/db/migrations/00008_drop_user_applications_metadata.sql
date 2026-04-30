-- +goose Up
-- The user_applications.metadata column is being removed: no Go-side
-- consumer maps into it, and the OlaresManifest "metadata" block (name /
-- title / description / icon / categories / locale) will simply fall
-- through to the spec catch-all column instead.

-- +goose StatementBegin
ALTER TABLE user_applications
    DROP CONSTRAINT IF EXISTS ck_user_applications_metadata_object;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_applications
    DROP COLUMN IF EXISTS metadata;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_applications
    ADD COLUMN metadata JSONB;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_applications
    ADD CONSTRAINT ck_user_applications_metadata_object
        CHECK (metadata IS NULL OR jsonb_typeof(metadata) = 'object');
-- +goose StatementEnd
