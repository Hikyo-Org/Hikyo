-- name: AdapterWorkerLoadExecutionQuery :one
SELECT a.provider,a.origin,a.id,a.credential_ciphertext,t.destination_kind,t.destination_owner,t.destination_name,t.destination_environment,t.destination_id,t.repository_id,t.visibility,t.selected_repository_ids,t.name_prefix,t.generation,CAST(CASE WHEN COALESCE(t.last_attempted_revision,0)>=COALESCE(t.converged_revision,0) THEN COALESCE(t.last_attempted_revision,0) ELSE COALESCE(t.converged_revision,0) END AS INTEGER) AS revision,a.spki_pin,a.ca_bundle_pem,CAST(CASE WHEN a.allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token,t.destination_scope,CAST(CASE WHEN t.variable_protected THEN 1 ELSE 0 END AS INTEGER) AS variable_protected,CAST(CASE WHEN t.variable_hidden THEN 1 ELSE 0 END AS INTEGER) AS variable_hidden,CAST(CASE WHEN t.variable_expand THEN 1 ELSE 0 END AS INTEGER) AS variable_expand FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_outbox j ON j.id=sqlc.arg(job_id) AND j.target_id=t.id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.generation=sqlc.arg(generation) AND j.state='running' AND j.lease_owner=sqlc.arg(lease_owner) AND ((j.kind='converge' AND t.state='active' AND a.state='active' AND t.paused_at IS NULL) OR (j.kind='activate' AND t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL) OR (j.kind='scrub' AND (t.state='tombstoned' OR (t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL AND j.route_move_id IS NOT NULL))));

-- name: AdapterWorkerPinExecutionRevision :execrows
UPDATE adapter_targets SET last_attempted_revision=CASE WHEN COALESCE(last_attempted_revision,0)<CAST(sqlc.arg(revision) AS BIGINT) THEN CAST(sqlc.arg(revision) AS BIGINT) ELSE last_attempted_revision END WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation);

-- name: AdapterWorkerPinExecutionLease :execrows
UPDATE adapter_outbox SET lease_owner=lease_owner WHERE id=sqlc.arg(job_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND state='running' AND lease_owner=sqlc.arg(lease_owner);

-- name: AdapterWorkerLoadExecutionLedgerQuery :many
SELECT l.surface,l.effective_name,l.state,l.missing,CAST(CASE WHEN l.state='owned' AND EXISTS (SELECT 1 FROM adapter_conflicts c WHERE c.target_id=l.target_id AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(chain_env) AND c.target_generation=t.generation-1 AND c.destination_id=l.destination_id AND c.repository_id=l.repository_id AND c.surface=l.surface AND UPPER(c.effective_name)=l.normalized_name AND c.adopted_at IS NOT NULL AND julianday(c.adopted_at) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM adapter_effects e WHERE e.target_id=l.target_id AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(chain_env) AND e.surface=l.surface AND UPPER(e.effective_name)=l.normalized_name AND e.disposition IN ('create','update') AND e.outcome='success' AND (julianday(e.finished_at) IS NULL OR julianday(e.finished_at)>=julianday(c.adopted_at)))) THEN 1 ELSE 0 END AS INTEGER) AS adoption_pending,(SELECT c.observed_provider_version FROM adapter_conflicts c WHERE c.target_id=l.target_id AND c.org_id=sqlc.arg(chain_org) AND c.project_id=sqlc.arg(chain_project) AND c.environment_id=sqlc.arg(chain_env) AND c.target_generation=t.generation-1 AND c.destination_id=l.destination_id AND c.repository_id=l.repository_id AND c.surface=l.surface AND UPPER(c.effective_name)=l.normalized_name AND c.adopted_at IS NOT NULL AND julianday(c.adopted_at) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM adapter_effects e WHERE e.target_id=l.target_id AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(chain_env) AND e.surface=l.surface AND UPPER(e.effective_name)=l.normalized_name AND e.disposition IN ('create','update') AND e.outcome='success' AND (julianday(e.finished_at) IS NULL OR julianday(e.finished_at)>=julianday(c.adopted_at))) AND c.observed_provider_version IS NOT NULL ORDER BY julianday(c.adopted_at) DESC,c.id DESC LIMIT 1) AS adoption_version FROM adapter_ledger l JOIN adapter_targets t ON t.id=l.target_id AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.generation=sqlc.arg(generation) WHERE l.target_id=sqlc.arg(target_id) AND l.org_id=sqlc.arg(chain_org) AND l.project_id=sqlc.arg(chain_project) AND l.environment_id=sqlc.arg(chain_env) AND l.state<>'released' ORDER BY l.surface,l.normalized_name;

-- name: AdapterWorkerLoadExecutionSnapshotQuery :one
SELECT id,revision,parameter_contract FROM snapshots WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND payload_present=1 ORDER BY revision DESC LIMIT 1;

-- Successful names resume only this leased job's exact loaded source. Legacy
-- INTENTs without the worker-bound numeric revision never authorize skipping.
-- name: AdapterWorkerLoadExecutionCompletedQuery :many
SELECT DISTINCT e.surface,e.effective_name,e.disposition
FROM adapter_effects e
JOIN adapter_outbox j ON j.id=e.job_id AND j.target_id=e.target_id AND j.org_id=e.org_id AND j.project_id=e.project_id AND j.environment_id=e.environment_id
JOIN adapter_targets t ON t.id=e.target_id AND t.org_id=e.org_id AND t.project_id=e.project_id AND t.environment_id=e.environment_id AND t.generation=j.generation
JOIN adapter_ledger l ON l.target_id=e.target_id AND l.org_id=e.org_id AND l.project_id=e.project_id AND l.environment_id=e.environment_id AND l.surface=e.surface AND l.effective_name=e.effective_name AND l.state='owned' AND l.missing=0
JOIN audit_tenant_events i ON i.id=e.intent_audit_id AND i.org_id=sqlc.arg(chain_org) AND i.project_id=sqlc.arg(chain_project) AND i.env_id=sqlc.arg(chain_env)
WHERE e.job_id=sqlc.arg(job_id) AND e.target_id=sqlc.arg(target_id) AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(chain_env)
AND j.generation=sqlc.arg(generation) AND j.kind='converge' AND j.state='running' AND j.lease_owner=sqlc.arg(lease_owner) AND j.authority_principal_id=sqlc.arg(authority_principal)
AND e.outcome='success' AND e.disposition IN ('create','update') AND i.type='adapter.push_intent' AND i.outcome='intent' AND i.schema_version=1 AND i.actor_id IS NULL AND i.actor_class='system' AND i.authority_id=sqlc.arg(authority_principal) AND i.scope_class='env' AND i.origin='adapter-job' AND i.object_type='adapter-target' AND i.object_id=e.target_id AND i.correlation_id=e.job_id
AND CAST(sqlc.arg(input_revision) AS INTEGER)>0 AND CASE WHEN json_valid(i.payload) AND json_type(i.payload,'$.input_revision')='integer' THEN json_extract(i.payload,'$.input_revision') ELSE NULL END=sqlc.arg(input_revision)
ORDER BY e.surface,e.effective_name,e.disposition;

-- name: AdapterWorkerLoadExecutionEntryQuery :many
SELECT e.id,e.snapshot_id,e.key_id,e.key_name,e.classification,e.ciphertext FROM snapshot_entries e JOIN adapter_target_keys k ON k.key_id=e.key_id AND k.target_id=sqlc.arg(target_id) AND k.org_id=e.org_id AND k.project_id=e.project_id AND k.environment_id=e.environment_id WHERE e.snapshot_id=sqlc.arg(snapshot_i_d) AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(chain_env) ORDER BY e.key_name;

-- name: AdapterWorkerLoadActivationQuery :one
SELECT a.provider,COALESCE(m.pending_origin,a.origin),a.id,COALESCE(m.pending_credential_ciphertext,a.credential_ciphertext),mt.environment_id,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.destination_id,mt.repository_id,mt.visibility,mt.selected_repository_ids,mt.name_prefix,t.generation,a.spki_pin,a.ca_bundle_pem,CAST(CASE WHEN a.allow_personal_token THEN 1 ELSE 0 END AS INTEGER) AS allow_personal_token,t.destination_scope,CAST(CASE WHEN t.variable_protected THEN 1 ELSE 0 END AS INTEGER) AS variable_protected,CAST(CASE WHEN t.variable_hidden THEN 1 ELSE 0 END AS INTEGER) AS variable_hidden,CAST(CASE WHEN t.variable_expand THEN 1 ELSE 0 END AS INTEGER) AS variable_expand FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_moves m ON m.id=j.route_move_id AND m.org_id=j.org_id AND m.project_id=j.project_id AND m.adapter_id=a.id JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.target_id=t.id AND mt.org_id=t.org_id AND mt.project_id=t.project_id WHERE j.id=sqlc.arg(job_id) AND j.route_move_id=sqlc.arg(route_move_id) AND j.target_id=sqlc.arg(target_id) AND j.org_id=sqlc.arg(chain_org) AND j.project_id=sqlc.arg(chain_project) AND j.environment_id=sqlc.arg(chain_env) AND j.generation=sqlc.arg(generation) AND j.kind='activate' AND j.state='running' AND j.lease_owner=sqlc.arg(lease_owner) AND m.state='activating' AND t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL;

-- name: AdapterWorkerTryEnqueueLookup :one
SELECT generation FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(now));

-- name: AdapterWorkerTryEnqueueExistsQuery :one
SELECT COUNT(*) FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerTryEnqueueDepthQuery :one
SELECT COUNT(*) FROM adapter_outbox WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state IN ('queued','running');

-- name: AdapterWorkerTryEnqueuePreviousQuery :one
SELECT id FROM adapter_outbox WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state IN ('queued','running') ORDER BY created_at DESC LIMIT 1;

-- name: AdapterWorkerTryEnqueueSupersede :execrows
UPDATE adapter_outbox SET state='superseded',finished_at=sqlc.arg(at),lease_owner=NULL,lease_expires_at=NULL WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state IN ('queued','running');

-- name: AdapterWorkerTryEnqueueInsert :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(job_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(target_id),sqlc.arg(kind),sqlc.arg(authority_principal),sqlc.arg(generation),sqlc.arg(target_id),0,sqlc.arg(at),'queued',sqlc.arg(at));

-- name: AdapterWorkerTryEnqueueUpdate :execrows
UPDATE adapter_targets SET generation=sqlc.arg(generation),state=sqlc.arg(state),sync_status='converging',active_job_id=sqlc.arg(job_id),provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(expected_generation) AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(at));

-- hikyo:reason Closed adapter worker claim authority selects one due or expired leased job installation-wide, enforcing per-org concurrency and same-target exclusion under the PostgreSQL row lock; the claimed row supplies the downstream tenant chain.
-- hikyo:instance-scoped
-- name: AdapterWorkerClaimDueSelectQuery :one
SELECT j.id,j.org_id,j.project_id,j.environment_id,j.target_id,j.kind,COALESCE(j.route_move_id,'') AS route_move_id,j.authority_principal_id,j.generation,j.attempt_count,j.created_at
             FROM adapter_outbox j
             JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id AND (t.paused_at IS NULL OR (j.kind='scrub' AND t.state='tombstoned'))
             JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id
             WHERE ((j.state='queued' AND j.next_attempt_at<=sqlc.arg(now)) OR (j.state='running' AND j.lease_expires_at<=sqlc.arg(now)))
               AND (SELECT COUNT(*) FROM adapter_outbox x WHERE x.org_id=j.org_id AND x.state='running' AND x.lease_expires_at>sqlc.arg(now)) < 4
               AND NOT EXISTS (SELECT 1 FROM adapter_outbox x WHERE x.target_id=j.target_id AND x.id<>j.id AND x.state='running' AND x.lease_expires_at>sqlc.arg(now))
               AND ((j.kind='converge' AND t.state='active' AND a.state='active' AND t.paused_at IS NULL) OR (j.kind='activate' AND t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL) OR (j.kind='scrub' AND (t.state='tombstoned' OR (t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL AND j.route_move_id IS NOT NULL))))
             ORDER BY j.next_attempt_at,j.id LIMIT 1;

-- hikyo:reason Closed adapter worker claim transaction updates only the globally unique outbox row selected and locked immediately above, setting its incremented attempt and new owner/deadline before scoped effect settlement.
-- hikyo:instance-scoped
-- name: AdapterWorkerClaimDueUpdate :execrows
UPDATE adapter_outbox SET state='running',attempt_count=sqlc.arg(attempt),lease_owner=sqlc.arg(worker),lease_expires_at=sqlc.arg(lease_until) WHERE id=sqlc.arg(job_id);

-- name: AdapterWorkerCloseIndeterminateEffectsQuery :many
SELECT e.id,e.job_id,e.surface,e.effective_name,e.disposition,o.authority_principal_id FROM adapter_effects e JOIN adapter_outbox o ON o.id=e.job_id AND o.org_id=e.org_id AND o.project_id=e.project_id AND o.environment_id=e.environment_id WHERE e.target_id=sqlc.arg(target_id) AND e.org_id=sqlc.arg(chain_org) AND e.project_id=sqlc.arg(chain_project) AND e.environment_id=sqlc.arg(chain_env) AND e.outcome IS NULL AND NOT EXISTS (SELECT 1 FROM adapter_targets t WHERE t.id=e.target_id AND t.org_id=e.org_id AND t.project_id=e.project_id AND t.environment_id=e.environment_id AND t.provider_lease_effect_id=e.id AND t.provider_lease_expires_at>sqlc.arg(at)) ORDER BY e.created_at,e.id;

-- name: AdapterWorkerCloseIndeterminateEffectsUpdate :execrows
UPDATE adapter_effects SET outcome_audit_id=sqlc.arg(outcome_audit_id),outcome='unknown',finding='crash_window',finished_at=sqlc.arg(at) WHERE id=sqlc.arg(effect_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND outcome IS NULL;

-- name: AdapterWorkerCloseIndeterminateEffectsRelease :execrows
UPDATE adapter_targets SET provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND provider_lease_effect_id=sqlc.arg(effect_id);

-- name: AdapterWorkerGateQuery :one
SELECT COUNT(*) FROM adapter_targets t JOIN adapter_outbox j ON j.target_id=t.id AND j.org_id=t.org_id AND j.project_id=t.project_id AND j.environment_id=t.environment_id JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.generation=sqlc.arg(generation) AND j.id=sqlc.arg(job_id) AND j.state='running' AND j.lease_owner=sqlc.arg(lease_owner) AND j.lease_expires_at>sqlc.arg(now) AND ((j.kind='converge' AND t.state='active' AND a.state='active' AND t.paused_at IS NULL) OR (j.kind='activate' AND t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL) OR (j.kind='scrub' AND (t.state='tombstoned' OR (t.state='moving' AND a.state IN ('active','moving') AND t.paused_at IS NULL AND j.route_move_id IS NOT NULL))));

-- Lock target before outbox, matching teardown and provider preparation.
-- No custody mutation can revive a superseded or inadmissible lifecycle.
-- name: AdapterWorkerLockCustodyTarget :execrows
UPDATE adapter_targets SET generation=adapter_targets.generation WHERE adapter_targets.id=sqlc.arg(target_id) AND adapter_targets.org_id=sqlc.arg(chain_org) AND adapter_targets.project_id=sqlc.arg(chain_project) AND adapter_targets.environment_id=sqlc.arg(chain_env) AND adapter_targets.generation=sqlc.arg(generation)
AND EXISTS (SELECT 1 FROM adapters a WHERE a.id=adapter_targets.adapter_id AND a.org_id=adapter_targets.org_id AND a.project_id=adapter_targets.project_id AND ((CAST(sqlc.arg(job_kind) AS TEXT)='converge' AND adapter_targets.state='active' AND a.state='active' AND adapter_targets.paused_at IS NULL) OR (CAST(sqlc.arg(job_kind) AS TEXT)='scrub' AND (adapter_targets.state='tombstoned' OR (adapter_targets.state='moving' AND a.state IN ('active','moving') AND adapter_targets.paused_at IS NULL))) OR (CAST(sqlc.arg(job_kind) AS TEXT)='activate' AND adapter_targets.state='moving' AND a.state IN ('active','moving') AND adapter_targets.paused_at IS NULL)))
AND EXISTS (SELECT 1 FROM adapter_outbox j WHERE j.id=sqlc.arg(job_id) AND j.target_id=sqlc.arg(target_id) AND j.org_id=sqlc.arg(chain_org) AND j.project_id=sqlc.arg(chain_project) AND j.environment_id=sqlc.arg(chain_env) AND j.generation=sqlc.arg(generation) AND j.kind=sqlc.arg(job_kind) AND j.state='running' AND j.lease_owner=sqlc.arg(lease_owner) AND j.lease_expires_at>sqlc.arg(now));

-- name: AdapterWorkerLockCustodyJob :execrows
UPDATE adapter_outbox SET lease_owner=lease_owner WHERE id=sqlc.arg(job_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND kind=sqlc.arg(job_kind) AND state='running' AND lease_owner=sqlc.arg(lease_owner) AND lease_expires_at>sqlc.arg(now);

-- name: AdapterWorkerReservePendingQuery :one
SELECT COUNT(*) FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_move_claims c ON c.provider_origin=a.origin AND c.destination_kind=t.destination_kind AND c.destination_owner=t.destination_owner AND c.destination_name=t.destination_name AND c.destination_environment=t.destination_environment AND c.destination_scope=t.destination_scope WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND c.target_id<>t.id AND c.surface=sqlc.arg(surface) AND c.normalized_name=sqlc.arg(normalized);

-- name: AdapterWorkerReserveSelectQuery :one
SELECT state FROM adapter_ledger WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized);

-- name: AdapterWorkerReserveCurrentRoute :one
SELECT a.origin,t.destination_kind,t.destination_id,t.repository_id,t.destination_scope FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerReserveReactivate :execrows
UPDATE adapter_ledger SET state='reserved',missing=0,effective_name=sqlc.arg(effective_name),provider_origin=sqlc.arg(origin),destination_kind=sqlc.arg(destination_kind),repository_id=sqlc.arg(repository_i_d),destination_id=sqlc.arg(destination_i_d),destination_scope=sqlc.arg(destination_scope),updated_at=sqlc.arg(now) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized) AND state='released';

-- name: AdapterWorkerReserveCountQuery :one
SELECT COUNT(*) FROM adapter_ledger WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'released';

-- name: AdapterWorkerReserveLookup :one
SELECT a.origin,t.destination_kind,t.destination_id,t.repository_id,t.destination_scope FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerReserveInsert :execrows
INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_kind,repository_id,destination_id,destination_scope,surface,effective_name,normalized_name,state,updated_at) VALUES (sqlc.arg(new_id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(target_id),sqlc.arg(origin),sqlc.arg(destination_kind),sqlc.arg(repository_i_d),sqlc.arg(destination_i_d),sqlc.arg(destination_scope),sqlc.arg(surface),sqlc.arg(effective_name),sqlc.arg(normalized),sqlc.arg(reserved),sqlc.arg(now));

-- name: AdapterWorkerPrepareProviderLease :execrows
UPDATE adapter_targets SET provider_lease_job_id=sqlc.arg(job_id),provider_lease_effect_id=sqlc.arg(effect_i_d),provider_lease_expires_at=sqlc.arg(lease_until) WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND (provider_lease_job_id IS NULL OR provider_lease_expires_at<=sqlc.arg(now_stamp));

-- hikyo:reason Closed adapter effect preparation extends only the leased job identified by its globally unique id and current running owner after obtaining the scoped target provider fence in this transaction; a zero-row match rolls back preparation.
-- hikyo:instance-scoped
-- name: AdapterWorkerPrepareLease :execrows
UPDATE adapter_outbox SET lease_expires_at=sqlc.arg(lease_until) WHERE id=sqlc.arg(job_id) AND state='running' AND lease_owner=sqlc.arg(lease_owner);

-- name: AdapterWorkerPrepareUpdate :execrows
UPDATE adapter_ledger SET state='dispatched',updated_at=sqlc.arg(now) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized_name) AND state='reserved';

-- Explicit Vault recovery keeps held custody but forbids writer inference
-- after process death, including a mutation of a previously owned path.
-- name: AdapterWorkerPrepareExplicitRecovery :execrows
UPDATE adapter_ledger SET state='dispatched',updated_at=sqlc.arg(now) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized_name) AND state IN ('reserved','dispatched','owned');

-- name: AdapterWorkerPrepareInsert :execrows
INSERT INTO adapter_effects (id,org_id,project_id,environment_id,target_id,job_id,surface,effective_name,disposition,intent_audit_id,created_at) VALUES (sqlc.arg(effect_i_d),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(target_id),sqlc.arg(job_id),sqlc.arg(surface),sqlc.arg(effective_name),sqlc.arg(disposition),sqlc.arg(intent_i_d),sqlc.arg(now));

-- hikyo:reason Closed adapter journal completion settles only the globally unique effect id retained after successful Prepare; the enclosing transaction inserts its job-chain outcome and requires the matching scoped target/effect provider fence before commit.
-- hikyo:instance-scoped
-- name: AdapterWorkerFinishUpdateEffect :execrows
UPDATE adapter_effects SET outcome_audit_id=sqlc.arg(outcome_audit_id),outcome=sqlc.arg(outcome),finding=sqlc.arg(finding),finished_at=sqlc.arg(now) WHERE id=sqlc.arg(effect_i_d) AND outcome IS NULL;

-- name: AdapterWorkerFinishRemove :execrows
DELETE FROM adapter_ledger WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized_name);

-- name: AdapterWorkerFinishUpdate :execrows
UPDATE adapter_ledger SET state=CASE WHEN state='released' THEN 'released' ELSE sqlc.arg(state) END,missing=sqlc.arg(missing),updated_at=sqlc.arg(now) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized_name) AND NOT (state='released' AND sqlc.arg(missing));

-- name: AdapterWorkerFinishReleaseLease :execrows
UPDATE adapter_targets SET provider_lease_job_id=NULL,provider_lease_effect_id=NULL,provider_lease_expires_at=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND provider_lease_job_id=sqlc.arg(job_id) AND provider_lease_effect_id=sqlc.arg(effect_i_d);

-- name: AdapterWorkerRefuseRemove :execrows
DELETE FROM adapter_ledger WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND surface=sqlc.arg(surface) AND normalized_name=sqlc.arg(normalized_name) AND state='reserved';

-- name: AdapterWorkerReleaseReservationRemove :execrows
DELETE FROM adapter_ledger AS l WHERE l.org_id=sqlc.arg(chain_org) AND l.project_id=sqlc.arg(chain_project) AND l.environment_id=sqlc.arg(chain_env) AND l.target_id=sqlc.arg(target_id) AND l.surface=sqlc.arg(surface) AND l.normalized_name=sqlc.arg(normalized_name) AND l.state='reserved' AND EXISTS (SELECT 1 FROM adapter_targets t JOIN adapter_outbox o ON o.target_id=t.id AND o.org_id=t.org_id AND o.project_id=t.project_id AND o.environment_id=t.environment_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.generation=sqlc.arg(generation) AND o.id=sqlc.arg(job_id) AND o.generation=t.generation AND o.state='running' AND o.lease_owner=sqlc.arg(lease_owner) AND o.lease_expires_at>sqlc.arg(now));

-- name: AdapterWorkerInsertConflictDedup :one
SELECT COUNT(*) FROM adapter_conflicts WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_generation=sqlc.arg(generation) AND surface=sqlc.arg(surface) AND effective_name=sqlc.arg(effective_name) AND adopted_at IS NULL;

-- name: AdapterWorkerInsertConflictInsert :execrows
INSERT INTO adapter_conflicts (id,artifact_id,org_id,project_id,environment_id,target_id,job_id,destination_id,repository_id,target_generation,surface,effective_name,created_at) SELECT sqlc.arg(new_id),sqlc.arg(artifact_i_d),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(target_id),sqlc.arg(job_id),destination_id,repository_id,sqlc.arg(generation),sqlc.arg(surface),sqlc.arg(effective_name),sqlc.arg(now) FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerInsertAdapterJobAuditWithIDQuery :execrows
INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (sqlc.arg(effect_id),sqlc.arg(typ),1,sqlc.arg(at),0,sqlc.arg(at),NULL,'system',sqlc.arg(authority_principal),'env',sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),'adapter-target',sqlc.arg(target_id),sqlc.arg(outcome),sqlc.arg(job_id),'adapter-job',sqlc.arg(payload));

-- name: AdapterWorkerActivateFinish :execrows
UPDATE adapter_outbox SET state='succeeded',finished_at=sqlc.arg(at),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(job_id) AND route_move_id=sqlc.arg(route_move_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND kind='activate' AND state='running' AND lease_owner=sqlc.arg(lease_owner);

-- name: AdapterWorkerActivateLookup :one
SELECT t.adapter_id,t.environment_id,mt.environment_id,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.visibility,mt.selected_repository_ids,mt.name_prefix,m.kind,COALESCE(m.pending_origin,a.origin) FROM adapter_targets t JOIN adapters a ON a.id=t.adapter_id AND a.org_id=t.org_id AND a.project_id=t.project_id JOIN adapter_route_moves m ON m.id=sqlc.arg(route_move_id) AND m.org_id=t.org_id AND m.project_id=t.project_id AND m.adapter_id=t.adapter_id JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.target_id=t.id AND mt.org_id=t.org_id AND mt.project_id=t.project_id WHERE t.id=sqlc.arg(target_id) AND t.org_id=sqlc.arg(chain_org) AND t.project_id=sqlc.arg(chain_project) AND t.environment_id=sqlc.arg(chain_env) AND t.generation=sqlc.arg(generation) AND t.state='moving' AND m.state='activating' AND (m.kind='origin' OR (m.kind='target' AND m.target_id=t.id)) AND a.state IN ('active','moving') AND t.paused_at IS NULL;

-- name: AdapterWorkerActivateCollisionQuery :one
SELECT (SELECT COUNT(*) FROM adapter_route_move_claims c JOIN adapter_targets other ON other.org_id=c.org_id AND other.project_id=c.project_id AND other.id<>c.target_id AND other.state='active' JOIN adapters oa ON oa.id=other.adapter_id AND oa.org_id=other.org_id AND oa.project_id=other.project_id LEFT JOIN adapter_target_keys tk ON tk.target_id=other.id AND tk.org_id=other.org_id AND tk.project_id=other.project_id AND tk.environment_id=other.environment_id LEFT JOIN keys k ON k.id=tk.key_id AND k.org_id=tk.org_id AND k.project_id=tk.project_id WHERE c.move_id=sqlc.arg(route_move_id) AND c.target_id=sqlc.arg(target_id) AND oa.origin=sqlc.arg(pending_origin) AND other.destination_kind=sqlc.arg(kind) AND other.repository_id=sqlc.arg(repository_i_d) AND other.destination_id=sqlc.arg(destination_i_d) AND other.destination_scope=c.destination_scope AND (c.effective_name=other.name_prefix||sqlc.arg(sentinel_name) OR c.effective_name=other.name_prefix||k.name))+(SELECT COUNT(*) FROM adapter_route_move_claims c JOIN adapter_ledger l ON l.provider_origin=sqlc.arg(pending_origin) AND l.destination_kind=sqlc.arg(kind) AND l.repository_id=sqlc.arg(repository_i_d) AND l.destination_id=sqlc.arg(destination_i_d) AND l.destination_scope=c.destination_scope AND l.surface=c.surface AND l.normalized_name=c.normalized_name AND l.state<>'released' AND l.target_id<>c.target_id WHERE c.move_id=sqlc.arg(route_move_id) AND c.target_id=sqlc.arg(target_id));

-- name: AdapterWorkerActivateSetResolved :execrows
UPDATE adapter_route_move_targets SET destination_id=sqlc.arg(destination_i_d),repository_id=sqlc.arg(repository_i_d) WHERE move_id=sqlc.arg(route_move_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(pending_environment) AND destination_id=0;

-- name: AdapterWorkerActivateDeleteKeys :execrows
DELETE FROM adapter_target_keys WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(current_environment);

-- name: AdapterWorkerActivateInsertKeys :execrows
INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) SELECT k.org_id,k.project_id,k.environment_id,k.target_id,sqlc.arg(adapter_i_d),k.key_id FROM adapter_route_move_keys k WHERE k.move_id=sqlc.arg(route_move_id) AND k.target_id=sqlc.arg(target_id) AND k.org_id=sqlc.arg(chain_org) AND k.project_id=sqlc.arg(chain_project) AND k.environment_id=sqlc.arg(pending_environment);

-- name: AdapterWorkerActivateInsertJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(converge_i_d),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(pending_environment),sqlc.arg(target_id),'converge',sqlc.arg(route_move_id),sqlc.arg(authority_principal),sqlc.arg(generation),sqlc.arg(target_id),0,sqlc.arg(at),'queued',sqlc.arg(at));

-- name: AdapterWorkerActivateApplyTarget :execrows
UPDATE adapter_targets SET destination_kind=sqlc.arg(kind),destination_owner=sqlc.arg(owner),destination_name=sqlc.arg(name),destination_environment=sqlc.arg(destination_environment),destination_id=sqlc.arg(destination_i_d),repository_id=sqlc.arg(repository_i_d),visibility=sqlc.arg(visibility),selected_repository_ids=sqlc.arg(selected_raw),name_prefix=sqlc.arg(prefix),generation=sqlc.arg(generation),state='active',sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(converge_i_d) WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(current_environment) AND generation=sqlc.arg(expected_generation) AND state='moving' AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerActivateUpdateExpiry :execrows
UPDATE adapters SET credential_expires_at=sqlc.arg(credential_expires_at) WHERE id=sqlc.arg(adapter_i_d) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: AdapterWorkerActivateDeleteClaims :execrows
DELETE FROM adapter_route_move_claims WHERE move_id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterWorkerActivateCompleteMove :execrows
UPDATE adapter_route_moves SET state='completed' WHERE id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND target_id=sqlc.arg(target_id) AND state='activating';

-- name: AdapterWorkerActivateOriginRouteMoveMarkProbe :execrows
UPDATE adapter_targets SET active_job_id=NULL WHERE id=sqlc.arg(target_id) AND adapter_id=sqlc.arg(adapter_i_d) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND state='moving' AND active_job_id=sqlc.arg(job_id) AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerActivateOriginRouteMoveCountUnresolved :one
SELECT COUNT(*) FROM adapter_route_move_targets WHERE move_id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND destination_id=0;

-- name: AdapterWorkerActivateOriginRouteMoveLoadPending :one
SELECT pending_origin,pending_credential_ciphertext FROM adapter_route_moves WHERE id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND adapter_id=sqlc.arg(adapter_i_d) AND kind='origin' AND target_id IS NULL AND state='activating';

-- name: AdapterWorkerActivateOriginRouteMoveTargetQuery :many
SELECT t.id,t.environment_id,t.generation,mt.destination_kind,mt.destination_owner,mt.destination_name,mt.destination_environment,mt.destination_id,mt.repository_id,mt.visibility,mt.selected_repository_ids,mt.name_prefix FROM adapter_route_move_targets mt JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id WHERE mt.move_id=sqlc.arg(route_move_id) AND mt.org_id=sqlc.arg(chain_org) AND mt.project_id=sqlc.arg(chain_project) AND t.adapter_id=sqlc.arg(adapter_i_d) AND t.state='moving' AND t.active_job_id IS NULL AND t.provider_lease_job_id IS NULL ORDER BY t.id;

-- name: AdapterWorkerActivateOriginRouteMoveDeleteKeys :execrows
DELETE FROM adapter_target_keys WHERE target_id=sqlc.arg(effect_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment);

-- name: AdapterWorkerActivateOriginRouteMoveInsertKeys :execrows
INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) SELECT k.org_id,k.project_id,k.environment_id,k.target_id,sqlc.arg(adapter_i_d),k.key_id FROM adapter_route_move_keys k WHERE k.move_id=sqlc.arg(route_move_id) AND k.target_id=sqlc.arg(effect_id) AND k.org_id=sqlc.arg(chain_org) AND k.project_id=sqlc.arg(chain_project) AND k.environment_id=sqlc.arg(environment);

-- name: AdapterWorkerActivateOriginRouteMoveInsertJob :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(converge_i_d),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment),sqlc.arg(effect_id),'converge',sqlc.arg(route_move_id),sqlc.arg(authority_principal),sqlc.arg(generation),sqlc.arg(effect_id),0,sqlc.arg(at),'queued',sqlc.arg(at));

-- name: AdapterWorkerActivateOriginRouteMoveApplyTarget :execrows
UPDATE adapter_targets SET destination_kind=sqlc.arg(kind),destination_owner=sqlc.arg(owner),destination_name=sqlc.arg(name),destination_environment=sqlc.arg(destination_environment),destination_id=sqlc.arg(destination_i_d),repository_id=sqlc.arg(repository_i_d),visibility=sqlc.arg(visibility),selected_repository_ids=sqlc.arg(selected_raw),name_prefix=sqlc.arg(prefix),generation=sqlc.arg(generation),state='active',sync_status='converging',failure_names='[]',active_job_id=sqlc.arg(converge_i_d) WHERE id=sqlc.arg(effect_id) AND adapter_id=sqlc.arg(adapter_i_d) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment) AND generation=sqlc.arg(expected_generation) AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerActivateOriginRouteMoveActivateAdapter :execrows
UPDATE adapters SET origin=sqlc.arg(pending_origin),credential_ciphertext=sqlc.arg(pending_credential),credential_set_at=sqlc.arg(at),credential_expires_at=sqlc.arg(expires),state='active' WHERE id=sqlc.arg(adapter_i_d) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='moving';

-- name: AdapterWorkerActivateOriginRouteMoveDeleteClaims :execrows
DELETE FROM adapter_route_move_claims WHERE move_id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project);

-- name: AdapterWorkerActivateOriginRouteMoveCompleteMove :execrows
UPDATE adapter_route_moves SET state='completed',pending_origin=NULL,pending_credential_ciphertext=NULL WHERE id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND adapter_id=sqlc.arg(adapter_i_d) AND kind='origin' AND state='activating';

-- hikyo:reason Closed adapter worker retry matches the claimed outbox row id, lease owner, stored org/project/environment/target/generation/authority/kind/route identity before any settlement or audit; ordinary retry continuation also retains scoped current-target generation/provider fences.
-- hikyo:instance-scoped
-- name: AdapterWorkerFinishJobQuery :execrows
UPDATE adapter_outbox SET state='queued',next_attempt_at=sqlc.arg(due),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(job_id) AND lease_owner=sqlc.arg(lease_owner) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND generation=sqlc.arg(generation) AND authority_principal_id=sqlc.arg(authority_principal) AND kind=sqlc.arg(kind) AND COALESCE(route_move_id,'')=CAST(sqlc.arg(route_move_id) AS TEXT);

-- hikyo:reason Closed adapter worker terminal settlement matches the claimed outbox row id, lease owner, stored org/project/environment/target/generation/authority/kind/route identity before any audit. Ordinary outcomes retain scoped current-target checks; intentional stale-generation abort settles and audits only that stored claimed identity without requiring the superseded current-target fence, in the same transaction.
-- hikyo:instance-scoped
-- name: AdapterWorkerCompleteJob :execrows
UPDATE adapter_outbox SET state=sqlc.arg(state),finished_at=sqlc.arg(at),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(job_id) AND lease_owner=sqlc.arg(lease_owner) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND target_id=sqlc.arg(target_id) AND generation=sqlc.arg(generation) AND authority_principal_id=sqlc.arg(authority_principal) AND kind=sqlc.arg(kind) AND COALESCE(route_move_id,'')=CAST(sqlc.arg(route_move_id) AS TEXT);

-- name: AdapterWorkerFinishJobLookup :one
SELECT adapter_id FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- Only the just-settled old job's pointer is detached; replacement pointers
-- and all provider effect fences survive a superseded-generation abort.
-- name: AdapterWorkerDetachSupersededJob :execrows
UPDATE adapter_targets SET active_job_id=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND active_job_id=sqlc.arg(job_id);

-- name: AdapterWorkerFinishJobMarkTarget :execrows
UPDATE adapter_targets SET state='tombstoned',sync_status='converged',active_job_id=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerFinishJobErase :execrows
UPDATE adapters SET credential_ciphertext=NULL,credential_set_at=NULL,credential_expires_at=NULL WHERE adapters.id=sqlc.arg(adapter_i_d) AND adapters.org_id=sqlc.arg(chain_org) AND adapters.project_id=sqlc.arg(chain_project) AND NOT EXISTS (SELECT 1 FROM adapter_targets retained WHERE retained.adapter_id=adapters.id AND retained.org_id=adapters.org_id AND retained.project_id=adapters.project_id AND retained.state<>'tombstoned') AND NOT EXISTS (SELECT 1 FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id WHERE t.adapter_id=adapters.id AND t.org_id=adapters.org_id AND t.project_id=adapters.project_id AND j.kind='scrub' AND j.state IN ('queued','running'));

-- name: AdapterWorkerFinishJobAttention :execrows
UPDATE adapter_route_moves SET state='attention_required' WHERE id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='activating';

-- name: AdapterWorkerFinishJobSupersede :execrows
UPDATE adapter_outbox SET state='superseded',finished_at=sqlc.arg(at),lease_owner=NULL,lease_expires_at=NULL WHERE route_move_id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id<>sqlc.arg(job_id) AND kind='activate' AND state IN ('queued','running');

-- name: AdapterWorkerFinishJobMarkTargets :execrows
UPDATE adapter_targets SET sync_status='failed',failure_names='["route"]',active_job_id=NULL WHERE adapter_targets.org_id=sqlc.arg(chain_org) AND adapter_targets.project_id=sqlc.arg(chain_project) AND adapter_targets.id IN (SELECT mt.target_id FROM adapter_route_move_targets mt WHERE mt.move_id=sqlc.arg(route_move_id) AND mt.org_id=sqlc.arg(chain_org) AND mt.project_id=sqlc.arg(chain_project)) AND adapter_targets.state='moving';

-- name: AdapterWorkerRecordJobOutcome :execrows
UPDATE adapter_targets SET sync_status=sqlc.arg(target_status),converged_revision=CASE WHEN CAST(sqlc.arg(converged_revision) AS BIGINT)>0 THEN sqlc.arg(converged_rev) ELSE converged_revision END,failure_names=sqlc.arg(failure_j_s_o_n),warnings=sqlc.arg(warning_j_s_o_n),last_attempted_revision=CASE WHEN CAST(sqlc.arg(revision) AS BIGINT)>0 THEN sqlc.arg(rev) ELSE last_attempted_revision END,last_attempted_at=sqlc.arg(attempted_at),last_error_class=sqlc.arg(error_class),drift_attention=CASE WHEN CAST(sqlc.arg(attention_mode) AS INTEGER)=0 THEN 0 WHEN CAST(sqlc.arg(attention_mode) AS INTEGER)=1 THEN 1 ELSE drift_attention END,active_job_id=CASE WHEN CAST(sqlc.arg(retain_active_job) AS INTEGER)=1 THEN active_job_id ELSE NULL END WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerRaiseDriftAttentionQuery :execrows
UPDATE adapter_targets SET drift_attention=1 WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(org) AND project_id=sqlc.arg(project) AND environment_id=sqlc.arg(environment_i_d);

-- name: AdapterWorkerFinishRouteMoveScrubLookupMove :one
SELECT m.kind FROM adapter_route_moves m JOIN adapter_route_move_targets mt ON mt.move_id=m.id AND mt.org_id=m.org_id AND mt.project_id=m.project_id WHERE m.id=sqlc.arg(route_move_id) AND m.org_id=sqlc.arg(chain_org) AND m.project_id=sqlc.arg(chain_project) AND m.state='scrubbing' AND mt.target_id=sqlc.arg(target_id) AND mt.environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerFinishRouteMoveScrubActiveLedgerQuery :one
SELECT COUNT(*) FROM adapter_ledger WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'released';

-- name: AdapterWorkerFinishRouteMoveScrubPersistOrphans :execrows
UPDATE adapter_route_move_targets SET orphaned_names=sqlc.arg(orphan_j_s_o_n) WHERE move_id=sqlc.arg(route_move_id) AND target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerFinishRouteMoveScrubMove :execrows
UPDATE adapter_route_moves SET state='activating' WHERE id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND target_id=sqlc.arg(target_id) AND state='scrubbing';

-- name: AdapterWorkerFinishRouteMoveScrubInsert :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(activate_i_d),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(target_id),'activate',sqlc.arg(route_move_id),sqlc.arg(authority_principal),sqlc.arg(generation),sqlc.arg(target_id),0,sqlc.arg(at),'queued',sqlc.arg(at));

-- name: AdapterWorkerFinishRouteMoveScrubMark :execrows
UPDATE adapter_targets SET state='moving',sync_status='converging',failure_names=sqlc.arg(failure_j_s_o_n),active_job_id=sqlc.arg(activate_i_d) WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerFinishOriginRouteMoveScrubMarkDone :execrows
UPDATE adapter_targets SET state='moving',sync_status='converging',failure_names=sqlc.arg(failure_j_s_o_n),active_job_id=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND state='moving' AND active_job_id=sqlc.arg(job_id) AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerFinishOriginRouteMoveScrubCountPending :one
SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND kind='scrub' AND state IN ('queued','running');

-- name: AdapterWorkerFinishOriginRouteMoveScrubActivateMove :execrows
UPDATE adapter_route_moves SET state='activating' WHERE id=sqlc.arg(route_move_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND kind='origin' AND target_id IS NULL AND state='scrubbing';

-- name: AdapterWorkerFinishOriginRouteMoveScrubTargetQuery :many
SELECT t.id,t.environment_id,t.generation,m.authority_principal_id FROM adapter_route_move_targets mt JOIN adapter_route_moves m ON m.id=mt.move_id AND m.org_id=mt.org_id AND m.project_id=mt.project_id JOIN adapter_targets t ON t.id=mt.target_id AND t.org_id=mt.org_id AND t.project_id=mt.project_id AND t.environment_id=mt.environment_id WHERE mt.move_id=sqlc.arg(route_move_id) AND mt.org_id=sqlc.arg(chain_org) AND mt.project_id=sqlc.arg(chain_project) AND t.state='moving' AND t.active_job_id IS NULL ORDER BY t.id;

-- name: AdapterWorkerFinishOriginRouteMoveScrubInsert :execrows
INSERT INTO adapter_outbox (id,org_id,project_id,environment_id,target_id,kind,route_move_id,authority_principal_id,generation,dedup_key,attempt_count,next_attempt_at,state,created_at) VALUES (sqlc.arg(activate_i_d),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(environment),sqlc.arg(effect_id),'activate',sqlc.arg(route_move_id),sqlc.arg(authority),sqlc.arg(generation),sqlc.arg(effect_id),0,sqlc.arg(at),'queued',sqlc.arg(at));

-- name: AdapterWorkerFinishOriginRouteMoveScrubMark :execrows
UPDATE adapter_targets SET active_job_id=sqlc.arg(activate_i_d) WHERE id=sqlc.arg(effect_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(environment) AND generation=sqlc.arg(generation) AND state='moving' AND active_job_id IS NULL AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerFinishDeadCredentialScrubLookup :one
SELECT adapter_id FROM adapter_targets WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: AdapterWorkerFinishDeadCredentialScrubReleaseLedger :execrows
UPDATE adapter_ledger SET state='released',missing=0,updated_at=sqlc.arg(at) WHERE target_id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state<>'released';

-- name: AdapterWorkerFinishDeadCredentialScrubMarkTarget :execrows
UPDATE adapter_targets SET state='tombstoned',sync_status='failed',failure_names=sqlc.arg(failure_j_s_o_n),active_job_id=NULL WHERE id=sqlc.arg(target_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND generation=sqlc.arg(generation) AND provider_lease_job_id IS NULL;

-- name: AdapterWorkerFinishDeadCredentialScrubErase :execrows
UPDATE adapters SET credential_ciphertext=NULL,credential_set_at=NULL,credential_expires_at=NULL WHERE adapters.id=sqlc.arg(adapter_i_d) AND adapters.org_id=sqlc.arg(chain_org) AND adapters.project_id=sqlc.arg(chain_project) AND NOT EXISTS (SELECT 1 FROM adapter_targets retained WHERE retained.adapter_id=adapters.id AND retained.org_id=adapters.org_id AND retained.project_id=adapters.project_id AND retained.state<>'tombstoned') AND NOT EXISTS (SELECT 1 FROM adapter_outbox j JOIN adapter_targets t ON t.id=j.target_id AND t.org_id=j.org_id AND t.project_id=j.project_id AND t.environment_id=j.environment_id WHERE t.adapter_id=adapters.id AND t.org_id=adapters.org_id AND t.project_id=adapters.project_id AND j.kind='scrub' AND j.state IN ('queued','running'));

-- hikyo:reason Closed host-only provider-switch authority rejects network operation contexts and returns only whether any retained adapter configuration or configuration fence exists, to refuse an unsafe provider change.
-- hikyo:instance-scoped
-- name: AdapterWorkerCheckProviderSwitchQuery :one
SELECT CAST(CASE WHEN EXISTS (SELECT 1 FROM adapters) OR EXISTS (SELECT 1 FROM adapter_configure_fences) THEN 1 ELSE 0 END AS INTEGER) AS retained_configuration;
