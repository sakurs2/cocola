-- +goose Up
-- +goose StatementBegin
-- The initial scheduled task implementation defaulted to one turn, which is
-- too low for Claude Code whenever a task needs tool use. Keep the first
-- product version on a backend-owned execution budget and repair tasks created
-- before this migration.
ALTER TABLE scheduled_tasks ALTER COLUMN max_turns SET DEFAULT 30;
UPDATE scheduled_tasks SET max_turns = 30 WHERE max_turns < 30;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE scheduled_tasks ALTER COLUMN max_turns SET DEFAULT 1;
-- +goose StatementEnd
