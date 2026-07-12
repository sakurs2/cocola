-- +goose Up
-- +goose StatementBegin
-- Clear values written by the abandoned one-shot publisher. Warm-pool sizing
-- remains a runtime setting, but is re-created through the reconciled path.
DELETE FROM system_settings
WHERE key IN ('sandbox.warm_pool_enabled', 'sandbox.warm_pool_size');

-- Fake providers are a hermetic test seam, not a production configuration.
DELETE FROM llm_model_routes
WHERE provider_id IN (SELECT id FROM llm_providers WHERE type = 'fake');
DELETE FROM llm_providers WHERE type = 'fake';
-- +goose StatementEnd

-- +goose Down
-- This one-time stale-value cleanup is intentionally irreversible.
SELECT 1;
