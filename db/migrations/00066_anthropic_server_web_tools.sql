-- +goose Up
-- +goose StatementBegin
ALTER TABLE llm_model_routes
    ADD COLUMN anthropic_server_web_tools_enabled BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE llm_model_routes AS route
SET anthropic_server_web_tools_enabled = TRUE
FROM llm_providers AS provider
WHERE provider.id = route.provider_id
    AND route.protocol = 'anthropic-messages'
    AND lower(btrim(provider.base_url)) ~ '^https://api[.]anthropic[.]com(:[0-9]+)?([/?#]|$)';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE llm_model_routes
    DROP COLUMN IF EXISTS anthropic_server_web_tools_enabled;
-- +goose StatementEnd
