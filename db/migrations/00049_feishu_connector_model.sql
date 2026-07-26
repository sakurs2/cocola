-- +goose Up
ALTER TABLE channel_connectors
    ADD COLUMN model_route_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN model_alias TEXT NOT NULL DEFAULT '';

ALTER TABLE channel_connectors
    ADD CONSTRAINT channel_connectors_model_check CHECK (
        char_length(model_route_id) <= 256
        AND char_length(model_alias) <= 256
        AND (
            (model_route_id = '' AND model_alias = '')
            OR (model_route_id <> '' AND model_alias <> '')
        )
    );

-- +goose Down
ALTER TABLE channel_connectors
    DROP CONSTRAINT IF EXISTS channel_connectors_model_check,
    DROP COLUMN IF EXISTS model_alias,
    DROP COLUMN IF EXISTS model_route_id;
