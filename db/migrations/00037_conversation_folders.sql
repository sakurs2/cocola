-- +goose Up
-- +goose StatementBegin
CREATE TABLE conversation_folders (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT conversation_folders_name_check
        CHECK (name = BTRIM(name) AND CHAR_LENGTH(name) BETWEEN 1 AND 80),
    CONSTRAINT conversation_folders_id_user_unique UNIQUE (id, user_id)
);

CREATE UNIQUE INDEX idx_conversation_folders_user_name
    ON conversation_folders (user_id, LOWER(name));

ALTER TABLE conversations
    ADD COLUMN folder_id TEXT;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_folder_id_fkey
    FOREIGN KEY (folder_id, user_id) REFERENCES conversation_folders(id, user_id) ON DELETE CASCADE;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_folder_chat_type_check
    CHECK (folder_id IS NULL OR chat_type = 'chat');

CREATE INDEX idx_conversations_user_folder_updated
    ON conversations (user_id, folder_id, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_conversations_user_folder_updated;
ALTER TABLE conversations
    DROP CONSTRAINT IF EXISTS conversations_folder_chat_type_check;
ALTER TABLE conversations
    DROP CONSTRAINT IF EXISTS conversations_folder_id_fkey;
ALTER TABLE conversations
    DROP COLUMN IF EXISTS folder_id;
DROP TABLE IF EXISTS conversation_folders;
-- +goose StatementEnd
