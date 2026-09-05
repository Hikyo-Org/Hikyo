-- hikyo:instance-scoped
-- name: GetAuditRetentionPolicy :one
SELECT access_days, security_days FROM audit_retention_policy WHERE singleton = 1;

-- hikyo:instance-scoped
-- name: SetAuditRetentionPolicy :exec
INSERT INTO audit_retention_policy (singleton, access_days, security_days)
VALUES (1, sqlc.arg(access_days), sqlc.arg(security_days))
ON CONFLICT (singleton) DO UPDATE SET access_days = excluded.access_days, security_days = excluded.security_days;

-- hikyo:instance-scoped
-- name: PruneTenantAuditRetention :many
DELETE FROM audit_tenant_events
WHERE (audit_tenant_events.org_id, COALESCE(audit_tenant_events.correlation_id, audit_tenant_events.id)) IN (
    SELECT audit_tenant_events.org_id, COALESCE(audit_tenant_events.correlation_id, audit_tenant_events.id) FROM audit_tenant_events
    WHERE audit_tenant_events.recorded_at < CASE WHEN audit_tenant_events.type IN (SELECT value FROM json_each(CAST(sqlc.arg(access_types) AS TEXT))) THEN sqlc.arg(access_cutoff_time) ELSE sqlc.arg(security_cutoff_time) END
      AND NOT EXISTS (
        SELECT 1 FROM audit_tenant_events sibling
        WHERE sibling.org_id = audit_tenant_events.org_id AND COALESCE(sibling.correlation_id, sibling.id) = COALESCE(audit_tenant_events.correlation_id, audit_tenant_events.id)
          AND sibling.recorded_at >= CASE WHEN sibling.type IN (SELECT value FROM json_each(CAST(sqlc.arg(access_types) AS TEXT))) THEN sqlc.arg(access_cutoff_time) ELSE sqlc.arg(security_cutoff_time) END
      )
    GROUP BY audit_tenant_events.org_id, COALESCE(audit_tenant_events.correlation_id, audit_tenant_events.id)
    ORDER BY MIN(audit_tenant_events.recorded_at) LIMIT 100
)
RETURNING type, recorded_at;

-- hikyo:instance-scoped
-- name: PruneInstanceAuditRetention :many
DELETE FROM audit_instance_events
WHERE COALESCE(audit_instance_events.correlation_id, audit_instance_events.id) IN (
    SELECT COALESCE(audit_instance_events.correlation_id, audit_instance_events.id) FROM audit_instance_events
    WHERE audit_instance_events.recorded_at < CASE WHEN audit_instance_events.type IN (SELECT value FROM json_each(CAST(sqlc.arg(access_types) AS TEXT))) THEN sqlc.arg(access_cutoff_time) ELSE sqlc.arg(security_cutoff_time) END
      AND NOT EXISTS (
        SELECT 1 FROM audit_instance_events sibling
        WHERE COALESCE(sibling.correlation_id, sibling.id) = COALESCE(audit_instance_events.correlation_id, audit_instance_events.id)
          AND sibling.recorded_at >= CASE WHEN sibling.type IN (SELECT value FROM json_each(CAST(sqlc.arg(access_types) AS TEXT))) THEN sqlc.arg(access_cutoff_time) ELSE sqlc.arg(security_cutoff_time) END
      )
    GROUP BY COALESCE(audit_instance_events.correlation_id, audit_instance_events.id)
    ORDER BY MIN(audit_instance_events.recorded_at) LIMIT 100
)
RETURNING type, recorded_at;

-- hikyo:instance-scoped
-- name: AuditPrunedSince :one
SELECT EXISTS (SELECT 1 FROM audit_instance_events WHERE type = 'retention.audit_pruned' AND recorded_at >= sqlc.arg(since_time));
