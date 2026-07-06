-- +goose Up
CREATE TABLE IF NOT EXISTS mcp_servers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	transport TEXT NOT NULL,
	command TEXT NOT NULL DEFAULT '',
	args_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	url TEXT NOT NULL DEFAULT '',
	env_ciphertext_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	env_hint_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	header_ciphertext_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	header_hint_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	enabled BOOLEAN NOT NULL DEFAULT TRUE,
	default_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	source TEXT NOT NULL DEFAULT 'admin',
	status TEXT NOT NULL DEFAULT 'active',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	created_by TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_mcp_servers_enabled_default ON mcp_servers(enabled, default_enabled);

CREATE TABLE IF NOT EXISTS user_mcp_preferences (
	user_id TEXT NOT NULL,
	mcp_id TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
	enabled BOOLEAN NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (user_id, mcp_id)
);

-- +goose Down
DROP TABLE IF EXISTS user_mcp_preferences;
DROP INDEX IF EXISTS idx_mcp_servers_enabled_default;
DROP TABLE IF EXISTS mcp_servers;
