-- Email invitations of existing accounts (#1207, docs/spec/email-invitations.md).
-- The table is class=authn: acceptance resolves an opaque bearer verifier for
-- a caller who holds no grant in the inviting org, so every statement rides the
-- proof-free resolution surface AFTER the owning operation authorizes (the
-- send, list and revoke operations at the org, the acceptance by the session's
-- own account binding).

-- hikyo:reason A send writes or replaces the one pending row of an organisation and address; the operation authorized manage-members at that org.
-- hikyo:authn-resolution
-- name: UpsertOrgEmailInvitation :one
INSERT INTO org_email_invitations
    (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (org_id, email) DO UPDATE SET
    template = excluded.template, inviter_principal_id = excluded.inviter_principal_id,
    account_id = excluded.account_id, token_verifier = excluded.token_verifier,
    credential_epoch = excluded.credential_epoch, created_at = excluded.created_at,
    expires_at = excluded.expires_at
RETURNING id;

-- hikyo:reason The send charges the per-organisation pending cap after authorizing manage-members at that org.
-- hikyo:authn-resolution
-- name: CountLiveOrgEmailInvitations :one
SELECT COUNT(*) FROM org_email_invitations WHERE org_id = ? AND expires_at > ?;

-- hikyo:reason The pending list is read after authorizing manage-members at the addressed org and never selects the matched account.
-- hikyo:authn-resolution
-- name: ListLiveOrgEmailInvitations :many
SELECT id, email, template, inviter_principal_id, created_at, expires_at
FROM org_email_invitations WHERE org_id = ? AND expires_at > ? ORDER BY created_at, id;

-- hikyo:reason Revocation reads the addressed org's own row after authorizing manage-members at that org.
-- hikyo:authn-resolution
-- name: GetOrgEmailInvitationInOrg :one
SELECT id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at
FROM org_email_invitations WHERE id = ? AND org_id = ?;

-- hikyo:reason Revocation deletes the addressed org's own row after authorizing manage-members at that org.
-- hikyo:authn-resolution
-- name: DeleteOrgEmailInvitationInOrg :execrows
DELETE FROM org_email_invitations WHERE id = ? AND org_id = ?;

-- hikyo:reason Preview and acceptance resolve an opaque single-use bearer verifier for the caller's own session before any org authority exists.
-- hikyo:authn-resolution
-- name: GetOrgEmailInvitationByVerifier :one
SELECT id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at
FROM org_email_invitations WHERE token_verifier = ?;

-- hikyo:reason Acceptance CAS-consumes the verified row by id and verifier inside the grant transaction.
-- hikyo:authn-resolution
-- name: ConsumeOrgEmailInvitation :execrows
DELETE FROM org_email_invitations WHERE id = ? AND token_verifier = ?;

-- hikyo:reason The hourly pruning run deletes only rows already past their expiry.
-- hikyo:authn-resolution
-- name: PruneExpiredOrgEmailInvitations :execrows
DELETE FROM org_email_invitations WHERE expires_at <= ?;

-- hikyo:reason Account erasure unbinds every invitation bound to the erased account; the row stays listed until it expires and can never validate.
-- hikyo:authn-resolution
-- name: PrivacyEraseOrgEmailInvitations :exec
UPDATE org_email_invitations SET account_id = NULL WHERE account_id = ?;

-- The restore catalog probe confirms the table exists before this runs.
-- hikyo:reason restore must outrun every attacker-controlled email invitation epoch stamp
-- hikyo:authn-resolution
-- name: OrgEmailInvitationMaxCredentialEpoch :one
SELECT COALESCE(MAX(credential_epoch), 0) AS max_epoch FROM org_email_invitations;
