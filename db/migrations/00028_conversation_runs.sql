-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS conversation_runs (
    trace_id          TEXT PRIMARY KEY,
    root_span_id      TEXT NOT NULL DEFAULT '',
    conversation_id   TEXT NOT NULL DEFAULT '',
    conversation_title TEXT NOT NULL DEFAULT '',
    user_id           TEXT NOT NULL DEFAULT '',
    user_email        TEXT NOT NULL DEFAULT '',
    source            TEXT NOT NULL DEFAULT 'interactive',
    model_alias       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'running',
    started_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ,
    last_activity_at  TIMESTAMPTZ NOT NULL,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    ttft_ms           BIGINT NOT NULL DEFAULT 0,
    llm_call_count    BIGINT NOT NULL DEFAULT 0,
    tool_call_count   BIGINT NOT NULL DEFAULT 0,
    input_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens     BIGINT NOT NULL DEFAULT 0,
    cache_tokens      BIGINT NOT NULL DEFAULT 0,
    error_code        TEXT NOT NULL DEFAULT '',
    safe_error_summary TEXT NOT NULL DEFAULT '',
    detail_status     TEXT NOT NULL DEFAULT 'available',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT conversation_runs_source_check
        CHECK (source IN ('interactive', 'scheduled_task')),
    CONSTRAINT conversation_runs_status_check
        CHECK (status IN ('running', 'success', 'error', 'cancelled', 'interrupted')),
    CONSTRAINT conversation_runs_detail_status_check
        CHECK (detail_status IN ('available', 'partial', 'expired'))
);

CREATE INDEX IF NOT EXISTS idx_conversation_runs_started
    ON conversation_runs (started_at DESC, trace_id DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_runs_user_started
    ON conversation_runs (user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_runs_conversation_started
    ON conversation_runs (conversation_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_runs_status_started
    ON conversation_runs (status, started_at DESC);

CREATE TABLE IF NOT EXISTS conversation_trace_spans (
    id              BIGSERIAL PRIMARY KEY,
    trace_id        TEXT NOT NULL REFERENCES conversation_runs(trace_id) ON DELETE CASCADE,
    span_id         TEXT NOT NULL,
    parent_span_id  TEXT NOT NULL DEFAULT '',
    schema_version  INTEGER NOT NULL DEFAULT 1,
    service         TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    category        TEXT NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL,
    duration_us     BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'running',
    attributes_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (trace_id, span_id)
);

CREATE INDEX IF NOT EXISTS idx_conversation_trace_spans_trace_started
    ON conversation_trace_spans (trace_id, started_at ASC, id ASC);
CREATE INDEX IF NOT EXISTS idx_conversation_trace_spans_created
    ON conversation_trace_spans (created_at DESC, id DESC);

-- A conversation run is the only remaining audit record. Preserve historical
-- chat.send rows and intentionally discard every other audit category.
INSERT INTO conversation_runs (
    trace_id, root_span_id, conversation_id, user_id, user_email, source,
    model_alias, status, started_at, completed_at, last_activity_at,
    duration_ms, ttft_ms, error_code, detail_status, created_at, updated_at
)
SELECT
    e.trace_id,
    substring(md5(e.trace_id || ':root'), 1, 16),
    COALESCE(NULLIF(e.metadata_json->>'conversation_id', ''), e.resource_id),
    e.actor_user_id,
    e.actor_email,
    CASE WHEN e.metadata_json->>'chat_type' = 'scheduled_task'
        THEN 'scheduled_task' ELSE 'interactive' END,
    COALESCE(e.metadata_json->>'model_alias', ''),
    CASE WHEN e.result = 'success' THEN 'success' ELSE 'error' END,
    e.ts - (CASE
        WHEN e.metadata_json->>'duration_ms' ~ '^[0-9]+$'
        THEN (e.metadata_json->>'duration_ms')::bigint
        ELSE 0
    END * interval '1 millisecond'),
    e.ts,
    e.ts,
    CASE
        WHEN e.metadata_json->>'duration_ms' ~ '^[0-9]+$'
        THEN (e.metadata_json->>'duration_ms')::bigint
        ELSE 0
    END,
    0,
    e.error_code,
    CASE WHEN EXISTS (SELECT 1 FROM trace_events t WHERE t.trace_id = e.trace_id)
        THEN 'available' ELSE 'partial' END,
    e.ts,
    e.ts
FROM audit_events e
WHERE e.action = 'chat.send' AND e.trace_id <> ''
ON CONFLICT (trace_id) DO NOTHING;

INSERT INTO conversation_trace_spans (
    trace_id, span_id, parent_span_id, schema_version, service, name, category,
    started_at, duration_us, status, attributes_json, created_at, updated_at
)
SELECT
    r.trace_id,
    r.root_span_id,
    '',
    1,
    'gateway',
    'conversation.run',
    'request',
    r.started_at,
    r.duration_ms * 1000,
    r.status,
    jsonb_build_object('legacy', true),
    r.created_at,
    r.updated_at
FROM conversation_runs r
ON CONFLICT (trace_id, span_id) DO NOTHING;

INSERT INTO conversation_trace_spans (
    trace_id, span_id, parent_span_id, schema_version, service, name, category,
    started_at, duration_us, status, attributes_json, created_at, updated_at
)
SELECT
    t.trace_id,
    lpad(to_hex(t.id), 16, '0'),
    r.root_span_id,
    1,
    t.service,
    t.name,
    t.category,
    t.started_at,
    t.duration_ms * 1000,
    CASE WHEN t.status IN ('failure', 'error') THEN 'error' ELSE 'success' END,
    jsonb_build_object('legacy', true),
    t.created_at,
    t.created_at
FROM trace_events t
JOIN conversation_runs r ON r.trace_id = t.trace_id
ON CONFLICT (trace_id, span_id) DO NOTHING;

DELETE FROM audit_events WHERE action <> 'chat.send';
DELETE FROM audit_log;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_conversation_trace_spans_created;
DROP INDEX IF EXISTS idx_conversation_trace_spans_trace_started;
DROP TABLE IF EXISTS conversation_trace_spans;
DROP INDEX IF EXISTS idx_conversation_runs_status_started;
DROP INDEX IF EXISTS idx_conversation_runs_conversation_started;
DROP INDEX IF EXISTS idx_conversation_runs_user_started;
DROP INDEX IF EXISTS idx_conversation_runs_started;
DROP TABLE IF EXISTS conversation_runs;
-- +goose StatementEnd
