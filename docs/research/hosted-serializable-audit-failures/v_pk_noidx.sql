-- variant: same UPDATE, predicate rewritten so only the seq primary key is usable
CREATE OR REPLACE FUNCTION assign_audit_tenant_commit_seq() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1464159830, 84);
    UPDATE audit_tenant_events SET commit_seq = nextval('audit_tenant_commit_seq')
    WHERE seq = NEW.seq AND commit_seq IS NOT DISTINCT FROM NULL;
    IF NOT FOUND THEN RAISE EXCEPTION 'no position %', NEW.seq; END IF;
    RETURN NULL;
END; $$;
DROP INDEX audit_tenant_events_org_commit_seq, audit_tenant_events_project_commit_seq, audit_tenant_events_env_commit_seq; ALTER TABLE audit_tenant_events DROP CONSTRAINT audit_tenant_events_commit_seq_unique;
