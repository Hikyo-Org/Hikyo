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
FROM registration_policies WHERE org_id = $1;

-- hikyo:authn-resolution
-- name: GetInstanceRegistrationPolicy :one
SELECT id, org_id, authority_principal_id, landing, template, local_enabled,
       fresh_org_cap, row_version, created_at, updated_at
FROM registration_policies WHERE org_id IS NULL;

-- hikyo:authn-resolution
-- name: ListRegistrationPolicyDomains :many
SELECT domain FROM registration_policy_domains WHERE policy_id = $1 ORDER BY domain;

-- hikyo:authn-resolution
-- name: ListRegistrationPolicyEntries :many
SELECT id, provider_kind, provider_id, claim, created_at
FROM registration_policy_entries WHERE policy_id = $1 ORDER BY created_at, id;

-- hikyo:authn-resolution
-- name: ListRegistrationPolicyEntryValues :many
SELECT v.entry_id, v.value
FROM registration_policy_entry_values v
JOIN registration_policy_entries e ON e.id = v.entry_id
WHERE e.policy_id = $1 ORDER BY v.entry_id, v.value;

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicy :exec
INSERT INTO registration_policies
    (id, org_id, authority_principal_id, landing, template, local_enabled,
     fresh_org_cap, row_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $9);

-- hikyo:authn-resolution
-- name: UpdateRegistrationPolicyCAS :execrows
UPDATE registration_policies
SET authority_principal_id = $1, landing = $2, template = $3, local_enabled = $4,
    fresh_org_cap = $5, row_version = row_version + 1, updated_at = $6
WHERE id = $7 AND row_version = $8;

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicyEntryValues :exec
DELETE FROM registration_policy_entry_values
WHERE entry_id IN (SELECT id FROM registration_policy_entries WHERE policy_id = $1);

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicyEntries :exec
DELETE FROM registration_policy_entries WHERE policy_id = $1;

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicyDomains :exec
DELETE FROM registration_policy_domains WHERE policy_id = $1;

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicyDomain :exec
INSERT INTO registration_policy_domains (policy_id, domain) VALUES ($1, $2);

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicyEntry :exec
INSERT INTO registration_policy_entries (id, policy_id, provider_kind, provider_id, claim, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- hikyo:authn-resolution
-- name: InsertRegistrationPolicyEntryValue :exec
INSERT INTO registration_policy_entry_values (entry_id, value) VALUES ($1, $2);

-- hikyo:authn-resolution
-- name: DeleteRegistrationPolicy :execrows
DELETE FROM registration_policies WHERE id = $1 AND row_version = $2;

-- The live count behind a fresh-org policy's `n / cap` (#585 d9): orgs the
-- policy minted that still exist. Instance-wide by construction: the orgs a
-- policy mints are not under any one tenant the reader could be bound to.
-- hikyo:authn-resolution
-- name: CountRegistrationPolicyOrgs :one
SELECT COUNT(*) FROM orgs WHERE registration_policy_id = $1;

-- Pending local sign-ups (#584). The request writer lands with #608; the
-- policy delete below already has to clear them (spec section 4).
-- hikyo:authn-resolution
-- name: InsertRegistrationSignup :exec
INSERT INTO registration_signups
    (id, email, token_verifier, policy_id, signup_scope_org_id, credential_epoch, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- hikyo:authn-resolution
-- name: ListRegistrationSignupsForPolicy :many
SELECT id, signup_scope_org_id FROM registration_signups WHERE policy_id = $1 ORDER BY id;

-- hikyo:authn-resolution
-- name: DeleteRegistrationSignup :execrows
DELETE FROM registration_signups WHERE id = $1;
