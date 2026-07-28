-- +goose Up
-- +goose StatementBegin
ALTER TABLE agents
    ADD COLUMN knowledge_revision BIGINT NOT NULL DEFAULT 1,
    ADD CONSTRAINT agents_knowledge_revision_check CHECK (knowledge_revision > 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_knowledge_revision_check,
    DROP COLUMN IF EXISTS knowledge_revision;
-- +goose StatementEnd
