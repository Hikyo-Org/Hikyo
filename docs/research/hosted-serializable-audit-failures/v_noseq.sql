CREATE OR REPLACE FUNCTION assign_audit_tenant_commit_seq() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1464159830, 84);
    UPDATE audit_tenant_events SET commit_seq = -NEW.seq WHERE seq = NEW.seq AND commit_seq IS NOT DISTINCT FROM NULL;
    RETURN NULL;
END; $$;
