-- OAuth2 profile administration and single-use callbacks (#609).
-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: CreateOAuth2Provider :exec
INSERT INTO oauth2_providers (id, slug, display_name, kind, profile, issuer, client_id, client_secret, redirect_uri, enabled, dek_version, row_version, created_at, updated_at)
VALUES (?, ?, ?, 'oauth2', ?, ?, ?, ?, ?, ?, ?, 1, ?, ?);

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetOAuth2ProviderBySlug :one
SELECT * FROM oauth2_providers WHERE slug = ?;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: ListOAuth2Providers :many
SELECT * FROM oauth2_providers ORDER BY slug;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetOAuth2ProviderForCallback :one
SELECT * FROM oauth2_providers WHERE id = ?;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: UpdateOAuth2ProviderCAS :execrows
UPDATE oauth2_providers SET display_name = ?, client_id = ?, client_secret = ?, redirect_uri = ?, enabled = ?, dek_version = ?, row_version = row_version + 1, updated_at = ? WHERE id = ? AND row_version = ?;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: LockOAuth2ProviderForDelete :one
SELECT id FROM oauth2_providers WHERE id = ?;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: DeleteOAuth2Provider :exec
DELETE FROM oauth2_providers WHERE id = ?;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GuardOAuth2ProviderForMint :execrows
UPDATE oauth2_providers SET row_version = row_version WHERE id = ? AND row_version = ? AND issuer = ? AND enabled = 1;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: InsertOAuth2Transaction :exec
INSERT INTO oauth2_transactions (id, state_verifier, pkce_verifier, provider_id, issuer, redirect_uri, purpose, intent, signup_scope_org_id, binding_kind, initiating_session_id, browser_binding_verifier, account_id, authority_id, ceremony_id, browser, credential_epoch, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetOAuth2TransactionByState :one
SELECT * FROM oauth2_transactions WHERE state_verifier = ?;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: ConsumeOAuth2Transaction :execrows
UPDATE oauth2_transactions SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: BindSessionToOAuth2Provider :execrows
UPDATE sessions SET oauth2_provider_id = ? WHERE id = ? AND provider_id IS NULL AND saml_provider_id IS NULL AND oauth2_provider_id IS NULL;

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: DeleteSessionsForOAuth2Provider :execrows
DELETE FROM sessions WHERE sessions.oauth2_provider_id = sqlc.narg(oauth2_provider_id)
OR sessions.id IN (SELECT e.session_id FROM credential_establish_evidence e
 JOIN external_identities i ON i.id = e.identity_id
 WHERE i.kind = 'oauth2' AND i.provider_id = sqlc.narg(oauth2_provider_id));

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: GetCredentialAuthorityByID :one
SELECT * FROM credential_authorities WHERE id = ?;

-- hikyo:reason A federated claim (#610) spends an invitation or reset authority at an OIDC or OAuth2 callback, before any principal exists in the request; the service binds the authority to its transaction, account, epoch and kind before mutation.
-- hikyo:authn-resolution
-- name: ClaimFederatedAuthority :execrows
UPDATE credential_authorities SET consumed_at = ?, established_credential_kind = ?
WHERE id = ? AND consumed_at IS NULL AND issued_by <> 'recovery';

-- hikyo:reason OAuth2 identity configuration and callback artifacts are instance-wide authentication resolution; service guards bind their provider, purpose, epoch and initiator before mutation.
-- hikyo:authn-resolution
-- name: StampCredentialEstablish :exec
INSERT INTO credential_establish_evidence (session_id, identity_id, purpose, expires_at)
VALUES (?, ?, 'account-security', ?)
ON CONFLICT (session_id) DO UPDATE SET identity_id = excluded.identity_id, expires_at = excluded.expires_at;

-- hikyo:reason Account-security credential creation reads only the acting session's purpose-bound establish stamp; service rechecks its principal, expiry and live no-local-proof precondition inside the write transaction.
-- hikyo:authn-resolution
-- name: GetCredentialEstablish :one
SELECT e.identity_id, e.expires_at FROM credential_establish_evidence e
JOIN sessions s ON s.id = e.session_id
JOIN external_identities i ON i.id = e.identity_id
JOIN accounts a ON a.id = i.account_id
WHERE e.session_id = ? AND s.principal_id = ? AND a.principal_id = s.principal_id
AND e.purpose = 'account-security' AND e.expires_at > ?
AND i.credential_epoch = s.credential_epoch;
