-- Host-local privacy operations. Explicit subject scope, never a network surface.

-- hikyo:authn-resolution
-- name: PrivacyAccount :one
SELECT a.id, a.principal_id, a.username, a.display_name, a.created_at, p.privacy_state FROM accounts AS a JOIN principals AS p ON p.id = a.principal_id WHERE a.principal_id = sqlc.arg(principal_id);

-- hikyo:authn-resolution
-- name: PrivacySetState :exec
UPDATE principals SET privacy_state = sqlc.arg(privacy_state), session_generation = session_generation + 1 WHERE id = sqlc.arg(principal_id) AND kind = 'human' AND privacy_state <> 'erased';

-- hikyo:authn-resolution
-- name: PrivacyEraseAccount :exec
UPDATE accounts SET username = sqlc.arg(username), display_name = '', webauthn_user_handle = NULL WHERE id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacySessions :many
SELECT id, artifact, auth_method, created_at, last_seen_at, source_ip, user_agent FROM sessions WHERE principal_id = sqlc.arg(principal_id) ORDER BY id LIMIT 10001;

-- hikyo:authn-resolution
-- name: PrivacyAuditInstance :many
SELECT id, type, occurred_at, outcome, source_ip, user_agent FROM audit_instance_events WHERE actor_id = sqlc.arg(principal_id) ORDER BY occurred_at, id LIMIT 10001;

-- hikyo:authn-resolution
-- name: PrivacyAuditTenant :many
SELECT id, type, occurred_at, outcome, source_ip, user_agent FROM audit_tenant_events WHERE actor_id = sqlc.arg(principal_id) ORDER BY occurred_at, id LIMIT 10001;

-- hikyo:authn-resolution
-- name: PrivacyEraseAuthorities :exec
DELETE FROM credential_authorities WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyErasePasswords :exec
DELETE FROM password_credentials WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseTOTP :exec
DELETE FROM totp_credentials WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseTOTPChallenges :exec
DELETE FROM totp_challenges WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseRecoveryCodes :exec
DELETE FROM recovery_codes WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseWebAuthn :exec
DELETE FROM webauthn_credentials WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseCeremonies :exec
DELETE FROM webauthn_ceremonies WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseOIDCTransactions :exec
DELETE FROM oidc_transactions WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseSAMLTransactions :exec
DELETE FROM saml_transactions WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseExternalIdentities :exec
DELETE FROM external_identities WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseHandoffs :exec
DELETE FROM cli_reauth_handoffs WHERE principal_id = sqlc.arg(principal_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseSCIMMembers :exec
DELETE FROM scim_group_members WHERE user_id IN (SELECT scim_users.id FROM scim_users WHERE scim_users.account_id = sqlc.arg(account_id));

-- hikyo:authn-resolution
-- name: PrivacyEraseSCIMUsers :exec
DELETE FROM scim_users WHERE account_id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: PrivacyEraseGrantOrigins :exec
DELETE FROM grant_origins WHERE grant_id IN (SELECT grants.id FROM grants WHERE grants.principal_id = sqlc.arg(principal_id));

-- hikyo:authn-resolution
-- name: PrivacyEraseGrants :exec
DELETE FROM grants WHERE principal_id = sqlc.arg(principal_id);

-- hikyo:authn-resolution
-- name: PrivacyCorrectAccount :exec
UPDATE accounts SET username = sqlc.arg(username), display_name = sqlc.arg(display_name) WHERE id = sqlc.arg(account_id);

-- Authenticated historical scratch recovery before migration 47 only. Ordinary
-- runtime and current-schema recovery use ListGrantsForPrincipal.
-- hikyo:authn-resolution
-- name: RecoveryListGrantsBeforePrivacy :many
SELECT g.capability, g.org_id, g.project_id, g.env_id FROM grants AS g
JOIN principals AS p ON p.id = g.principal_id
WHERE g.principal_id = sqlc.arg(principal_id)
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1);
