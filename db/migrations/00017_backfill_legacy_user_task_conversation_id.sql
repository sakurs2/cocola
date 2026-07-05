-- +goose Up
-- +goose StatementBegin
UPDATE scheduled_tasks
SET conversation_id = 'sched-' || id
WHERE owner_type = 'user'
    AND conversation_id = '';

UPDATE conversations AS c
SET chat_type = 'scheduled_task'
FROM scheduled_tasks AS t
WHERE t.owner_type = 'user'
    AND c.chat_type = 'chat'
    AND (
        c.id = t.conversation_id
        OR c.id = 'sched-' || t.id
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
