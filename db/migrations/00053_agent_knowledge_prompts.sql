-- +goose Up
-- +goose StatementBegin
ALTER TABLE agents
    ADD COLUMN knowledge_sources JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN suggested_prompts JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE agents
    ADD CONSTRAINT agents_knowledge_sources_check CHECK (
        jsonb_typeof(knowledge_sources) = 'array'
        AND jsonb_array_length(knowledge_sources) <= 10
    ),
    ADD CONSTRAINT agents_suggested_prompts_check CHECK (
        jsonb_typeof(suggested_prompts) = 'array'
        AND jsonb_array_length(suggested_prompts) <= 4
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_suggested_prompts_check,
    DROP CONSTRAINT IF EXISTS agents_knowledge_sources_check,
    DROP COLUMN IF EXISTS suggested_prompts,
    DROP COLUMN IF EXISTS knowledge_sources;
-- +goose StatementEnd
