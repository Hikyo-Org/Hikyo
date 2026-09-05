-- +goose Up
-- hikyo:table audit_retention_policy class=instance chain=-
CREATE TABLE audit_retention_policy (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    access_days INTEGER NOT NULL CHECK (access_days BETWEEN 1 AND 3650),
    security_days INTEGER NOT NULL CHECK (security_days BETWEEN access_days AND 3650)
);
CREATE INDEX audit_tenant_retention_time ON audit_tenant_events (recorded_at);
CREATE INDEX audit_tenant_retention_unit ON audit_tenant_events (org_id, COALESCE(correlation_id, id), recorded_at);
CREATE INDEX audit_instance_retention_time ON audit_instance_events (recorded_at);
CREATE INDEX audit_instance_retention_unit ON audit_instance_events (COALESCE(correlation_id, id), recorded_at);
