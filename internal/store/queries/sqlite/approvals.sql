-- Secret-change approvals (#151). ASCII ONLY: sqlc's sqlite path silently
-- mis-slices statements containing non-ASCII.
--
-- Tenant-scoped statements bind org_id and project_id from the proof's resolved
-- chain, never from caller arguments; environment_id is bound from the proof on
-- the environment-addressed statements. Policy, request and vote identity and
-- the vote decision are caller data. Every statement is single-table so the
-- predicate analyzer (analyzer 2) can prove tenant scoping; the two
-- installation-wide sweep statements carry the instance-scoped annotation and
-- are content-pinned instead.

-- name: InsertApprovalPolicy :exec
INSERT INTO approval_policies (
    id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetApprovalPolicy :one
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = ? AND project_id = ? AND id = ?;

-- GetApprovalPolicyForEnvironment is the exact-environment coverage lookup: an
-- enabled policy narrowed to this environment. Concrete match beats the
-- project-wide default, so the service tries this first.
-- name: GetApprovalPolicyForEnvironment :one
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND enabled = 1;

-- GetApprovalPolicyProjectWide is the project-wide coverage lookup
-- (environment_id = ''), consulted only when no exact-environment policy exists.
-- name: GetApprovalPolicyProjectWide :one
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = ? AND project_id = ? AND environment_id = '' AND enabled = 1;

-- name: ListApprovalPolicies :many
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = ? AND project_id = ?
ORDER BY environment_id, id;

-- UpdateApprovalPolicy bumps version so any request pinned to the older version
-- fails closed. The service invalidates open requests to the covered
-- environment in the same transaction.
-- name: UpdateApprovalPolicy :execrows
UPDATE approval_policies
SET min_approvals = ?, allow_self_approval = ?, request_ttl_seconds = ?,
    enabled = ?, version = version + 1, updated_at = ?
WHERE org_id = ? AND project_id = ? AND id = ?;

-- name: DeleteApprovalPolicy :execrows
DELETE FROM approval_policies
WHERE org_id = ? AND project_id = ? AND id = ?;

-- name: InsertApprovalPolicyApprover :exec
INSERT INTO approval_policy_approvers (
    id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListApprovalPolicyApprovers :many
SELECT id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id
FROM approval_policy_approvers
WHERE org_id = ? AND project_id = ? AND policy_id = ?
ORDER BY kind, subject_id;

-- name: DeleteApprovalPolicyApprovers :execrows
DELETE FROM approval_policy_approvers
WHERE org_id = ? AND project_id = ? AND policy_id = ?;

-- name: InsertApprovalPolicyBypasser :exec
INSERT INTO approval_policy_bypassers (
    id, org_id, project_id, policy_id, principal_id
) VALUES (?, ?, ?, ?, ?);

-- name: ListApprovalPolicyBypassers :many
SELECT id, org_id, project_id, policy_id, principal_id
FROM approval_policy_bypassers
WHERE org_id = ? AND project_id = ? AND policy_id = ?
ORDER BY principal_id;

-- name: DeleteApprovalPolicyBypassers :execrows
DELETE FROM approval_policy_bypassers
WHERE org_id = ? AND project_id = ? AND policy_id = ?;

-- GetApprovalPolicyBypasser reports whether one principal is a named bypasser of
-- one policy. Absence is sql.ErrNoRows.
-- name: GetApprovalPolicyBypasser :one
SELECT id FROM approval_policy_bypassers
WHERE org_id = ? AND project_id = ? AND policy_id = ? AND principal_id = ?;

-- name: InsertApprovalRequest :exec
INSERT INTO approval_requests (
    id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, version_ids, closed_version_ids, key_ids, preview_token_digest,
    base_revision, purpose, state, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetApprovalRequest :one
SELECT id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, version_ids, closed_version_ids, key_ids, preview_token_digest,
    base_revision, purpose, state, invalidated_cause, created_at, expires_at, resolved_at
FROM approval_requests
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND id = ?;

-- name: ListApprovalRequestsForEnvironment :many
SELECT id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, version_ids, closed_version_ids, key_ids, preview_token_digest,
    base_revision, purpose, state, invalidated_cause, created_at, expires_at, resolved_at
FROM approval_requests
WHERE org_id = ? AND project_id = ? AND environment_id = ?
ORDER BY created_at DESC, id;


-- UpdateApprovalRequestState transitions one request. resolved_at is NULL for
-- the approved state and set for every terminal state; invalidated_cause is ''
-- unless the terminal state is invalidated.
-- name: UpdateApprovalRequestState :execrows
UPDATE approval_requests
SET state = ?, invalidated_cause = ?, resolved_at = ?
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND id = ?;

-- SelectExpiredApprovalRequests is the installation-wide expiry sweep: every
-- active request past its expiry, across all tenants, read under scheduler
-- authority. Cross-tenant by definition; annotated and content-pinned.
-- hikyo:instance-scoped
-- name: SelectExpiredApprovalRequests :many
SELECT id, org_id, project_id, environment_id, policy_id, requester_principal_id, expires_at
FROM approval_requests
WHERE resolved_at IS NULL AND expires_at < ?
ORDER BY id;

-- MarkApprovalRequestExpired resolves one request as expired, idempotent by the
-- resolved_at IS NULL guard so a concurrent merge that already resolved it wins.
-- Runs under scheduler authority (cross-tenant); annotated and content-pinned.
-- hikyo:instance-scoped
-- name: MarkApprovalRequestExpired :execrows
UPDATE approval_requests
SET state = 'expired', resolved_at = ?
WHERE id = ? AND resolved_at IS NULL;

-- name: InsertApprovalVote :exec
INSERT INTO approval_votes (
    id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- GetApprovalVote returns one approver's existing vote on one request, so a
-- repeated identical decision is idempotent and a conflicting one is refused.
-- name: GetApprovalVote :one
SELECT id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
FROM approval_votes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND request_id = ? AND principal_id = ?;

-- name: ListApprovalVotes :many
SELECT id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
FROM approval_votes
WHERE org_id = ? AND project_id = ? AND environment_id = ? AND request_id = ?
ORDER BY created_at, id;
