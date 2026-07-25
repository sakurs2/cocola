-- +goose Up
-- +goose StatementBegin
CREATE TABLE wiki_nodes (
    id                 UUID PRIMARY KEY,
    tenant_id          TEXT NOT NULL DEFAULT '',
    user_id            TEXT NOT NULL,
    parent_id          UUID,
    kind               TEXT NOT NULL,
    name               TEXT NOT NULL,
    extension          TEXT NOT NULL DEFAULT '',
    mime_type          TEXT NOT NULL DEFAULT '',
    current_version_id UUID,
    sort_order         BIGINT NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    deleted_at         TIMESTAMPTZ,
    CONSTRAINT wiki_nodes_owner_unique UNIQUE (id, tenant_id, user_id),
    CONSTRAINT wiki_nodes_kind_check CHECK (kind IN ('folder', 'file')),
    CONSTRAINT wiki_nodes_name_check CHECK (
        name = BTRIM(name)
        AND CHAR_LENGTH(name) BETWEEN 1 AND 160
        AND POSITION('/' IN name) = 0
        AND POSITION('\' IN name) = 0
        AND name NOT IN ('.', '..')
    ),
    CONSTRAINT wiki_nodes_parent_fkey
        FOREIGN KEY (parent_id, tenant_id, user_id)
        REFERENCES wiki_nodes (id, tenant_id, user_id)
        ON DELETE RESTRICT,
    CONSTRAINT wiki_nodes_folder_metadata_check CHECK (
        kind = 'file'
        OR (extension = '' AND mime_type = '' AND current_version_id IS NULL)
    )
);

CREATE UNIQUE INDEX wiki_nodes_active_sibling_name_unique
    ON wiki_nodes (
        tenant_id,
        user_id,
        COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid),
        LOWER(name)
    )
    WHERE deleted_at IS NULL;

CREATE INDEX wiki_nodes_owner_parent_order
    ON wiki_nodes (tenant_id, user_id, parent_id, sort_order, LOWER(name))
    WHERE deleted_at IS NULL;

CREATE TABLE wiki_versions (
    id          UUID PRIMARY KEY,
    node_id     UUID NOT NULL REFERENCES wiki_nodes(id) ON DELETE CASCADE,
    revision    BIGINT NOT NULL CHECK (revision > 0),
    object_key  TEXT NOT NULL UNIQUE,
    size_bytes  BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256      TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    CONSTRAINT wiki_versions_node_revision_unique UNIQUE (node_id, revision),
    CONSTRAINT wiki_versions_id_node_unique UNIQUE (id, node_id)
);

ALTER TABLE wiki_nodes
    ADD CONSTRAINT wiki_nodes_current_version_fkey
    FOREIGN KEY (current_version_id, id)
    REFERENCES wiki_versions (id, node_id)
    ON DELETE RESTRICT;

CREATE INDEX wiki_versions_node_created
    ON wiki_versions (node_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE wiki_nodes
    DROP CONSTRAINT IF EXISTS wiki_nodes_current_version_fkey;
DROP TABLE IF EXISTS wiki_versions;
DROP TABLE IF EXISTS wiki_nodes;
-- +goose StatementEnd
