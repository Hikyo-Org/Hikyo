-- Generic file synchronization (#164): file targets and their key selection.

-- name: InsertFileTarget :exec
INSERT INTO file_targets (
    id, org_id, project_id, environment_id, name, service_account_id, principal_id,
    generation, authority_principal_id, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id), sqlc.arg(environment_id),
    sqlc.arg(name), sqlc.arg(service_account_id), sqlc.arg(principal_id), 1,
    sqlc.arg(authority_principal_id), sqlc.arg(created_at), sqlc.arg(created_at)
);

-- name: GetFileTarget :one
SELECT id, environment_id, name, service_account_id, principal_id, generation,
       authority_principal_id, created_at, updated_at, report_state, report_revision,
       report_stamp, report_generation, reported_at, received_at
FROM file_targets
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id);

-- name: GetFileTargetForPrincipal :one
SELECT id, environment_id, name, service_account_id, principal_id, generation,
       authority_principal_id, created_at, updated_at, report_state, report_revision,
       report_stamp, report_generation, reported_at, received_at
FROM file_targets
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND principal_id = sqlc.arg(principal_id);

-- name: ListFileTargets :many
SELECT id, environment_id, name, service_account_id, principal_id, generation,
       authority_principal_id, created_at, updated_at, report_state, report_revision,
       report_stamp, report_generation, reported_at, received_at
FROM file_targets
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
ORDER BY environment_id, name, id;

-- name: ListFileTargetKeyIDs :many
SELECT key_id FROM file_target_keys
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND target_id = sqlc.arg(target_id)
ORDER BY key_id;

-- The selection's names and classifications come from the project catalogue,
-- read whole and filtered in Go: a JOIN is not a shape the predicate analyzer
-- can prove, and a catalogue is small.
-- name: ListFileTargetCatalogue :many
SELECT id, name, classification FROM keys
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
ORDER BY name, id;

-- name: InsertFileTargetKey :exec
INSERT INTO file_target_keys (org_id, project_id, environment_id, target_id, key_id)
VALUES (sqlc.arg(chain_org_id), sqlc.arg(chain_project_id), sqlc.arg(environment_id),
        sqlc.arg(target_id), sqlc.arg(key_id));

-- name: DeleteFileTargetKeys :exec
DELETE FROM file_target_keys
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND target_id = sqlc.arg(target_id);

-- A key-selection replacement is a compare-and-swap on the generation, so two
-- concurrent edits cannot both apply.
-- name: BumpFileTargetGeneration :execrows
UPDATE file_targets
SET generation = generation + 1, updated_at = sqlc.arg(updated_at),
    authority_principal_id = sqlc.arg(authority_principal_id)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id) AND generation = sqlc.arg(expected_generation);

-- name: DeleteFileTarget :execrows
DELETE FROM file_targets
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id);

-- The report is accepted only from the bound principal, in the target's own
-- environment: the WHERE clause is the authority, not a second check.
-- name: RecordFileTargetReport :execrows
UPDATE file_targets
SET report_state = sqlc.arg(report_state), report_revision = sqlc.arg(report_revision),
    report_stamp = sqlc.arg(report_stamp), report_generation = sqlc.arg(report_generation),
    reported_at = sqlc.arg(reported_at), received_at = sqlc.arg(received_at)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(id)
  AND principal_id = sqlc.arg(principal_id);
