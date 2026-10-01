-- name: AdapterListForReencrypt :many
SELECT id,credential_ciphertext FROM adapters WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id>sqlc.arg(cursor) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: AdapterReencrypt :execrows
UPDATE adapters SET credential_ciphertext=sqlc.arg(new_ciphertext) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=sqlc.arg(id) AND credential_ciphertext=sqlc.arg(old_ciphertext);

-- name: AdapterListMovesForReencrypt :many
SELECT id,adapter_id,pending_credential_ciphertext FROM adapter_route_moves WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id>sqlc.arg(cursor) ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: AdapterReencryptMove :execrows
UPDATE adapter_route_moves SET pending_credential_ciphertext=sqlc.arg(new_ciphertext) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=sqlc.arg(id) AND pending_credential_ciphertext=sqlc.arg(old_ciphertext);

-- name: AdapterGet :one
SELECT id,provider,origin,CAST(CASE WHEN credential_ciphertext IS NULL THEN 0 ELSE 1 END AS INTEGER) AS credential_present,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,CAST(CASE WHEN allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token FROM adapters WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state<>'tombstoned';

-- name: AdapterConfiguration :one
SELECT id,provider,origin,CAST(CASE WHEN credential_ciphertext IS NULL THEN 0 ELSE 1 END AS INTEGER) AS credential_present,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,CAST(CASE WHEN allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token,credential_ciphertext FROM adapters WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state<>'tombstoned';

-- name: AdapterList :many
SELECT id,provider,origin,CAST(CASE WHEN credential_ciphertext IS NULL THEN 0 ELSE 1 END AS INTEGER) AS credential_present,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,CAST(CASE WHEN allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token FROM adapters WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state<>'tombstoned' ORDER BY id;

-- name: AdapterGetTarget :one
SELECT t.id,t.adapter_id,t.environment_id,a.provider,a.origin,t.destination_kind,t.destination_owner,t.destination_name,t.destination_environment,t.destination_id,t.repository_id,t.visibility,t.selected_repository_ids,t.name_prefix,t.generation,t.state,t.sync_status,t.converged_revision,t.failure_names,t.warnings,a.authority_principal_id,t.paused_at,t.last_attempted_revision,t.last_attempted_at,t.last_error_class,CAST(CASE WHEN t.drift_attention THEN 1 ELSE 0 END AS INTEGER) AS drift_attention,CAST(COALESCE(j.state,'') AS TEXT) AS active_job_state,j.next_attempt_at,CAST(COALESCE(j.attempt_count,0) AS INTEGER) AS attempt_count,t.destination_scope,CAST(CASE WHEN t.variable_protected THEN 1 ELSE 0 END AS INTEGER) AS variable_protected,CAST(CASE WHEN t.variable_hidden THEN 1 ELSE 0 END AS INTEGER) AS variable_hidden,CAST(CASE WHEN t.variable_expand THEN 1 ELSE 0 END AS INTEGER) AS variable_expand FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id LEFT JOIN adapter_outbox j ON j.id=t.active_job_id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project);

-- name: AdapterListTargets :many
SELECT t.id,t.adapter_id,t.environment_id,a.provider,a.origin,t.destination_kind,t.destination_owner,t.destination_name,t.destination_environment,t.destination_id,t.repository_id,t.visibility,t.selected_repository_ids,t.name_prefix,t.generation,t.state,t.sync_status,t.converged_revision,t.failure_names,t.warnings,a.authority_principal_id,t.paused_at,t.last_attempted_revision,t.last_attempted_at,t.last_error_class,CAST(CASE WHEN t.drift_attention THEN 1 ELSE 0 END AS INTEGER) AS drift_attention,CAST(COALESCE(j.state,'') AS TEXT) AS active_job_state,j.next_attempt_at,CAST(COALESCE(j.attempt_count,0) AS INTEGER) AS attempt_count,t.destination_scope,CAST(CASE WHEN t.variable_protected THEN 1 ELSE 0 END AS INTEGER) AS variable_protected,CAST(CASE WHEN t.variable_hidden THEN 1 ELSE 0 END AS INTEGER) AS variable_hidden,CAST(CASE WHEN t.variable_expand THEN 1 ELSE 0 END AS INTEGER) AS variable_expand FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id LEFT JOIN adapter_outbox j ON j.id=t.active_job_id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id WHERE t.adapter_id=sqlc.arg(adapter_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active' ORDER BY t.id;

-- name: AdapterGetActiveTargetForUpdate :one
SELECT t.id,t.adapter_id,t.environment_id,a.provider,a.origin,t.destination_kind,t.destination_owner,t.destination_name,t.destination_environment,t.destination_id,t.repository_id,t.visibility,t.selected_repository_ids,t.name_prefix,t.generation,t.state,t.sync_status,t.converged_revision,t.failure_names,t.warnings,a.authority_principal_id,t.paused_at,t.last_attempted_revision,t.last_attempted_at,t.last_error_class,CAST(CASE WHEN t.drift_attention THEN 1 ELSE 0 END AS INTEGER) AS drift_attention,CAST(COALESCE(j.state,'') AS TEXT) AS active_job_state,j.next_attempt_at,CAST(COALESCE(j.attempt_count,0) AS INTEGER) AS attempt_count,t.destination_scope,CAST(CASE WHEN t.variable_protected THEN 1 ELSE 0 END AS INTEGER) AS variable_protected,CAST(CASE WHEN t.variable_hidden THEN 1 ELSE 0 END AS INTEGER) AS variable_hidden,CAST(CASE WHEN t.variable_expand THEN 1 ELSE 0 END AS INTEGER) AS variable_expand FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id LEFT JOIN adapter_outbox j ON j.id=t.active_job_id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active';

-- name: AdapterTargetKeyIDs :many
SELECT key_id FROM adapter_target_keys WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) ORDER BY key_id;

-- name: AdapterTargetKeys :many
SELECT k.id,k.name,k.classification FROM adapter_target_keys tk JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id WHERE tk.target_id=sqlc.arg(target_id) AND tk.org_id=sqlc.arg(chain_org) AND tk.project_id=sqlc.arg(chain_project) ORDER BY k.name;

-- name: AdapterProvider :many
SELECT provider FROM adapters WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterGetActiveForUpdate :one
SELECT id,provider,origin,CAST(CASE WHEN credential_ciphertext IS NULL THEN 0 ELSE 1 END AS INTEGER) AS credential_present,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,CAST(CASE WHEN allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token FROM adapters WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterRecordCredentialExpiry :execrows
UPDATE adapters SET credential_expires_at=sqlc.arg(expires_at) WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterBeginConfigureEffect :exec
INSERT INTO adapter_configure_fences (target_id,org_id,project_id,environment_id,destination_kind,destination_owner,destination_name,destination_environment,generation,effect_id,lease_expires_at,state,created_at) VALUES (sqlc.arg(target_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(destination_kind),sqlc.arg(destination_owner),sqlc.arg(destination_name),sqlc.arg(destination_environment),sqlc.arg(generation),sqlc.arg(effect_id),sqlc.arg(lease_expires_at),'leased',sqlc.arg(created_at));

-- name: AdapterFinishConfigureEffect :execrows
UPDATE adapter_configure_fences SET state=sqlc.arg(outcome),completed_at=sqlc.arg(completed_at) WHERE target_id=sqlc.arg(target_id) AND effect_id=sqlc.arg(effect_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='leased';

-- name: AdapterInsertTarget :exec
INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,generation,state,sync_status,created_at,destination_scope,variable_protected,variable_hidden,variable_expand) VALUES (sqlc.arg(target_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(adapter_id),sqlc.arg(destination_kind),sqlc.arg(destination_owner),sqlc.arg(destination_name),sqlc.arg(destination_environment),sqlc.arg(destination_id),sqlc.arg(repository_id),sqlc.arg(visibility),sqlc.arg(selected_repository_ids),sqlc.arg(name_prefix),1,'active','never',sqlc.arg(created_at),sqlc.arg(destination_scope),sqlc.arg(variable_protected),sqlc.arg(variable_hidden),sqlc.arg(variable_expand));

-- name: AdapterInsertTargetKey :exec
INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES (sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(target_id),sqlc.arg(adapter_id),sqlc.arg(key_id));

-- name: AdapterCreate :exec
INSERT INTO adapters (id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,allow_personal_token) VALUES (sqlc.arg(adapter_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(provider),sqlc.arg(origin),sqlc.arg(credential_ciphertext),sqlc.arg(credential_set_at),sqlc.narg(credential_expires_at),sqlc.arg(authority_principal_id),'active',sqlc.arg(created_at),sqlc.arg(spki_pin),sqlc.arg(ca_bundle_pem),sqlc.arg(allow_personal_token));

-- name: AdapterUpdateAuthorityExpiry :execrows
UPDATE adapters SET authority_principal_id=sqlc.arg(authority_principal_id),credential_expires_at=COALESCE(sqlc.narg(credential_expires_at),credential_expires_at) WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterTargetActiveJob :one
SELECT CAST(COALESCE(active_job_id,'') AS TEXT) AS active_job_id FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id);

-- name: AdapterDeleteTargetKeys :exec
DELETE FROM adapter_target_keys WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id);

-- name: AdapterUpdateTargetConfig :execrows
UPDATE adapter_targets SET visibility=sqlc.arg(visibility),selected_repository_ids=sqlc.arg(selected_repository_ids),name_prefix=sqlc.arg(name_prefix),variable_protected=sqlc.arg(variable_protected),variable_hidden=sqlc.arg(variable_hidden),variable_expand=sqlc.arg(variable_expand) WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND generation=sqlc.arg(expected_generation) AND state='active' AND provider_lease_job_id IS NULL;

-- name: AdapterUpdateActiveAuthority :execrows
UPDATE adapters SET authority_principal_id=sqlc.arg(authority_principal_id) WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterMapping :many
SELECT e.key_id,e.key_name,e.classification FROM snapshot_entries e JOIN adapter_target_keys k ON k.key_id=e.key_id AND k.target_id=sqlc.arg(target_id) AND k.org_id=e.org_id AND k.project_id=e.project_id AND k.environment_id=e.environment_id JOIN adapter_targets t ON t.id=k.target_id AND t.org_id=k.org_id AND t.project_id=k.project_id AND t.environment_id=k.environment_id WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND e.snapshot_id=(SELECT latest.id FROM snapshots latest WHERE latest.org_id=t.org_id AND latest.project_id=t.project_id AND latest.environment_id=t.environment_id AND latest.payload_present=1 ORDER BY latest.revision DESC LIMIT 1) ORDER BY e.key_name;

-- name: AdapterPlanCredential :one
SELECT credential_ciphertext,spki_pin,ca_bundle_pem,CAST(CASE WHEN allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token FROM adapters WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterPlanManifest :many
SELECT e.key_id,e.key_name,e.classification FROM snapshot_entries e JOIN adapter_target_keys k ON k.key_id=e.key_id AND k.target_id=sqlc.arg(target_id) AND k.org_id=e.org_id AND k.project_id=e.project_id AND k.environment_id=e.environment_id WHERE e.snapshot_id=(SELECT latest.id FROM snapshots latest WHERE latest.org_id=sqlc.arg(chain_org) AND latest.project_id=sqlc.arg(chain_project) AND latest.environment_id=sqlc.arg(environment_id) AND latest.payload_present=1 ORDER BY latest.revision DESC LIMIT 1) AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(environment_id) ORDER BY e.key_name;

-- name: AdapterPlanLedger :many
SELECT l.surface,l.effective_name,l.state,l.missing,CAST(CASE WHEN l.state='owned' AND EXISTS (SELECT 1 FROM adapter_conflicts c WHERE c.target_id=l.target_id AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(environment_id) AND c.target_generation=t.generation-1 AND c.destination_id=l.destination_id AND c.repository_id=l.repository_id AND c.surface=l.surface AND UPPER(c.effective_name)=l.normalized_name AND c.adopted_at IS NOT NULL AND julianday(c.adopted_at) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM adapter_effects e WHERE e.target_id=l.target_id AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(environment_id) AND e.surface=l.surface AND UPPER(e.effective_name)=l.normalized_name AND e.disposition IN ('create','update') AND e.outcome='success' AND (julianday(e.finished_at) IS NULL OR julianday(e.finished_at)>=julianday(c.adopted_at)))) THEN 1 ELSE 0 END AS INTEGER) AS adoption_pending FROM adapter_ledger l JOIN adapter_targets t ON t.id=l.target_id AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(environment_id) AND t.generation=sqlc.arg(generation) WHERE l.target_id=sqlc.arg(target_id) AND l.org_id=sqlc.arg(chain_org) AND l.project_id=sqlc.arg(chain_project) AND l.environment_id=sqlc.arg(environment_id) AND l.state<>'released' ORDER BY l.surface,l.effective_name;

-- name: AdapterTargetEnvironments :many
SELECT sibling.environment_id FROM adapter_targets target JOIN adapter_targets sibling ON sibling.adapter_id=target.adapter_id AND sibling.org_id=target.org_id AND sibling.project_id=target.project_id WHERE target.id=sqlc.arg(target_id) AND target.org_id=sqlc.arg(chain_org) AND target.project_id=sqlc.arg(chain_project) AND sibling.state<>'tombstoned' ORDER BY sibling.environment_id;

-- name: AdapterEnvironments :many
SELECT DISTINCT environment_id FROM adapter_targets WHERE adapter_id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active' ORDER BY environment_id;

-- name: AdapterConflicts :many
SELECT c.artifact_id,c.target_id,c.job_id,c.destination_id,c.repository_id,c.target_generation,c.surface,c.effective_name,c.created_at FROM adapter_conflicts c JOIN adapter_targets t ON t.id=c.target_id AND t.org_id=c.org_id AND t.project_id=c.project_id WHERE c.target_id=sqlc.arg(target_id) AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.adopted_at IS NULL AND c.target_generation=t.generation ORDER BY c.created_at,c.artifact_id,c.surface,c.effective_name;

-- name: AdapterPlanTarget :one
SELECT environment_id,destination_id,repository_id,generation FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterInsertConflict :execrows
INSERT INTO adapter_conflicts (id,artifact_id,org_id,project_id,environment_id,target_id,job_id,destination_id,repository_id,target_generation,surface,effective_name,created_at) VALUES (sqlc.arg(id),sqlc.arg(artifact_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(target_id),NULL,sqlc.arg(destination_id),sqlc.arg(repository_id),sqlc.arg(target_generation),sqlc.arg(surface),sqlc.arg(effective_name),sqlc.arg(created_at));

-- name: AdapterAdoptionTarget :one
SELECT t.adapter_id,t.environment_id,a.origin,t.destination_kind,t.repository_id,t.destination_id,t.generation,CAST(CASE WHEN t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(at) THEN 1 ELSE 0 END AS INTEGER) AS provider_busy,CAST(COALESCE(t.active_job_id,'') AS TEXT) AS prior_job FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active';

-- name: AdapterAdoptionConflictCount :one
SELECT COUNT(*) FROM adapter_conflicts WHERE artifact_id=sqlc.arg(artifact_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND repository_id=sqlc.arg(repository_id) AND destination_id=sqlc.arg(destination_id) AND target_generation=sqlc.arg(generation) AND surface=sqlc.arg(surface) AND effective_name=sqlc.arg(effective_name) AND adopted_at IS NULL;

-- name: AdapterAdoptionInsertLedger :execrows
INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,surface,effective_name,normalized_name,state,updated_at) VALUES (sqlc.arg(ledger_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(target_id),sqlc.arg(provider_origin),sqlc.arg(destination_kind),sqlc.arg(repository_id),sqlc.arg(destination_id),sqlc.arg(surface),sqlc.arg(effective_name),sqlc.arg(normalized_name),'owned',sqlc.arg(updated_at));

-- name: AdapterAdoptionMarkConflict :execrows
UPDATE adapter_conflicts SET adopted_at=sqlc.arg(adopted_at) WHERE artifact_id=sqlc.arg(artifact_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND surface=sqlc.arg(surface) AND effective_name=sqlc.arg(effective_name) AND adopted_at IS NULL;

-- name: AdapterAdoptionSupersedeJob :execrows
UPDATE adapter_outbox SET state='superseded',finished_at=sqlc.arg(finished_at),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(job_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND state IN ('queued','running');

-- name: AdapterAdoptionInsertJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(job_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(target_id),'converge',sqlc.arg(authority_principal_id),sqlc.arg(generation),sqlc.arg(dedup_key),0,sqlc.arg(next_attempt_at),'queued',sqlc.arg(created_at));

-- name: AdapterAdoptionUpdateTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(next_generation),sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(job_id),provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND generation=sqlc.arg(expected_generation) AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(at));

-- name: AdapterAdoptionUpdateAuthority :execrows
UPDATE adapters SET authority_principal_id=sqlc.arg(authority_principal_id) WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterPublishedTargets :many
SELECT t.id,t.environment_id,a.authority_principal_id,t.generation,CAST(COALESCE(t.active_job_id,'') AS TEXT) AS active_job_id FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.state='active' AND t.paused_at IS NULL ORDER BY t.id;

-- name: AdapterEnqueueTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(next_generation),sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(job_id) WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND generation=sqlc.arg(expected_generation);

-- name: AdapterManualTarget :one
SELECT t.id,t.environment_id,t.generation,CAST(COALESCE(t.active_job_id,'') AS TEXT) AS active_job_id,CAST(CASE WHEN t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(at) THEN 1 ELSE 0 END AS INTEGER) AS provider_busy,CAST(CASE WHEN t.paused_at IS NULL THEN 0 ELSE 1 END AS INTEGER) AS paused FROM adapter_targets t WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active';

-- name: AdapterPauseTarget :one
SELECT t.adapter_id,t.id,t.environment_id,a.authority_principal_id,t.generation,CAST(COALESCE(t.active_job_id,'') AS TEXT) AS active_job_id,CAST(CASE WHEN t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(at) THEN 1 ELSE 0 END AS INTEGER) AS provider_busy,CAST(CASE WHEN t.paused_at IS NULL THEN 0 ELSE 1 END AS INTEGER) AS paused FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active';

-- name: AdapterPauseTargetUpdate :execrows
UPDATE adapter_targets SET paused_at=sqlc.arg(paused_at),generation=sqlc.arg(next_generation),active_job_id=NULL,provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND generation=sqlc.arg(expected_generation) AND state='active' AND paused_at IS NULL AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(at));

-- name: AdapterResumeTarget :execrows
UPDATE adapter_targets SET paused_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND generation=sqlc.arg(expected_generation) AND paused_at IS NOT NULL;

-- name: AdapterResumeRevision :one
SELECT CAST(COALESCE(MAX(revision),0) AS INTEGER) AS revision FROM snapshots WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND payload_present=1;

-- hikyo:reason Verified instance HealthCounts authority reads only aggregate target/outbox counts for operator gauges; no tenant rows or writes are returned.
-- hikyo:instance-scoped
-- name: AdapterHealthCounts :one
SELECT CAST((SELECT COUNT(*) FROM adapter_targets WHERE state='active' AND sync_status='failed' AND paused_at IS NULL) AS INTEGER) AS targets_failed,CAST((SELECT COUNT(*) FROM adapter_targets WHERE state='active' AND paused_at IS NOT NULL) AS INTEGER) AS targets_paused,CAST((SELECT COUNT(*) FROM adapter_targets WHERE state='active' AND drift_attention=1) AS INTEGER) AS targets_attention,CAST((SELECT COUNT(*) FROM adapter_outbox WHERE state='queued') AS INTEGER) AS jobs_queued;

-- name: AdapterTeardownTarget :one
SELECT t.adapter_id,t.id,t.environment_id,a.authority_principal_id,t.generation,CAST(COALESCE(t.active_job_id,'') AS TEXT) AS active_job_id,CAST(CASE WHEN t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(at) THEN 1 ELSE 0 END AS INTEGER) AS provider_busy FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active';

-- name: AdapterTeardownAuthority :one
SELECT authority_principal_id FROM adapters WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterTeardownTargets :many
SELECT t.adapter_id,t.id,t.environment_id,a.authority_principal_id,t.generation,CAST(COALESCE(t.active_job_id,'') AS TEXT) AS active_job_id,CAST(CASE WHEN t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(at) THEN 1 ELSE 0 END AS INTEGER) AS provider_busy FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.adapter_id=sqlc.arg(adapter_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.state='active' ORDER BY t.id;

-- name: AdapterOrphans :many
SELECT surface,effective_name FROM adapter_ledger WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND state IN ('owned','dispatched') ORDER BY surface,effective_name;

-- name: AdapterReleaseLedger :exec
UPDATE adapter_ledger SET state='released',missing=0,updated_at=sqlc.arg(updated_at) WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND state<>'released';

-- name: AdapterRetainTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(next_generation),state='tombstoned',sync_status='converged',failure_names='[]',active_job_id=NULL,provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND generation=sqlc.arg(expected_generation) AND state='active' AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(at));

-- name: AdapterInsertScrubJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(job_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment_id),sqlc.arg(target_id),'scrub',sqlc.arg(authority_principal_id),sqlc.arg(generation),sqlc.arg(dedup_key),0,sqlc.arg(next_attempt_at),'queued',sqlc.arg(created_at));

-- name: AdapterScrubTarget :execrows
UPDATE adapter_targets SET generation=sqlc.arg(next_generation),state='tombstoned',sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(job_id),provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment_id) AND generation=sqlc.arg(expected_generation) AND state='active' AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(at));

-- name: AdapterMarkTombstoned :execrows
UPDATE adapters SET state='tombstoned' WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterEraseUnusedCredential :exec
UPDATE adapters SET credential_ciphertext=NULL,credential_set_at=NULL,credential_expires_at=NULL WHERE adapters.id=sqlc.arg(adapter_id) AND adapters.org_id=sqlc.arg(chain_org) AND adapters.project_id=sqlc.arg(chain_project) AND NOT EXISTS (SELECT 1 FROM adapter_targets WHERE adapter_id=sqlc.arg(adapter_id) AND state<>'tombstoned') AND NOT EXISTS (SELECT 1 FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id WHERE t.adapter_id=sqlc.arg(adapter_id) AND j.kind='scrub' AND j.state IN ('queued','running'));

-- name: AdapterReplaceCredentialTarget :one
SELECT a.authority_principal_id,CAST((SELECT COUNT(*) FROM adapter_targets t WHERE t.adapter_id=a.id AND t.org_id=a.org_id AND t.project_id=a.project_id AND t.state='active') AS INTEGER) AS target_count,CAST((SELECT COUNT(*) FROM adapter_targets t WHERE t.adapter_id=a.id AND t.org_id=a.org_id AND t.project_id=a.project_id AND t.state='active' AND t.provider_lease_job_id IS NOT NULL AND t.provider_lease_expires_at>sqlc.arg(at)) AS INTEGER) AS provider_busy FROM adapters a WHERE a.id=sqlc.arg(adapter_id) AND a.org_id=sqlc.arg(chain_org) AND a.project_id=sqlc.arg(chain_project) AND a.state='active';

-- name: AdapterReplaceCredential :execrows
UPDATE adapters SET credential_ciphertext=sqlc.arg(credential_ciphertext),credential_set_at=sqlc.arg(at),credential_expires_at=NULL,authority_principal_id=sqlc.arg(authority_principal_id) WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterReplaceCredentialBump :execrows
UPDATE adapter_targets SET generation=generation+1,provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE adapter_id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active' AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(at));

-- name: AdapterRevokeCredentialTarget :one
SELECT a.authority_principal_id,CAST((SELECT COUNT(*) FROM adapter_targets t WHERE t.adapter_id=a.id AND t.org_id=a.org_id AND t.project_id=a.project_id AND t.state='active') AS INTEGER) AS target_count FROM adapters a WHERE a.id=sqlc.arg(adapter_id) AND a.org_id=sqlc.arg(chain_org) AND a.project_id=sqlc.arg(chain_project) AND a.state='active';

-- name: AdapterRevokeCredential :execrows
UPDATE adapters SET credential_ciphertext=NULL,credential_set_at=NULL,credential_expires_at=NULL WHERE id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterRevokeCredentialBump :execrows
UPDATE adapter_targets SET generation=generation+1 WHERE adapter_id=sqlc.arg(adapter_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';
