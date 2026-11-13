-- variant: same UPDATE, but the NULL test is not indexable, so only the seq primary key can serve it
CREATE OR REPLACE FUNCTION assign_audit_tenant_commit_seq() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1464159830, 84);
    UPDATE audit_tenant_events SET commit_seq = nextval('audit_tenant_commit_seq')
    WHERE seq = NEW.seq AND (commit_seq + 0) IS NULL;
    IF NOT FOUND THEN RAISE EXCEPTION 'no position %', NEW.seq; END IF;
    RETURN NULL;
END; $$;
