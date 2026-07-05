-- +goose Up
-- +goose StatementBegin
ALTER TABLE session_map
    ADD COLUMN IF NOT EXISTS checkpoint_status TEXT NOT NULL DEFAULT '';
ALTER TABLE session_map
    ADD COLUMN IF NOT EXISTS checkpoint_size_bytes BIGINT NOT NULL DEFAULT 0;
ALTER TABLE session_map
    ADD COLUMN IF NOT EXISTS checkpoint_error TEXT NOT NULL DEFAULT '';
ALTER TABLE session_map
    ADD COLUMN IF NOT EXISTS checkpoint_updated_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE session_map
    DROP COLUMN IF EXISTS checkpoint_updated_at;
ALTER TABLE session_map
    DROP COLUMN IF EXISTS checkpoint_error;
ALTER TABLE session_map
    DROP COLUMN IF EXISTS checkpoint_size_bytes;
ALTER TABLE session_map
    DROP COLUMN IF EXISTS checkpoint_status;
-- +goose StatementEnd
