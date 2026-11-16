-- name: RotationCreate :execrows
INSERT INTO rotations (id,org_id,project_id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(provider_id),sqlc.arg(key_id),sqlc.narg(username_key_id),sqlc.arg(interval_seconds),sqlc.arg(revocation_delay_seconds),sqlc.arg(authority_principal_id),sqlc.arg(authority_principal_id),'active',sqlc.arg(next_run_at),1,sqlc.arg(at),sqlc.arg(at));

-- name: RotationGet :one
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RotationList :many
SELECT id,environment_id,provider_id,key_id,username_key_id,interval_seconds,revocation_delay_seconds,authority_principal_id,created_by,state,next_run_at,generation,created_at,updated_at FROM rotations WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY id;

-- name: RotationSetState :execrows
UPDATE rotations SET state=sqlc.arg(state),next_run_at=sqlc.narg(next_run_at),generation=generation+1,updated_at=sqlc.arg(at) WHERE id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(expected_generation) AND state<>'disabled';

-- name: RotationCountOpenRuns :one
SELECT COUNT(*) FROM rotation_runs WHERE rotation_id=sqlc.arg(rotation_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND phase NOT IN ('done','quarantine','failed');
