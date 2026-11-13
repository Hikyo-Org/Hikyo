CREATE OR REPLACE FUNCTION assign_audit_tenant_commit_seq() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(1464159830, 84);
    PERFORM nextval('audit_tenant_commit_seq');
    RETURN NULL;
END; $$;
