-- +goose Up
-- +goose StatementBegin
CREATE TABLE channel_connectors (
    id                    UUID PRIMARY KEY,
    tenant_id             TEXT NOT NULL DEFAULT '',
    user_id               TEXT NOT NULL,
    provider              TEXT NOT NULL DEFAULT 'feishu',
    domain                TEXT NOT NULL,
    app_id                TEXT NOT NULL,
    app_secret_ciphertext TEXT NOT NULL,
    owner_open_id         TEXT NOT NULL DEFAULT '',
    bot_open_id           TEXT NOT NULL DEFAULT '',
    bot_name              TEXT NOT NULL DEFAULT '',
    desired_enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    status                TEXT NOT NULL,
    bind_code_hash        TEXT NOT NULL DEFAULT '',
    bind_expires_at       TIMESTAMPTZ,
    last_connected_at     TIMESTAMPTZ,
    last_error_code       TEXT NOT NULL DEFAULT '',
    lease_owner           TEXT NOT NULL DEFAULT '',
    lease_expires_at      TIMESTAMPTZ,
    version               BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL,
    CONSTRAINT channel_connectors_provider_check CHECK (provider = 'feishu'),
    CONSTRAINT channel_connectors_domain_check CHECK (domain IN ('feishu', 'lark')),
    CONSTRAINT channel_connectors_status_check CHECK (
        status IN (
            'awaiting_bind',
            'connecting',
            'ready',
            'action_required',
            'disabled',
            'error'
        )
    ),
    CONSTRAINT channel_connectors_owner_unique UNIQUE (tenant_id, user_id, provider),
    CONSTRAINT channel_connectors_app_unique UNIQUE (provider, domain, app_id)
);

CREATE INDEX channel_connectors_reconcile
    ON channel_connectors (desired_enabled, lease_expires_at, updated_at)
    WHERE desired_enabled = TRUE;

CREATE TABLE channel_registration_flows (
    id               UUID PRIMARY KEY,
    tenant_id        TEXT NOT NULL DEFAULT '',
    user_id          TEXT NOT NULL,
    provider         TEXT NOT NULL DEFAULT 'feishu',
    status           TEXT NOT NULL,
    verification_url TEXT NOT NULL DEFAULT '',
    expires_at       TIMESTAMPTZ NOT NULL,
    error_code       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    CONSTRAINT channel_registration_flows_provider_check CHECK (provider = 'feishu'),
    CONSTRAINT channel_registration_flows_status_check CHECK (
        status IN (
            'starting',
            'awaiting_user',
            'authorizing',
            'ready',
            'denied',
            'expired',
            'failed',
            'interrupted',
            'cancelled'
        )
    )
);

CREATE UNIQUE INDEX channel_registration_flows_active_user
    ON channel_registration_flows (tenant_id, user_id, provider)
    WHERE status IN ('starting', 'awaiting_user', 'authorizing');

CREATE INDEX channel_registration_flows_owner_created
    ON channel_registration_flows (tenant_id, user_id, created_at DESC);

CREATE TABLE channel_sessions (
    connector_id             UUID NOT NULL REFERENCES channel_connectors(id) ON DELETE CASCADE,
    external_chat_id         TEXT NOT NULL,
    conversation_id          TEXT NOT NULL,
    pending_question_id      UUID,
    pending_question_version INTEGER,
    pending_options_json     JSONB,
    updated_at               TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (connector_id, external_chat_id),
    CONSTRAINT channel_sessions_pending_question_check CHECK (
        (pending_question_id IS NULL AND pending_question_version IS NULL AND pending_options_json IS NULL)
        OR
        (pending_question_id IS NOT NULL AND pending_question_version > 0 AND pending_options_json IS NOT NULL)
    )
);

CREATE TABLE channel_inbox (
    id                  UUID PRIMARY KEY,
    connector_id        UUID NOT NULL REFERENCES channel_connectors(id) ON DELETE CASCADE,
    event_id            TEXT NOT NULL,
    external_message_id TEXT NOT NULL,
    external_chat_id    TEXT NOT NULL,
    normalized_payload  JSONB,
    priority            INTEGER NOT NULL DEFAULT 0,
    status              TEXT NOT NULL,
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at     TIMESTAMPTZ NOT NULL,
    lease_owner         TEXT NOT NULL DEFAULT '',
    lease_expires_at    TIMESTAMPTZ,
    error_code          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    CONSTRAINT channel_inbox_status_check CHECK (
        status IN ('pending', 'processing', 'retry', 'done', 'rejected', 'failed')
    ),
    CONSTRAINT channel_inbox_event_unique UNIQUE (connector_id, event_id)
);

CREATE INDEX channel_inbox_dispatch
    ON channel_inbox (connector_id, status, priority DESC, next_attempt_at, created_at)
    WHERE status IN ('pending', 'retry');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS channel_inbox;
DROP TABLE IF EXISTS channel_sessions;
DROP TABLE IF EXISTS channel_registration_flows;
DROP TABLE IF EXISTS channel_connectors;
-- +goose StatementEnd
