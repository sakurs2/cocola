-- +goose Up
-- +goose StatementBegin
-- cocola persistence schema v6: Auth.js-backed web whitelist.
--
-- Auth.js owns the browser session cookie. The admin-api remains the source of
-- truth for who may log in, which role they have, and the password hash used by
-- the Credentials provider login path.

CREATE TABLE IF NOT EXISTS auth_users (
    id                  TEXT PRIMARY KEY,
    username_normalized TEXT NOT NULL UNIQUE,
    email_normalized    TEXT NOT NULL UNIQUE,
    name                TEXT NOT NULL DEFAULT '',
    role                TEXT NOT NULL DEFAULT 'user',
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    password_hash       TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    last_login_at       TIMESTAMPTZ,
    created_by          TEXT NOT NULL DEFAULT '',
    updated_by          TEXT NOT NULL DEFAULT '',
    password_updated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_auth_users_role ON auth_users (role);
CREATE INDEX IF NOT EXISTS idx_auth_users_enabled ON auth_users (enabled);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS auth_users;
-- +goose StatementEnd
