-- +goose Up
-- Shared MCP rate and concurrency state (#629). SQLite serves one node, but
-- keeps the same durable coordination model as PostgreSQL so engine behavior
-- and future migrations remain identical.

-- hikyo:table mcp_rate_buckets class=instance chain=-
CREATE TABLE mcp_rate_buckets (
    principal_id TEXT PRIMARY KEY REFERENCES principals(id) ON DELETE CASCADE,
    next_at      TEXT NOT NULL
);

-- hikyo:table mcp_inflight class=instance chain=-
CREATE TABLE mcp_inflight (
    call_id      TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals(id) ON DELETE CASCADE,
    org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    expires_at   TEXT NOT NULL
);

CREATE INDEX mcp_inflight_principal ON mcp_inflight (principal_id);
CREATE INDEX mcp_inflight_org ON mcp_inflight (org_id);
CREATE INDEX mcp_inflight_expiry ON mcp_inflight (expires_at);

-- +goose Down
DROP TABLE mcp_inflight;
DROP TABLE mcp_rate_buckets;
