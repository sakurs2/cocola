-- +goose Up
-- +goose StatementBegin
-- Backfill username support for dev databases that already applied the first
-- auth_users migration before username login existed.

ALTER TABLE auth_users ADD COLUMN IF NOT EXISTS username_normalized TEXT;

WITH candidates AS (
    SELECT
        id,
        lower(split_part(email_normalized, '@', 1)) AS base,
        count(*) OVER (PARTITION BY lower(split_part(email_normalized, '@', 1))) AS base_count
    FROM auth_users
    WHERE username_normalized IS NULL OR username_normalized = ''
)
UPDATE auth_users u
SET username_normalized = CASE
    WHEN c.base_count = 1 THEN c.base
    ELSE left(c.base, 54) || '-' || substr(md5(c.id), 1, 8)
END
FROM candidates c
WHERE u.id = c.id;

ALTER TABLE auth_users ALTER COLUMN username_normalized SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_users_username_normalized
    ON auth_users (username_normalized);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_auth_users_username_normalized;
ALTER TABLE auth_users DROP COLUMN IF EXISTS username_normalized;
-- +goose StatementEnd
