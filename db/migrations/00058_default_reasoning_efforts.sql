-- +goose Up
-- +goose StatementBegin
UPDATE llm_model_routes
SET reasoning_efforts = CASE protocol
    WHEN 'anthropic-messages' THEN ARRAY['low', 'medium', 'high', 'xhigh', 'max']::TEXT[]
    WHEN 'openai-responses' THEN ARRAY['minimal', 'low', 'medium', 'high', 'xhigh']::TEXT[]
    ELSE '{}'::TEXT[]
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE llm_model_routes
SET reasoning_efforts = '{}'::TEXT[];
-- +goose StatementEnd
