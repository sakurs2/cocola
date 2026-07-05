-- +goose Up
-- +goose StatementBegin
-- cocola persistence schema v1 (M7).
--
-- Single source of truth for ALL services' relational state. Go services apply
-- this via goose (embedded); Python services run psycopg against the same DB
-- and MUST NOT redefine schema -- they assume goose has applied this file.
--
-- Backend selection is per-service: each service uses Postgres only when its
-- COCOLA_PG_DSN is set, otherwise it falls back to its in-memory store. This
-- migration is owned by whichever service boots first with a DSN (admin-api in
-- the full stack); applying it twice is a no-op (goose tracks versions).
--
-- Domains share one database but are namespaced by table name prefix:
--   admin: token_records, quota_overrides, skill_entries, audit_log
--   gateway: usage_ledger, quota_counters
--   agent-runtime: session_map

-- ===== admin domain =====

-- Metadata about minted tokens (the token string itself is never stored).
CREATE TABLE IF NOT EXISTS token_records (
    id          TEXT PRIMARY KEY,            -- jti-like opaque id + revocation key
    user_id     TEXT NOT NULL DEFAULT '',    -- sub
    tenant_id   TEXT NOT NULL DEFAULT '',    -- ten
    issuer      TEXT NOT NULL DEFAULT '',    -- iss
    issued_at   TIMESTAMPTZ NOT NULL,        -- iat
    expires_at  TIMESTAMPTZ,                 -- NULL = non-expiring
    revoked     BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at  TIMESTAMPTZ,                 -- NULL when not revoked
    created_by  TEXT NOT NULL DEFAULT ''     -- admin who minted it
);
CREATE INDEX IF NOT EXISTS idx_token_records_user_id ON token_records (user_id);
CREATE INDEX IF NOT EXISTS idx_token_records_tenant_id ON token_records (tenant_id);

-- Per-subject quota caps superseding the gateway's static env defaults.
-- limit_tokens = 0 means "explicitly unlimited" for that subject.
CREATE TABLE IF NOT EXISTS quota_overrides (
    scope        TEXT NOT NULL,              -- 'user' | 'tenant'
    subject      TEXT NOT NULL,              -- user_id / tenant_id
    limit_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL,
    updated_by   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (scope, subject)
);

-- Skill-Market catalog. Runtime consumes only enabled entries.
CREATE TABLE IF NOT EXISTS skill_entries (
    id          TEXT PRIMARY KEY,            -- stable kebab id
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    version     TEXT NOT NULL DEFAULT '',
    entrypoint  TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_entries_enabled ON skill_entries (enabled);

-- Append-only admin write audit. Reads are not audited.
CREATE TABLE IF NOT EXISTS audit_log (
    id       BIGSERIAL PRIMARY KEY,
    ts       TIMESTAMPTZ NOT NULL,
    actor    TEXT NOT NULL DEFAULT '',       -- admin principal
    action   TEXT NOT NULL DEFAULT '',       -- e.g. 'token.issue'
    resource TEXT NOT NULL DEFAULT '',       -- affected id
    detail   TEXT NOT NULL DEFAULT ''        -- human-readable summary
);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log (ts);

-- ===== gateway domain =====

-- One row per completed model call (accounting source of truth).
CREATE TABLE IF NOT EXISTS usage_ledger (
    request_id       TEXT PRIMARY KEY,
    ts               TIMESTAMPTZ NOT NULL,
    user_id          TEXT NOT NULL DEFAULT '',
    session_id       TEXT NOT NULL DEFAULT '',
    alias            TEXT NOT NULL DEFAULT '',   -- caller-facing model alias
    real_model       TEXT NOT NULL DEFAULT '',   -- resolved upstream model
    provider         TEXT NOT NULL DEFAULT '',
    prompt_tokens    BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd         DOUBLE PRECISION NOT NULL DEFAULT 0,
    status           TEXT NOT NULL DEFAULT 'ok', -- ok | error
    error            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_usage_ledger_user_id ON usage_ledger (user_id);
CREATE INDEX IF NOT EXISTS idx_usage_ledger_session_id ON usage_ledger (session_id);
CREATE INDEX IF NOT EXISTS idx_usage_ledger_ts ON usage_ledger (ts);

-- Period-windowed token counters (Redis is the fast-path mirror).
CREATE TABLE IF NOT EXISTS quota_counters (
    scope       TEXT NOT NULL,               -- 'user' | 'tenant'
    subject     TEXT NOT NULL,
    period_key  TEXT NOT NULL,               -- e.g. '2026-06' or '2026-06-11'
    used_tokens BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, subject, period_key)
);
CREATE INDEX IF NOT EXISTS idx_quota_counters_period_key ON quota_counters (period_key);

-- ===== agent-runtime domain =====

-- session_id -> Claude on-disk session binding so a follow-up turn can
-- --resume after an agent-runtime restart.
CREATE TABLE IF NOT EXISTS session_map (
    session_id        TEXT PRIMARY KEY,
    claude_session_id TEXT NOT NULL DEFAULT '',
    user_id           TEXT NOT NULL DEFAULT '',
    sandbox_id        TEXT NOT NULL DEFAULT '',
    checkpoint_object_key TEXT NOT NULL DEFAULT '',
    checkpoint_status TEXT NOT NULL DEFAULT '',
    checkpoint_size_bytes BIGINT NOT NULL DEFAULT 0,
    checkpoint_error TEXT NOT NULL DEFAULT '',
    checkpoint_updated_at TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_map_user_id ON session_map (user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS session_map;
DROP TABLE IF EXISTS quota_counters;
DROP TABLE IF EXISTS usage_ledger;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS skill_entries;
DROP TABLE IF EXISTS quota_overrides;
DROP TABLE IF EXISTS token_records;
-- +goose StatementEnd
