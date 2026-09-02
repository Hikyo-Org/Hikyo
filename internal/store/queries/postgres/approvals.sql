-- Secret-change approvals (#151). ASCII ONLY, matching the sqlite twin.
--
-- Tenant-scoped statements bind org_id and project_id from the proof's resolved
-- chain (the reserved chain_* parameters), never from caller arguments;
-- environment_id is bound from the proof (chain_env_id) on the
-- environment-addressed statements. Policy, request and vote identity and the
-- vote decision are caller data. Every statement is single-table so the
-- predicate analyzer can prove tenant scoping; the two installation-wide sweep
-- statements carry the instance-scoped annotation and are content-pinned.

-- name: InsertApprovalPolicy :exec
INSERT INTO approval_policies (
    id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id),
    sqlc.arg(environment_id), sqlc.arg(min_approvals), sqlc.arg(allow_self_approval),
    sqlc.arg(request_ttl_seconds), sqlc.arg(enabled), sqlc.arg(version),
    sqlc.arg(created_by), sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- name: GetApprovalPolicy :one
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id);

-- GetApprovalPolicyForEnvironment is the coverage lookup. The service calls it
-- with the concrete environment first, then with '' for the project-wide
-- default: a concrete-environment policy beats the project-wide one. `enabled`
-- is a bound parameter (always the true value) so the predicate analyzer sees a
-- column-OP-param shape rather than a literal.
-- name: GetApprovalPolicyForEnvironment :one
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(environment_id) AND enabled = sqlc.arg(enabled);

-- name: ListApprovalPolicies :many
SELECT id, org_id, project_id, environment_id, min_approvals, allow_self_approval,
    request_ttl_seconds, enabled, version, created_by, created_at, updated_at
FROM approval_policies
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
ORDER BY environment_id, id;

-- UpdateApprovalPolicy bumps version so any request pinned to the older version
-- fails closed. The service invalidates open requests to the covered
-- environment in the same transaction.
-- name: UpdateApprovalPolicy :execrows
UPDATE approval_policies
SET min_approvals = sqlc.arg(min_approvals), allow_self_approval = sqlc.arg(allow_self_approval),
    request_ttl_seconds = sqlc.arg(request_ttl_seconds), enabled = sqlc.arg(enabled),
    version = version + 1, updated_at = sqlc.arg(updated_at)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id);

-- name: DeleteApprovalPolicy :execrows
DELETE FROM approval_policies
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id);

-- name: InsertApprovalPolicyApprover :exec
INSERT INTO approval_policy_approvers (
    id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id),
    sqlc.arg(policy_id), sqlc.arg(kind), sqlc.arg(subject_id), sqlc.arg(scope_binding_id)
);

-- name: ListApprovalPolicyApprovers :many
SELECT id, org_id, project_id, policy_id, kind, subject_id, scope_binding_id
FROM approval_policy_approvers
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND policy_id = sqlc.arg(policy_id)
ORDER BY kind, subject_id;

-- name: DeleteApprovalPolicyApprovers :execrows
DELETE FROM approval_policy_approvers
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND policy_id = sqlc.arg(policy_id);

-- name: InsertApprovalPolicyBypasser :exec
INSERT INTO approval_policy_bypassers (
    id, org_id, project_id, policy_id, principal_id
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id),
    sqlc.arg(policy_id), sqlc.arg(principal_id)
);

-- name: ListApprovalPolicyBypassers :many
SELECT id, org_id, project_id, policy_id, principal_id
FROM approval_policy_bypassers
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND policy_id = sqlc.arg(policy_id)
ORDER BY principal_id;

-- name: DeleteApprovalPolicyBypassers :execrows
DELETE FROM approval_policy_bypassers
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND policy_id = sqlc.arg(policy_id);

-- GetApprovalPolicyBypasser reports whether one principal is a named bypasser of
-- one policy. Absence is pgx.ErrNoRows.
-- name: GetApprovalPolicyBypasser :one
SELECT id FROM approval_policy_bypassers
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND policy_id = sqlc.arg(policy_id) AND principal_id = sqlc.arg(principal_id);

-- name: InsertApprovalRequest :exec
INSERT INTO approval_requests (
    id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, version_ids, closed_version_ids, key_ids, preview_token_digest,
    base_revision, purpose, state, created_at, expires_at
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id),
    sqlc.arg(chain_env_id), sqlc.arg(policy_id), sqlc.arg(policy_version),
    sqlc.arg(requester_principal_id), sqlc.arg(version_ids), sqlc.arg(closed_version_ids),
    sqlc.arg(key_ids), sqlc.arg(preview_token_digest), sqlc.arg(base_revision), sqlc.arg(purpose),
    sqlc.arg(state), sqlc.arg(created_at), sqlc.arg(expires_at)
);

-- name: GetApprovalRequest :one
SELECT id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, version_ids, closed_version_ids, key_ids, preview_token_digest,
    base_revision, purpose, state, invalidated_cause, created_at, expires_at, resolved_at
FROM approval_requests
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(id);

-- name: ListApprovalRequestsForEnvironment :many
SELECT id, org_id, project_id, environment_id, policy_id, policy_version,
    requester_principal_id, version_ids, closed_version_ids, key_ids, preview_token_digest,
    base_revision, purpose, state, invalidated_cause, created_at, expires_at, resolved_at
FROM approval_requests
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
ORDER BY created_at DESC, id;


-- UpdateApprovalRequestState transitions one request. resolved_at is NULL for
-- the approved state and set for every terminal state; invalidated_cause is ''
-- unless the terminal state is invalidated.
-- name: UpdateApprovalRequestState :execrows
UPDATE approval_requests
SET state = sqlc.arg(state), invalidated_cause = sqlc.arg(invalidated_cause),
    resolved_at = sqlc.arg(resolved_at)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND id = sqlc.arg(id);

-- SelectExpiredApprovalRequests is the installation-wide expiry sweep: every
-- active request past its expiry, across all tenants, read under scheduler
-- authority. Cross-tenant by definition; annotated and content-pinned.
-- hikyo:instance-scoped
-- name: SelectExpiredApprovalRequests :many
SELECT id, org_id, project_id, environment_id, policy_id, requester_principal_id, expires_at
FROM approval_requests
WHERE resolved_at IS NULL AND expires_at < sqlc.arg(now)
ORDER BY id;

-- MarkApprovalRequestExpired resolves one request as expired, idempotent by the
-- resolved_at IS NULL guard so a concurrent merge that already resolved it wins.
-- Runs under scheduler authority (cross-tenant); annotated and content-pinned.
-- hikyo:instance-scoped
-- name: MarkApprovalRequestExpired :execrows
UPDATE approval_requests
SET state = 'expired', resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(id) AND resolved_at IS NULL;

-- name: InsertApprovalVote :exec
INSERT INTO approval_votes (
    id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id),
    sqlc.arg(chain_env_id), sqlc.arg(request_id), sqlc.arg(principal_id),
    sqlc.arg(decision), sqlc.arg(created_at)
);

-- GetApprovalVote returns one approver's existing vote on one request, so a
-- repeated identical decision is idempotent and a conflicting one is refused.
-- name: GetApprovalVote :one
SELECT id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
FROM approval_votes
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND request_id = sqlc.arg(request_id)
  AND principal_id = sqlc.arg(principal_id);

-- name: ListApprovalVotes :many
SELECT id, org_id, project_id, environment_id, request_id, principal_id, decision, created_at
FROM approval_votes
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND request_id = sqlc.arg(request_id)
ORDER BY created_at, id;

-- Operational metrics (#151). Installation-wide counts read at /metrics scrape
-- under scheduler authority; cross-tenant by definition, annotated and pinned.
-- hikyo:instance-scoped
-- name: CountActiveApprovalRequests :one
SELECT COUNT(*) FROM approval_requests WHERE resolved_at IS NULL;

-- hikyo:instance-scoped
-- name: CountExpiredApprovalRequests :one
SELECT COUNT(*) FROM approval_requests WHERE state = 'expired';
