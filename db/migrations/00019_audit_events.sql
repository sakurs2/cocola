-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS audit_events (
    id             BIGSERIAL PRIMARY KEY,
    ts             TIMESTAMPTZ NOT NULL,
    actor_type     TEXT NOT NULL DEFAULT '',
    actor_user_id  TEXT NOT NULL DEFAULT '',
    actor_email    TEXT NOT NULL DEFAULT '',
    action         TEXT NOT NULL DEFAULT '',
    resource_type  TEXT NOT NULL DEFAULT '',
    resource_id    TEXT NOT NULL DEFAULT '',
    result         TEXT NOT NULL DEFAULT '',
    http_method    TEXT NOT NULL DEFAULT '',
    route          TEXT NOT NULL DEFAULT '',
    status_code    INTEGER NOT NULL DEFAULT 0,
    request_id     TEXT NOT NULL DEFAULT '',
    trace_id       TEXT NOT NULL DEFAULT '',
    client_ip      TEXT NOT NULL DEFAULT '',
    user_agent     TEXT NOT NULL DEFAULT '',
    metadata_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_audit_events_ts
    ON audit_events (ts DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_ts
    ON audit_events (actor_user_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_action_ts
    ON audit_events (action, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_resource_ts
    ON audit_events (resource_type, resource_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_trace_id
    ON audit_events (trace_id)
    WHERE trace_id <> '';
CREATE INDEX IF NOT EXISTS idx_audit_events_request_id
    ON audit_events (request_id)
    WHERE request_id <> '';

INSERT INTO audit_events (
    id, ts, actor_type, actor_user_id, actor_email, action, resource_type,
    resource_id, result, metadata_json
)
SELECT
    id,
    ts,
    'admin',
    actor,
    actor,
    action,
    split_part(action, '.', 1),
    resource,
    'success',
    jsonb_build_object('detail', detail, 'legacy_table', 'audit_log')
FROM audit_log
WHERE NOT EXISTS (
    SELECT 1 FROM audit_events e
    WHERE e.metadata_json->>'legacy_table' = 'audit_log'
      AND e.id = audit_log.id
);

SELECT setval(
    pg_get_serial_sequence('audit_events', 'id'),
    GREATEST(
        COALESCE((SELECT MAX(id) FROM audit_events), 1),
        COALESCE((SELECT last_value FROM audit_log_id_seq), 1)
    ),
    true
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_audit_events_request_id;
DROP INDEX IF EXISTS idx_audit_events_trace_id;
DROP INDEX IF EXISTS idx_audit_events_resource_ts;
DROP INDEX IF EXISTS idx_audit_events_action_ts;
DROP INDEX IF EXISTS idx_audit_events_actor_ts;
DROP INDEX IF EXISTS idx_audit_events_ts;
DROP TABLE IF EXISTS audit_events;
-- +goose StatementEnd
