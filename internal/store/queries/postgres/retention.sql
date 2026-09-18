-- Retention/GC (#53). Scheduler statements are cross-tenant by definition and
-- run only under the scheduler system-proof site. Tenant policy and pin reads
-- carry the ordinary proof-bound chain conjuncts.

-- name: LockSnapshotForRetentionConsequence :one
SELECT id FROM snapshots
WHERE org_id = sqlc.arg(chain_org_id)
  AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
  AND id = sqlc.arg(snapshot_id)
FOR UPDATE;

-- ListEligibleSnapshotPayloads selects the next batch of collectable payloads.
-- The latest-N cutoff is a per-environment count over the
-- (org_id, project_id, environment_id, revision DESC) index: a snapshot is
-- outside the window exactly when at least revision_count newer revisions
-- exist in its environment (collected or not, lineage is the unit of the
-- window). The outer scan touches only live payloads, so a collected backlog
-- never re-enters the ranking work.
-- hikyo:instance-scoped
-- name: ListEligibleSnapshotPayloads :many
SELECT s.id, s.org_id, s.project_id, s.environment_id, s.revision,
       COALESCE(p.retention_age_seconds, o.retention_age_seconds) AS age_seconds,
       COALESCE(p.retention_revision_count, o.retention_revision_count) AS revision_count
FROM snapshots AS s
JOIN projects AS p ON p.org_id = s.org_id AND p.id = s.project_id
JOIN orgs AS o ON o.id = s.org_id
WHERE s.payload_present
  AND (p.retention_age_seconds IS NOT NULL OR o.retention_mode <> 'unlimited')
  AND s.published_at < sqlc.arg(now)::timestamptz -
      COALESCE(p.retention_age_seconds, o.retention_age_seconds) * INTERVAL '1 second'
  AND (
      SELECT COUNT(*) FROM snapshots AS n
      WHERE n.org_id = s.org_id AND n.project_id = s.project_id
        AND n.environment_id = s.environment_id AND n.revision > s.revision
  ) >= COALESCE(p.retention_revision_count, o.retention_revision_count)
  AND NOT EXISTS (SELECT 1 FROM self_config_retention r WHERE r.snapshot_id = s.id)
  AND NOT EXISTS (
      SELECT 1 FROM revision_pins
      WHERE revision_pins.snapshot_id = s.id
        AND revision_pins.expires_at > sqlc.arg(now)::timestamptz
  )
ORDER BY s.org_id, s.project_id, s.environment_id, s.revision
LIMIT sqlc.arg(batch_limit);

-- MarkSnapshotCollected stamps the collection and drops the payload-class
-- columns held on the header row itself. The parameter contract is payload:
-- it is charged to the project storage quota beside the entries, every reader
-- of it needs the entries the collection removes, and '{}' decodes as the
-- valid empty contract.
-- hikyo:instance-scoped
-- name: MarkSnapshotCollected :execrows
UPDATE snapshots
SET payload_present = FALSE,
    collected_at = sqlc.arg(collected_at),
    collected_policy = sqlc.arg(collected_policy),
    parameter_contract = '{}'
WHERE snapshots.id = sqlc.arg(snapshot_id) AND snapshots.payload_present
  AND NOT EXISTS (SELECT 1 FROM self_config_retention r WHERE r.snapshot_id = snapshots.id)
  AND NOT EXISTS (
      SELECT 1 FROM revision_pins
      WHERE revision_pins.snapshot_id = snapshots.id
        AND revision_pins.expires_at > sqlc.arg(now)::timestamptz
  );

-- hikyo:instance-scoped
-- name: DeleteCollectedSnapshotEntries :execrows
DELETE FROM snapshot_entries
WHERE snapshot_id = sqlc.arg(snapshot_id)
  AND EXISTS (
      SELECT 1 FROM snapshots
      WHERE snapshots.id = snapshot_entries.snapshot_id
        AND NOT snapshots.payload_present
  );

-- hikyo:instance-scoped
-- name: GetLastPruneSuccess :one
SELECT last_prune_success FROM retention_runtime WHERE id = 1;

-- hikyo:instance-scoped
-- name: SetLastPruneSuccess :exec
INSERT INTO retention_runtime (id, last_prune_success)
VALUES (1, sqlc.arg(last_prune_success))
ON CONFLICT (id) DO UPDATE
SET last_prune_success = EXCLUDED.last_prune_success;
