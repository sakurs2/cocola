-- +goose Up
ALTER TABLE mcp_servers
	ADD COLUMN IF NOT EXISTS url_var_ciphertext_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	ADD COLUMN IF NOT EXISTS url_var_hint_json JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE mcp_servers
	DROP COLUMN IF EXISTS url_var_hint_json,
	DROP COLUMN IF EXISTS url_var_ciphertext_json;
