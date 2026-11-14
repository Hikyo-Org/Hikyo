-- Permission model, full (#55). Structurally identical to the sqlite
-- dialect; see that file for the reasoning.

-- hikyo:authn-resolution
-- name: ListGrantRowsForPrincipal :many
SELECT id, capability, org_id, project_id, env_id FROM grants
WHERE principal_id = sqlc.arg(principal_id);

-- A principal's footprint: every org that holds any row about it (grants, timed
-- access grants whether or not expired, member-access rules, its service
-- account, its developer credential, its SCIM provisioning). An instance-scope
-- grant yields a NULL org. Deliberately not gated on privacy state.
-- hikyo:reason Authorized grant and rule writers read only the distinct org ids in which the target principal already holds any row, to refuse a target outside the actor's administered orgs with the nonexistent-principal answer; it returns no tenant identifiers beyond those org ids and writes nothing. Network credential-reset classification reads the same set to count the target's orgs.
-- hikyo:authn-resolution
-- name: ListPrincipalFootprintOrgs :many
SELECT g.org_id FROM grants g WHERE g.principal_id = sqlc.arg(principal_id)
UNION
SELECT ag.org_id FROM access_grants ag WHERE ag.principal_id = sqlc.arg(principal_id)
UNION
SELECT r.org_id FROM rules r WHERE r.principal_id = sqlc.arg(principal_id)
UNION
SELECT sa.org_id FROM service_accounts sa WHERE sa.principal_id = sqlc.arg(principal_id)
UNION
SELECT dc.org_id FROM developer_credentials dc WHERE dc.principal_id = sqlc.arg(principal_id)
UNION
SELECT u.org_id FROM scim_users u JOIN accounts a ON a.id = u.account_id WHERE a.principal_id = sqlc.arg(principal_id);

-- hikyo:authn-resolution
-- name: InsertGrantOrigin :exec
INSERT INTO grant_origins (id, grant_id, kind, subject, created_at)
VALUES (sqlc.arg(id), sqlc.arg(grant_id), sqlc.arg(kind), sqlc.arg(subject), sqlc.arg(created_at));

-- hikyo:authn-resolution
-- name: DeleteGrantOrigin :execrows
DELETE FROM grant_origins
WHERE grant_id = sqlc.arg(grant_id) AND kind = sqlc.arg(kind) AND subject = sqlc.arg(subject);

-- hikyo:authn-resolution
-- name: CountGrantOrigins :one
SELECT COUNT(*) FROM grant_origins WHERE grant_id = sqlc.arg(grant_id);

-- hikyo:authn-resolution
-- name: DeleteGrantRow :execrows
DELETE FROM grants WHERE id = sqlc.arg(id);

-- hikyo:authn-resolution
-- name: ListGrantsWithOriginsForOrg :many
SELECT g.id, g.principal_id, g.capability, g.org_id, g.project_id, g.env_id,
       g.created_at, o.kind, o.subject
FROM grants AS g
INNER JOIN grant_origins AS o ON o.grant_id = g.id
WHERE g.org_id = sqlc.arg(org_id)
ORDER BY g.principal_id, g.capability, g.id, o.kind, o.subject;

-- The project-scoped membership surface; see the sqlite dialect.
-- hikyo:authn-resolution
-- name: ListGrantsWithOriginsForProject :many
SELECT g.id, g.principal_id, g.capability, g.org_id, g.project_id, g.env_id,
       g.created_at, o.kind, o.subject
FROM grants AS g
INNER JOIN grant_origins AS o ON o.grant_id = g.id
WHERE g.org_id = sqlc.arg(org_id) AND g.project_id = sqlc.arg(project_id)
ORDER BY g.principal_id, g.capability, g.id, o.kind, o.subject;

-- hikyo:authn-resolution
-- name: ListGrantsWithOriginsAtInstance :many
SELECT g.id, g.principal_id, g.capability, g.org_id, g.project_id, g.env_id,
       g.created_at, o.kind, o.subject
FROM grants AS g
INNER JOIN grant_origins AS o ON o.grant_id = g.id
WHERE g.org_id IS NULL
ORDER BY g.principal_id, g.capability, g.id, o.kind, o.subject;

-- The project_id IS NULL conjunct is load-bearing: see the sqlite dialect.
-- hikyo:authn-resolution
-- name: ListManageMembersHoldersForOrg :many
SELECT DISTINCT principal_id FROM grants
WHERE capability = 'manage-members'
  AND project_id IS NULL
  AND (org_id = sqlc.arg(org_id) OR org_id IS NULL)
AND principal_id IN (SELECT principals.id FROM principals WHERE principals.privacy_state = 'active' AND principals.containment_state = 'none')
ORDER BY principal_id;

-- hikyo:authn-resolution
-- name: ListManageMembersHoldersAtInstance :many
SELECT DISTINCT principal_id FROM grants
WHERE capability = 'manage-members' AND org_id IS NULL
AND principal_id IN (SELECT principals.id FROM principals WHERE principals.privacy_state = 'active' AND principals.containment_state = 'none')
ORDER BY principal_id;

-- hikyo:authn-resolution
-- name: EnvironmentReauthSettings :one
SELECT protected, reauth_window_seconds FROM environments WHERE id = sqlc.arg(id);

-- hikyo:authn-resolution
-- name: ProjectMachineReveal :one
SELECT machine_reveal, machine_reveal_generation FROM projects WHERE id = sqlc.arg(id);

-- hikyo:authn-resolution
-- name: GetPrincipalClass :one
SELECT kind, class FROM principals WHERE id = sqlc.arg(id);

-- hikyo:authn-resolution
-- name: ListGrantOriginsForGrant :many
SELECT kind, subject FROM grant_origins WHERE grant_id = sqlc.arg(grant_id) ORDER BY kind, subject;

-- hikyo:authn-resolution
-- name: CountGrantsForOrg :one
SELECT COUNT(*) FROM grants WHERE org_id = sqlc.arg(org_id);

-- Approval-mediated temporary access (#152). The time-bound grant rows are on
-- the resolution surface for the same reason `grants` is: authorize() reads
-- them. The writers take the principal-row lock like every grant writer.
-- hikyo:authn-resolution
-- name: InsertAccessGrant :exec
INSERT INTO access_grants (id, principal_id, capability, org_id, project_id, env_id, request_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- Revocation and expiry release a request's rows together.
-- hikyo:authn-resolution
-- name: DeleteAccessGrantsForRequest :execrows
DELETE FROM access_grants WHERE request_id = $1 AND principal_id = $2;

-- A principal's live temporary grants, for the membership surface.
-- hikyo:authn-resolution
-- name: ListLiveAccessGrantsForPrincipal :many
SELECT id, capability, org_id, project_id, env_id, request_id, created_at, expires_at
FROM access_grants
WHERE principal_id = $1 AND expires_at > $2
ORDER BY expires_at, id;
