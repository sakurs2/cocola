-- +goose Up
-- +goose StatementBegin
-- A route is an independent resource. Alias is only unique inside one
-- provider; callers use the immutable route id so two providers can expose the
-- same alias without making routing ambiguous.
ALTER TABLE llm_model_routes
    ADD COLUMN id TEXT;
UPDATE llm_model_routes SET id = alias WHERE id IS NULL OR id = '';
ALTER TABLE llm_model_routes ALTER COLUMN id SET NOT NULL;

ALTER TABLE llm_model_routes
    ADD COLUMN protocol TEXT;
UPDATE llm_model_routes r
SET protocol = CASE p.type
    WHEN 'openai_responses' THEN 'openai-responses'
    ELSE 'anthropic-messages'
END
FROM llm_providers p
WHERE p.id = r.provider_id;
ALTER TABLE llm_model_routes ALTER COLUMN protocol SET NOT NULL;

ALTER TABLE llm_model_routes DROP CONSTRAINT llm_model_routes_pkey;
ALTER TABLE llm_model_routes ADD PRIMARY KEY (id);
CREATE UNIQUE INDEX idx_llm_model_routes_provider_alias
    ON llm_model_routes (provider_id, alias);

-- Each Agent model protocol has its own default. A single global default cannot
-- serve both Claude Code and Codex because they use different wire protocols.
DROP INDEX IF EXISTS idx_llm_model_routes_one_default;
CREATE UNIQUE INDEX idx_llm_model_routes_one_default
    ON llm_model_routes (protocol)
    WHERE is_default;

-- Keep aliases as display/audit snapshots while persisting the route identity
-- actually sent to the Agent Runtime. Existing ids equal their old aliases, so
-- in-flight browser state and historical scheduled tasks upgrade safely.
ALTER TABLE conversation_runs
    ADD COLUMN model_route_id TEXT NOT NULL DEFAULT '';
UPDATE conversation_runs SET model_route_id = model_alias
WHERE model_route_id = '' AND model_alias <> '';

ALTER TABLE scheduled_tasks
    ADD COLUMN model_route_id TEXT NOT NULL DEFAULT '';
UPDATE scheduled_tasks SET model_route_id = model_alias
WHERE model_route_id = '' AND model_alias <> '';

ALTER TABLE scheduled_task_runs
    ADD COLUMN model_route_id TEXT NOT NULL DEFAULT '';
UPDATE scheduled_task_runs SET model_route_id = model_alias
WHERE model_route_id = '' AND model_alias <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE scheduled_task_runs DROP COLUMN model_route_id;
ALTER TABLE scheduled_tasks DROP COLUMN model_route_id;
ALTER TABLE conversation_runs DROP COLUMN model_route_id;

DROP INDEX IF EXISTS idx_llm_model_routes_one_default;
DROP INDEX IF EXISTS idx_llm_model_routes_provider_alias;
ALTER TABLE llm_model_routes DROP CONSTRAINT llm_model_routes_pkey;
ALTER TABLE llm_model_routes DROP COLUMN protocol;
ALTER TABLE llm_model_routes DROP COLUMN id;
-- This statement intentionally fails if provider-scoped duplicate aliases were
-- created after the upgrade; rollback must never delete or merge user routes.
ALTER TABLE llm_model_routes ADD PRIMARY KEY (alias);
CREATE UNIQUE INDEX idx_llm_model_routes_one_default
    ON llm_model_routes ((is_default))
    WHERE is_default;
-- +goose StatementEnd
