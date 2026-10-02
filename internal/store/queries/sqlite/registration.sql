-- Registration policy (#606, social-signin spec section 2.1). The policy
-- tables are class=authn: the policy decides whether an unknown identity may
-- become an account, and the sign-up legs that read it (#607, #608) run before
-- any principal exists, so every statement rides the proof-free, enumerated
-- authn resolution surface. Administration is authorized at the chokepoint
-- (registration-policy.*) before any writer here runs, exactly like provider
-- administration.

-- hikyo:authn-resolution
-- name: GetOrgRegistrationPolicy :one
SELECT id, org_id, authority_principal_id, landing, template, local_enabled,
       fresh_org_cap, row_version, created_at, updated_at
FROM registration_policies WHERE org_id = ?;

-- hikyo:authn-resolution
-- name: GetInstanceRegistrationPolicy :one
SELECT id, org_id, authority_principal_id, landing, template, local_enabled,
       fresh_org_cap, row_version, created_at, updated_at
FROM registration_policies WHERE org_id IS NULL;

-- hikyo:authn-resolution
-- name: ListRegistrationPolicyDomains :many
SELECT domain FROM registration_policy_domains WHERE policy_id = ? ORDER BY domain;

-- hikyo:authn-resolution
-- name: ListRegistrationPolicyEntries :many
SELECT id, provider_kind, provider_id, claim, created_at
FROM registration_policy_entries WHERE policy_id = ? ORDER BY created_at, id;

-- hikyo:authn-resolution
-- name: ListRegistrationPolicyEntryValues :many
SELECT v.entry_id, v.value
FROM registration_policy_entry_values v
JOIN registration_policy_entries e ON e.id = v.entry_id
WHERE e.policy_id = ? ORDER BY v.entry_id, v.value;

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicy :exec
INSERT INTO registration_policies
    (id, org_id, authority_principal_id, landing, template, local_enabled,
     fresh_org_cap, row_version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?);

-- hikyo:authn-resolution
-- name: UpdateRegistrationPolicyCAS :execrows
UPDATE registration_policies
SET authority_principal_id = ?, landing = ?, template = ?, local_enabled = ?,
    fresh_org_cap = ?, row_version = row_version + 1, updated_at = ?
WHERE id = ? AND row_version = ?;

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicyEntryValues :exec
DELETE FROM registration_policy_entry_values
WHERE entry_id IN (SELECT id FROM registration_policy_entries WHERE policy_id = ?);

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicyEntries :exec
DELETE FROM registration_policy_entries WHERE policy_id = ?;

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicyDomains :exec
DELETE FROM registration_policy_domains WHERE policy_id = ?;

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicyDomain :exec
INSERT INTO registration_policy_domains (policy_id, domain) VALUES (?, ?);

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicyEntry :exec
INSERT INTO registration_policy_entries (id, policy_id, provider_kind, provider_id, claim, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicyEntryValue :exec
INSERT INTO registration_policy_entry_values (entry_id, value) VALUES (?, ?);

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicy :execrows
DELETE FROM registration_policies WHERE id = ? AND row_version = ?;

-- The live count behind a fresh-org policy's `n / cap` (#585 d9): orgs the
-- policy minted that still exist. Instance-wide by construction: the orgs a
-- policy mints are not under any one tenant the reader could be bound to.
-- hikyo:authn-resolution
-- name: CountRegistrationPolicyOrgs :one
SELECT COUNT(*) FROM orgs WHERE registration_policy_id = ?;

-- Pending local sign-ups (#584). The request writer lands with #608; the
-- policy delete below already has to clear them (spec section 4).
-- hikyo:authn-resolution
-- name: InsertRegistrationSignup :exec
INSERT INTO registration_signups
    (id, email, token_verifier, policy_id, signup_scope_org_id, credential_epoch, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- hikyo:authn-resolution
-- name: ListRegistrationSignupsForPolicy :many
SELECT id, signup_scope_org_id FROM registration_signups WHERE policy_id = ? ORDER BY id;

-- hikyo:authn-resolution
-- name: DeleteRegistrationSignup :execrows
DELETE FROM registration_signups WHERE id = ?;

-- hikyo:reason The admitted signup request resolves its canonical address to one pending signup row before issuing mail.
-- hikyo:authn-resolution
-- name: GetRegistrationSignupByEmail :one
SELECT id, email, token_verifier, policy_id, signup_scope_org_id, credential_epoch, created_at, expires_at FROM registration_signups WHERE email = ?;

-- hikyo:reason The signup ceremony resolves an opaque bearer verifier without exposing the pending row to authenticated callers.
-- hikyo:authn-resolution
-- name: GetRegistrationSignupByVerifier :one
SELECT id, email, token_verifier, policy_id, signup_scope_org_id, credential_epoch, created_at, expires_at FROM registration_signups WHERE token_verifier = ?;

-- hikyo:reason The admitted local signup request rotates only the matched pending signup row under the live registration policy.
-- hikyo:authn-resolution
-- name: ReissueRegistrationSignup :execrows
UPDATE registration_signups SET token_verifier = ?, policy_id = ?, signup_scope_org_id = ?, credential_epoch = ?, expires_at = ? WHERE id = ?;

-- hikyo:reason A verified signup bearer is CAS-consumed by its row id and verifier inside the account-creation transaction.
-- hikyo:authn-resolution
-- name: ConsumeRegistrationSignup :execrows
DELETE FROM registration_signups WHERE id = ? AND token_verifier = ?;

-- hikyo:reason The singleton registration reaper enumerates only expired pending rows for audited cleanup.
-- hikyo:authn-resolution
-- name: ListExpiredRegistrationSignups :many
SELECT id, policy_id, signup_scope_org_id FROM registration_signups WHERE expires_at <= ? ORDER BY id;

-- hikyo:reason Local login resolves a canonical verified address only to an active principal, never as a federated linking key.
-- hikyo:authn-resolution
-- name: GetAccountByEmail :one
SELECT id, principal_id, username, display_name, created_at FROM accounts WHERE email = ? AND email_verified_at IS NOT NULL AND principal_id IN (SELECT id FROM principals WHERE privacy_state = 'active');

-- hikyo:reason The successful single-use signup ceremony sets the verified address only on its newly created local account.
-- hikyo:authn-resolution
-- name: SetLocalAccountEmail :exec
UPDATE accounts SET email = ?, email_verified_at = ? WHERE id = ?;

-- hikyo:reason The singleton reaper deletes only its observed expired row and rechecks expiry to preserve concurrent resend.
-- hikyo:authn-resolution
-- name: PruneExpiredRegistrationSignup :execrows
DELETE FROM registration_signups WHERE id = ? AND expires_at <= ?;

-- hikyo:reason The admitted signup request checks address ownership regardless of principal privacy only to select the no-link notice; its public answer remains uniform.
-- hikyo:authn-resolution
-- name: VerifiedAccountEmailExists :one
SELECT EXISTS(SELECT 1 FROM accounts WHERE email = ? AND email_verified_at IS NOT NULL);
