-- Route-move state transitions use the caller verified org/project chain.

-- name: AdapterMoveGet :one
SELECT m.id AS id,m.adapter_id AS adapter_id,m.kind AS kind,m.state AS state,m.keep_remote AS keep_remote,COALESCE(m.pending_origin,a.origin) AS pending_origin,m.created_at AS created_at,a.authority_principal_id AS authority_principal_id FROM adapter_route_moves m JOIN adapters a ON a.id=m.adapter_id AND a.org_id=m.org_id AND a.project_id=m.project_id WHERE m.id=sqlc.arg(move_id) AND m.org_id=sqlc.arg(chain_org) AND m.project_id=sqlc.arg(chain_project);

-- name: AdapterMoveGetLocked :one
SELECT m.id AS id,m.adapter_id AS adapter_id,m.kind AS kind,m.state AS state,m.keep_remote AS keep_remote,COALESCE(m.pending_origin,a.origin) AS pending_origin,m.created_at AS created_at,a.authority_principal_id AS authority_principal_id FROM adapter_route_moves m JOIN adapters a ON a.id=m.adapter_id AND a.org_id=m.org_id AND a.project_id=m.project_id WHERE m.id=sqlc.arg(move_id) AND m.org_id=sqlc.arg(chain_org) AND m.project_id=sqlc.arg(chain_project);

-- name: AdapterMoveTargets :many
SELECT target_id AS target_id,environment_id AS environment_id,destination_kind AS destination_kind,destination_owner AS destination_owner,destination_name AS destination_name,destination_environment AS destination_environment,destination_scope AS destination_scope,destination_id AS destination_id,repository_id AS repository_id,visibility AS visibility,CAST(selected_repository_ids AS TEXT) AS selected_repository_ids,name_prefix AS name_prefix,CAST(orphaned_names AS TEXT) AS orphaned_names FROM adapter_route_move_targets WHERE move_id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) ORDER BY target_id;

-- name: AdapterMoveJobs :many
SELECT id AS id,target_id AS target_id,kind AS kind,state AS state FROM adapter_outbox WHERE route_move_id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) ORDER BY created_at,id;

-- name: AdapterMoveCancelTarget :one
SELECT generation AS generation,CAST(CASE WHEN provider_lease_job_id IS NULL THEN 0 ELSE 1 END AS INTEGER) AS provider_busy FROM adapter_targets WHERE id=sqlc.arg(target_target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND state='moving' AND active_job_id IS NULL;

-- name: AdapterMoveInsertConvergeJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(job_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_target_id),'converge',sqlc.arg(move_id),sqlc.arg(authority_principal_id),sqlc.arg(next_generation),sqlc.arg(target_target_id2),0,sqlc.arg(next_attempt_at),'queued',sqlc.arg(created_at));

-- name: AdapterMoveActivateCanceledTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(next_generation),state='active',sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(job_id) WHERE id=sqlc.arg(target_target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND generation=sqlc.arg(generation) AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL;

-- name: AdapterMoveRestoreAdapter :execrows
UPDATE adapters SET state='active',authority_principal_id=sqlc.arg(authority_principal_id) WHERE id=sqlc.arg(move_adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state IN ('active','moving');

-- name: AdapterMoveDeleteClaims :execrows
DELETE FROM adapter_route_move_claims WHERE move_id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterMoveCancel :execrows
UPDATE adapter_route_moves SET state='canceled',pending_origin=NULL,pending_credential_ciphertext=NULL WHERE id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='attention_required';

-- name: AdapterMoveDeleteKeys :execrows
DELETE FROM adapter_route_move_keys WHERE move_id=sqlc.arg(move_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id);

-- name: AdapterMoveUpdatePendingTarget :execrows
UPDATE adapter_route_move_targets SET destination_kind=sqlc.arg(target_destination_kind),destination_owner=sqlc.arg(target_destination_owner),destination_name=sqlc.arg(target_destination_name),destination_environment=sqlc.arg(target_destination_environment),destination_scope=sqlc.arg(target_destination_scope),destination_id=0,repository_id=sqlc.arg(target_repository_id),visibility=sqlc.arg(target_visibility),selected_repository_ids=sqlc.arg(selected_repository_ids),name_prefix=sqlc.arg(target_name_prefix) WHERE move_id=sqlc.arg(move_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id);

-- name: AdapterMoveInsertKey :execrows
INSERT INTO adapter_route_move_keys (move_id,org_id,project_id,environment_id,target_id,key_id) VALUES (sqlc.arg(move_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_id),sqlc.arg(key_id));

-- name: AdapterMoveReplaceOriginCollisions :one
SELECT CAST((SELECT COUNT(*) FROM adapters WHERE adapters.org_id=sqlc.arg(chain_org) AND adapters.project_id=sqlc.arg(chain_project) AND adapters.id<>sqlc.arg(move_adapter_id) AND adapters.state<>'tombstoned' AND adapters.origin=sqlc.arg(origin))+(SELECT COUNT(*) FROM adapter_route_moves WHERE adapter_route_moves.org_id=sqlc.arg(chain_org2) AND adapter_route_moves.project_id=sqlc.arg(chain_project2) AND adapter_route_moves.id<>sqlc.arg(move_id) AND adapter_route_moves.state NOT IN ('completed','canceled') AND adapter_route_moves.pending_origin=sqlc.arg(origin2)) AS INTEGER) AS collisions;

-- name: AdapterMoveUpdatePendingOrigin :execrows
UPDATE adapter_route_moves SET pending_origin=sqlc.arg(origin),pending_credential_ciphertext=sqlc.arg(pending_credential) WHERE id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='attention_required' AND kind='origin';

-- name: AdapterMoveResetDestinations :execrows
UPDATE adapter_route_move_targets SET destination_id=0 WHERE move_id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterMovePendingKeyIDs :many
SELECT key_id AS key_id FROM adapter_route_move_keys WHERE move_id=sqlc.arg(move_id) AND target_id=sqlc.arg(target_target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) ORDER BY key_id;

-- name: AdapterMoveActivate :execrows
UPDATE adapter_route_moves SET state='activating',authority_principal_id=sqlc.arg(authority_principal_id) WHERE id=sqlc.arg(move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='attention_required';

-- name: AdapterMoveResumeTarget :one
SELECT generation AS generation FROM adapter_targets WHERE id=sqlc.arg(target_target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL;

-- name: AdapterMoveInsertActivateJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(job_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_target_id),'activate',sqlc.arg(move_id),sqlc.arg(authority_principal_id),sqlc.arg(next_generation),sqlc.arg(target_target_id2),0,sqlc.arg(next_attempt_at),'queued',sqlc.arg(created_at));

-- name: AdapterMoveMarkResumingTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(next_generation),sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(job_id) WHERE id=sqlc.arg(target_target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND generation=sqlc.arg(generation) AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL;

-- name: AdapterMoveUpdateAuthority :execrows
UPDATE adapters SET authority_principal_id=sqlc.arg(authority_principal_id) WHERE id=sqlc.arg(move_adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state IN ('active','moving');

-- name: AdapterMoveBeginOriginAdapter :one
SELECT a.origin AS current_origin,CAST((SELECT COUNT(*) FROM adapter_targets t WHERE t.adapter_id=a.id AND t.org_id=a.org_id AND t.project_id=a.project_id AND t.state='active' AND t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(observed_at)) AS INTEGER) AS provider_busy FROM adapters a WHERE a.id=sqlc.arg(mutation_adapter_id) AND a.org_id=sqlc.arg(chain_org) AND a.project_id=sqlc.arg(chain_project) AND a.state='active';

-- name: AdapterMoveBeginOriginCollisions :one
SELECT CAST((SELECT COUNT(*) FROM adapters WHERE adapters.org_id=sqlc.arg(chain_org) AND adapters.project_id=sqlc.arg(chain_project) AND adapters.id<>sqlc.arg(mutation_adapter_id) AND adapters.state<>'tombstoned' AND adapters.origin=sqlc.arg(mutation_origin))+(SELECT COUNT(*) FROM adapter_route_moves WHERE adapter_route_moves.org_id=sqlc.arg(chain_org2) AND adapter_route_moves.project_id=sqlc.arg(chain_project2) AND adapter_route_moves.state<>'completed' AND adapter_route_moves.pending_origin=sqlc.arg(mutation_origin2)) AS INTEGER) AS collision;

-- name: AdapterMoveBeginOriginTargets :many
SELECT t.id AS id,t.environment_id AS environment_id,t.destination_kind AS kind,t.destination_owner AS owner,t.destination_name AS name,t.destination_environment AS destination_environment,t.destination_scope AS destination_scope,t.destination_id AS destination_id,t.repository_id AS repository_id,t.visibility AS visibility,CAST(t.selected_repository_ids AS TEXT) AS selected_repository_ids,t.name_prefix AS prefix,t.generation AS generation,COALESCE(t.active_job_id,'') AS active_job,CAST(COALESCE((SELECT json_group_array(value) FROM (SELECT surface||':'||effective_name AS value FROM adapter_ledger WHERE target_id=t.id AND org_id=t.org_id AND project_id=t.project_id AND environment_id=t.environment_id AND state IN ('owned','dispatched') ORDER BY surface,effective_name)),'[]') AS TEXT) AS orphaned_names FROM adapter_targets t WHERE t.adapter_id=sqlc.arg(mutation_adapter_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active' ORDER BY t.id;

-- name: AdapterMoveInsertOrigin :execrows
INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,kind,pending_origin,pending_credential_ciphertext,authority_principal_id,state,keep_remote,created_at) VALUES (sqlc.arg(mutation_move_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(mutation_adapter_id),'origin',sqlc.arg(mutation_origin),sqlc.arg(mutation_pending_credential_ciphertext),sqlc.arg(mutation_authority_principal_id),sqlc.arg(move_state),sqlc.arg(keep_remote),sqlc.arg(created_at));

-- name: AdapterMoveInsertTarget :execrows
INSERT INTO adapter_route_move_targets (move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_environment,destination_scope,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,orphaned_names) VALUES (sqlc.arg(mutation_move_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_id),sqlc.arg(target_kind),sqlc.arg(target_owner),sqlc.arg(target_name),sqlc.arg(target_destination_environment),sqlc.arg(target_destination_scope),0,sqlc.arg(target_repository_id),sqlc.arg(target_visibility),sqlc.arg(selected_repository_ids),sqlc.arg(target_prefix),sqlc.arg(orphaned_names));

-- name: AdapterMoveTargetKeyIDs :many
SELECT key_id AS key_id FROM adapter_target_keys WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) ORDER BY key_id;

-- name: AdapterMoveSupersedeJob :execrows
UPDATE adapter_outbox SET state='superseded',finished_at=sqlc.arg(finished_at),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(target_active_job) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND state IN ('queued','running');

-- name: AdapterMoveReleaseLedger :execrows
UPDATE adapter_ledger SET state='released',missing=0,updated_at=sqlc.arg(updated_at) WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND state<>'released';

-- name: AdapterMoveInsertJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(job_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_id),sqlc.arg(job_kind),sqlc.arg(mutation_move_id),sqlc.arg(mutation_authority_principal_id),sqlc.arg(generation),sqlc.arg(target_id2),0,sqlc.arg(next_attempt_at),'queued',sqlc.arg(created_at));

-- name: AdapterMoveMarkMovingTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(generation),state='moving',sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(job_id) WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(target_environment_id) AND generation=sqlc.arg(target_generation) AND state='active' AND provider_lease_job_id IS NULL;

-- name: AdapterMoveMarkAdapterMoving :execrows
UPDATE adapters SET state='moving',authority_principal_id=sqlc.arg(mutation_authority_principal_id) WHERE id=sqlc.arg(mutation_adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterMoveBeginTarget :one
SELECT t.adapter_id AS adapter_id,a.origin AS origin,t.environment_id AS environment_id,t.destination_kind AS kind,t.destination_owner AS owner,t.destination_name AS name,t.destination_environment AS destination_environment,t.destination_scope AS destination_scope,t.destination_id AS destination_id,t.name_prefix AS prefix,t.generation AS generation,COALESCE(t.active_job_id,'') AS active_job,CAST(CASE WHEN t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(observed_at) THEN 1 ELSE 0 END AS INTEGER) AS provider_busy,CAST(COALESCE((SELECT json_group_array(value) FROM (SELECT surface||':'||effective_name AS value FROM adapter_ledger WHERE target_id=t.id AND org_id=t.org_id AND project_id=t.project_id AND environment_id=t.environment_id AND state IN ('owned','dispatched') ORDER BY surface,effective_name)),'[]') AS TEXT) AS orphaned_names FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(mutation_target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active' AND a.state='active';

-- name: AdapterMoveInsertTargetMove :execrows
INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,target_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES (sqlc.arg(mutation_move_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(current_adapter_id),sqlc.arg(mutation_target_id),'target',sqlc.arg(mutation_authority_principal_id),sqlc.arg(move_state),sqlc.arg(keep_remote),sqlc.arg(created_at));

-- name: AdapterMoveSetActiveAuthority :execrows
UPDATE adapters SET authority_principal_id=sqlc.arg(mutation_authority_principal_id) WHERE id=sqlc.arg(current_adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterMoveConfiguredCollision :one
SELECT CAST(COUNT(*) AS INTEGER) AS configured FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id LEFT JOIN adapter_target_keys tk ON tk.target_id=t.id AND tk.org_id=t.org_id AND tk.project_id=t.project_id AND tk.environment_id=t.environment_id LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.id<>sqlc.arg(target_id) AND t.state='active' AND a.state='active' AND a.origin=sqlc.arg(origin) AND t.destination_kind=sqlc.arg(target_destination_kind) AND t.destination_owner=sqlc.arg(target_destination_owner) AND t.destination_name=sqlc.arg(target_destination_name) AND t.destination_environment=sqlc.arg(target_destination_environment) AND t.destination_scope=sqlc.arg(target_destination_scope) AND (sqlc.arg(pending_effective)=t.name_prefix||sqlc.arg(sentinel_name) OR (sqlc.arg(pending_surface)=CASE WHEN k.classification='config' AND a.provider NOT IN ('cloudflare','vault-kv') THEN 'variable' ELSE 'secret' END AND sqlc.arg(pending_effective2)=t.name_prefix||k.name));

-- name: AdapterMoveInsertClaim :execrows
INSERT INTO adapter_route_move_claims (move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,destination_scope,surface,effective_name,normalized_name) VALUES (sqlc.arg(move_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_id),sqlc.arg(key_id),sqlc.arg(origin),sqlc.arg(target_destination_kind),sqlc.arg(target_destination_owner),sqlc.arg(target_destination_name),sqlc.arg(target_destination_environment),sqlc.arg(target_destination_scope),sqlc.arg(pending_surface),sqlc.arg(pending_effective),sqlc.arg(normalized_name));

-- name: AdapterMoveAWSConfiguredNames :many
SELECT t.id AS target_id,t.destination_kind AS kind,t.destination_name AS name,t.name_prefix AS prefix,COALESCE(k.name,'') AS key_name FROM adapter_targets t
		JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
		LEFT JOIN adapter_target_keys tk ON tk.target_id=t.id AND tk.org_id=t.org_id AND tk.project_id=t.project_id AND tk.environment_id=t.environment_id
		LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id
		WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.id<>sqlc.arg(target_id) AND t.state='active' AND a.state='active' AND a.origin=sqlc.arg(origin)
		AND t.destination_kind IN ('json-object','per-key') AND t.destination_owner=sqlc.arg(target_destination_owner)
		ORDER BY t.id,k.name;

-- name: AdapterMoveAWSPendingNames :many
SELECT target_id AS other_target,effective_name AS effective FROM adapter_route_move_claims WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND provider_origin=sqlc.arg(origin) AND destination_kind IN ('json-object','per-key') AND destination_owner=sqlc.arg(target_destination_owner) AND target_id<>sqlc.arg(target_id) ORDER BY target_id,effective_name;

-- name: AdapterMoveInsertAWSClaim :execrows
INSERT INTO adapter_route_move_claims (move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,surface,effective_name,normalized_name) VALUES (sqlc.arg(move_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(target_environment_id),sqlc.arg(target_id),sqlc.arg(key_id),sqlc.arg(origin),sqlc.arg(target_destination_kind),sqlc.arg(target_destination_owner),sqlc.arg(target_destination_name),sqlc.arg(target_destination_environment),sqlc.arg(surface),sqlc.arg(claim_effective_name),sqlc.arg(normalized_name));

-- name: AdapterMoveFlags :one
SELECT a.provider AS provider,t.variable_protected AS variable_protected,t.variable_hidden AS variable_hidden,t.variable_expand AS variable_expand FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.adapter_id=sqlc.arg(target_adapter_id) AND t.id=sqlc.arg(target_id);
