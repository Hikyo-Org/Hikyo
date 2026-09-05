-- Public instance-wide aggregate metadata. No tenant identities or key blobs.
-- hikyo:instance-scoped
-- name: GetOpsDiagnostics :one
SELECT escrow_verified_at, escrow_instance_id, escrow_incarnation, escrow_root_epoch,
    last_reencrypt_success AS last_reencrypt_success_at,
    (SELECT COALESCE(MAX(root_key_epoch),0) FROM master_keys WHERE state='active')::bigint AS root_epoch,
    (SELECT COUNT(*) FROM master_keys WHERE state='active')::bigint AS root_wrappers,
    (SELECT COUNT(*) FROM (SELECT purpose,org_id,project_id FROM tier3_keys WHERE state='retiring' GROUP BY purpose,org_id,project_id) AS retiring)::bigint AS retiring_scopes,
    (SELECT COUNT(*) FROM revision_pins WHERE expires_at <= sqlc.arg(now)::timestamptz)::bigint AS pins_expired,
    (SELECT COUNT(*) FROM revision_pins WHERE expires_at > sqlc.arg(now)::timestamptz AND expires_at <= sqlc.arg(day_at)::timestamptz)::bigint AS pins_day,
    (SELECT COUNT(*) FROM revision_pins WHERE expires_at > sqlc.arg(day_at)::timestamptz AND expires_at <= sqlc.arg(week_at)::timestamptz)::bigint AS pins_week,
    (SELECT COUNT(*) FROM revision_pins WHERE expires_at > sqlc.arg(week_at)::timestamptz AND expires_at <= sqlc.arg(month_at)::timestamptz)::bigint AS pins_month
FROM ops_diagnostics WHERE singleton=1;

-- Local host escrow proof writes public metadata only under the hierarchy fence.
-- hikyo:instance-scoped
-- name: SetEscrowVerification :exec
UPDATE ops_diagnostics SET escrow_verified_at=sqlc.arg(verified_at),
    escrow_instance_id=sqlc.arg(instance_id), escrow_incarnation=sqlc.arg(incarnation),
    escrow_root_epoch=sqlc.arg(root_epoch) WHERE singleton=1;

-- A successful complete reencrypt operation records its own completion.
-- hikyo:instance-scoped
-- name: SetReencryptSuccess :exec
UPDATE ops_diagnostics SET last_reencrypt_success=sqlc.arg(completed_at) WHERE singleton=1;
