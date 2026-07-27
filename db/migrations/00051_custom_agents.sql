-- +goose Up
-- +goose StatementBegin
CREATE TABLE agents (
    id             UUID PRIMARY KEY,
    tenant_id      TEXT NOT NULL DEFAULT '',
    owner_user_id  TEXT NOT NULL,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    instructions   TEXT NOT NULL DEFAULT '',
    avatar_key     TEXT NOT NULL DEFAULT 'sparkle',
    avatar_color   TEXT NOT NULL DEFAULT 'blue',
    runtime_id     TEXT NOT NULL,
    model_route_id TEXT NOT NULL,
    model_alias    TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    version        BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL,
    archived_at    TIMESTAMPTZ,
    CONSTRAINT agents_status_check CHECK (status IN ('active', 'archived')),
    CONSTRAINT agents_avatar_key_check CHECK (
        avatar_key IN ('sparkle', 'robot', 'code', 'chart', 'document', 'search', 'briefcase', 'support')
    ),
    CONSTRAINT agents_avatar_color_check CHECK (
        avatar_color IN ('slate', 'blue', 'cyan', 'emerald', 'amber', 'orange', 'rose', 'violet')
    ),
    CONSTRAINT agents_content_check CHECK (
        char_length(name) BETWEEN 1 AND 100
        AND char_length(description) <= 500
        AND octet_length(instructions) <= 32768
    ),
    CONSTRAINT agents_runtime_model_check CHECK (
        char_length(runtime_id) BETWEEN 1 AND 256
        AND char_length(model_route_id) BETWEEN 1 AND 256
        AND char_length(model_alias) BETWEEN 1 AND 256
    )
);

CREATE UNIQUE INDEX agents_owner_name_active
    ON agents (tenant_id, owner_user_id, LOWER(name))
    WHERE status = 'active';
CREATE INDEX agents_owner_updated
    ON agents (tenant_id, owner_user_id, updated_at DESC, id DESC);

-- This is a test-stage schema transition. Existing singleton Feishu connectors
-- have no unambiguous Agent owner, so they are intentionally removed instead
-- of being attached to a synthetic/default Agent.
DELETE FROM channel_registration_flows;
DELETE FROM channel_connectors;

DROP INDEX IF EXISTS channel_registration_flows_active_user;
ALTER TABLE channel_registration_flows
    ADD COLUMN agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX channel_registration_flows_active_agent
    ON channel_registration_flows (agent_id)
    WHERE status IN ('starting', 'awaiting_user', 'authorizing');
CREATE INDEX channel_registration_flows_agent_created
    ON channel_registration_flows (agent_id, created_at DESC);

ALTER TABLE channel_connectors
    DROP CONSTRAINT IF EXISTS channel_connectors_owner_unique,
    DROP CONSTRAINT IF EXISTS channel_connectors_model_check,
    DROP COLUMN IF EXISTS model_alias,
    DROP COLUMN IF EXISTS model_route_id,
    ADD COLUMN agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE RESTRICT;
ALTER TABLE channel_connectors
    ADD CONSTRAINT channel_connectors_agent_unique UNIQUE (agent_id);
CREATE INDEX channel_connectors_owner
    ON channel_connectors (tenant_id, user_id, updated_at DESC);

ALTER TABLE conversations
    ADD COLUMN agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    ADD COLUMN agent_version BIGINT,
    ADD COLUMN agent_snapshot_json JSONB,
    ADD COLUMN channel_connector_id UUID REFERENCES channel_connectors(id) ON DELETE SET NULL;
ALTER TABLE conversations
    ADD CONSTRAINT conversations_agent_snapshot_check CHECK (
        (
            agent_id IS NULL
            AND agent_version IS NULL
            AND agent_snapshot_json IS NULL
        )
        OR
        (
            agent_id IS NOT NULL
            AND agent_version > 0
            AND agent_snapshot_json IS NOT NULL
        )
    );
CREATE INDEX conversations_agent_updated
    ON conversations (agent_id, updated_at DESC)
    WHERE agent_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE conversations
    DROP CONSTRAINT IF EXISTS conversations_agent_snapshot_check,
    DROP CONSTRAINT IF EXISTS conversations_channel_connector_id_fkey,
    DROP CONSTRAINT IF EXISTS conversations_agent_id_fkey;
DROP INDEX IF EXISTS conversations_agent_updated;
ALTER TABLE conversations
    DROP COLUMN IF EXISTS channel_connector_id,
    DROP COLUMN IF EXISTS agent_snapshot_json,
    DROP COLUMN IF EXISTS agent_version,
    DROP COLUMN IF EXISTS agent_id;

DELETE FROM channel_registration_flows;
DELETE FROM channel_connectors;

DROP INDEX IF EXISTS channel_registration_flows_agent_created;
DROP INDEX IF EXISTS channel_registration_flows_active_agent;
ALTER TABLE channel_registration_flows DROP COLUMN IF EXISTS agent_id;
CREATE UNIQUE INDEX channel_registration_flows_active_user
    ON channel_registration_flows (tenant_id, user_id, provider)
    WHERE status IN ('starting', 'awaiting_user', 'authorizing');

DROP INDEX IF EXISTS channel_connectors_owner;
ALTER TABLE channel_connectors
    DROP CONSTRAINT IF EXISTS channel_connectors_agent_unique,
    DROP COLUMN IF EXISTS agent_id,
    ADD COLUMN model_route_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN model_alias TEXT NOT NULL DEFAULT '';
ALTER TABLE channel_connectors
    ADD CONSTRAINT channel_connectors_owner_unique UNIQUE (tenant_id, user_id, provider),
    ADD CONSTRAINT channel_connectors_model_check CHECK (
        char_length(model_route_id) <= 256
        AND char_length(model_alias) <= 256
        AND (
            (model_route_id = '' AND model_alias = '')
            OR (model_route_id <> '' AND model_alias <> '')
        )
    );

DROP TABLE IF EXISTS agents;
-- +goose StatementEnd
