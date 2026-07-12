-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversation_runs
    ADD COLUMN IF NOT EXISTS client_request_id TEXT NOT NULL DEFAULT '';

-- A request-bound MVP process cannot hand an in-flight execution to the new
-- background runner. Close those rows honestly before enforcing single-flight.
UPDATE conversation_runs
SET status = 'interrupted', error_code = 'MIGRATION_INTERRUPTED',
    completed_at = COALESCE(completed_at, now()),
    last_activity_at = now(), updated_at = now()
WHERE status = 'running';

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_runs_client_request
    ON conversation_runs (user_id, conversation_id, client_request_id)
    WHERE client_request_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_runs_single_flight
    ON conversation_runs (conversation_id)
    WHERE conversation_id <> '' AND status = 'running';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_conversation_runs_single_flight;
DROP INDEX IF EXISTS idx_conversation_runs_client_request;
ALTER TABLE conversation_runs DROP COLUMN IF EXISTS client_request_id;
-- +goose StatementEnd
