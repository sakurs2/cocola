-- +goose Up
CREATE TABLE IF NOT EXISTS agent_prompts (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	enabled BOOLEAN NOT NULL DEFAULT FALSE,
	scope TEXT NOT NULL DEFAULT 'global',
	priority INTEGER NOT NULL DEFAULT 100,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	created_by TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_prompts_effective
	ON agent_prompts (enabled, scope, priority, id);

-- +goose Down
DROP INDEX IF EXISTS idx_agent_prompts_effective;
DROP TABLE IF EXISTS agent_prompts;
