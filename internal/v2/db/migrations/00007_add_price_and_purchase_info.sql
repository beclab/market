-- +goose Up
-- Add the price column to applications and the price + purchase_info
-- columns to user_applications. All three are JSONB and nullable; the
-- check constraints ensure that whatever lands in the column is either
-- NULL or a JSON object (matching the surrounding columns' contracts).

-- +goose StatementBegin
ALTER TABLE applications
    ADD COLUMN IF NOT EXISTS price JSONB;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE applications
    DROP CONSTRAINT IF EXISTS ck_applications_price_object;
ALTER TABLE applications
    ADD CONSTRAINT ck_applications_price_object
        CHECK (price IS NULL OR jsonb_typeof(price) = 'object');
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_applications
    ADD COLUMN IF NOT EXISTS price JSONB;
ALTER TABLE user_applications
    ADD COLUMN IF NOT EXISTS purchase_info JSONB;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE user_applications
    DROP CONSTRAINT IF EXISTS ck_user_applications_price_object;
ALTER TABLE user_applications
    ADD CONSTRAINT ck_user_applications_price_object
        CHECK (price IS NULL OR jsonb_typeof(price) = 'object');
ALTER TABLE user_applications
    DROP CONSTRAINT IF EXISTS ck_user_applications_purchase_info_object;
ALTER TABLE user_applications
    ADD CONSTRAINT ck_user_applications_purchase_info_object
        CHECK (purchase_info IS NULL OR jsonb_typeof(purchase_info) = 'object');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_applications
    DROP CONSTRAINT IF EXISTS ck_user_applications_purchase_info_object;
ALTER TABLE user_applications
    DROP CONSTRAINT IF EXISTS ck_user_applications_price_object;
ALTER TABLE user_applications
    DROP COLUMN IF EXISTS purchase_info;
ALTER TABLE user_applications
    DROP COLUMN IF EXISTS price;

ALTER TABLE applications
    DROP CONSTRAINT IF EXISTS ck_applications_price_object;
ALTER TABLE applications
    DROP COLUMN IF EXISTS price;
-- +goose StatementEnd
