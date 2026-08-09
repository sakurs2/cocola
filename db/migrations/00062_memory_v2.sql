-- +goose Up
-- +goose StatementBegin
-- Memory was never released as a supported capability. Start the V2 contract
-- from a clean control plane instead of carrying provider-specific recovery
-- state from the disabled implementation.
TRUNCATE TABLE memory_capture_jobs, memory_user_settings;
DELETE FROM memory_index_state;
UPDATE memory_config
SET enabled = FALSE,
    extraction_model_route_id = NULL,
    embedding_model_route_id = NULL,
    version = version + 1,
    updated_at = now(),
    updated_by = 'memory-v2-migration';

ALTER TABLE memory_index_state
    ADD COLUMN embedding_model_route_id TEXT NOT NULL;

ALTER TABLE memory_capture_jobs
    RENAME COLUMN attempts TO attempt_count;
ALTER TABLE memory_capture_jobs
    RENAME CONSTRAINT memory_capture_jobs_attempts_check
    TO memory_capture_jobs_attempt_count_check;
ALTER TABLE memory_capture_jobs
    RENAME COLUMN openviking_session_id TO provider_session_id;
ALTER TABLE memory_capture_jobs
    RENAME COLUMN openviking_task_id TO provider_task_id;

DROP INDEX IF EXISTS idx_memory_capture_jobs_ready;
ALTER TABLE memory_capture_jobs
    DROP CONSTRAINT memory_capture_jobs_status_check;
ALTER TABLE memory_capture_jobs
    ADD CONSTRAINT memory_capture_jobs_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'dead', 'cancelled'));
CREATE INDEX idx_memory_capture_jobs_ready
    ON memory_capture_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
TRUNCATE TABLE memory_capture_jobs, memory_user_settings;
DELETE FROM memory_index_state;

DROP INDEX IF EXISTS idx_memory_capture_jobs_ready;
ALTER TABLE memory_capture_jobs
    DROP CONSTRAINT memory_capture_jobs_status_check;
ALTER TABLE memory_capture_jobs
    ADD CONSTRAINT memory_capture_jobs_status_check
    CHECK (status IN ('pending', 'submitted', 'completed', 'retry', 'dead', 'cancelled'));
ALTER TABLE memory_capture_jobs
    RENAME COLUMN attempt_count TO attempts;
ALTER TABLE memory_capture_jobs
    RENAME CONSTRAINT memory_capture_jobs_attempt_count_check
    TO memory_capture_jobs_attempts_check;
ALTER TABLE memory_capture_jobs
    RENAME COLUMN provider_session_id TO openviking_session_id;
ALTER TABLE memory_capture_jobs
    RENAME COLUMN provider_task_id TO openviking_task_id;
CREATE INDEX idx_memory_capture_jobs_ready
    ON memory_capture_jobs (next_attempt_at, created_at)
    WHERE status IN ('pending', 'submitted', 'retry');

ALTER TABLE memory_index_state
    DROP COLUMN embedding_model_route_id;
-- +goose StatementEnd
