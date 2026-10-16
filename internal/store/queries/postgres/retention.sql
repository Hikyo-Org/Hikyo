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
-- Walk at most the retained N lineage headers per environment using indexed
-- predecessor lookups, then collect only below the Nth newest revision. A
-- missing predecessor stops the walk: histories shorter than N have no cutoff.
-- All lineage counts, including collected payloads and sparse revision numbers.
-- The cutoff is recomputed in this statement's transaction snapshot each batch;
-- it is never cached across policy changes or retries. This bounds ranking work,
-- not the outer candidate scan or the complete sweep's transaction/audit work.
-- hikyo:instance-scoped
-- name: ListEligibleSnapshotPayloads :many
WITH RECURSIVE retention_window (
    org_id, project_id, environment_id, cutoff_revision, remaining
) AS (
    SELECT e.org_id, e.project_id, e.id,
           (SELECT n.revision FROM snapshots AS n
            WHERE n.org_id = e.org_id AND n.project_id = e.project_id
              AND n.environment_id = e.id
            ORDER BY n.revision DESC LIMIT 1),
           COALESCE(p.retention_revision_count, o.retention_revision_count)
    FROM environments AS e
    JOIN projects AS p ON p.org_id = e.org_id AND p.id = e.project_id
    JOIN orgs AS o ON o.id = e.org_id
    WHERE p.retention_age_seconds IS NOT NULL OR o.retention_mode <> 'unlimited'

    UNION ALL

    SELECT w.org_id, w.project_id, w.environment_id,
           (SELECT n.revision FROM snapshots AS n
            WHERE n.org_id = w.org_id AND n.project_id = w.project_id
              AND n.environment_id = w.environment_id
              AND n.revision < w.cutoff_revision
            ORDER BY n.revision DESC LIMIT 1),
           w.remaining - 1
    FROM retention_window AS w
    WHERE w.remaining > 1 AND w.cutoff_revision IS NOT NULL
)
SELECT s.id, s.org_id, s.project_id, s.environment_id, s.revision,
       COALESCE(p.retention_age_seconds, o.retention_age_seconds) AS age_seconds,
       COALESCE(p.retention_revision_count, o.retention_revision_count) AS revision_count
FROM snapshots AS s
JOIN projects AS p ON p.org_id = s.org_id AND p.id = s.project_id
JOIN orgs AS o ON o.id = s.org_id
JOIN retention_window AS w
  ON w.org_id = s.org_id AND w.project_id = s.project_id
 AND w.environment_id = s.environment_id AND w.remaining = 1
 AND s.revision < w.cutoff_revision
WHERE s.payload_present
  AND (p.retention_age_seconds IS NOT NULL OR o.retention_mode <> 'unlimited')
  AND s.published_at < sqlc.arg(now)::timestamptz -
      COALESCE(p.retention_age_seconds, o.retention_age_seconds) * INTERVAL '1 second'
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
