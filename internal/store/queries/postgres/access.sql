-- Approval-mediated temporary access (#152). The postgres twin of the sqlite
-- queries; ASCII only, matching every other query file.
--
-- Tenant-scoped statements bind org_id and project_id from the proof's
-- resolved chain, never from caller arguments; environment_id is bound from
-- the proof on the environment-addressed statements. Every statement is
-- single-table so the predicate analyzer can prove tenant scoping; the
-- installation-wide sweep and metric statements carry the instance-scoped
-- annotation and are content-pinned instead.
--
-- No statement here updates a request's capabilities, duration, reason or
-- pinned policy version: a request cannot expand once written.

-- name: InsertAccessPolicy :exec
INSERT INTO access_policies (
    id, org_id, project_id, environment_id, capabilities, max_duration_seconds,
    min_approvals, allow_self_approval, request_ttl_seconds, enabled, version,
    created_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- Policy reads hold the decision lock through approval or emergency grant commit.
-- name: GetAccessPolicy :one
SELECT id, org_id, project_id, environment_id, capabilities, max_duration_seconds,
    min_approvals, allow_self_approval, request_ttl_seconds, enabled, version,
    created_by, created_at, updated_at
FROM access_policies
WHERE org_id = $1 AND project_id = $2 AND id = $3
FOR UPDATE;

-- GetAccessPolicyForEnvironment is the coverage lookup: the service asks for
-- the concrete environment first, then '' for the project-wide policy.
-- Disabled policies are returned too; the service refuses on them by name.
-- name: GetAccessPolicyForEnvironment :one
SELECT id, org_id, project_id, environment_id, capabilities, max_duration_seconds,
    min_approvals, allow_self_approval, request_ttl_seconds, enabled, version,
    created_by, created_at, updated_at
FROM access_policies
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3
FOR UPDATE;

-- name: ListAccessPolicies :many
SELECT id, org_id, project_id, environment_id, capabilities, max_duration_seconds,
    min_approvals, allow_self_approval, request_ttl_seconds, enabled, version,
    created_by, created_at, updated_at
FROM access_policies
WHERE org_id = $1 AND project_id = $2
ORDER BY environment_id, id;

-- UpdateAccessPolicy bumps version so every open request pinned to the older
-- version fails closed at its next decision.
-- name: UpdateAccessPolicy :execrows
UPDATE access_policies
SET capabilities = $1, max_duration_seconds = $2, min_approvals = $3,
    allow_self_approval = $4, request_ttl_seconds = $5, enabled = $6,
    version = version + 1, updated_at = $7
WHERE org_id = $8 AND project_id = $9 AND id = $10;

-- name: DeleteAccessPolicy :execrows
DELETE FROM access_policies
WHERE org_id = $1 AND project_id = $2 AND id = $3;

-- name: InsertAccessPolicyApprover :exec
INSERT INTO access_policy_approvers (
    id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListAccessPolicyApprovers :many
SELECT id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id
FROM access_policy_approvers
WHERE org_id = $1 AND project_id = $2 AND policy_id = $3
ORDER BY kind, subject_id;

-- name: DeleteAccessPolicyApprovers :execrows
DELETE FROM access_policy_approvers
WHERE org_id = $1 AND project_id = $2 AND policy_id = $3;

-- name: InsertAccessPolicyBypasser :exec
INSERT INTO access_policy_bypassers (
    id, org_id, project_id, policy_id, principal_id
) VALUES ($1, $2, $3, $4, $5);

-- name: ListAccessPolicyBypassers :many
SELECT id, org_id, project_id, policy_id, principal_id
FROM access_policy_bypassers
WHERE org_id = $1 AND project_id = $2 AND policy_id = $3
ORDER BY principal_id;

-- name: DeleteAccessPolicyBypassers :execrows
DELETE FROM access_policy_bypassers
WHERE org_id = $1 AND project_id = $2 AND policy_id = $3;

-- name: GetAccessPolicyBypasser :one
SELECT id FROM access_policy_bypassers
WHERE org_id = $1 AND project_id = $2 AND policy_id = $3 AND principal_id = $4;

-- name: InsertAccessRequest :exec
INSERT INTO access_requests (
    id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, capabilities, duration_seconds, reason, bypassed,
    state, resolved_by, created_at, review_expires_at, granted_at, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17);

-- Serialize votes and lifecycle transitions before reading state and quorum.
-- name: GetAccessRequest :one
SELECT id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, capabilities, duration_seconds, reason, bypassed,
    state, invalidated_cause, resolved_by, created_at, review_expires_at,
    granted_at, expires_at, resolved_at
FROM access_requests
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND id = $4
FOR UPDATE;

-- name: ListAccessRequestsForEnvironment :many
SELECT id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, capabilities, duration_seconds, reason, bypassed,
    state, invalidated_cause, resolved_by, created_at, review_expires_at,
    granted_at, expires_at, resolved_at
FROM access_requests
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3
ORDER BY created_at DESC, id
LIMIT 200;

-- GrantAccessRequest moves an OPEN request to granted with its absolute
-- expiry. The state guard (always 'open', bound as a parameter so the
-- predicate analyzer sees a column-OP-param shape) makes a racing decision
-- lose rather than grant twice.
-- name: GrantAccessRequest :execrows
UPDATE access_requests
SET state = 'granted', granted_at = $1, expires_at = $2
WHERE org_id = $3 AND project_id = $4 AND environment_id = $5 AND id = $6 AND state = $7;

-- ResolveAccessRequest moves an ACTIVE request to a terminal state. from_state
-- is the state the caller read, so a concurrent resolution makes this a no-op
-- the caller can detect.
-- name: ResolveAccessRequest :execrows
UPDATE access_requests
SET state = sqlc.arg(to_state), invalidated_cause = sqlc.arg(cause),
    resolved_by = sqlc.arg(resolved_by), resolved_at = sqlc.arg(resolved_at)
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id)
  AND environment_id = sqlc.arg(environment_id) AND id = sqlc.arg(id)
  AND state = sqlc.arg(from_state);

-- name: InsertAccessVote :exec
INSERT INTO access_votes (
    id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetAccessVote :one
SELECT id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
FROM access_votes
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND request_id = $4 AND principal_id = $5;

-- name: ListAccessVotes :many
SELECT id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
FROM access_votes
WHERE org_id = $1 AND project_id = $2 AND environment_id = $3 AND request_id = $4
ORDER BY created_at, id;

-- SelectDueAccessRequests is one bounded installation-wide sweep batch: open
-- requests whose review window lapsed and granted requests whose absolute
-- expiry passed. Expiry is already enforced by the chokepoint's row filter;
-- the sweep is bookkeeping, session rotation and the audit event.
-- hikyo:instance-scoped
-- name: SelectDueAccessRequests :many
SELECT id, org_id, project_id, environment_id, policy_id, requester_principal_id, state
FROM access_requests
WHERE (state = 'open' AND review_expires_at <= sqlc.arg(now))
   OR (state = 'granted' AND expires_at <= sqlc.arg(now))
ORDER BY id
LIMIT 100;

-- MarkAccessRequestExpired resolves one active request as expired, guarded by
-- the state it was selected in so a concurrent decision or revoke wins.
-- hikyo:instance-scoped
-- name: MarkAccessRequestExpired :execrows
UPDATE access_requests
SET state = 'expired', resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(id) AND state = sqlc.arg(from_state);

-- hikyo:instance-scoped
-- name: CountOpenAccessRequests :one
SELECT COUNT(*) FROM access_requests WHERE state = 'open';

-- hikyo:instance-scoped
-- name: CountActiveAccessGrants :one
SELECT COUNT(*) FROM access_requests WHERE state = 'granted' AND expires_at > $1;
