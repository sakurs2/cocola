-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM conversations WHERE runtime_id = 'codex') THEN
        RAISE EXCEPTION 'cannot retire Codex while Codex conversations still exist';
    END IF;
END $$;

-- OpenAI Responses is intentionally retired without a compatibility path.
-- Clear any disabled or seeded configuration before narrowing the constraints.
UPDATE memory_config
SET enabled = FALSE,
    extraction_model_route_id = NULL,
    embedding_model_route_id = NULL,
    version = version + 1,
    updated_at = now(),
    updated_by = 'retire-openai-responses-migration'
WHERE extraction_model_route_id IN (
        SELECT id FROM llm_model_routes WHERE protocol = 'openai-responses'
    )
   OR embedding_model_route_id IN (
        SELECT id FROM llm_model_routes WHERE protocol = 'openai-responses'
    );

DELETE FROM llm_model_routes
WHERE protocol = 'openai-responses'
   OR provider_id IN (
        SELECT id FROM llm_providers WHERE type = 'openai_responses'
    );

DELETE FROM llm_providers
WHERE type = 'openai_responses';

ALTER TABLE llm_providers
    DROP CONSTRAINT IF EXISTS llm_providers_type_check;
ALTER TABLE llm_providers
    ADD CONSTRAINT llm_providers_type_check
    CHECK (type IN ('anthropic', 'openai_embeddings'));

ALTER TABLE llm_model_routes
    ADD CONSTRAINT llm_model_routes_protocol_check
    CHECK (protocol IN ('anthropic-messages', 'openai-embeddings'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE llm_providers
    DROP CONSTRAINT IF EXISTS llm_providers_type_check;
ALTER TABLE llm_providers
    ADD CONSTRAINT llm_providers_type_check
    CHECK (type IN ('anthropic', 'openai_responses', 'openai_embeddings'));

ALTER TABLE llm_model_routes
    DROP CONSTRAINT IF EXISTS llm_model_routes_protocol_check;
-- +goose StatementEnd
