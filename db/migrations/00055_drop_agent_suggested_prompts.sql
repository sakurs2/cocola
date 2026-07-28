-- +goose Up
-- +goose StatementBegin
ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_suggested_prompts_check,
    DROP COLUMN IF EXISTS suggested_prompts;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agents
    ADD COLUMN suggested_prompts JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT agents_suggested_prompts_check CHECK (
        jsonb_typeof(suggested_prompts) = 'array'
        AND jsonb_array_length(suggested_prompts) <= 4
    );
-- +goose StatementEnd
