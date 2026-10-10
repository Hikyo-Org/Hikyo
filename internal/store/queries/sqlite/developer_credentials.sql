-- hikyo:reason Mint authenticated human delegation only after exact scope-bound reauthentication and tenant authorization in this transaction.
-- hikyo:authn-resolution
-- name: InsertDeveloperCredential :exec
INSERT INTO developer_credentials (id,principal_id,authority_principal_id,org_id,project_id,env_id,verifier,prefix_hint,parent_session_id,provider_id,oauth2_provider_id,saml_provider_id,auth_method,authority_generation,credential_epoch,created_at,expires_at,revoked_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?);

-- hikyo:reason Authentication resolves the presented verifier before any authorization proof can exist.
-- hikyo:authn-resolution
-- name: DeveloperCredentialByVerifier :one
SELECT id,principal_id,authority_principal_id,org_id,project_id,env_id,verifier,prefix_hint,parent_session_id,provider_id,oauth2_provider_id,saml_provider_id,auth_method,authority_generation,credential_epoch,created_at,expires_at,revoked_at FROM developer_credentials WHERE verifier = ?;

-- hikyo:reason Transaction-local lifecycle administration filters this metadata after self or project authorization; no bearer can be recovered.
-- hikyo:authn-resolution
-- name: ListDeveloperCredentials :many
SELECT id,principal_id,authority_principal_id,org_id,project_id,env_id,verifier,prefix_hint,parent_session_id,provider_id,oauth2_provider_id,saml_provider_id,auth_method,authority_generation,credential_epoch,created_at,expires_at,revoked_at FROM developer_credentials ORDER BY created_at,id;

-- hikyo:reason Terminal security invalidation writes authentication state atomically with its authorizing mutation.
-- hikyo:authn-resolution
-- name: RevokeDeveloperCredential :exec
UPDATE developer_credentials SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL;

-- hikyo:reason Environment read authorization precedes the non-locking lifetime ceiling affordance.
-- hikyo:authn-resolution
-- name: PeekDeveloperCredentialPolicy :one
SELECT max_lifetime_seconds FROM developer_credential_policy WHERE id=1;

-- hikyo:reason Mint and policy tightening serialize on this shared instance policy before changing delegation authority.
-- hikyo:authn-resolution
-- name: GetDeveloperCredentialPolicy :one
SELECT max_lifetime_seconds FROM developer_credential_policy WHERE id=1;

-- hikyo:reason Instance-config authorization precedes the shared lifetime policy mutation in the same transaction.
-- hikyo:authn-resolution
-- name: SetDeveloperCredentialPolicy :exec
UPDATE developer_credential_policy SET max_lifetime_seconds = ? WHERE id=1;

-- hikyo:reason Authorized instance policy lowering persists monotonic expiry reductions before commit.
-- hikyo:authn-resolution
-- name: ClampDeveloperCredentialExpiry :exec
UPDATE developer_credentials SET expires_at = sqlc.arg(ceiling) WHERE id = sqlc.arg(id) AND expires_at > sqlc.arg(ceiling);

-- Only untouched migration policy defaults may be replaced by a restore.
-- hikyo:reason restore target admission checks developer policy without exposing authentication verifiers
-- hikyo:authn-resolution
-- name: RestoreDeveloperPolicySeedOccupied :one
SELECT EXISTS (SELECT 1 FROM developer_credential_policy WHERE id <> 1 OR max_lifetime_seconds <> 28800);
