-- +goose Up
-- +goose StatementBegin
ALTER TABLE projects DROP CONSTRAINT projects_status_check;
ALTER TABLE projects
    ADD CONSTRAINT projects_status_check CHECK (status IN (
        'provisioning', 'ready', 'failed', 'archiving', 'archive_failed', 'archived'
    )),
    ADD COLUMN provision_attempt_id UUID,
    ADD COLUMN provision_attempt_started_at TIMESTAMPTZ,
    ADD COLUMN archive_attempt_id UUID,
    ADD COLUMN archive_error_code TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE projects
SET status = 'failed', archive_error_code = ''
WHERE status IN ('archiving', 'archive_failed');
ALTER TABLE projects DROP CONSTRAINT projects_status_check;
ALTER TABLE projects
    ADD CONSTRAINT projects_status_check CHECK (status IN (
        'provisioning', 'ready', 'failed', 'archived'
    )),
    DROP COLUMN archive_error_code,
    DROP COLUMN archive_attempt_id,
    DROP COLUMN provision_attempt_started_at,
    DROP COLUMN provision_attempt_id;
-- +goose StatementEnd
