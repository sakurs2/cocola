-- migrate:up
ALTER TABLE llm_providers
    ADD COLUMN IF NOT EXISTS icon_type TEXT NOT NULL DEFAULT 'simple-icons',
    ADD COLUMN IF NOT EXISTS icon_slug TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS icon_url TEXT NOT NULL DEFAULT '';

UPDATE llm_providers
SET icon_slug = CASE
    WHEN type = 'anthropic' THEN 'anthropic'
    ELSE 'openai'
END
WHERE icon_slug = '';

-- migrate:down
ALTER TABLE llm_providers
    DROP COLUMN IF EXISTS icon_url,
    DROP COLUMN IF EXISTS icon_slug,
    DROP COLUMN IF EXISTS icon_type;
