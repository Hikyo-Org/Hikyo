-- Recovery and audit-export runtime guards. Catalog-driven archive protocols
-- remain separate because historical schemas do not have the current shape.

-- hikyo:reason Admitted audit-export transaction takes the exclusive installation writer/pruner gate through cutoff capture or final-page completion; no tenant payload is read.
-- hikyo:instance-scoped
-- name: AuditExportWriterBarrier :exec
SELECT pg_advisory_xact_lock(1464159830, 85);

-- hikyo:reason Admitted audit-export transaction captures the live database event clock under the writer gate; clock_timestamp intentionally differs from coordination now().
-- hikyo:instance-scoped
-- name: AuditExportSnapshotClock :one
SELECT clock_timestamp()::timestamptz;

-- hikyo:reason Recovery-authorized restore transaction checks only installation diagnostics against migration seed defaults under target table locks, refusing non-seed metadata before replacement.
-- hikyo:instance-scoped
-- name: RestoreDiagnosticsSeedOccupied :one
SELECT EXISTS (SELECT 1 FROM ops_diagnostics WHERE escrow_verified_at IS NOT NULL OR escrow_instance_id<>'' OR escrow_incarnation<>'' OR escrow_root_epoch<>0 OR last_reencrypt_success IS NOT NULL);

-- hikyo:reason Runtime admission takes the caller-selected installation session lock on one held connection before its serializable transaction; no tenant data is read.
-- hikyo:instance-scoped
-- name: AdmissionSerializedLock :exec
SELECT pg_advisory_lock(sqlc.arg(namespace)::integer, sqlc.arg(key)::integer);

-- hikyo:reason Runtime admission releases that exact connection's session lock once; an unconfirmed unlock discards the owner rather than returning it to the pool.
-- hikyo:instance-scoped
-- name: AdmissionSerializedUnlock :one
SELECT pg_advisory_unlock(sqlc.arg(namespace)::integer, sqlc.arg(key)::integer)::boolean;
