-- backup_state is the single-row disaster-recovery health record (#145). Every
-- statement here addresses no tenant: the row is instance operational state,
-- so each query is annotated instance-scoped and content-pinned.

-- hikyo:instance-scoped
-- name: GetBackupState :one
SELECT last_success_at, last_artifact_name, last_artifact_bytes, last_artifact_sha256,
       last_failure_at, last_failure_reason, last_prune_at,
       last_prune_attempt_at, last_prune_attempt_id, prune_failure_reason, retained_expired_archives,
       last_drill_at, last_drill_ok, last_drill_archive, last_drill_elapsed_ms,
       last_drill_binary_version, last_drill_schema_version
FROM backup_state WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupExportSuccess :execrows
UPDATE backup_state
SET last_success_at = sqlc.arg(last_success_at),
    last_artifact_name = sqlc.arg(last_artifact_name),
    last_artifact_bytes = sqlc.arg(last_artifact_bytes),
    last_artifact_sha256 = sqlc.arg(last_artifact_sha256)
WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupExportFailure :exec
UPDATE backup_state
SET last_failure_at = sqlc.arg(last_failure_at),
    last_failure_reason = sqlc.arg(last_failure_reason)
WHERE id = 1;

-- hikyo:reason scheduler records instance-wide archive compliance in the singleton backup state; no tenant data is addressed
-- hikyo:instance-scoped
-- name: SetBackupPruneResult :execrows
UPDATE backup_state
SET last_prune_attempt_at = sqlc.arg(attempt_at),
    last_prune_attempt_id = sqlc.arg(attempt_id),
    prune_failure_reason = sqlc.arg(failure_reason),
    retained_expired_archives = CASE WHEN sqlc.arg(failure_reason) = 'in_progress' OR sqlc.arg(failure_reason) = 'directory_unavailable' THEN retained_expired_archives ELSE sqlc.arg(retained_expired_archives) END,
    last_prune_at = CASE WHEN sqlc.arg(failure_reason) = '' THEN sqlc.arg(attempt_at) ELSE last_prune_at END
WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupDrill :exec
UPDATE backup_state
SET last_drill_at = sqlc.arg(last_drill_at),
    last_drill_ok = sqlc.arg(last_drill_ok),
    last_drill_archive = sqlc.arg(last_drill_archive),
    last_drill_elapsed_ms = sqlc.arg(last_drill_elapsed_ms),
    last_drill_binary_version = sqlc.arg(last_drill_binary_version),
    last_drill_schema_version = sqlc.arg(last_drill_schema_version)
WHERE id = 1;
