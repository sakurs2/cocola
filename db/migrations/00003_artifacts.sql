-- +goose Up
-- +goose StatementBegin
-- User-downloadable files produced by agent turns. The bytes live in the
-- object store; this table is the ownership and metadata index used by the
-- gateway download endpoint.
CREATE TABLE IF NOT EXISTS artifacts (
    id               TEXT PRIMARY KEY,
    conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id          TEXT NOT NULL DEFAULT '',
    tenant_id        TEXT NOT NULL DEFAULT '',
    filename         TEXT NOT NULL DEFAULT '',
    mime             TEXT NOT NULL DEFAULT '',
    size_bytes       BIGINT NOT NULL DEFAULT 0,
    object_key       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_artifacts_conv_created
    ON artifacts (conversation_id, created_at);
CREATE INDEX IF NOT EXISTS idx_artifacts_user_conv
    ON artifacts (user_id, conversation_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS artifacts;
-- +goose StatementEnd
