-- +goose Up
-- +goose StatementBegin
-- Some development databases briefly ran the abandoned distributed-run
-- migration 00029. Converge both those databases and fresh installs on the
-- single-Gateway model: one idempotency key, no event log and no worker lease.
DROP TABLE IF EXISTS conversation_run_events;
DROP INDEX IF EXISTS idx_conversation_runs_lease;
DROP INDEX IF EXISTS idx_conversation_runs_single_flight;

-- Session ownership became mandatory after the MVP. Recover rows whose owner
-- is unambiguous from the owning conversation; discard unsafe orphaned rows so
-- the next request rebuilds them without exposing another user's context.
UPDATE session_map AS sm
SET user_id = c.user_id, updated_at = now()
FROM conversations AS c
WHERE sm.session_id = c.id AND sm.user_id = '' AND c.user_id <> '';
DELETE FROM session_map WHERE user_id = '';

UPDATE conversation_runs
SET status = 'interrupted', error_code = 'MIGRATION_INTERRUPTED',
    completed_at = COALESCE(completed_at, now()),
    last_activity_at = now(), updated_at = now()
WHERE status IN ('running', 'cancelling');

ALTER TABLE conversation_runs DROP CONSTRAINT IF EXISTS conversation_runs_status_check;
ALTER TABLE conversation_runs ADD CONSTRAINT conversation_runs_status_check
    CHECK (status IN ('running', 'success', 'error', 'cancelled', 'interrupted'));

ALTER TABLE conversation_runs
    ADD COLUMN IF NOT EXISTS client_request_id TEXT NOT NULL DEFAULT '',
    DROP COLUMN IF EXISTS worker_id,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS last_event_seq;

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_runs_client_request
    ON conversation_runs (user_id, conversation_id, client_request_id)
    WHERE client_request_id <> '';
CREATE UNIQUE INDEX idx_conversation_runs_single_flight
    ON conversation_runs (conversation_id)
    WHERE conversation_id <> '' AND status = 'running';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_conversation_runs_single_flight;
-- The removed distributed fields and event table are intentionally not
-- recreated: this migration is a one-way convergence to the simpler model.
-- +goose StatementEnd
