-- +goose Up
ALTER TABLE skill_entries
	ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'admin',
	ADD COLUMN IF NOT EXISTS owner_user_id TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'manual',
	ADD COLUMN IF NOT EXISTS source_url TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS source_ref TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS source_path TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS bundle_object_key TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS content_sha256 TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS manifest_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	ADD COLUMN IF NOT EXISTS frontmatter_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	ADD COLUMN IF NOT EXISTS skill_md TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS file_count INTEGER NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS size_bytes BIGINT NOT NULL DEFAULT 0,
	ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS updated_by TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_skill_entries_scope_owner ON skill_entries(scope, owner_user_id, enabled);
CREATE INDEX IF NOT EXISTS idx_skill_entries_content_sha256 ON skill_entries(content_sha256);

CREATE TABLE IF NOT EXISTS user_skill_preferences (
	user_id TEXT NOT NULL,
	skill_id TEXT NOT NULL REFERENCES skill_entries(id) ON DELETE CASCADE,
	enabled BOOLEAN NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (user_id, skill_id)
);

-- +goose Down
DROP TABLE IF EXISTS user_skill_preferences;
DROP INDEX IF EXISTS idx_skill_entries_content_sha256;
DROP INDEX IF EXISTS idx_skill_entries_scope_owner;
ALTER TABLE skill_entries
	DROP COLUMN IF EXISTS updated_by,
	DROP COLUMN IF EXISTS created_by,
	DROP COLUMN IF EXISTS size_bytes,
	DROP COLUMN IF EXISTS file_count,
	DROP COLUMN IF EXISTS skill_md,
	DROP COLUMN IF EXISTS frontmatter_json,
	DROP COLUMN IF EXISTS manifest_json,
	DROP COLUMN IF EXISTS content_sha256,
	DROP COLUMN IF EXISTS bundle_object_key,
	DROP COLUMN IF EXISTS source_path,
	DROP COLUMN IF EXISTS source_ref,
	DROP COLUMN IF EXISTS source_url,
	DROP COLUMN IF EXISTS source_type,
	DROP COLUMN IF EXISTS owner_user_id,
	DROP COLUMN IF EXISTS scope;
