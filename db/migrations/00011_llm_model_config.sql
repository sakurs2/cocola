-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS llm_providers (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL DEFAULT '',
    type               TEXT NOT NULL DEFAULT '',
    base_url           TEXT NOT NULL DEFAULT '',
    api_key_ciphertext TEXT NOT NULL DEFAULT '',
    api_key_hint       TEXT NOT NULL DEFAULT '',
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_llm_providers_enabled ON llm_providers (enabled);

CREATE TABLE IF NOT EXISTS llm_model_routes (
    alias       TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL REFERENCES llm_providers(id) ON DELETE RESTRICT,
    real_model  TEXT NOT NULL DEFAULT '',
    runtime     TEXT NOT NULL DEFAULT 'claude-code',
    label       TEXT NOT NULL DEFAULT '',
    icon_type   TEXT NOT NULL DEFAULT 'simple-icons',
    icon_slug   TEXT NOT NULL DEFAULT '',
    icon_url    TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    visible     BOOLEAN NOT NULL DEFAULT TRUE,
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_llm_model_routes_provider_id ON llm_model_routes (provider_id);
CREATE INDEX IF NOT EXISTS idx_llm_model_routes_visible ON llm_model_routes (enabled, visible);
CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_model_routes_one_default
    ON llm_model_routes ((is_default))
    WHERE is_default;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_llm_model_routes_one_default;
DROP INDEX IF EXISTS idx_llm_model_routes_visible;
DROP INDEX IF EXISTS idx_llm_model_routes_provider_id;
DROP TABLE IF EXISTS llm_model_routes;
DROP INDEX IF EXISTS idx_llm_providers_enabled;
DROP TABLE IF EXISTS llm_providers;
-- +goose StatementEnd
