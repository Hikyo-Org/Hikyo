-- Run against a copy of a freshly migrated Hikyo database named `tmpl`:
--   createdb -T tmpl tmpl_big && psql -d tmpl_big -f template.sql
-- 500,000 committed audit rows over 1,000 orgs, then statistics, so the
-- trigger's UPDATE is planned the way a production table plans it.
INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, actor_id, actor_class, scope_class, org_id, outcome, origin, payload)
SELECT gen_random_uuid()::text, 'secret.read', 1, now(), true, 'usr_x', 'human', 'org', 'org_' || (1 + i % 1000), 'success', 'api', '{}' FROM generate_series(1, 500000) i;
VACUUM ANALYZE audit_tenant_events;
