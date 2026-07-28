-- +goose Up
-- +goose StatementBegin
ALTER TABLE agents
    ADD COLUMN skill_ids JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE agents
    ADD CONSTRAINT agents_skill_ids_check CHECK (
        jsonb_typeof(skill_ids) = 'array'
        AND jsonb_array_length(skill_ids) <= 32
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_skill_ids_check,
    DROP COLUMN IF EXISTS skill_ids;
-- +goose StatementEnd
