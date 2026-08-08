-- +goose Up
-- +goose StatementBegin
-- Local Project workspaces used to be authoritative, single-task repositories.
-- They cannot be reinterpreted as clones of the new internal SCM, so remove
-- only Local Project data. GitHub Projects remain intact.
DELETE FROM conversations
WHERE project_id IN (SELECT id FROM projects WHERE repository_provider = 'local');
DELETE FROM projects WHERE repository_provider = 'local';

DROP INDEX IF EXISTS idx_projects_primary_conversation;
ALTER TABLE projects DROP COLUMN IF EXISTS primary_conversation_id;
ALTER TABLE projects DROP COLUMN IF EXISTS github_publish_status;
ALTER TABLE projects
    ADD COLUMN repository_clone_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN repository_token_id BIGINT,
    ADD COLUMN repository_token_ciphertext TEXT NOT NULL DEFAULT '';

CREATE TABLE project_change_requests (
    conversation_id    TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    project_id         UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider           TEXT NOT NULL CHECK (provider IN ('local', 'github')),
    external_number    BIGINT,
    external_url       TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'working' CHECK (status IN (
        'working', 'open', 'checks_pending', 'conflict', 'merged', 'closed', 'failed'
    )),
    base_sha           TEXT NOT NULL DEFAULT '',
    head_sha           TEXT NOT NULL DEFAULT '',
    merge_sha          TEXT NOT NULL DEFAULT '',
    error_code         TEXT NOT NULL DEFAULT '',
    version            BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    merged_at          TIMESTAMPTZ,
    UNIQUE (project_id, external_number)
);
CREATE INDEX idx_project_change_requests_project
    ON project_change_requests (project_id, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_change_requests;
ALTER TABLE projects DROP COLUMN IF EXISTS repository_token_ciphertext;
ALTER TABLE projects DROP COLUMN IF EXISTS repository_token_id;
ALTER TABLE projects DROP COLUMN IF EXISTS repository_clone_url;
ALTER TABLE projects
    ADD COLUMN primary_conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL;
ALTER TABLE projects
    ADD COLUMN github_publish_status TEXT NOT NULL DEFAULT 'unpublished'
    CHECK (github_publish_status IN ('unpublished', 'pending', 'published'));
CREATE UNIQUE INDEX idx_projects_primary_conversation
    ON projects (primary_conversation_id)
    WHERE primary_conversation_id IS NOT NULL;
-- Deleted Local Project data is intentionally not recreated.
-- +goose StatementEnd
