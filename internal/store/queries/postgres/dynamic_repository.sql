-- name: DynamicCreateProvider :execrows
INSERT INTO dynamic_providers (id,org_id,project_id,kind,origin,tls_mode,grant_role,admin_credential_ciphertext,credential_set_at,authority_principal_id,state,created_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(kind),sqlc.arg(origin),sqlc.arg(tls_mode),sqlc.arg(grant_role),sqlc.arg(credential),sqlc.arg(at),sqlc.arg(authority_principal_id), 'active', sqlc.arg(at));

-- name: DynamicGetProvider :one
SELECT id,kind,origin,tls_mode,grant_role,CAST(CASE WHEN admin_credential_ciphertext IS NULL THEN 0 ELSE 1 END AS BIGINT) AS credential_present,credential_set_at,authority_principal_id,state,created_at FROM dynamic_providers WHERE id=sqlc.arg(provider_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: DynamicProviderCredentialCiphertext :one
SELECT admin_credential_ciphertext FROM dynamic_providers WHERE id=sqlc.arg(provider_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: DynamicListProviders :many
SELECT id,kind,origin,tls_mode,grant_role,CAST(CASE WHEN admin_credential_ciphertext IS NULL THEN 0 ELSE 1 END AS BIGINT) AS credential_present,credential_set_at,authority_principal_id,state,created_at FROM dynamic_providers WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active' ORDER BY id;

-- name: DynamicReplaceProviderCredential :execrows
UPDATE dynamic_providers SET admin_credential_ciphertext=sqlc.arg(credential),credential_set_at=sqlc.arg(at) WHERE id=sqlc.arg(provider_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: DynamicRevokeProviderCredential :execrows
UPDATE dynamic_providers SET admin_credential_ciphertext=NULL,credential_set_at=NULL WHERE id=sqlc.arg(provider_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: DynamicDeleteProvider :execrows
UPDATE dynamic_providers SET state='tombstoned' WHERE id=sqlc.arg(provider_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state='active';

-- name: DynamicCreateLease :execrows
INSERT INTO dynamic_leases (id,org_id,project_id,environment_id,provider_id,principal_id,principal_class,provider_handle,state,max_ttl_seconds,last_transition_at,attempt_count,next_attempt_at,created_at) VALUES (sqlc.arg(id),sqlc.arg(chain_org),sqlc.arg(chain_project),sqlc.arg(chain_env),sqlc.arg(provider_id),sqlc.arg(principal_id),sqlc.arg(principal_class),sqlc.arg(provider_handle), 'minting', sqlc.arg(max_ttl_seconds), sqlc.arg(at), 0, sqlc.arg(grace), sqlc.arg(at));

-- name: DynamicGetLease :one
SELECT id,provider_id,environment_id,principal_id,principal_class,provider_handle,state,issued_at,expires_at,max_ttl_seconds,last_transition_at,created_at FROM dynamic_leases WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env));

-- name: DynamicListLeasesForEnvironment :many
SELECT id,provider_id,environment_id,principal_id,principal_class,provider_handle,state,issued_at,expires_at,max_ttl_seconds,last_transition_at,created_at FROM dynamic_leases WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) ORDER BY id;

-- name: DynamicActiveLeaseIDsForProvider :many
SELECT id,provider_id,environment_id,principal_id,principal_class,provider_handle,state,issued_at,expires_at,max_ttl_seconds,last_transition_at,created_at FROM dynamic_leases WHERE provider_id=sqlc.arg(provider_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND state NOT IN ('revoked','expired','failed') ORDER BY id;

-- name: DynamicFinishMint :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),issued_at=sqlc.narg(issued_at),expires_at=sqlc.narg(expires_at),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND environment_id=sqlc.arg(chain_env) AND state='minting' AND lease_owner IS NULL;

-- name: DynamicReencryptProvider :execrows
UPDATE dynamic_providers SET admin_credential_ciphertext=sqlc.arg(new_ciphertext) WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=sqlc.arg(id) AND admin_credential_ciphertext=sqlc.arg(old_ciphertext);

-- name: DynamicListProvidersForReencrypt :many
SELECT id, admin_credential_ciphertext FROM dynamic_providers WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id>sqlc.arg(cursor) AND admin_credential_ciphertext IS NOT NULL ORDER BY id LIMIT CAST(sqlc.arg(page_limit) AS BIGINT);

-- name: DynamicEnqueueRevoking :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env)) AND state NOT IN ('revoked','expired','failed','revoking');

-- name: DynamicEnqueueRevokingWithTTL :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),max_ttl_seconds=sqlc.arg(max_ttl_seconds),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env)) AND state NOT IN ('revoked','expired','failed','revoking');

-- name: DynamicEnqueueRenewing :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env)) AND state='active';

-- name: DynamicEnqueueRenewingWithTTL :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),max_ttl_seconds=sqlc.arg(max_ttl_seconds),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env)) AND state='active';

-- name: DynamicEnqueueUnknown :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env)) AND state='unknown';

-- name: DynamicEnqueueUnknownWithTTL :execrows
UPDATE dynamic_leases SET state=sqlc.arg(state),max_ttl_seconds=sqlc.arg(max_ttl_seconds),last_transition_at=sqlc.arg(at),next_attempt_at=sqlc.arg(next_attempt_at),attempt_count=0,lease_owner=NULL,lease_expires_at=NULL WHERE id=sqlc.arg(lease_id) AND org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND (CAST(sqlc.arg(chain_env) AS TEXT)='' OR environment_id=sqlc.arg(chain_env)) AND state='unknown';
