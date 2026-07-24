-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversation_runs
    ADD COLUMN interaction_mode TEXT NOT NULL DEFAULT 'execute',
    ADD CONSTRAINT conversation_runs_interaction_mode_check
        CHECK (interaction_mode IN ('execute', 'plan'));

CREATE TABLE conversation_plans (
    id                 UUID PRIMARY KEY,
    conversation_id    TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    version            INTEGER NOT NULL CHECK (version > 0),
    status             TEXT NOT NULL,
    source_run_id      TEXT NOT NULL UNIQUE REFERENCES conversation_runs(trace_id) ON DELETE CASCADE,
    runtime_id         TEXT NOT NULL,
    model_route_id     TEXT NOT NULL DEFAULT '',
    model_alias        TEXT NOT NULL DEFAULT '',
    content_markdown   TEXT NOT NULL,
    workspace_revision TEXT NOT NULL DEFAULT '',
    approved_by        TEXT NOT NULL DEFAULT '',
    approved_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    CONSTRAINT conversation_plans_version_unique UNIQUE (conversation_id, version),
    CONSTRAINT conversation_plans_status_check CHECK (
        status IN ('ready', 'executing', 'completed', 'stopped', 'failed', 'superseded', 'cancelled')
    ),
    CONSTRAINT conversation_plans_content_check CHECK (
        octet_length(content_markdown) > 0 AND octet_length(content_markdown) <= 131072
    )
);

CREATE UNIQUE INDEX conversation_plans_one_current
    ON conversation_plans (conversation_id)
    WHERE status IN ('ready', 'executing', 'stopped');

ALTER TABLE conversation_runs
    ADD COLUMN plan_id UUID REFERENCES conversation_plans(id) ON DELETE SET NULL;

CREATE INDEX conversation_runs_plan_started
    ON conversation_runs (plan_id, started_at DESC)
    WHERE plan_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS conversation_runs_plan_started;
ALTER TABLE conversation_runs DROP COLUMN IF EXISTS plan_id;
DROP INDEX IF EXISTS conversation_plans_one_current;
DROP TABLE IF EXISTS conversation_plans;
ALTER TABLE conversation_runs
    DROP CONSTRAINT IF EXISTS conversation_runs_interaction_mode_check,
    DROP COLUMN IF EXISTS interaction_mode;
-- +goose StatementEnd
