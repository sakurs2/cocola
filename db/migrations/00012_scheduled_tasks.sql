-- +goose Up
-- +goose StatementBegin
-- Admin-owned system scheduled tasks. User-created tasks are intentionally
-- deferred; owner_type leaves room for that later without reshaping the schema.
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id             TEXT PRIMARY KEY,
    owner_type     TEXT NOT NULL DEFAULT 'system',
    name           TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'active',
    schedule_kind  TEXT NOT NULL DEFAULT '',
    schedule_spec  JSONB NOT NULL DEFAULT '{}'::jsonb,
    timezone       TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    prompt         TEXT NOT NULL DEFAULT '',
    model_alias    TEXT NOT NULL DEFAULT '',
    max_turns      INTEGER NOT NULL DEFAULT 30,
    config_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_run_at    TIMESTAMPTZ,
    last_run_at    TIMESTAMPTZ,
    run_count      BIGINT NOT NULL DEFAULT 0,
    last_status    TEXT NOT NULL DEFAULT '',
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    created_by     TEXT NOT NULL DEFAULT '',
    updated_by     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_due
    ON scheduled_tasks (status, next_run_at)
    WHERE next_run_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_updated
    ON scheduled_tasks (updated_at DESC);

CREATE TABLE IF NOT EXISTS scheduled_task_attachments (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL DEFAULT '',
    mime        TEXT NOT NULL DEFAULT '',
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    object_key  TEXT NOT NULL DEFAULT '',
    content_b64 TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    created_by  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_attachments_task
    ON scheduled_task_attachments (task_id, created_at);

CREATE TABLE IF NOT EXISTS scheduled_task_runs (
    id             TEXT PRIMARY KEY,
    task_id        TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
    scheduled_for  TIMESTAMPTZ,
    status         TEXT NOT NULL DEFAULT 'queued',
    worker_id      TEXT NOT NULL DEFAULT '',
    session_id     TEXT NOT NULL DEFAULT '',
    model_alias    TEXT NOT NULL DEFAULT '',
    output_text    TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_task_created
    ON scheduled_task_runs (task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_status
    ON scheduled_task_runs (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS scheduled_task_run_events (
    id          BIGSERIAL PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES scheduled_task_runs(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    kind        TEXT NOT NULL DEFAULT '',
    data_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL,
    UNIQUE (run_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_run_events_run
    ON scheduled_task_run_events (run_id, seq);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS scheduled_task_run_events;
DROP TABLE IF EXISTS scheduled_task_runs;
DROP TABLE IF EXISTS scheduled_task_attachments;
DROP TABLE IF EXISTS scheduled_tasks;
-- +goose StatementEnd
