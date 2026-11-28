-- name: ReadInstanceAudit :many
SELECT id FROM audit_instance_events;

-- name: TamperInstanceAudit :exec
UPDATE audit_instance_events SET id = $1;
