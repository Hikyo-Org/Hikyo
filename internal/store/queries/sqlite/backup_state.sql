-- backup_state is the single-row disaster-recovery health record (#145). Every
-- statement here addresses no tenant: the row is instance operational state,
-- so each query is annotated instance-scoped and content-pinned.

-- hikyo:instance-scoped
-- name: GetBackupState :one
SELECT last_success_at, last_artifact_name, last_artifact_bytes,
       last_failure_at, last_failure_reason, last_prune_at,
       last_drill_at, last_drill_ok, last_drill_archive, last_drill_elapsed_ms,
       last_drill_binary_version, last_drill_schema_version
FROM backup_state WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupExportSuccess :exec
UPDATE backup_state
SET last_success_at = ?, last_artifact_name = ?, last_artifact_bytes = ?
WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupExportFailure :exec
UPDATE backup_state
SET last_failure_at = ?, last_failure_reason = ?
WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupPruneSuccess :exec
UPDATE backup_state SET last_prune_at = ? WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetBackupDrill :exec
UPDATE backup_state
SET last_drill_at = ?, last_drill_ok = ?, last_drill_archive = ?,
    last_drill_elapsed_ms = ?, last_drill_binary_version = ?,
    last_drill_schema_version = ?
WHERE id = 1;
