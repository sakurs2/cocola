-- +goose Up
UPDATE mcp_servers
SET status = 'configured'
WHERE status = 'verified';

-- +goose Down
UPDATE mcp_servers
SET status = 'verified'
WHERE status = 'configured';
