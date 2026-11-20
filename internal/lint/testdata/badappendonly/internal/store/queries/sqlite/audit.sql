-- name: AppendTenantAudit :exec
INSERT INTO audit_tenant_events (id) VALUES (?);

-- name: TamperTenantAudit :exec
DELETE FROM audit_tenant_events WHERE id = ?;
