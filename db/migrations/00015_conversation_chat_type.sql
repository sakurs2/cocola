-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS chat_type TEXT NOT NULL DEFAULT 'chat';

CREATE INDEX IF NOT EXISTS idx_conversations_user_type_updated
    ON conversations (user_id, chat_type, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_conversations_user_type_updated;
ALTER TABLE conversations
    DROP COLUMN IF EXISTS chat_type;
-- +goose StatementEnd
