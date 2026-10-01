-- hikyo:reason DynamicRuntime closed scheduler claims one due lease across tenants under the per-org concurrency cap; writes bind the selected immutable chain and crash-fence token.
-- hikyo:instance-scoped
-- name: RuntimeDynamicClaimDueLease :one
SELECT l.id,l.org_id,l.project_id,l.environment_id,l.provider_id,l.provider_handle,l.principal_id,l.principal_class,l.state,l.max_ttl_seconds,l.issued_at,l.expires_at,l.attempt_count,l.lease_claim_token
             FROM dynamic_leases l
             WHERE ((l.state IN ('minting','renewing','revoking','unknown') AND l.next_attempt_at<=sqlc.arg(now)) OR (l.state='active' AND l.expires_at IS NOT NULL AND l.expires_at<=sqlc.arg(now) AND l.next_attempt_at<=sqlc.arg(now)))
               AND (l.lease_owner IS NULL OR l.lease_expires_at IS NULL OR l.lease_expires_at<=sqlc.arg(now))
               AND (SELECT COUNT(*) FROM dynamic_leases x WHERE x.org_id=l.org_id AND x.lease_owner IS NOT NULL AND x.lease_expires_at>sqlc.arg(now)) < 4
             ORDER BY l.next_attempt_at,l.id FOR UPDATE SKIP LOCKED LIMIT 1;

-- name: RuntimeDynamicClaimLease :exec
UPDATE dynamic_leases SET attempt_count=sqlc.arg(attempt),lease_owner=sqlc.arg(worker),lease_expires_at=sqlc.arg(until),lease_claim_token=sqlc.arg(claim_token) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RuntimeDynamicLoadProviderMaterial :one
SELECT p.kind,p.origin,p.tls_mode,p.grant_role,p.id,p.admin_credential_ciphertext FROM dynamic_providers p JOIN dynamic_leases l ON l.provider_id=p.id AND l.org_id=p.org_id AND l.project_id=p.project_id WHERE l.id=sqlc.arg(lease_id) AND l.org_id=sqlc.arg(chain_org) AND l.project_id=sqlc.arg(chain_project) AND l.lease_owner=sqlc.arg(worker) AND l.lease_expires_at>sqlc.arg(now) AND l.lease_claim_token=sqlc.arg(claim_token) AND l.environment_id=sqlc.arg(chain_env);

-- name: RuntimeDynamicLatestEffectKind :one
SELECT kind FROM dynamic_effects WHERE lease_id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY created_at DESC,id DESC LIMIT 1;

-- name: RuntimeDynamicInsertEffect :exec
INSERT INTO dynamic_effects (id,org_id,project_id,environment_id,lease_id,kind,intent_audit_id,created_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(lease_id),sqlc.arg(kind),sqlc.arg(intent_audit_id),sqlc.arg(at));

-- name: RuntimeDynamicCloseEffect :exec
UPDATE dynamic_effects SET outcome=sqlc.arg(outcome),outcome_audit_id=sqlc.arg(outcome_audit_id),finished_at=sqlc.arg(at) WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RuntimeDynamicSettleLease :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),issued_at=COALESCE(sqlc.narg(issued_at),issued_at),expires_at=COALESCE(sqlc.narg(expires_at),expires_at),last_transition_at=sqlc.arg(now),next_attempt_at=sqlc.arg(next_attempt),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND lease_owner=sqlc.arg(worker) AND lease_expires_at>sqlc.arg(now) AND lease_claim_token=sqlc.arg(claim_token) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RuntimeDynamicRetryLease :execrows
UPDATE dynamic_leases SET next_attempt_at=sqlc.arg(next_attempt),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND lease_owner=sqlc.arg(worker) AND lease_expires_at>sqlc.arg(now) AND lease_claim_token=sqlc.arg(claim_token) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RuntimeDynamicAssertLease :one
SELECT COUNT(*) FROM dynamic_leases WHERE id=sqlc.arg(id) AND org_id=sqlc.arg(chain_org) AND lease_owner=sqlc.arg(worker) AND lease_expires_at>sqlc.arg(now) AND lease_claim_token=sqlc.arg(claim_token) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env);

-- name: RuntimeDynamicInsertLeaseAudit :exec
INSERT INTO audit_tenant_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,authority_id,scope_class,org_id,project_id,env_id,object_type,object_id,outcome,correlation_id,origin,payload) VALUES (sqlc.arg(id),sqlc.arg(type),1,sqlc.arg(at),false,sqlc.arg(at),NULL,'system',sqlc.arg(authority_id),'env',sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),'dynamic-lease',sqlc.arg(lease_id),sqlc.arg(outcome),sqlc.arg(lease_id),'system',sqlc.arg(payload));

-- hikyo:reason DynamicRuntime metrics scrape returns only the instance-wide count of active leases through the closed runtime boundary.
-- hikyo:instance-scoped
-- name: RuntimeDynamicCountActiveLeases :one
SELECT COUNT(*) FROM dynamic_leases WHERE state='active';

-- hikyo:reason DynamicRuntime metrics scrape returns only the instance-wide count of unknown effects through the closed runtime boundary.
-- hikyo:instance-scoped
-- name: RuntimeDynamicCountUnknownEffects :one
SELECT COUNT(*) FROM dynamic_effects WHERE outcome='unknown';
