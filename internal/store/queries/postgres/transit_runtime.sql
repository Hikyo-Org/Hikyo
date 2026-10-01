-- hikyo:reason Closed TransitRuntime scrape authority returns only the installation-wide label-free live-key count, never key content or material.
-- hikyo:instance-scoped
-- name: TransitCountLive :one
SELECT COUNT(*) FROM transit_keys WHERE state<>'destroyed';

-- hikyo:reason Closed TransitRuntime scrape authority returns only the installation-wide label-free pending-deletion count, never key content or material.
-- hikyo:instance-scoped
-- name: TransitCountPendingDeletion :one
SELECT COUNT(*) FROM transit_keys WHERE state='pending-deletion';

-- hikyo:reason Closed TransitRuntime scrape authority reads only period/latest-created timestamps to compute the existing label-free rotation-due count; material and identities are excluded.
-- hikyo:instance-scoped
-- name: TransitGaugeRotationCandidates :many
SELECT k.rotation_period_seconds,v.created_at FROM transit_keys k JOIN transit_key_versions v ON v.key_id=k.id AND v.org_id=k.org_id AND v.version=k.latest_version WHERE k.state='active' AND k.rotation_period_seconds>0;
