-- +goose Up
-- +goose StatementBegin
ALTER TABLE session_map
    ADD COLUMN IF NOT EXISTS checkpoint_object_key TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE session_map
    DROP COLUMN IF EXISTS checkpoint_object_key;
-- +goose StatementEnd
