-- +goose Up
-- Secret-change approvals (#151). Policy-bound, multi-person review-and-merge
-- for secret and configuration CHANGES in sensitive scopes. See the postgres
-- copy for the full rationale; this is the sqlite twin, timestamps in the
-- canonical RFC3339 microsecond TEXT form the rest of the schema uses. ASCII
-- only, matching every other migration.
--
-- An approval authorises ONE exact reviewed change set and is never a second
-- mutation path: merge rides the ordinary validated publish, and the request
-- pins the existing publish preview-token digest over the closed selection.

-- hikyo:table approval_policies class=project chain=org_id,project_id
-- A policy binds to a project, optionally narrowed to one environment.
-- environment_id = '' means "all environments in the project"; a concrete id
-- narrows it to that environment. No FK on environment_id: '' is a real value
-- the composite chain FK could not satisfy, so environment existence is
-- validated in the service under the same project authority that writes here.
-- version increments on every update so an open request can detect that the
-- policy it was pinned to has moved and fail closed (invalidated).
CREATE TABLE approval_policies (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL DEFAULT '',
    min_approvals INTEGER NOT NULL CHECK (min_approvals >= 1),
    allow_self_approval INTEGER NOT NULL DEFAULT 0 CHECK (allow_self_approval IN (0, 1)),
    request_ttl_seconds INTEGER NOT NULL CHECK (request_ttl_seconds > 0),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by TEXT NOT NULL REFERENCES principals (id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (org_id, project_id, environment_id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id) ON DELETE CASCADE
);

CREATE INDEX approval_policies_project ON approval_policies (org_id, project_id);

-- hikyo:table approval_policy_approvers class=project chain=org_id,project_id
-- The approver set: a principal directly, or a SCIM group whose current active
-- members are eligible. subject_id is a principal id when kind='principal' and
-- a scim_groups.id when kind='scim_group'; no FK because the two kinds
-- reference different tables and eligibility is re-resolved live at vote and
-- merge time (an approver removed after approving must not still count).
-- scope_binding_id is the SCIM binding a group approver belongs to (its members
-- are resolved through that binding); '' for a principal approver.
CREATE TABLE approval_policy_approvers (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    policy_id TEXT NOT NULL REFERENCES approval_policies (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('principal', 'scim_group')),
    subject_id TEXT NOT NULL,
    scope_binding_id TEXT NOT NULL DEFAULT '',
    UNIQUE (policy_id, kind, subject_id)
);

CREATE INDEX approval_policy_approvers_policy ON approval_policy_approvers (policy_id);

-- hikyo:table approval_policy_bypassers class=project chain=org_id,project_id
-- Narrowly scoped emergency bypassers. A bypasser can merge a covered change
-- WITHOUT the quorum, but only through the dedicated bypass path: current
-- reauthentication, a reason, and a high-signal audit event.
CREATE TABLE approval_policy_bypassers (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    policy_id TEXT NOT NULL REFERENCES approval_policies (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    UNIQUE (policy_id, principal_id)
);

CREATE INDEX approval_policy_bypassers_policy ON approval_policy_bypassers (principal_id);

-- hikyo:table approval_requests class=environment chain=org_id,project_id
-- One immutable request pinned to the exact draft preview, target revision,
-- actor, scope and purpose. version_ids / closed_version_ids are JSON arrays of
-- the pending-change version ids (the selection and the key-group closure
-- additions). preview_token_digest is the SHA-256 hex of the existing publish
-- preview token over the closed selection, recomputed and compared
-- constant-time at merge; a later edit, environment advance or policy version
-- change makes the recomputation differ and the request fails closed.
--
-- policy_id / policy_version are a soft reference, not an FK: a merged or
-- bypassed request must remain as audit evidence after its policy is deleted.
CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    policy_id TEXT NOT NULL,
    policy_version INTEGER NOT NULL,
    requester_principal_id TEXT NOT NULL REFERENCES principals (id),
    version_ids TEXT NOT NULL,
    closed_version_ids TEXT NOT NULL,
    key_ids TEXT NOT NULL,
    preview_token_digest TEXT NOT NULL,
    base_revision INTEGER NOT NULL,
    purpose TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (
        state IN ('open', 'approved', 'merged', 'rejected', 'expired', 'invalidated', 'bypassed')
    ),
    invalidated_cause TEXT NOT NULL DEFAULT '' CHECK (
        invalidated_cause IN ('', 'policy_changed', 'draft_edited', 'env_advanced', 'approver_removed')
    ),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    resolved_at TEXT,
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE
);

CREATE INDEX approval_requests_env ON approval_requests (org_id, project_id, environment_id, state);
CREATE INDEX approval_requests_open ON approval_requests (state, expires_at);

-- hikyo:table approval_votes class=environment chain=org_id,project_id
-- One vote per approver per request, idempotent by the UNIQUE: a repeated
-- identical decision is a no-op, a second CONFLICTING decision is refused. A
-- reject is a veto that resolves the request; approves accumulate toward the
-- policy's quorum. Self-approval is refused at vote time unless the policy
-- explicitly permits it.
CREATE TABLE approval_votes (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    request_id TEXT NOT NULL REFERENCES approval_requests (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    decision TEXT NOT NULL CHECK (decision IN ('approve', 'reject')),
    created_at TEXT NOT NULL,
    UNIQUE (request_id, principal_id)
);

CREATE INDEX approval_votes_request ON approval_votes (request_id);

-- +goose Down
DROP TABLE approval_votes;
DROP TABLE approval_requests;
DROP TABLE approval_policy_bypassers;
DROP TABLE approval_policy_approvers;
DROP TABLE approval_policies;
