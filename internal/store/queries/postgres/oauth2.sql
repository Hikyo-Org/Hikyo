-- OAuth2 profile administration and single-use callbacks (#609).
-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: CreateOAuth2Provider :exec
INSERT INTO oauth2_providers (id, slug, display_name, kind, profile, issuer, client_id, client_secret, redirect_uri, enabled, dek_version, row_version, created_at, updated_at)
VALUES ($1, $2, $3, 'oauth2', $4, $5, $6, $7, $8, $9, $10, 1, $11, $12);

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetOAuth2ProviderBySlug :one
SELECT * FROM oauth2_providers WHERE slug = $1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: ListOAuth2Providers :many
SELECT * FROM oauth2_providers ORDER BY slug;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetOAuth2ProviderForCallback :one
SELECT * FROM oauth2_providers WHERE id = $1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: UpdateOAuth2ProviderCAS :execrows
UPDATE oauth2_providers SET display_name = $1, client_id = $2, client_secret = $3, redirect_uri = $4, enabled = $5, dek_version = $6, row_version = row_version + 1, updated_at = $7 WHERE id = $8 AND row_version = $9;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: LockOAuth2ProviderForDelete :one
SELECT id FROM oauth2_providers WHERE id = $1 FOR UPDATE;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: DeleteOAuth2Provider :exec
DELETE FROM oauth2_providers WHERE id = $1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GuardOAuth2ProviderForMint :execrows
UPDATE oauth2_providers SET row_version = row_version WHERE id = $1 AND row_version = $2 AND issuer = $3 AND enabled = 1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: InsertOAuth2Transaction :exec
INSERT INTO oauth2_transactions (id, state_verifier, pkce_verifier, provider_id, issuer, redirect_uri, purpose, intent, signup_scope_org_id, binding_kind, initiating_session_id, browser_binding_verifier, account_id, authority_id, ceremony_id, browser, credential_epoch, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19);

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetOAuth2TransactionByState :one
SELECT * FROM oauth2_transactions WHERE state_verifier = $1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: ConsumeOAuth2Transaction :execrows
UPDATE oauth2_transactions SET consumed_at = $1 WHERE id = $2 AND consumed_at IS NULL;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: BindSessionToOAuth2Provider :execrows
UPDATE sessions SET oauth2_provider_id = $1 WHERE id = $2 AND provider_id IS NULL AND saml_provider_id IS NULL AND oauth2_provider_id IS NULL;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: DeleteSessionsForOAuth2Provider :execrows
DELETE FROM sessions WHERE oauth2_provider_id = $1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetCredentialAuthorityByID :one
SELECT * FROM credential_authorities WHERE id = $1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: ClaimOAuth2Authority :execrows
UPDATE credential_authorities SET consumed_at = $1, established_credential_kind = 'oauth2'
WHERE id = $2 AND consumed_at IS NULL AND issued_by <> 'recovery';

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: StampCredentialEstablish :exec
INSERT INTO credential_establish_evidence (session_id, identity_id, purpose, expires_at)
VALUES ($1, $2, 'account-security', $3)
ON CONFLICT (session_id) DO UPDATE SET identity_id = excluded.identity_id, expires_at = excluded.expires_at;
