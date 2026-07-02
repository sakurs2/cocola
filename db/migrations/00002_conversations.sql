-- +goose Up
-- +goose StatementBegin
-- cocola persistence schema v2: conversation history (gateway domain).
--
-- Extends the gateway domain from 00001 with durable conversation storage so a
-- user's sidebar can list their conversations and clicking one re-renders its
-- full history. This is a "route A" UI-message MIRROR: the gateway persists the
-- exact parts the web client renders (text / reasoning / tool-call), while the
-- sandbox's on-disk claude JSONL remains the source of truth for agent resume.
-- The two are intentionally separate concerns (slight redundancy accepted).
--
-- Ownership / gating identical to 00001: applied by goose from the embedded db
-- module by whichever service boots first with COCOLA_PG_DSN set. Idempotent.

-- One row per conversation. id reuses the frontend session_id (1:1), so no
-- extra id mapping is needed and a follow-up turn on the same session updates
-- the same row (and lets the agent --resume via session_map).
CREATE TABLE IF NOT EXISTS conversations (
    id          TEXT PRIMARY KEY,            -- == frontend session_id
    user_id     TEXT NOT NULL DEFAULT '',    -- owner; list is filtered by this
    tenant_id   TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',    -- MVP: truncated first user message
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL         -- sidebar orders by this DESC
);
CREATE INDEX IF NOT EXISTS idx_conversations_user_updated
    ON conversations (user_id, updated_at DESC);

-- One row per rendered message. parts_json holds the web client's UiPart[]
-- verbatim (types: text | reasoning | tool-call), so a read replays straight
-- into convertMessage with zero schema drift.
CREATE TABLE IF NOT EXISTS messages (
    id               TEXT PRIMARY KEY,
    conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role             TEXT NOT NULL DEFAULT '',   -- 'user' | 'assistant'
    parts_json       JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_conv_created
    ON messages (conversation_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
-- +goose StatementEnd
