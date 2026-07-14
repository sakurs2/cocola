-- +goose Up
-- +goose StatementBegin
ALTER TABLE skill_entries
    ADD COLUMN runtime_id TEXT;

UPDATE skill_entries
SET runtime_id = CASE
    WHEN scope = 'user' THEN REGEXP_REPLACE(id, '^user-[0-9a-f]{8}-', '')
    ELSE id
END;

UPDATE skill_entries
SET entrypoint = '$CLAUDE_CONFIG_DIR/skills/' || runtime_id
WHERE scope = 'user'
  AND entrypoint = '$CLAUDE_CONFIG_DIR/skills/' || id;

ALTER TABLE skill_entries
    ALTER COLUMN runtime_id SET NOT NULL,
    ADD CONSTRAINT skill_entries_runtime_id_check CHECK (runtime_id <> '');

CREATE UNIQUE INDEX idx_skill_entries_admin_runtime_id
    ON skill_entries (runtime_id)
    WHERE scope = 'admin';
CREATE UNIQUE INDEX idx_skill_entries_user_runtime_id
    ON skill_entries (owner_user_id, runtime_id)
    WHERE scope = 'user';

-- Skill selection originally persisted the catalog's storage ID. Rewrite
-- existing user-message metadata so history and subsequent clients only see
-- the Runtime-native ID.
UPDATE messages AS message
SET metadata_json = JSONB_SET(
    message.metadata_json,
    '{skill_id}',
    TO_JSONB(skill.runtime_id),
    TRUE
)
FROM skill_entries AS skill
WHERE message.metadata_json->>'skill_id' = skill.id
  AND skill.runtime_id <> skill.id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_skill_entries_user_runtime_id;
DROP INDEX IF EXISTS idx_skill_entries_admin_runtime_id;
ALTER TABLE skill_entries
    DROP CONSTRAINT IF EXISTS skill_entries_runtime_id_check,
    DROP COLUMN IF EXISTS runtime_id;
-- +goose StatementEnd
