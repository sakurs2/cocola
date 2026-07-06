-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS trace_events (
    id             BIGSERIAL PRIMARY KEY,
    trace_id       TEXT NOT NULL,
    service        TEXT NOT NULL DEFAULT '',
    name           TEXT NOT NULL DEFAULT '',
    category       TEXT NOT NULL DEFAULT '',
    started_at     TIMESTAMPTZ NOT NULL,
    duration_ms    BIGINT NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT '',
    metadata_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_trace_events_trace_started
    ON trace_events (trace_id, started_at ASC, id ASC);
CREATE INDEX IF NOT EXISTS idx_trace_events_started
    ON trace_events (started_at DESC, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_trace_events_started;
DROP INDEX IF EXISTS idx_trace_events_trace_started;
DROP TABLE IF EXISTS trace_events;
-- +goose StatementEnd
