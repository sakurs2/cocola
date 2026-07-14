-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM llm_providers
        WHERE LOWER(TRIM(type)) = 'openai_compat'
    ) THEN
        RAISE EXCEPTION 'remove OpenAI Chat Completions providers before upgrading Cocola';
    END IF;
END $$;

ALTER TABLE llm_providers
    DROP CONSTRAINT IF EXISTS llm_providers_type_check;
ALTER TABLE llm_providers
    ADD CONSTRAINT llm_providers_type_check
    CHECK (type IN ('anthropic', 'openai_responses'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE llm_providers
    DROP CONSTRAINT IF EXISTS llm_providers_type_check;
ALTER TABLE llm_providers
    ADD CONSTRAINT llm_providers_type_check
    CHECK (type IN ('anthropic', 'openai_compat', 'openai_responses'));
-- +goose StatementEnd
