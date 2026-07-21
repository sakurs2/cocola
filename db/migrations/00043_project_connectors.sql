-- +goose Up
-- +goose StatementBegin
-- Project and SCM were introduced while Cocola was still in its test stage.
-- The old rows are tied to one operator-owned GitHub App, so they cannot be
-- safely reinterpreted as per-user App registrations. Reset only Project data;
-- ordinary chats, folders, users and administrator configuration are retained.
DELETE FROM conversations WHERE project_id IS NOT NULL;
DELETE FROM projects;
DELETE FROM scm_oauth_states;
DELETE FROM scm_connections;

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_repository_mode_check;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_repository_provider_check;
ALTER TABLE projects
    ADD CONSTRAINT projects_repository_mode_check
    CHECK (repository_mode IN ('empty', 'create', 'import'));
ALTER TABLE projects
    ADD CONSTRAINT projects_repository_provider_check
    CHECK (repository_provider IN ('local', 'github'));
ALTER TABLE projects
    ADD COLUMN primary_conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL;
ALTER TABLE projects
    ADD COLUMN github_publish_status TEXT NOT NULL DEFAULT 'unpublished'
    CHECK (github_publish_status IN ('unpublished', 'pending', 'published'));
CREATE UNIQUE INDEX idx_projects_primary_conversation
    ON projects (primary_conversation_id)
    WHERE primary_conversation_id IS NOT NULL;

CREATE TABLE scm_app_registrations (
    id                         UUID PRIMARY KEY,
    tenant_id                  TEXT NOT NULL DEFAULT '',
    user_id                    TEXT NOT NULL,
    provider                   TEXT NOT NULL CHECK (provider = 'github'),
    app_id                     BIGINT NOT NULL,
    app_slug                   TEXT NOT NULL,
    client_id                  TEXT NOT NULL,
    client_secret_ciphertext   TEXT NOT NULL,
    private_key_ciphertext     TEXT NOT NULL,
    owner_external_id          BIGINT NOT NULL DEFAULT 0,
    owner_login                TEXT NOT NULL DEFAULT '',
    public_origin              TEXT NOT NULL,
    status                     TEXT NOT NULL CHECK (status IN (
        'app_created', 'installation_required', 'authorization_required',
        'ready', 'reauthorization_required', 'error'
    )),
    version                    BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at                 TIMESTAMPTZ NOT NULL,
    updated_at                 TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, user_id, provider),
    UNIQUE (provider, app_id)
);

CREATE TABLE scm_flow_states (
    nonce_hash       TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL,
    provider         TEXT NOT NULL CHECK (provider = 'github'),
    flow_type        TEXT NOT NULL CHECK (flow_type IN ('manifest', 'oauth', 'installation')),
    return_to        TEXT NOT NULL DEFAULT '/connectors',
    public_origin    TEXT NOT NULL,
    registration_id UUID REFERENCES scm_app_registrations(id) ON DELETE CASCADE,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_scm_flow_states_expiry ON scm_flow_states (expires_at);

ALTER TABLE scm_connections
    ADD COLUMN registration_id UUID REFERENCES scm_app_registrations(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX idx_scm_connections_registration
    ON scm_connections (registration_id)
    WHERE registration_id IS NOT NULL;

CREATE TABLE scm_approvals (
    id              UUID PRIMARY KEY,
    tenant_id       TEXT NOT NULL DEFAULT '',
    user_id         TEXT NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    run_id           TEXT NOT NULL,
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_id   BIGINT NOT NULL,
    command_digest  TEXT NOT NULL,
    command_category TEXT NOT NULL,
    command_label   TEXT NOT NULL,
    permissions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL CHECK (status IN (
        'pending', 'approved', 'denied', 'expired', 'consumed'
    )),
    expires_at      TIMESTAMPTZ NOT NULL,
    decided_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    UNIQUE (run_id, command_digest)
);
CREATE INDEX idx_scm_approvals_conversation_status
    ON scm_approvals (conversation_id, status, created_at DESC);

CREATE TABLE scm_token_leases (
    id               UUID PRIMARY KEY,
    approval_id      UUID REFERENCES scm_approvals(id) ON DELETE SET NULL,
    tenant_id        TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL,
    conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    run_id            TEXT NOT NULL,
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_id    BIGINT NOT NULL,
    command_category TEXT NOT NULL,
    permissions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    token_ciphertext TEXT NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    revoked_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_scm_token_leases_active
    ON scm_token_leases (run_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE scm_audit_events (
    id               BIGSERIAL PRIMARY KEY,
    tenant_id        TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL,
    project_id       UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_id    BIGINT NOT NULL,
    run_id            TEXT NOT NULL,
    command_category TEXT NOT NULL,
    permissions_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    result            TEXT NOT NULL,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_scm_audit_project_created
    ON scm_audit_events (project_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_scm_connections_registration;
DROP INDEX IF EXISTS idx_projects_primary_conversation;
DROP TABLE IF EXISTS scm_audit_events;
DROP TABLE IF EXISTS scm_token_leases;
DROP TABLE IF EXISTS scm_approvals;
ALTER TABLE scm_connections DROP COLUMN IF EXISTS registration_id;
DROP TABLE IF EXISTS scm_flow_states;
DROP TABLE IF EXISTS scm_app_registrations;
ALTER TABLE projects DROP COLUMN IF EXISTS primary_conversation_id;
ALTER TABLE projects DROP COLUMN IF EXISTS github_publish_status;

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_repository_mode_check;
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_repository_provider_check;
ALTER TABLE projects
    ADD CONSTRAINT projects_repository_mode_check
    CHECK (repository_mode IN ('create', 'import'));
ALTER TABLE projects
    ADD CONSTRAINT projects_repository_provider_check
    CHECK (repository_provider = 'github');
-- Deleted test-stage Project and SCM rows are intentionally not recreated.
-- +goose StatementEnd
