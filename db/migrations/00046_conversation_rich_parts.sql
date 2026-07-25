-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversation_runs
    DROP CONSTRAINT conversation_runs_status_check,
    ADD CONSTRAINT conversation_runs_status_check
        CHECK (status IN ('running', 'waiting_input', 'success', 'error', 'cancelled', 'interrupted'));

CREATE TABLE conversation_questions (
    id               UUID PRIMARY KEY,
    conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    version          INTEGER NOT NULL CHECK (version > 0),
    status           TEXT NOT NULL,
    source_run_id    TEXT NOT NULL UNIQUE REFERENCES conversation_runs(trace_id) ON DELETE CASCADE,
    answer_run_id    TEXT UNIQUE REFERENCES conversation_runs(trace_id) ON DELETE SET NULL,
    interaction_mode TEXT NOT NULL,
    runtime_id       TEXT NOT NULL,
    model_route_id   TEXT NOT NULL DEFAULT '',
    model_alias      TEXT NOT NULL DEFAULT '',
    skill_id         TEXT NOT NULL DEFAULT '',
    question_text    TEXT NOT NULL,
    options_json     JSONB NOT NULL DEFAULT '[]'::jsonb,
    answer_json      JSONB,
    answered_by      TEXT NOT NULL DEFAULT '',
    answered_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    CONSTRAINT conversation_questions_version_unique UNIQUE (conversation_id, version),
    CONSTRAINT conversation_questions_status_check
        CHECK (status IN ('pending', 'answering', 'answered', 'cancelled')),
    CONSTRAINT conversation_questions_mode_check
        CHECK (interaction_mode IN ('execute', 'plan')),
    CONSTRAINT conversation_questions_text_check
        CHECK (octet_length(question_text) > 0 AND octet_length(question_text) <= 16384),
    CONSTRAINT conversation_questions_options_array_check
        CHECK (jsonb_typeof(options_json) = 'array')
);

CREATE UNIQUE INDEX conversation_questions_one_current
    ON conversation_questions (conversation_id)
    WHERE status IN ('pending', 'answering');

CREATE INDEX conversation_questions_conversation_created
    ON conversation_questions (conversation_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS conversation_questions_conversation_created;
DROP INDEX IF EXISTS conversation_questions_one_current;
DROP TABLE IF EXISTS conversation_questions;

UPDATE conversation_runs
SET status = 'interrupted',
    error_code = CASE WHEN error_code = '' THEN 'QUESTION_STATE_REMOVED' ELSE error_code END
WHERE status = 'waiting_input';

ALTER TABLE conversation_runs
    DROP CONSTRAINT conversation_runs_status_check,
    ADD CONSTRAINT conversation_runs_status_check
        CHECK (status IN ('running', 'success', 'error', 'cancelled', 'interrupted'));
-- +goose StatementEnd
