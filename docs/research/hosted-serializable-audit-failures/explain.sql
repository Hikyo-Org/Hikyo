-- Shows the plan the commit-order trigger's UPDATE gets, and the SIRead locks it leaves.
LOAD 'auto_explain';
SET auto_explain.log_min_duration = 0;
SET auto_explain.log_nested_statements = on;
SET auto_explain.log_level = notice;
BEGIN ISOLATION LEVEL SERIALIZABLE;
INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, actor_class, scope_class, org_id, outcome, origin, payload)
VALUES (gen_random_uuid()::text, 't', 1, now(), true, 'human', 'org', 'org_1', 'success', 'api', '{}');
SET CONSTRAINTS ALL IMMEDIATE;
SELECT l.locktype, c.relname, l.page, l.tuple FROM pg_locks l LEFT JOIN pg_class c ON c.oid = l.relation
WHERE l.mode = 'SIReadLock' AND l.pid = pg_backend_pid();
ROLLBACK;
