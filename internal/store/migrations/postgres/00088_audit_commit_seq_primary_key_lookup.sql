-- +goose Up
-- Look up the row awaiting its commit-order position by primary key only (#1351).
-- A `commit_seq IS NULL` predicate lets the planner walk the *_commit_seq_unique
-- NULL tail, which holds every org's in-flight rows, and that raises
-- cross-org SSI rw-conflicts. Behaviour is otherwise identical to 00011.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION assign_audit_tenant_commit_seq() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    current_commit_seq BIGINT;
BEGIN
    IF NEW.commit_seq IS NOT NULL THEN
        RAISE EXCEPTION 'audit_tenant_events.commit_seq is database-owned';
    END IF;
    PERFORM pg_advisory_xact_lock(1464159830, 84);
    SELECT commit_seq INTO current_commit_seq FROM audit_tenant_events WHERE seq = NEW.seq;
    IF NOT FOUND OR current_commit_seq IS NOT NULL THEN
        RAISE EXCEPTION 'audit_tenant_events % has no commit-order position', NEW.seq;
    END IF;
    UPDATE audit_tenant_events SET commit_seq = nextval('audit_tenant_commit_seq') WHERE seq = NEW.seq;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION assign_audit_instance_commit_seq() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    current_commit_seq BIGINT;
BEGIN
    IF NEW.commit_seq IS NOT NULL THEN
        RAISE EXCEPTION 'audit_instance_events.commit_seq is database-owned';
    END IF;
    PERFORM pg_advisory_xact_lock(1464159830, 84);
    SELECT commit_seq INTO current_commit_seq FROM audit_instance_events WHERE seq = NEW.seq;
    IF NOT FOUND OR current_commit_seq IS NOT NULL THEN
        RAISE EXCEPTION 'audit_instance_events % has no commit-order position', NEW.seq;
    END IF;
    UPDATE audit_instance_events SET commit_seq = nextval('audit_instance_commit_seq') WHERE seq = NEW.seq;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
