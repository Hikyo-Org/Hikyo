-- +goose Up
-- Scheduled credential rotation (#148, slice S1: configuration). Roll-forward
-- only. A rotation is an environment-scoped standing configuration that acts as
-- its recorded authority principal (the human who configured it); a run is one
-- rotation cycle and is itself the durable job (per-row lease_owner /
-- lease_expires_at / lease_claim_token fence, the dynamic_leases shape, so every
-- node can run the executor without a singleton scheduler lease);
-- rotation_effects is the INTENT/OUTCOME ledger (the dynamic_effects shape). No
-- column here holds a secret: the candidate password exists only in worker
-- memory and in the single publish transaction, never stored (see
-- docs/handoff/148-credential-rotation.md). Handles are public role names.
-- Slice S1 creates every table so the executor slice needs no table rebuild;
-- nothing writes rotation_runs or rotation_effects until it ships.

-- hikyo:table rotations class=environment chain=org_id,project_id
-- state: 'paused' is an operator or cascade pause (provider deleted, environment
-- protected, approval policy added); 'halted' is the executor's named stop after a
-- failed authority gate, with the short non-secret halt_reason the doctor shows;
-- 'disabled' is the soft delete. Only a full resume authorization leaves paused or
-- halted. S1 writes active, paused and disabled; halted and halt_reason are settled
-- now so the executor slice needs no table rebuild.
-- key_id and username_key_id carry no foreign key on purpose: a disabled rotation
-- is history and must outlive a deleted key, and the executor's gate re-checks
-- that both keys still exist and are secret-classified before every transition.
-- Both keys are required: the generated password and the generated role name are
-- delivered together, so a consumer always reads a matching pair.
CREATE TABLE rotations (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    username_key_id TEXT NOT NULL,
    interval_seconds BIGINT NOT NULL CHECK (interval_seconds > 0),
    revocation_delay_seconds BIGINT NOT NULL CHECK (revocation_delay_seconds > 0),
    authority_principal_id TEXT NOT NULL,
    created_by TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'paused', 'halted', 'disabled')),
    halt_reason TEXT CHECK (halt_reason IS NULL OR length(halt_reason) <= 200),
    next_run_at TIMESTAMPTZ,
    generation BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, project_id, environment_id, id),
    CHECK ((state = 'active') = (next_run_at IS NOT NULL)),
    CHECK (halt_reason IS NULL OR state = 'halted'),
    CHECK (username_key_id <> key_id),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, provider_id) REFERENCES dynamic_providers (org_id, project_id, id),
    FOREIGN KEY (authority_principal_id) REFERENCES principals (id),
    FOREIGN KEY (created_by) REFERENCES principals (id)
);
CREATE INDEX rotations_due ON rotations (state, next_run_at);
CREATE INDEX rotations_provider ON rotations (provider_id, state);
CREATE INDEX rotations_key ON rotations (environment_id, key_id);

-- hikyo:table rotation_key_claims class=environment chain=org_id,project_id
-- One live rotation may drive a key, whichever role (password or username) the
-- key plays, so two rotations never fight over one value. The primary key is the
-- exclusion: a rotation inserts a claim for each of its two keys when it is
-- created and releases them when it is disabled, and the environment delete
-- purges them with the rotation.
CREATE TABLE rotation_key_claims (
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    rotation_id TEXT NOT NULL,
    PRIMARY KEY (environment_id, key_id),
    FOREIGN KEY (org_id, project_id, environment_id, rotation_id) REFERENCES rotations (org_id, project_id, environment_id, id)
);
CREATE INDEX rotation_key_claims_rotation ON rotation_key_claims (rotation_id);

-- hikyo:table rotation_runs class=environment chain=org_id,project_id
-- phase 'staged' is reserved: the chosen design stages and publishes in one
-- transaction, so it is never entered, but a SQLite CHECK enum cannot be
-- altered without a table rebuild. 'revoke_refused' is the named, retried
-- state of a revoke the provider refused (a role that owns objects); it is
-- non-terminal, so it keeps the one-open-run slot and blocks an environment
-- delete. published_revision is required from the
-- publish onward, so revoking the superseded credential is unreachable without
-- a recorded publish (crash-safety rule 2).
CREATE TABLE rotation_runs (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    rotation_id TEXT NOT NULL,
    generation BIGINT NOT NULL,
    run_trigger TEXT NOT NULL CHECK (run_trigger IN ('schedule', 'manual')),
    phase TEXT NOT NULL CHECK (phase IN ('minting', 'minted', 'staged', 'published', 'overlap', 'revoking', 'revoke_refused', 'done', 'quarantine', 'failed')),
    candidate_handle TEXT NOT NULL,
    superseded_handle TEXT,
    candidate_valid_until TIMESTAMPTZ,
    published_revision BIGINT,
    revocation_delay_seconds BIGINT NOT NULL CHECK (revocation_delay_seconds >= 0),
    started_at TIMESTAMPTZ NOT NULL,
    phase_at TIMESTAMPTZ NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    lease_claim_token BIGINT NOT NULL DEFAULT 0,
    attempt_count BIGINT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error TEXT CHECK (last_error IS NULL OR length(last_error) <= 200),
    UNIQUE (org_id, project_id, environment_id, id),
    CHECK (phase NOT IN ('published', 'overlap', 'revoking', 'revoke_refused', 'done') OR published_revision IS NOT NULL),
    FOREIGN KEY (org_id, project_id, environment_id, rotation_id) REFERENCES rotations (org_id, project_id, environment_id, id)
);
-- At most one non-terminal run per rotation. quarantine and failed are terminal
-- for scheduling, but an environment delete still refuses while a quarantined or
-- post-publish failed run may hold a live role (see RotationBlockingRunRotations and
-- RotationPublishedFailedRunRotations).
CREATE UNIQUE INDEX rotation_runs_open ON rotation_runs (rotation_id) WHERE phase NOT IN ('done', 'quarantine', 'failed');
CREATE INDEX rotation_runs_due ON rotation_runs (phase, next_attempt_at);
CREATE INDEX rotation_runs_rotation ON rotation_runs (rotation_id, started_at);
-- The delivered credential's expiry reads only runs that published.
CREATE INDEX rotation_runs_published ON rotation_runs (rotation_id, started_at) WHERE published_revision IS NOT NULL;

-- hikyo:table rotation_effects class=environment chain=org_id,project_id
CREATE TABLE rotation_effects (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('mint', 'publish', 'extend', 'revoke', 'drop_candidate')),
    intent_audit_id TEXT NOT NULL UNIQUE,
    outcome_audit_id TEXT UNIQUE,
    outcome TEXT CHECK (outcome IN ('success', 'failure', 'unknown')),
    created_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    CHECK ((outcome IS NULL) = (outcome_audit_id IS NULL) AND (outcome IS NULL) = (finished_at IS NULL)),
    FOREIGN KEY (org_id, project_id, environment_id, run_id) REFERENCES rotation_runs (org_id, project_id, environment_id, id)
);
CREATE INDEX rotation_effects_run ON rotation_effects (run_id);
CREATE INDEX rotation_effects_unknown ON rotation_effects (outcome) WHERE outcome = 'unknown';
