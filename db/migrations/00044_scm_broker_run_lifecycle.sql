-- +goose Up
-- +goose StatementBegin
CREATE TABLE scm_broker_runs (
    run_id           TEXT PRIMARY KEY REFERENCES conversation_runs(trace_id) ON DELETE CASCADE,
    tenant_id        TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL,
    conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_id    BIGINT NOT NULL,
    registration_id  UUID NOT NULL REFERENCES scm_app_registrations(id) ON DELETE CASCADE,
    expires_at       TIMESTAMPTZ NOT NULL,
    revoked_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_scm_broker_runs_active
    ON scm_broker_runs (run_id, expires_at)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS scm_broker_runs;
-- +goose StatementEnd
