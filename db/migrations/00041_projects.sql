-- +goose Up
-- +goose StatementBegin
CREATE TABLE scm_connections (
    id                         UUID PRIMARY KEY,
    tenant_id                  TEXT NOT NULL DEFAULT '',
    user_id                    TEXT NOT NULL,
    provider                   TEXT NOT NULL CHECK (provider = 'github'),
    external_user_id           BIGINT NOT NULL,
    external_login             TEXT NOT NULL,
    installation_id            BIGINT,
    access_token_ciphertext    TEXT NOT NULL,
    access_token_expires_at    TIMESTAMPTZ,
    refresh_token_ciphertext   TEXT NOT NULL DEFAULT '',
    refresh_token_expires_at   TIMESTAMPTZ,
    status                     TEXT NOT NULL CHECK (status IN (
        'installation_required', 'ready', 'reauthorization_required'
    )),
    created_at                 TIMESTAMPTZ NOT NULL,
    updated_at                 TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, user_id, provider),
    UNIQUE (tenant_id, provider, external_user_id)
);

CREATE TABLE scm_oauth_states (
    nonce_hash  TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL DEFAULT '',
    user_id     TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_scm_oauth_states_expiry ON scm_oauth_states (expires_at);

CREATE TABLE projects (
    id                       UUID PRIMARY KEY,
    tenant_id                TEXT NOT NULL DEFAULT '',
    owner_user_id            TEXT NOT NULL,
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    runtime_id               TEXT NOT NULL DEFAULT 'claude-code',
    repository_mode          TEXT NOT NULL CHECK (repository_mode IN ('create', 'import')),
    repository_provider      TEXT NOT NULL DEFAULT 'github' CHECK (repository_provider = 'github'),
    repository_external_id   BIGINT,
    repository_owner         TEXT NOT NULL DEFAULT '',
    repository_name          TEXT NOT NULL,
    repository_html_url      TEXT NOT NULL DEFAULT '',
    installation_id          BIGINT,
    default_branch           TEXT NOT NULL DEFAULT '',
    visibility               TEXT NOT NULL CHECK (visibility IN ('private', 'public')),
    repository_size_kb       BIGINT NOT NULL DEFAULT 0,
    repository_has_lfs       BOOLEAN NOT NULL DEFAULT FALSE,
    repository_has_submodule BOOLEAN NOT NULL DEFAULT FALSE,
    status                   TEXT NOT NULL CHECK (status IN ('provisioning', 'ready', 'failed', 'archived')),
    provision_error_code     TEXT NOT NULL DEFAULT '',
    provision_request_id     TEXT NOT NULL,
    provision_started_at     TIMESTAMPTZ NOT NULL,
    version                  BIGINT NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ NOT NULL,
    updated_at               TIMESTAMPTZ NOT NULL,
    archived_at              TIMESTAMPTZ,
    UNIQUE (tenant_id, owner_user_id, provision_request_id)
);

CREATE UNIQUE INDEX idx_projects_owner_name_active
    ON projects (tenant_id, owner_user_id, LOWER(name))
    WHERE status <> 'archived';
CREATE UNIQUE INDEX idx_projects_repository_active
    ON projects (tenant_id, repository_provider, repository_external_id)
    WHERE repository_external_id IS NOT NULL AND status <> 'archived';
CREATE INDEX idx_projects_owner_updated
    ON projects (tenant_id, owner_user_id, updated_at DESC);

ALTER TABLE conversations
    ADD COLUMN project_id UUID;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_folder_project_exclusive_check
    CHECK (folder_id IS NULL OR project_id IS NULL);
ALTER TABLE conversations
    ADD CONSTRAINT conversations_project_chat_type_check
    CHECK (project_id IS NULL OR chat_type = 'chat');
CREATE INDEX idx_conversations_project_updated
    ON conversations (project_id, updated_at DESC)
    WHERE project_id IS NOT NULL;

CREATE TABLE project_workspaces (
    conversation_id       TEXT PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    project_id            UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    base_ref              TEXT NOT NULL,
    base_sha              TEXT NOT NULL DEFAULT '',
    branch_name           TEXT NOT NULL,
    head_sha              TEXT NOT NULL DEFAULT '',
    bootstrap_status      TEXT NOT NULL DEFAULT 'pending' CHECK (bootstrap_status IN ('pending', 'ready', 'failed')),
    bootstrap_error_code  TEXT NOT NULL DEFAULT '',
    git_snapshot_json     JSONB NOT NULL DEFAULT '{}'::jsonb,
    git_snapshot_at       TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_project_workspaces_project
    ON project_workspaces (project_id, updated_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_workspaces;
DROP INDEX IF EXISTS idx_conversations_project_updated;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_project_chat_type_check;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_folder_project_exclusive_check;
ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_project_id_fkey;
ALTER TABLE conversations DROP COLUMN IF EXISTS project_id;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS scm_oauth_states;
DROP TABLE IF EXISTS scm_connections;
-- +goose StatementEnd
