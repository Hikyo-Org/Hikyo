-- +goose Up
-- Approval-mediated temporary access (#152). A human who already has an
-- identity requests named capabilities in ONE environment for a bounded
-- duration; approval under the environment's access policy writes time-bound
-- grants with an absolute expiry. See the postgres copy for the full
-- rationale; this is the sqlite twin, timestamps in the fixed-width
-- microsecond TEXT form so the expiry range filters compare lexically. ASCII
-- only, matching every other migration.
--
-- Temporary grants live in their OWN table, never in `grants`: a row there can
-- never be converted into a permanent grant by an origin moving, and it is
-- never read by the grant writer's grantor bound, so temporary authority can be
-- USED but never re-granted. The chokepoint's grant lookup unions the
-- unexpired rows of access_grants into the principal's grant set, evaluated
-- against the transaction clock on every protected operation.

-- hikyo:table access_policies class=project chain=org_id,project_id
-- What may be requested where. environment_id = '' covers every environment in
-- the project; a concrete id narrows it to that environment (the concrete one
-- wins). capabilities is a JSON array drawn from the closed requestable set
-- the service enforces. version increments on every update so an open request
-- pinned to an older version fails closed.
CREATE TABLE access_policies (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL DEFAULT '',
    capabilities TEXT NOT NULL,
    max_duration_seconds INTEGER NOT NULL CHECK (max_duration_seconds > 0),
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

-- hikyo:table access_policy_approvers class=project chain=org_id,project_id
-- The approver set, same shape as approval_policy_approvers (#151): a principal
-- directly, or a SCIM group whose active members are eligible, re-resolved
-- live at every vote.
CREATE TABLE access_policy_approvers (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    policy_id TEXT NOT NULL REFERENCES access_policies (id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('principal', 'scim_group')),
    subject_id TEXT NOT NULL,
    scope_binding_id TEXT NOT NULL DEFAULT '',
    UNIQUE (policy_id, kind, subject_id)
);

CREATE INDEX access_policy_approvers_policy ON access_policy_approvers (policy_id);

-- hikyo:table access_policy_bypassers class=project chain=org_id,project_id
-- Emergency-access principals: they may take the policy's capabilities without
-- the quorum, only with current reauthentication and a reason, and only for a
-- bounded duration.
CREATE TABLE access_policy_bypassers (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    policy_id TEXT NOT NULL REFERENCES access_policies (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    UNIQUE (policy_id, principal_id)
);

CREATE INDEX access_policy_bypassers_policy ON access_policy_bypassers (principal_id);

-- hikyo:table access_requests class=environment chain=org_id,project_id
-- One immutable request: capabilities, duration, reason and the pinned policy
-- version are written once and no statement updates them, so a request cannot
-- expand during review. Only the lifecycle columns move. open and granted are
-- the two ACTIVE states (resolved_at is NULL for both); the rest are terminal.
-- expires_at is the absolute grant expiry, set when the request is granted;
-- review_expires_at bounds how long it may wait for a decision. policy_id is a
-- soft reference so a resolved request survives its policy as evidence.
CREATE TABLE access_requests (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    policy_id TEXT NOT NULL,
    policy_version INTEGER NOT NULL,
    requester_principal_id TEXT NOT NULL REFERENCES principals (id),
    capabilities TEXT NOT NULL,
    duration_seconds INTEGER NOT NULL CHECK (duration_seconds > 0),
    reason TEXT NOT NULL,
    bypassed INTEGER NOT NULL DEFAULT 0 CHECK (bypassed IN (0, 1)),
    state TEXT NOT NULL CHECK (
        state IN ('open', 'granted', 'rejected', 'cancelled', 'expired', 'invalidated', 'revoked')
    ),
    invalidated_cause TEXT NOT NULL DEFAULT '' CHECK (
        invalidated_cause IN ('', 'policy_changed', 'policy_disabled', 'approver_removed')
    ),
    resolved_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    review_expires_at TEXT NOT NULL,
    granted_at TEXT,
    expires_at TEXT,
    resolved_at TEXT,
    CHECK ((state = 'open' AND granted_at IS NULL) OR state <> 'open'),
    CHECK ((granted_at IS NULL AND expires_at IS NULL) OR (granted_at IS NOT NULL AND expires_at IS NOT NULL)),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE
);

CREATE INDEX access_requests_env ON access_requests (org_id, project_id, environment_id, state);
CREATE INDEX access_requests_review ON access_requests (state, review_expires_at);
CREATE INDEX access_requests_expiry ON access_requests (state, expires_at);

-- hikyo:table access_votes class=environment chain=org_id,project_id
-- One vote per approver per request; a reject resolves the request, approves
-- accumulate toward the quorum of currently-eligible approvers.
CREATE TABLE access_votes (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    request_id TEXT NOT NULL REFERENCES access_requests (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    decision TEXT NOT NULL CHECK (decision IN ('approve', 'reject')),
    created_at TEXT NOT NULL,
    UNIQUE (request_id, principal_id)
);

CREATE INDEX access_votes_request ON access_votes (request_id);

-- hikyo:table access_grants class=authn chain=-
-- The time-bound grant rows. They live on the resolution surface beside
-- `grants` because authorize() reads them to mint a proof. Every row is
-- environment-scoped (org, project and env all set) and carries an absolute
-- expires_at: the chokepoint admits a row only while expires_at is after the
-- transaction clock, so expiry needs no sweep, no session rotation and no
-- client cooperation to take effect. Revocation deletes the rows.
CREATE TABLE access_grants (
    id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id) ON DELETE CASCADE,
    capability TEXT NOT NULL,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    env_id TEXT NOT NULL,
    request_id TEXT NOT NULL REFERENCES access_requests (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    UNIQUE (request_id, capability),
    FOREIGN KEY (org_id, project_id, env_id) REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE
);

CREATE INDEX access_grants_principal ON access_grants (principal_id, expires_at);
CREATE INDEX access_grants_expiry ON access_grants (expires_at);

-- +goose Down
DROP TABLE access_grants;
DROP TABLE access_votes;
DROP TABLE access_requests;
DROP TABLE access_policy_bypassers;
DROP TABLE access_policy_approvers;
DROP TABLE access_policies;
