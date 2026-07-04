-- +goose Up
-- +goose StatementBegin
-- Soft-delete auth users without releasing their login identifiers. Keeping
-- username/email uniqueness across deleted users preserves audit identity and
-- prevents a new person from inheriting another user's historical identity.

ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS deleted_by TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_auth_users_deleted_at ON auth_users (deleted_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_auth_users_deleted_at;
ALTER TABLE auth_users DROP COLUMN IF EXISTS deleted_by;
ALTER TABLE auth_users DROP COLUMN IF EXISTS deleted_at;
-- +goose StatementEnd
