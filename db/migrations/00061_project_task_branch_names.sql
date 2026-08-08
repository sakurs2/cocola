-- +goose Up
-- +goose StatementBegin
CREATE UNIQUE INDEX project_workspaces_project_branch_key
    ON project_workspaces (project_id, LOWER(branch_name));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS project_workspaces_project_branch_key;
-- +goose StatementEnd
