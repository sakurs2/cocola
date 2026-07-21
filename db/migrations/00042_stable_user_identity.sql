-- +goose Up
-- +goose StatementBegin
-- Runtime ownership now uses the immutable auth_users.id rather than the
-- mutable email address. Cocola is still pre-GA, so legacy user-scoped data is
-- deliberately discarded instead of maintaining a permanent dual-identity
-- compatibility path. Accounts and administrator-owned configuration remain.

ALTER TABLE auth_users
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0);

-- Delete dependent records before their roots. Rows already owned by a stable
-- auth user id are retained, which keeps data created after this migration
-- safe if the statement is replayed in a restored environment.
DELETE FROM memory_capture_jobs AS job
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = job.user_id);

DELETE FROM conversation_runs AS run
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = run.user_id);

DELETE FROM conversations AS conversation
USING projects AS project
WHERE conversation.project_id = project.id
  AND NOT EXISTS (
      SELECT 1 FROM auth_users AS usr WHERE usr.id = project.owner_user_id
  );

DELETE FROM conversations AS conversation
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = conversation.user_id);

DELETE FROM conversation_folders AS folder
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = folder.user_id);

DELETE FROM projects AS project
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = project.owner_user_id);

DELETE FROM scm_oauth_states AS state
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = state.user_id);

DELETE FROM scm_connections AS connection
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = connection.user_id);

DELETE FROM scheduled_tasks AS task
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = task.owner_user_id);

DELETE FROM user_skill_preferences AS preference
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = preference.user_id);

DELETE FROM skill_entries AS skill
WHERE skill.scope = 'user'
  AND NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = skill.owner_user_id);

DELETE FROM user_mcp_preferences AS preference
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = preference.user_id);

DELETE FROM memory_user_settings AS setting
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = setting.user_id);

DELETE FROM session_map AS session
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = session.user_id);

DELETE FROM session_storage AS storage
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = storage.user_id);

DELETE FROM usage_ledger AS usage
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = usage.user_id);

DELETE FROM token_records AS token
WHERE NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = token.user_id);

DELETE FROM quota_counters AS counter
WHERE counter.scope = 'user'
  AND NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = counter.subject);

DELETE FROM quota_overrides AS quota
WHERE quota.scope = 'user'
  AND NOT EXISTS (SELECT 1 FROM auth_users AS usr WHERE usr.id = quota.subject);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- User history deleted above is intentionally not recreated on rollback.
ALTER TABLE auth_users DROP COLUMN IF EXISTS version;
-- +goose StatementEnd
