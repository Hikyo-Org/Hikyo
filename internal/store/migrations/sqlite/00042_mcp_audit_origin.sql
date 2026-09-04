-- +goose Up
-- SQLite cannot alter a CHECK constraint. Rebuild both append-only audit
-- tables in one migration transaction and preserve every row and sequence.
CREATE TABLE audit_tenant_events_mcp (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    occurred_asserted INTEGER NOT NULL CHECK (occurred_asserted IN (0, 1)),
    recorded_at TEXT NOT NULL,
    actor_id TEXT,
    actor_class TEXT NOT NULL CHECK (actor_class IN ('human', 'machine', 'system', 'break-glass', 'unauthenticated')),
    actor_credential_id TEXT,
    authority_id TEXT,
    scope_class TEXT NOT NULL CHECK (scope_class IN ('org', 'project', 'env')),
    org_id TEXT NOT NULL,
    project_id TEXT,
    env_id TEXT,
    object_type TEXT,
    object_id TEXT,
    outcome TEXT NOT NULL CHECK (outcome IN ('intent', 'success', 'denied', 'failure', 'unknown', 'disconnected')),
    correlation_id TEXT,
    source_ip TEXT,
    user_agent TEXT,
    origin TEXT NOT NULL CHECK (origin IN ('web', 'cli', 'api', 'mcp', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')),
    payload TEXT NOT NULL,
    CHECK (
        (scope_class = 'org' AND project_id IS NULL AND env_id IS NULL)
        OR (scope_class = 'project' AND project_id IS NOT NULL AND env_id IS NULL)
        OR (scope_class = 'env' AND project_id IS NOT NULL AND env_id IS NOT NULL)
    )
);
INSERT INTO audit_tenant_events_mcp SELECT * FROM audit_tenant_events;
DROP TABLE audit_tenant_events;
ALTER TABLE audit_tenant_events_mcp RENAME TO audit_tenant_events;
CREATE INDEX audit_tenant_events_org_seq ON audit_tenant_events (org_id, seq);

CREATE TABLE audit_instance_events_mcp (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    occurred_asserted INTEGER NOT NULL CHECK (occurred_asserted IN (0, 1)),
    recorded_at TEXT NOT NULL,
    actor_id TEXT,
    actor_class TEXT NOT NULL CHECK (actor_class IN ('human', 'machine', 'system', 'break-glass', 'unauthenticated')),
    actor_credential_id TEXT,
    authority_id TEXT,
    object_type TEXT,
    object_id TEXT,
    outcome TEXT NOT NULL CHECK (outcome IN ('intent', 'success', 'denied', 'failure', 'unknown', 'disconnected')),
    correlation_id TEXT,
    source_ip TEXT,
    user_agent TEXT,
    origin TEXT NOT NULL CHECK (origin IN ('web', 'cli', 'api', 'mcp', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')),
    payload TEXT NOT NULL
);
INSERT INTO audit_instance_events_mcp SELECT * FROM audit_instance_events;
DROP TABLE audit_instance_events;
ALTER TABLE audit_instance_events_mcp RENAME TO audit_instance_events;

-- +goose Down
-- Existing mcp rows intentionally make this rollback fail closed.
CREATE TABLE audit_tenant_events_pre_mcp (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    occurred_asserted INTEGER NOT NULL CHECK (occurred_asserted IN (0, 1)),
    recorded_at TEXT NOT NULL,
    actor_id TEXT,
    actor_class TEXT NOT NULL CHECK (actor_class IN ('human', 'machine', 'system', 'break-glass', 'unauthenticated')),
    actor_credential_id TEXT,
    authority_id TEXT,
    scope_class TEXT NOT NULL CHECK (scope_class IN ('org', 'project', 'env')),
    org_id TEXT NOT NULL,
    project_id TEXT,
    env_id TEXT,
    object_type TEXT,
    object_id TEXT,
    outcome TEXT NOT NULL CHECK (outcome IN ('intent', 'success', 'denied', 'failure', 'unknown', 'disconnected')),
    correlation_id TEXT,
    source_ip TEXT,
    user_agent TEXT,
    origin TEXT NOT NULL CHECK (origin IN ('web', 'cli', 'api', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')),
    payload TEXT NOT NULL,
    CHECK (
        (scope_class = 'org' AND project_id IS NULL AND env_id IS NULL)
        OR (scope_class = 'project' AND project_id IS NOT NULL AND env_id IS NULL)
        OR (scope_class = 'env' AND project_id IS NOT NULL AND env_id IS NOT NULL)
    )
);
INSERT INTO audit_tenant_events_pre_mcp SELECT * FROM audit_tenant_events;
DROP TABLE audit_tenant_events;
ALTER TABLE audit_tenant_events_pre_mcp RENAME TO audit_tenant_events;
CREATE INDEX audit_tenant_events_org_seq ON audit_tenant_events (org_id, seq);

CREATE TABLE audit_instance_events_pre_mcp (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    occurred_asserted INTEGER NOT NULL CHECK (occurred_asserted IN (0, 1)),
    recorded_at TEXT NOT NULL,
    actor_id TEXT,
    actor_class TEXT NOT NULL CHECK (actor_class IN ('human', 'machine', 'system', 'break-glass', 'unauthenticated')),
    actor_credential_id TEXT,
    authority_id TEXT,
    object_type TEXT,
    object_id TEXT,
    outcome TEXT NOT NULL CHECK (outcome IN ('intent', 'success', 'denied', 'failure', 'unknown', 'disconnected')),
    correlation_id TEXT,
    source_ip TEXT,
    user_agent TEXT,
    origin TEXT NOT NULL CHECK (origin IN ('web', 'cli', 'api', 'operator-fetch', 'adapter-job', 'offline-reconciled', 'system')),
    payload TEXT NOT NULL
);
INSERT INTO audit_instance_events_pre_mcp SELECT * FROM audit_instance_events;
DROP TABLE audit_instance_events;
ALTER TABLE audit_instance_events_pre_mcp RENAME TO audit_instance_events;
