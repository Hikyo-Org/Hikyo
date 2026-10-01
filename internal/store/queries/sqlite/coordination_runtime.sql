-- Installation-wide coordination. These tables carry no tenant chain.

-- hikyo:reason Installation singleton leader fencing. Coordination admitted datastore transaction; no tenant row or tenant payload; preserves exact owner/name/fence and expiry predicates. GuardSingletonLease is root-owned scheduler transaction admission and retains database/process clocks.
-- hikyo:instance-scoped
-- name: GuardSingletonLease :execrows
UPDATE singleton_leases SET fence_token = fence_token
WHERE name = sqlc.arg(name) AND owner = sqlc.arg(owner)
  AND fence_token = sqlc.arg(fence_token) AND expires_at > sqlc.arg(now);

-- excluded.acquired_at is the supplied claim time, also used for expiry.
-- hikyo:reason Installation singleton leader fencing. Coordination admitted datastore transaction; no tenant row or tenant payload; preserves exact owner/name/fence and expiry predicates. GuardSingletonLease is root-owned scheduler transaction admission and retains database/process clocks.
-- hikyo:instance-scoped
-- name: CoordinationClaimLease :one
INSERT INTO singleton_leases (name, owner, fence_token, acquired_at, expires_at)
			 VALUES (sqlc.arg(name), sqlc.arg(owner), 1, sqlc.arg(now), sqlc.arg(expires))
			 ON CONFLICT (name) DO UPDATE
			   SET owner = excluded.owner,
			       fence_token = singleton_leases.fence_token + 1,
			       acquired_at = excluded.acquired_at,
			       expires_at = excluded.expires_at
			   WHERE singleton_leases.expires_at <= excluded.acquired_at
			 RETURNING fence_token;

-- hikyo:reason Installation singleton leader fencing. Coordination admitted datastore transaction; no tenant row or tenant payload; preserves exact owner/name/fence and expiry predicates. GuardSingletonLease is root-owned scheduler transaction admission and retains database/process clocks.
-- hikyo:instance-scoped
-- name: CoordinationRenewLease :execrows
UPDATE singleton_leases SET expires_at = sqlc.arg(expires)
			 WHERE name = sqlc.arg(name) AND owner = sqlc.arg(owner) AND fence_token = sqlc.arg(fence) AND expires_at > sqlc.arg(now);

-- hikyo:reason Installation singleton leader fencing. Coordination admitted datastore transaction; no tenant row or tenant payload; preserves exact owner/name/fence and expiry predicates. GuardSingletonLease is root-owned scheduler transaction admission and retains database/process clocks.
-- hikyo:instance-scoped
-- name: CoordinationReleaseLease :exec
UPDATE singleton_leases SET expires_at = sqlc.arg(expires) WHERE name = sqlc.arg(name) AND owner = sqlc.arg(owner) AND fence_token = sqlc.arg(fence);

-- hikyo:reason Installation singleton leader fencing. Coordination admitted datastore transaction; no tenant row or tenant payload; preserves exact owner/name/fence and expiry predicates. GuardSingletonLease is root-owned scheduler transaction admission and retains database/process clocks.
-- hikyo:instance-scoped
-- name: CoordinationLeaseHolder :one
SELECT owner, acquired_at, expires_at FROM singleton_leases WHERE name = sqlc.arg(name);

-- hikyo:reason Installation HA registry and mixed-root-key boot admission. Coordination admitted datastore transaction; rows are replica identity/version/fingerprint/heartbeat, no tenant chain. RegisterNodeChecked retains membership advisory lock before foreign-live-fingerprint check and upsert.
-- hikyo:instance-scoped
-- name: CoordinationUpsertNode :exec
INSERT INTO ha_nodes (node_id, binary_version, schema_version, root_key_fingerprint, started_at, heartbeat_at)
			 VALUES (sqlc.arg(node_id), sqlc.arg(binary_version), sqlc.arg(schema_version), sqlc.arg(root_key_fingerprint), sqlc.arg(started_at), sqlc.arg(heartbeat_at))
			 ON CONFLICT (node_id) DO UPDATE
			   SET binary_version = excluded.binary_version,
			       schema_version = excluded.schema_version,
			       root_key_fingerprint = excluded.root_key_fingerprint,
			       heartbeat_at = excluded.heartbeat_at;

-- hikyo:reason Installation HA registry and mixed-root-key boot admission. Coordination admitted datastore transaction; rows are replica identity/version/fingerprint/heartbeat, no tenant chain. RegisterNodeChecked retains membership advisory lock before foreign-live-fingerprint check and upsert.
-- hikyo:instance-scoped
-- name: CoordinationCountLiveNodes :one
SELECT COUNT(*) FROM ha_nodes WHERE heartbeat_at >= sqlc.arg(since);

-- hikyo:reason Installation HA registry and mixed-root-key boot admission. Coordination admitted datastore transaction; rows are replica identity/version/fingerprint/heartbeat, no tenant chain. RegisterNodeChecked retains membership advisory lock before foreign-live-fingerprint check and upsert.
-- hikyo:instance-scoped
-- name: CoordinationPruneNodes :exec
DELETE FROM ha_nodes WHERE heartbeat_at < sqlc.arg(cutoff);

-- hikyo:reason Installation HA registry and mixed-root-key boot admission. Coordination admitted datastore transaction; rows are replica identity/version/fingerprint/heartbeat, no tenant chain. RegisterNodeChecked retains membership advisory lock before foreign-live-fingerprint check and upsert.
-- hikyo:instance-scoped
-- name: CoordinationForeignRootKeyFingerprints :many
SELECT DISTINCT root_key_fingerprint FROM ha_nodes
			 WHERE node_id <> sqlc.arg(node_id) AND root_key_fingerprint <> sqlc.arg(fingerprint) AND heartbeat_at >= sqlc.arg(since);

-- hikyo:reason Shared pre-authentication admission counter/backoff infrastructure. Coordination admitted datastore transaction; bucket/subject keys carry uniform refusal state, not tenant resources. Atomic upserts and fixed account sentinel preserved; Postgres failure stamps use now() and monotonic GREATEST.
-- hikyo:instance-scoped
-- name: CoordinationBumpWindow :one
INSERT INTO admission_counters (bucket, subject, window_start, hits)
			 VALUES (sqlc.arg(bucket), sqlc.arg(subject), sqlc.arg(window_start), 1)
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET hits = admission_counters.hits + 1
			 RETURNING hits;

-- hikyo:reason Installation-wide authenticated MCP rate/concurrency admission. Coordination admitted datastore transaction; principal/org IDs are counter dimensions, not grants to tenant data. Class88 advisory lock encloses prune/count/rate-lock/update/claim; global count intentionally enforces instance capacity alongside per-principal/org limits.
-- hikyo:instance-scoped
-- name: CoordinationPruneMCPClaims :exec
DELETE FROM mcp_inflight WHERE expires_at <= sqlc.arg(now);

-- hikyo:reason Installation-wide authenticated MCP rate/concurrency admission. Coordination admitted datastore transaction; principal/org IDs are counter dimensions, not grants to tenant data. Class88 advisory lock encloses prune/count/rate-lock/update/claim; global count intentionally enforces instance capacity alongside per-principal/org limits.
-- hikyo:instance-scoped
-- name: CoordinationCountMCPClaims :one
SELECT
		CAST(COALESCE(SUM(CASE WHEN principal_id = sqlc.arg(principal_id) THEN 1 ELSE 0 END), 0) AS INTEGER) AS principal_count,
		CAST(COALESCE(SUM(CASE WHEN org_id = sqlc.arg(org_id) THEN 1 ELSE 0 END), 0) AS INTEGER) AS org_count,
		COUNT(*) AS instance_count
		FROM mcp_inflight;

-- hikyo:reason Installation-wide authenticated MCP rate/concurrency admission. Coordination admitted datastore transaction; principal/org IDs are counter dimensions, not grants to tenant data. Class88 advisory lock encloses prune/count/rate-lock/update/claim; global count intentionally enforces instance capacity alongside per-principal/org limits.
-- hikyo:instance-scoped
-- name: CoordinationMCPRateBucket :one
SELECT next_at FROM mcp_rate_buckets WHERE principal_id = sqlc.arg(principal_id);

-- hikyo:reason Installation-wide authenticated MCP rate/concurrency admission. Coordination admitted datastore transaction; principal/org IDs are counter dimensions, not grants to tenant data. Class88 advisory lock encloses prune/count/rate-lock/update/claim; global count intentionally enforces instance capacity alongside per-principal/org limits.
-- hikyo:instance-scoped
-- name: CoordinationSetMCPRateBucket :exec
INSERT INTO mcp_rate_buckets (principal_id, next_at) VALUES (sqlc.arg(principal_id), sqlc.arg(next_at))
		ON CONFLICT (principal_id) DO UPDATE SET next_at = excluded.next_at;

-- hikyo:reason Installation-wide authenticated MCP rate/concurrency admission. Coordination admitted datastore transaction; principal/org IDs are counter dimensions, not grants to tenant data. Class88 advisory lock encloses prune/count/rate-lock/update/claim; global count intentionally enforces instance capacity alongside per-principal/org limits.
-- hikyo:instance-scoped
-- name: CoordinationInsertMCPClaim :exec
INSERT INTO mcp_inflight (call_id, principal_id, org_id, expires_at) VALUES (sqlc.arg(call_id), sqlc.arg(principal_id), sqlc.arg(org_id), sqlc.arg(expires_at));

-- hikyo:reason Installation-wide authenticated MCP rate/concurrency admission. Coordination admitted datastore transaction; principal/org IDs are counter dimensions, not grants to tenant data. Class88 advisory lock encloses prune/count/rate-lock/update/claim; global count intentionally enforces instance capacity alongside per-principal/org limits.
-- hikyo:instance-scoped
-- name: CoordinationReleaseMCP :exec
DELETE FROM mcp_inflight WHERE call_id = sqlc.arg(call_id);

-- hikyo:reason Shared pre-authentication admission counter/backoff infrastructure. Coordination admitted datastore transaction; bucket/subject keys carry uniform refusal state, not tenant resources. Atomic upserts and fixed account sentinel preserved; Postgres failure stamps use now() and monotonic GREATEST.
-- hikyo:instance-scoped
-- name: CoordinationAccountFailureState :one
SELECT failures, until_at FROM admission_counters WHERE bucket = sqlc.arg(bucket) AND subject = sqlc.arg(subject) AND window_start = sqlc.arg(window_start);

-- Compare against the inserted failure time so delayed attempts cannot
-- move the stored instant backwards. The update remains one atomic upsert.
-- hikyo:reason Shared pre-authentication admission counter/backoff infrastructure. Coordination admitted datastore transaction; bucket/subject keys carry uniform refusal state, not tenant resources. Atomic upserts and fixed account sentinel preserved; Postgres failure stamps use now() and monotonic GREATEST.
-- hikyo:instance-scoped
-- name: CoordinationRecordAccountFailure :one
INSERT INTO admission_counters (bucket, subject, window_start, failures, until_at)
			 VALUES (sqlc.arg(bucket), sqlc.arg(subject), sqlc.arg(window_start), 1, sqlc.arg(now))
			 ON CONFLICT (bucket, subject, window_start) DO UPDATE
			   SET failures = admission_counters.failures + 1,
			       until_at = max(admission_counters.until_at, excluded.until_at)
			 RETURNING failures;

-- hikyo:reason Shared pre-authentication admission counter/backoff infrastructure. Coordination admitted datastore transaction; bucket/subject keys carry uniform refusal state, not tenant resources. Atomic upserts and fixed account sentinel preserved; Postgres failure stamps use now() and monotonic GREATEST.
-- hikyo:instance-scoped
-- name: CoordinationPruneAccountBackoff :exec
DELETE FROM admission_counters WHERE bucket = sqlc.arg(bucket) AND (until_at IS NULL OR until_at < sqlc.arg(cutoff));

-- hikyo:reason Shared pre-authentication admission counter/backoff infrastructure. Coordination admitted datastore transaction; bucket/subject keys carry uniform refusal state, not tenant resources. Atomic upserts and fixed account sentinel preserved; Postgres failure stamps use now() and monotonic GREATEST.
-- hikyo:instance-scoped
-- name: CoordinationClearAccount :exec
DELETE FROM admission_counters WHERE bucket = sqlc.arg(bucket) AND subject = sqlc.arg(subject) AND window_start = sqlc.arg(window_start);

-- hikyo:reason Shared pre-authentication admission counter/backoff infrastructure. Coordination admitted datastore transaction; bucket/subject keys carry uniform refusal state, not tenant resources. Atomic upserts and fixed account sentinel preserved; Postgres failure stamps use now() and monotonic GREATEST.
-- hikyo:instance-scoped
-- name: CoordinationPruneAdmissionWindows :exec
DELETE FROM admission_counters WHERE bucket <> sqlc.arg(bucket) AND window_start < sqlc.arg(cutoff);

-- hikyo:reason Instance-owned self-configuration rollout/seed admission metadata. Coordination admitted datastore transaction; singleton binding owner/incarnation/generation restricts rollout correspondence. Runtime process identity/template stamp checked by topologyLeaseAllowed, coordinated with final Apply membership lock; no tenant data access.
-- hikyo:instance-scoped
-- name: CoordinationSelfConfigSeedAttest :exec
INSERT INTO self_config_seed_attestations(node_id,schema_version,fingerprint,heartbeat_at) VALUES(sqlc.arg(node_id),sqlc.arg(schema_version),sqlc.arg(fingerprint),sqlc.arg(heartbeat_at)) ON CONFLICT(node_id) DO UPDATE SET schema_version=excluded.schema_version,fingerprint=excluded.fingerprint,heartbeat_at=excluded.heartbeat_at;

-- hikyo:reason Instance-owned self-configuration rollout/seed admission metadata. Coordination admitted datastore transaction; singleton binding owner/incarnation/generation restricts rollout correspondence. Runtime process identity/template stamp checked by topologyLeaseAllowed, coordinated with final Apply membership lock; no tenant data access.
-- hikyo:instance-scoped
-- name: CoordinationSelfConfigGeneration :one
SELECT b.owner_instance_id,b.generation,b.incarnation,b.suspended,EXISTS(SELECT 1 FROM self_config_jobs j JOIN self_config_rollouts r ON r.job_id=j.id WHERE j.generation=b.generation AND r.incarnation=b.incarnation AND json_extract(r.command_json,'$.command.action')='restore') AS deployment_restoring FROM self_config_binding b WHERE b.id=1;

-- hikyo:reason Instance-owned self-configuration rollout/seed admission metadata. Coordination admitted datastore transaction; singleton binding owner/incarnation/generation restricts rollout correspondence. Runtime process identity/template stamp checked by topologyLeaseAllowed, coordinated with final Apply membership lock; no tenant data access.
-- hikyo:instance-scoped
-- name: CoordinationCurrentTopology :one
SELECT r.command_json FROM self_config_rollouts r JOIN self_config_jobs j ON j.id=r.job_id JOIN self_config_binding b ON b.id=1 AND b.incarnation=r.incarnation WHERE j.generation<=b.generation AND j.status NOT IN ('preparing','aborted') AND json_type(r.command_json,'$.command.topology')='object' ORDER BY j.generation DESC LIMIT 1;

-- hikyo:reason Instance-owned self-configuration rollout/seed admission metadata. Coordination admitted datastore transaction; singleton binding owner/incarnation/generation restricts rollout correspondence. Runtime process identity/template stamp checked by topologyLeaseAllowed, coordinated with final Apply membership lock; no tenant data access.
-- hikyo:instance-scoped
-- name: CoordinationLatestRollout :one
SELECT r.command_json,r.response_json,j.generation=b.generation AS current_generation FROM self_config_rollouts r JOIN self_config_jobs j ON j.id=r.job_id JOIN self_config_binding b ON b.id=1 AND b.incarnation=r.incarnation WHERE j.generation<=b.generation AND j.status NOT IN ('preparing','aborted') ORDER BY j.generation DESC LIMIT 1;
