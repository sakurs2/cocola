-- +goose Up
-- +goose StatementBegin
ALTER TABLE scheduled_tasks
    ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS conversation_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_owner_user_updated
    ON scheduled_tasks (owner_type, owner_user_id, updated_at DESC);

ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS hidden BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_conversations_user_visible_updated
    ON conversations (user_id, hidden, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_conversations_user_visible_updated;
ALTER TABLE conversations
    DROP COLUMN IF EXISTS hidden;

DROP INDEX IF EXISTS idx_scheduled_tasks_owner_user_updated;
ALTER TABLE scheduled_tasks
    DROP COLUMN IF EXISTS conversation_id,
    DROP COLUMN IF EXISTS owner_user_id;
-- +goose StatementEnd
