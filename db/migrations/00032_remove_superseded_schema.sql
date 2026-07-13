-- +goose Up
-- +goose StatementBegin
-- Migration 00028 copied the only retained audit category and trace detail into
-- conversation_runs/conversation_trace_spans. No runtime code reads these old
-- tables anymore.
DROP TABLE IF EXISTS trace_events;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS audit_log;

-- Persist the single provider spelling accepted by the current Admin UI and
-- LLM Gateway before removing request-time aliases.
UPDATE llm_providers
SET type = 'openai_compat'
WHERE LOWER(TRIM(type)) IN ('openai', 'openai-compatible');

-- Arbitrary interval/cron schedules were removed from the product before GA.
-- Their semantics cannot be mapped faithfully to the calendar schedule model.
DELETE FROM scheduled_tasks
WHERE schedule_kind NOT IN ('once', 'hourly', 'daily', 'weekly', 'monthly');
DELETE FROM system_settings WHERE key = 'scheduler.min_interval_secs';
ALTER TABLE scheduled_tasks
    DROP CONSTRAINT IF EXISTS scheduled_tasks_schedule_kind_check;
ALTER TABLE scheduled_tasks
    ADD CONSTRAINT scheduled_tasks_schedule_kind_check
    CHECK (schedule_kind IN ('once', 'hourly', 'daily', 'weekly', 'monthly'));

-- Runtime URL encryption replaced plaintext/template MCP records. Records that
-- never completed that pre-GA migration cannot be recovered without retaining
-- a permanent startup-time compatibility scanner.
DELETE FROM mcp_servers
WHERE transport IN ('http', 'sse')
  AND (
      url <> '${__COCOLA_REMOTE_URL__}'
      OR COALESCE(url_var_ciphertext_json->>'__COCOLA_REMOTE_URL__', '') = ''
  );
-- +goose StatementEnd

-- +goose Down
-- The superseded audit tables are intentionally not recreated. Their retained
-- data remains in conversation_runs/conversation_trace_spans.
ALTER TABLE scheduled_tasks
    DROP CONSTRAINT IF EXISTS scheduled_tasks_schedule_kind_check;
SELECT 1;
