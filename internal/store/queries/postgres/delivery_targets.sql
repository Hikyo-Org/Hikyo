-- Delivery-target condition reporting (#788, k8s-condition-reporting ADR).

-- name: GetDeliveryTargetReport :one
SELECT id, org_id, project_id, environment_id, principal_id, cluster_id, instance_uid,
       target_uid, namespace, name, vocabulary, generation, observed_generation,
       reported_at, received_at, report_interval_seconds, lifecycle, conditions,
       reporter, reporter_version, refusal_cause, refused_at, created_at
FROM delivery_target_reports
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND principal_id = sqlc.arg(principal_id)
  AND cluster_id = sqlc.arg(cluster_id) AND instance_uid = sqlc.arg(instance_uid)
  AND target_uid = sqlc.arg(target_uid);

-- name: ListDeliveryTargetReports :many
SELECT id, org_id, project_id, environment_id, principal_id, cluster_id, instance_uid,
       target_uid, namespace, name, vocabulary, generation, observed_generation,
       reported_at, received_at, report_interval_seconds, lifecycle, conditions,
       reporter, reporter_version, refusal_cause, refused_at, created_at
FROM delivery_target_reports
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
ORDER BY cluster_id, namespace, name, id;

-- name: CountDeliveryTargetReportsForPrincipal :one
SELECT COUNT(*) FROM delivery_target_reports
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND principal_id = sqlc.arg(principal_id);

-- name: InsertDeliveryTargetReport :exec
INSERT INTO delivery_target_reports (
    id, org_id, project_id, environment_id, principal_id, cluster_id, instance_uid,
    target_uid, namespace, name, vocabulary, generation, observed_generation,
    reported_at, received_at, report_interval_seconds, lifecycle, conditions,
    reporter, reporter_version, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id), sqlc.arg(chain_env_id),
    sqlc.arg(principal_id), sqlc.arg(cluster_id), sqlc.arg(instance_uid), sqlc.arg(target_uid),
    sqlc.arg(namespace), sqlc.arg(name), sqlc.arg(vocabulary), sqlc.arg(generation),
    sqlc.arg(observed_generation), sqlc.arg(reported_at), sqlc.arg(received_at),
    sqlc.arg(report_interval_seconds), sqlc.arg(lifecycle), sqlc.arg(conditions),
    sqlc.arg(reporter), sqlc.arg(reporter_version), sqlc.arg(created_at)
);

-- An accepted report replaces the row's asserted state and clears any
-- recorded refusal: the refusal is older than the last accepted report now.
-- name: UpdateDeliveryTargetReport :execrows
UPDATE delivery_target_reports
SET namespace = sqlc.arg(namespace), name = sqlc.arg(name), vocabulary = sqlc.arg(vocabulary),
    generation = sqlc.arg(generation), observed_generation = sqlc.arg(observed_generation),
    reported_at = sqlc.arg(reported_at), received_at = sqlc.arg(received_at),
    report_interval_seconds = sqlc.arg(report_interval_seconds), lifecycle = sqlc.arg(lifecycle),
    conditions = sqlc.arg(conditions), reporter = sqlc.arg(reporter),
    reporter_version = sqlc.arg(reporter_version), refusal_cause = NULL, refused_at = NULL
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(id);

-- name: RecordDeliveryTargetRefusal :execrows
UPDATE delivery_target_reports
SET refusal_cause = sqlc.arg(refusal_cause), refused_at = sqlc.arg(refused_at)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(id);

-- name: DeleteDeliveryTargetReport :execrows
DELETE FROM delivery_target_reports
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(id);

-- name: UpdateDeliveryTargetQuotaNotice :execrows
UPDATE delivery_target_quota_notices SET refused_at = sqlc.arg(refused_at)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND principal_id = sqlc.arg(principal_id);

-- name: InsertDeliveryTargetQuotaNotice :exec
INSERT INTO delivery_target_quota_notices (principal_id, org_id, project_id, refused_at)
VALUES (sqlc.arg(principal_id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id), sqlc.arg(refused_at));

-- name: GetDeliveryTargetQuotaNotice :one
SELECT principal_id, refused_at FROM delivery_target_quota_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND principal_id = sqlc.arg(principal_id);

-- The server-observed layer (ADR D2): the last authenticated delivery fetch
-- of one principal in one environment, from the audit trail.
-- name: LastDeliveryFetchAt :one
SELECT actor_id, occurred_at FROM audit_tenant_events
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND env_id = sqlc.arg(chain_env_id) AND actor_id = sqlc.arg(actor_id)
  AND type = sqlc.arg(type)
ORDER BY seq DESC
LIMIT 1;

-- SelectExpiredDeliveryTargetReports is one bounded installation-wide purge
-- batch (ADR D6: no accepted report for 30 days). Repeated scheduler runs
-- commit progress without an unbounded backlog inside one transaction.
-- hikyo:instance-scoped
-- name: SelectExpiredDeliveryTargetReports :many
SELECT id, org_id, project_id, environment_id, principal_id, received_at
FROM delivery_target_reports
WHERE received_at < sqlc.arg(received_at)
ORDER BY received_at, id
LIMIT 100;

-- PurgeDeliveryTargetReport deletes one expired row, guarded by the same
-- cutoff so a report accepted since the select keeps its row.
-- hikyo:instance-scoped
-- name: PurgeDeliveryTargetReport :execrows
DELETE FROM delivery_target_reports
WHERE id = sqlc.arg(id) AND received_at < sqlc.arg(received_at);
