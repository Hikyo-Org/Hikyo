-- Personal values: matrix counts and the offboarding report (#1235,
-- personal-values ADR). Tenant-scoped statements: the reserved chain_*
-- parameters are bound by the store's binding layer from proof fields only.
-- No statement selects ciphertext or an audit payload: counts, key names,
-- environment ids and timestamps only. ASCII only.

-- name: PersonalValueHolderCounts :many
SELECT key_id, COUNT(*) AS holders FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
GROUP BY key_id
ORDER BY key_id;

-- name: PersonalValueNoticeCounts :many
SELECT key_name, COUNT(*) AS notices FROM personal_value_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
GROUP BY key_name
ORDER BY key_name;

-- name: PersonalRequiredKeysForEnvironment :many
SELECT key_id FROM key_personal_required_environments
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
ORDER BY key_id;

-- PersonalReportReceipts is the offboarding report's evidence: the shared
-- keys a member is recorded as receiving, per environment, with the latest
-- receipt. It matches the recorded human authority as well as the actor,
-- because a developer credential's actor is the developer principal. Only
-- successful disclosure events count; personal_value.* is the member's own.
-- name: PersonalReportReceipts :many
SELECT env_id, CAST(COALESCE(payload::jsonb->>'key', payload::jsonb->>'name', '') AS TEXT) AS key_name, CAST(MAX(occurred_at) AS TIMESTAMPTZ) AS last_received_at
FROM audit_tenant_events
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND env_id IS NOT NULL AND outcome = 'success'
  AND type IN ('identity.disclosure', 'disclosure.value_revealed')
  AND (actor_id = sqlc.arg(member) OR authority_id = sqlc.arg(member))
GROUP BY env_id, key_name
ORDER BY env_id, key_name;

-- PersonalReportKeyChanges is when each key name was last published in each
-- environment, so the report can say whether a value changed since receipt.
-- name: PersonalReportKeyChanges :many
SELECT c.environment_id, c.key_name, CAST(MAX(s.published_at) AS TIMESTAMPTZ) AS last_changed_at
FROM revision_key_changes AS c
JOIN snapshots AS s ON s.org_id = c.org_id AND s.project_id = c.project_id
  AND s.environment_id = c.environment_id AND s.revision = c.revision
WHERE c.org_id = sqlc.arg(chain_org_id) AND c.project_id = sqlc.arg(chain_project_id)
  AND s.org_id = sqlc.arg(chain_org_id) AND s.project_id = sqlc.arg(chain_project_id)
GROUP BY c.environment_id, c.key_name
ORDER BY c.environment_id, c.key_name;

-- PersonalReportEvidenceFloor is the oldest event the project's trail still
-- holds: the report's evidence cannot reach further back.
-- name: PersonalReportEvidenceFloor :one
SELECT id, recorded_at FROM audit_tenant_events
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
ORDER BY recorded_at
LIMIT 1;

-- PersonalReportMemberSeen finds one project event naming the member as
-- actor, recorded authority or object, so a report about someone with no
-- recorded tie to the project answers as a nonexistent member. A report read
-- about the member is not a tie, or one report would make every later one
-- answer.
-- name: PersonalReportMemberSeen :one
SELECT id FROM audit_tenant_events
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND type <> 'personal_value.offboarding_report_read'
  AND (actor_id = sqlc.arg(member) OR authority_id = sqlc.arg(member) OR object_id = sqlc.arg(member))
LIMIT 1;
