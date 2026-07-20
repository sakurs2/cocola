-- +goose Up
-- +goose StatementBegin
ALTER TABLE llm_providers
    DROP CONSTRAINT IF EXISTS llm_providers_type_check;
ALTER TABLE llm_providers
    ADD CONSTRAINT llm_providers_type_check
    CHECK (type IN ('anthropic', 'openai_responses', 'openai_embeddings'));

ALTER TABLE llm_model_routes
    ADD COLUMN embedding_dimension INTEGER NOT NULL DEFAULT 0;
ALTER TABLE llm_model_routes
    ADD CONSTRAINT llm_model_routes_embedding_dimension_check
    CHECK (
        (protocol = 'openai-embeddings' AND embedding_dimension > 0)
        OR (protocol <> 'openai-embeddings' AND embedding_dimension = 0)
    );

CREATE TABLE memory_config (
    singleton                 BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    enabled                   BOOLEAN NOT NULL DEFAULT FALSE,
    extraction_model_route_id TEXT REFERENCES llm_model_routes(id) ON DELETE RESTRICT,
    embedding_model_route_id  TEXT REFERENCES llm_model_routes(id) ON DELETE RESTRICT,
    version                   BIGINT NOT NULL DEFAULT 0,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by                TEXT NOT NULL DEFAULT ''
);
INSERT INTO memory_config (singleton) VALUES (TRUE)
ON CONFLICT (singleton) DO NOTHING;

CREATE TABLE memory_index_state (
    singleton           BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    embedding_dimension INTEGER NOT NULL CHECK (embedding_dimension > 0),
    locked_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE memory_user_settings (
    tenant_id    TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    use_enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    learn_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    epoch        BIGINT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);

CREATE TABLE memory_capture_jobs (
    run_id           TEXT PRIMARY KEY REFERENCES conversation_runs(trace_id) ON DELETE CASCADE,
    tenant_id        TEXT NOT NULL,
    user_id          TEXT NOT NULL,
    conversation_id  TEXT NOT NULL,
    epoch             BIGINT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'pending',
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    openviking_session_id TEXT NOT NULL DEFAULT '',
    openviking_task_id    TEXT NOT NULL DEFAULT '',
    recalled_uris     JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_error_code   TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT memory_capture_jobs_status_check
        CHECK (status IN ('pending', 'submitted', 'completed', 'retry', 'dead', 'cancelled')),
    CONSTRAINT memory_capture_jobs_attempts_check CHECK (attempts >= 0)
);
CREATE INDEX idx_memory_capture_jobs_ready
    ON memory_capture_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending', 'submitted', 'retry');
CREATE INDEX idx_memory_capture_jobs_subject
    ON memory_capture_jobs (tenant_id, user_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_memory_capture_jobs_subject;
DROP INDEX IF EXISTS idx_memory_capture_jobs_ready;
DROP TABLE IF EXISTS memory_capture_jobs;
DROP TABLE IF EXISTS memory_user_settings;
DROP TABLE IF EXISTS memory_index_state;
DROP TABLE IF EXISTS memory_config;
ALTER TABLE llm_model_routes
    DROP CONSTRAINT IF EXISTS llm_model_routes_embedding_dimension_check;
ALTER TABLE llm_model_routes DROP COLUMN IF EXISTS embedding_dimension;
ALTER TABLE llm_providers
    DROP CONSTRAINT IF EXISTS llm_providers_type_check;
ALTER TABLE llm_providers
    ADD CONSTRAINT llm_providers_type_check
    CHECK (type IN ('anthropic', 'openai_responses'));
-- +goose StatementEnd
