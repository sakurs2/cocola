-- +goose Up
CREATE TABLE user_agent_instructions (
    user_id    TEXT PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    content    TEXT NOT NULL DEFAULT '',
    version    BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS user_agent_instructions;
