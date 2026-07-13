-- +goose Up
-- +goose StatementBegin
-- A conversation chooses its Agent Runtime once. Existing conversations and
-- session bindings were all created by Claude Code, so the migration is
-- deterministic and does not require a compatibility flag.
ALTER TABLE conversations
    ADD COLUMN runtime_id TEXT NOT NULL DEFAULT 'claude-code';

ALTER TABLE session_map
    RENAME COLUMN claude_session_id TO runtime_session_id;
ALTER TABLE session_map
    ADD COLUMN runtime_id TEXT NOT NULL DEFAULT 'claude-code';

-- Model compatibility is expressed by the provider protocol. The old runtime
-- field was administrator-editable but never participated in routing.
ALTER TABLE llm_model_routes
    DROP COLUMN runtime;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE llm_model_routes
    ADD COLUMN runtime TEXT NOT NULL DEFAULT 'claude-code';

ALTER TABLE session_map
    DROP COLUMN runtime_id;
ALTER TABLE session_map
    RENAME COLUMN runtime_session_id TO claude_session_id;

ALTER TABLE conversations
    DROP COLUMN runtime_id;
-- +goose StatementEnd
