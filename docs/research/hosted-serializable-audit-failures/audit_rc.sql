\set org random(1, 1000)
BEGIN ISOLATION LEVEL READ COMMITTED;
SELECT 1 FROM upgrade_control c FOR SHARE OF c;
INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, actor_id, actor_class, scope_class, org_id, outcome, origin, payload)
VALUES (gen_random_uuid()::text, 'secret.read', 1, now(), true, 'usr_x', 'human', 'org', 'org_' || :org, 'success', 'api', '{}');
END;
