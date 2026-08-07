-- +goose Up
-- +goose StatementBegin
ALTER TABLE llm_model_routes
    ADD COLUMN reasoning_efforts TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD CONSTRAINT llm_model_routes_reasoning_efforts_check CHECK (
        reasoning_efforts <@ ARRAY['minimal', 'low', 'medium', 'high', 'xhigh', 'max']::TEXT[]
    );

ALTER TABLE conversation_runs
    ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT conversation_runs_reasoning_effort_check CHECK (
        reasoning_effort IN ('', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max')
    );

ALTER TABLE conversation_plans
    ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT conversation_plans_reasoning_effort_check CHECK (
        reasoning_effort IN ('', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max')
    );

ALTER TABLE conversation_questions
    ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT conversation_questions_reasoning_effort_check CHECK (
        reasoning_effort IN ('', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max')
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE conversation_questions
    DROP CONSTRAINT IF EXISTS conversation_questions_reasoning_effort_check,
    DROP COLUMN IF EXISTS reasoning_effort;

ALTER TABLE conversation_plans
    DROP CONSTRAINT IF EXISTS conversation_plans_reasoning_effort_check,
    DROP COLUMN IF EXISTS reasoning_effort;

ALTER TABLE conversation_runs
    DROP CONSTRAINT IF EXISTS conversation_runs_reasoning_effort_check,
    DROP COLUMN IF EXISTS reasoning_effort;

ALTER TABLE llm_model_routes
    DROP CONSTRAINT IF EXISTS llm_model_routes_reasoning_efforts_check,
    DROP COLUMN IF EXISTS reasoning_efforts;
-- +goose StatementEnd
