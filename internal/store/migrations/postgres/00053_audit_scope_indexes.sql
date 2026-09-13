-- +goose Up
-- Interactive audit pages and ceilings order by seq, not export commit_seq.
CREATE INDEX audit_tenant_events_project_seq
    ON audit_tenant_events (org_id, project_id, seq);
CREATE INDEX audit_tenant_events_env_seq
    ON audit_tenant_events (org_id, project_id, env_id, seq);
