-- +goose Up
-- +goose StatementBegin
-- Normalize all login handles into one globally unique identifier index. The
-- auth_users table keeps username/email as profile fields; login lookup uses
-- this table so future identifiers such as phone numbers can reuse the same
-- model instead of guessing by input shape.

CREATE TABLE IF NOT EXISTS auth_user_identifiers (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE,
    kind             TEXT NOT NULL,
    value_normalized TEXT NOT NULL UNIQUE,
    display_value    TEXT NOT NULL DEFAULT '',
    verified         BOOLEAN NOT NULL DEFAULT TRUE,
    is_primary       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_auth_user_identifiers_user_id
    ON auth_user_identifiers (user_id);

CREATE INDEX IF NOT EXISTS idx_auth_user_identifiers_kind
    ON auth_user_identifiers (kind);

INSERT INTO auth_user_identifiers (
    id, user_id, kind, value_normalized, display_value, verified, is_primary, created_at, updated_at
)
SELECT
    id || ':username:' || username_normalized,
    id,
    'username',
    username_normalized,
    username_normalized,
    TRUE,
    FALSE,
    created_at,
    updated_at
FROM auth_users
WHERE username_normalized <> '';

INSERT INTO auth_user_identifiers (
    id, user_id, kind, value_normalized, display_value, verified, is_primary, created_at, updated_at
)
SELECT
    id || ':email:' || email_normalized,
    id,
    'email',
    email_normalized,
    email_normalized,
    TRUE,
    TRUE,
    created_at,
    updated_at
FROM auth_users
WHERE email_normalized <> ''
  AND email_normalized <> username_normalized;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS auth_user_identifiers;
-- +goose StatementEnd
