-- +goose Up
-- +goose StatementBegin
-- Test-stage destructive migration: runtime bindings and partial MinIO
-- checkpoints are deliberately not carried into the node-local storage model.
TRUNCATE TABLE session_map;

-- Legacy metadata-only Skills cannot be materialized into the persistent
-- runtime directories. Keep them in the catalog, but require a valid payload
-- before they can be enabled again.
UPDATE skill_entries
SET enabled = FALSE
WHERE enabled = TRUE
  AND COALESCE(bundle_object_key, '') = ''
  AND COALESCE(skill_md, '') = '';

DELETE FROM system_settings
WHERE key IN ('sandbox.warm_pool_enabled', 'sandbox.warm_pool_size');

ALTER TABLE session_map
    DROP COLUMN IF EXISTS checkpoint_object_key,
    DROP COLUMN IF EXISTS checkpoint_status,
    DROP COLUMN IF EXISTS checkpoint_size_bytes,
    DROP COLUMN IF EXISTS checkpoint_error,
    DROP COLUMN IF EXISTS checkpoint_updated_at;

DROP TABLE IF EXISTS session_storage;

CREATE TABLE session_storage (
    storage_id UUID PRIMARY KEY,
    session_id TEXT UNIQUE NOT NULL,
    user_id TEXT NOT NULL,
    pvc_namespace TEXT NOT NULL,
    pvc_name TEXT UNIQUE NOT NULL,
    node_name TEXT NOT NULL,
    generation BIGINT NOT NULL DEFAULT 1 CHECK (generation > 0),
    requested_bytes BIGINT NOT NULL CHECK (requested_bytes > 0),
    last_reset_reason TEXT,
    last_reset_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_session_storage_user_id ON session_storage (user_id);
CREATE INDEX idx_session_storage_node_name ON session_storage (node_name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- This migration is intentionally irreversible. Test-stage storage data is
-- discarded rather than maintaining a second checkpoint-based rollback path.
-- +goose StatementEnd
