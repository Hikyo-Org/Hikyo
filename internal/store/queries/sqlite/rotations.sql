-- name: RotationCreate :execrows
INSERT INTO rotations (id,org_id,project_id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(provider_id),sqlc.arg(key_id),sqlc.arg(username_key_id),sqlc.arg(interval_seconds),sqlc.arg(revocation_delay_seconds),sqlc.arg(authority_principal_id),sqlc.arg(authority_principal_id),'active',sqlc.arg(next_run_at),1,sqlc.arg(at),sqlc.arg(at));

-- name: RotationGet :one
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationList :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'disabled' AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: RotationListAll :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND id > sqlc.arg(after_id) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: RotationSetState :execrows
UPDATE rotations SET state=sqlc.arg(state),halt_reason=NULL,next_run_at=sqlc.narg(next_run_at),generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(expected_generation) AND state<>'disabled';

-- name: RotationCountOpenRuns :one
SELECT COUNT(*) FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','quarantine','failed');

-- name: RotationClaimInsert :exec
INSERT INTO rotation_key_claims (org_id,project_id,environment_id,key_id,rotation_id) VALUES (sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(key_id),sqlc.arg(rotation_id));

-- name: RotationClaimsRelease :exec
DELETE FROM rotation_key_claims WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- The delivered credential's expiry: the VALID UNTIL of the latest published run
-- for this key across rotations, so a delete and recreate on the same keys still
-- reports the role that is live in them.
-- name: RotationCredentialExpiry :one
SELECT ru.id AS run_id, ru.candidate_valid_until AS expires_at FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ro.key_id=sqlc.arg(key_id) AND ru.published_revision IS NOT NULL ORDER BY ru.started_at DESC, ru.id DESC LIMIT 1;

-- The same expiry for every key of the environment in one read, newest first, so a
-- list never queries per row. The caller keeps the first row per key_id.
-- name: RotationCredentialExpiries :many
SELECT ro.key_id AS key_id, ru.id AS run_id, ru.candidate_valid_until AS expires_at FROM rotation_runs ru JOIN rotations ro ON ro.org_id=ru.org_id AND ro.project_id=ru.project_id AND ro.environment_id=ru.environment_id AND ro.id=ru.rotation_id WHERE ru.org_id=sqlc.arg(chain_org) AND ru.project_id=sqlc.arg(chain_project) AND ru.environment_id=sqlc.arg(chain_env) AND ru.published_revision IS NOT NULL ORDER BY ru.started_at DESC, ru.id DESC;

-- A run blocks an environment delete while it may hold a live role: every phase
-- except done and failed (quarantine and revoke_refused included), plus a failed
-- run that had already published. A failed run that never published had its
-- candidate confirmed dropped, so it and done are the only purgeable runs. The
-- rotation ids are returned so the refusal can name them (a page, with repeats).
-- name: RotationBlockingRunRotations :many
SELECT rotation_id FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','failed') ORDER BY rotation_id LIMIT sqlc.arg(page_limit);

-- name: RotationPublishedFailedRunRotations :many
SELECT rotation_id FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase='failed' AND published_revision IS NOT NULL ORDER BY rotation_id LIMIT sqlc.arg(page_limit);

-- name: RotationPurgeEffects :exec
DELETE FROM rotation_effects WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationPurgeRuns :exec
DELETE FROM rotation_runs WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationPurgeClaims :exec
DELETE FROM rotation_key_claims WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationPurge :exec
DELETE FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationListActiveByProvider :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND provider_id=sqlc.arg(provider_id) AND state='active' ORDER BY id;

-- name: RotationPauseActive :execrows
UPDATE rotations SET state='paused',next_run_at=NULL,generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: RotationListActiveInEnvironment :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND state='active' ORDER BY id;

-- name: RotationListActiveInProject :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active' ORDER BY id;
