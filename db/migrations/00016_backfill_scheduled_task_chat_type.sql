-- +goose Up
-- +goose StatementBegin
UPDATE conversations AS c
SET chat_type = 'scheduled_task'
FROM scheduled_tasks AS t
WHERE t.conversation_id <> ''
    AND t.conversation_id = c.id
    AND c.chat_type = 'chat';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE conversations AS c
SET chat_type = 'chat'
FROM scheduled_tasks AS t
WHERE t.conversation_id <> ''
    AND t.conversation_id = c.id
    AND c.chat_type = 'scheduled_task';
-- +goose StatementEnd
