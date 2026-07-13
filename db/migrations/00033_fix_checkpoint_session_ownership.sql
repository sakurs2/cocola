-- +goose Up
-- +goose StatementBegin
-- Checkpoint metadata briefly created session_map rows without an owner.
-- Recover rows whose conversation owner is unambiguous and discard unsafe
-- orphans so the next turn can rebuild its resume index.
UPDATE session_map AS sm
SET user_id = c.user_id, updated_at = now()
FROM conversations AS c
WHERE sm.session_id = c.id AND sm.user_id = '' AND c.user_id <> '';

DELETE FROM session_map WHERE user_id = '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Ownership repair is intentionally irreversible.
-- +goose StatementEnd
