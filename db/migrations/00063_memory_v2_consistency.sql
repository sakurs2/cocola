-- +goose Up
-- +goose StatementBegin
ALTER TABLE memory_config
    ADD COLUMN resetting BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE memory_capture_jobs
    ADD COLUMN cancellation_requested BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE memory_index_state
    ADD COLUMN embedding_provider_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '',
    ADD COLUMN embedding_base_url TEXT NOT NULL DEFAULT '';

CREATE TABLE memory_reset_accounts (
    tenant_id  TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP INDEX IF EXISTS idx_memory_capture_jobs_ready;
ALTER TABLE memory_capture_jobs
    DROP CONSTRAINT memory_capture_jobs_status_check;
ALTER TABLE memory_capture_jobs
    ADD CONSTRAINT memory_capture_jobs_status_check
    CHECK (status IN (
        'pending', 'processing', 'submitting', 'cancel_requested',
        'completed', 'dead', 'cancelled'
    ));
CREATE INDEX idx_memory_capture_jobs_ready
    ON memory_capture_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing', 'submitting', 'cancel_requested');
CREATE INDEX idx_memory_capture_jobs_retention
    ON memory_capture_jobs (updated_at)
    WHERE status IN ('completed', 'cancelled', 'dead');

-- Memory V2 has not been released as a compatible data contract. The storage
-- layout also moves to a fresh OpenViking volume/prefix in this release, so
-- reset the control-plane references instead of retaining unusable locks.
TRUNCATE TABLE memory_capture_jobs, memory_user_settings, memory_reset_accounts;
DELETE FROM memory_index_state;
UPDATE memory_config
SET enabled = FALSE,
    resetting = FALSE,
    extraction_model_route_id = NULL,
    embedding_model_route_id = NULL,
    version = version + 1,
    updated_at = now(),
    updated_by = 'memory-v2-consistency-migration';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_memory_capture_jobs_retention;
DROP INDEX IF EXISTS idx_memory_capture_jobs_ready;
UPDATE memory_capture_jobs
SET status = 'cancelled'
WHERE status IN ('submitting', 'cancel_requested');
ALTER TABLE memory_capture_jobs
    DROP CONSTRAINT memory_capture_jobs_status_check;
ALTER TABLE memory_capture_jobs
    ADD CONSTRAINT memory_capture_jobs_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'dead', 'cancelled'));
CREATE INDEX idx_memory_capture_jobs_ready
    ON memory_capture_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing');
ALTER TABLE memory_capture_jobs
    DROP COLUMN cancellation_requested;

DROP TABLE IF EXISTS memory_reset_accounts;
ALTER TABLE memory_index_state
    DROP COLUMN embedding_provider_id,
    DROP COLUMN embedding_model,
    DROP COLUMN embedding_base_url;
ALTER TABLE memory_config
    DROP COLUMN resetting;
-- +goose StatementEnd
