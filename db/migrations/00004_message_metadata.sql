-- +goose Up
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE messages
    DROP COLUMN IF EXISTS metadata_json;
