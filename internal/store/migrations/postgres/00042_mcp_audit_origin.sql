-- +goose Up
-- MCP emits through the existing append-only audit trails. Widen only the
-- closed origin vocabulary; every other audit invariant remains unchanged.
ALTER TABLE audit_tenant_events DROP CONSTRAINT audit_tenant_events_origin_check;
ALTER TABLE audit_tenant_events ADD CONSTRAINT audit_tenant_events_origin_check CHECK (
    origin IN ('web', 'cli', 'api', 'mcp', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')
);
ALTER TABLE audit_instance_events DROP CONSTRAINT audit_instance_events_origin_check;
ALTER TABLE audit_instance_events ADD CONSTRAINT audit_instance_events_origin_check CHECK (
    origin IN ('web', 'cli', 'api', 'mcp', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')
);

-- +goose Down
-- Existing mcp rows intentionally make this rollback fail closed rather than
-- silently relabeling or deleting forensic records.
ALTER TABLE audit_tenant_events DROP CONSTRAINT audit_tenant_events_origin_check;
ALTER TABLE audit_tenant_events ADD CONSTRAINT audit_tenant_events_origin_check CHECK (
    origin IN ('web', 'cli', 'api', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')
);
ALTER TABLE audit_instance_events DROP CONSTRAINT audit_instance_events_origin_check;
ALTER TABLE audit_instance_events ADD CONSTRAINT audit_instance_events_origin_check CHECK (
    origin IN ('web', 'cli', 'api', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')
);
