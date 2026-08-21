-- +goose Up
-- +goose StatementBegin
ALTER TABLE channel_registration_flows
    ALTER COLUMN agent_id DROP NOT NULL;
DROP INDEX IF EXISTS channel_registration_flows_active_agent;
CREATE UNIQUE INDEX channel_registration_flows_active_agent
    ON channel_registration_flows (agent_id)
    WHERE agent_id IS NOT NULL
        AND status IN ('starting', 'awaiting_user', 'authorizing');
CREATE UNIQUE INDEX channel_registration_flows_active_workspace
    ON channel_registration_flows (tenant_id, user_id, provider)
    WHERE agent_id IS NULL
        AND status IN ('starting', 'awaiting_user', 'authorizing');

ALTER TABLE channel_connectors
    DROP CONSTRAINT IF EXISTS channel_connectors_agent_unique,
    ALTER COLUMN agent_id DROP NOT NULL;
CREATE UNIQUE INDEX channel_connectors_agent_unique
    ON channel_connectors (agent_id)
    WHERE agent_id IS NOT NULL;
CREATE UNIQUE INDEX channel_connectors_workspace_unique
    ON channel_connectors (tenant_id, user_id, provider)
    WHERE agent_id IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM channel_registration_flows WHERE agent_id IS NULL;
DELETE FROM channel_connectors WHERE agent_id IS NULL;

DROP INDEX IF EXISTS channel_registration_flows_active_workspace;
DROP INDEX IF EXISTS channel_registration_flows_active_agent;
CREATE UNIQUE INDEX channel_registration_flows_active_agent
    ON channel_registration_flows (agent_id)
    WHERE status IN ('starting', 'awaiting_user', 'authorizing');
ALTER TABLE channel_registration_flows
    ALTER COLUMN agent_id SET NOT NULL;

DROP INDEX IF EXISTS channel_connectors_workspace_unique;
DROP INDEX IF EXISTS channel_connectors_agent_unique;
ALTER TABLE channel_connectors
    ALTER COLUMN agent_id SET NOT NULL,
    ADD CONSTRAINT channel_connectors_agent_unique UNIQUE (agent_id);
-- +goose StatementEnd
