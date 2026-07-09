-- +goose Up
-- +goose StatementBegin
-- Give each login principal an authoritative team/tenant. This becomes the
-- source of truth for the "ten" claim in minted runtime tokens, so tenant-level
-- quota, usage attribution, and team stats no longer depend on a per-request
-- caller-supplied tenant string. Empty string means "no team assigned".

ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_auth_users_tenant_id ON auth_users (tenant_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_auth_users_tenant_id;
ALTER TABLE auth_users DROP COLUMN IF EXISTS tenant_id;
-- +goose StatementEnd
