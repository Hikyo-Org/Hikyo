-- variant: per-org lock instead of global 84, original UPDATE predicate
CREATE OR REPLACE FUNCTION assign_audit_tenant_commit_seq() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext(NEW.org_id));
    UPDATE audit_tenant_events SET commit_seq = nextval('audit_tenant_commit_seq')
    WHERE seq = NEW.seq AND commit_seq IS NULL;
    IF NOT FOUND THEN RAISE EXCEPTION 'no position %', NEW.seq; END IF;
    RETURN NULL;
END; $$;
