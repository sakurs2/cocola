-- +goose Up
-- +goose StatementBegin
ALTER TABLE scheduled_tasks
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- Every task now runs as a concrete user. Preserve user tasks and recover the
-- owner of legacy system tasks when their creator is still an active account.
UPDATE scheduled_tasks AS task
SET owner_user_id = (
    SELECT usr.id
    FROM auth_users AS usr
    WHERE usr.deleted_at IS NULL
      AND (
          usr.id = task.created_by
          OR usr.email_normalized = LOWER(task.created_by)
          OR usr.username_normalized = LOWER(task.created_by)
      )
    ORDER BY CASE WHEN usr.id = task.created_by THEN 0 ELSE 1 END
    LIMIT 1
)
WHERE task.owner_user_id = ''
  AND EXISTS (
      SELECT 1
      FROM auth_users AS usr
      WHERE usr.deleted_at IS NULL
        AND (
            usr.id = task.created_by
            OR usr.email_normalized = LOWER(task.created_by)
            OR usr.username_normalized = LOWER(task.created_by)
        )
  );

-- Earlier user tasks stored the runtime subject (usually email) rather than the
-- stable auth user id. Normalize those records before owner-scoped reads switch
-- to id-only filtering.
UPDATE scheduled_tasks AS task
SET owner_user_id = usr.id
FROM auth_users AS usr
WHERE task.owner_user_id <> ''
  AND task.owner_user_id <> usr.id
  AND usr.deleted_at IS NULL
  AND (
      usr.email_normalized = LOWER(task.owner_user_id)
      OR usr.username_normalized = LOWER(task.owner_user_id)
  );

UPDATE scheduled_tasks AS task
SET owner_user_id = ''
WHERE task.owner_user_id <> ''
  AND NOT EXISTS (
      SELECT 1 FROM auth_users AS usr
      WHERE usr.id = task.owner_user_id AND usr.deleted_at IS NULL
  );

UPDATE scheduled_tasks
SET conversation_id = 'sched-' || id
WHERE owner_user_id <> '' AND conversation_id = '';

-- Unresolved legacy tasks cannot safely execute as an arbitrary user.
UPDATE scheduled_tasks
SET status = 'paused',
    next_run_at = NULL,
    last_error = 'Owner required before this task can run'
WHERE owner_user_id = '';

UPDATE scheduled_tasks AS task
SET status = 'paused',
    next_run_at = NULL,
    last_error = 'Owner disabled'
WHERE task.owner_user_id <> ''
  AND EXISTS (
      SELECT 1 FROM auth_users AS usr
      WHERE usr.id = task.owner_user_id AND NOT usr.enabled
  );

CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_owner_updated_v2
    ON scheduled_tasks (owner_user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_expiration
    ON scheduled_tasks (status, expires_at)
    WHERE expires_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_scheduled_tasks_expiration;
DROP INDEX IF EXISTS idx_scheduled_tasks_owner_updated_v2;
ALTER TABLE scheduled_tasks DROP COLUMN IF EXISTS expires_at;
-- +goose StatementEnd
