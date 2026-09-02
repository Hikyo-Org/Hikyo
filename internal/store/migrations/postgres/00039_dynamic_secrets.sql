-- +goose Up
-- Dynamic secrets: leased PostgreSQL credentials (#147). Roll-forward only.
-- A provider is project-scoped standing authority (like an adapter); a lease is
-- an environment-scoped short-lived credential the provider minted. The lease
-- row is itself the durable job (per-row lease_owner/lease_expires_at +
-- monotonic lease_claim_token fence, so every node in the #146 cluster can run
-- the sweeper concurrently without a singleton scheduler lease); dynamic_effects
-- is the INTENT/OUTCOME ledger, exactly the adapter_effects shape.

-- hikyo:table dynamic_providers class=project chain=org_id,project_id
CREATE TABLE dynamic_providers (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind = 'postgres'),
    origin TEXT NOT NULL,
    tls_mode TEXT NOT NULL CHECK (tls_mode = 'verify-full'),
    grant_role TEXT NOT NULL,
    admin_credential_ciphertext BYTEA,
    credential_set_at TIMESTAMPTZ,
    authority_principal_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'tombstoned')),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, project_id, id),
    UNIQUE (org_id, project_id, origin),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id),
    FOREIGN KEY (authority_principal_id) REFERENCES principals (id)
);

-- hikyo:table dynamic_leases class=environment chain=org_id,project_id
-- The lease is the job: lease_owner/lease_expires_at are the worker's crash
-- fence, attempt_count/next_attempt_at drive retry backoff. No ciphertext
-- column exists here by design: the minted password is delivered once and never
-- stored, not even hashed. provider_handle is public metadata (the role name).
-- principal_class is stored so the worker rebuilds the exact caller identity
-- and the machine-reveal opt-in is re-checked on every renew.
CREATE TABLE dynamic_leases (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    principal_class TEXT NOT NULL,
    provider_handle TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('minting', 'active', 'renewing', 'revoking', 'revoked', 'expired', 'unknown', 'failed')),
    issued_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    max_ttl_seconds BIGINT NOT NULL CHECK (max_ttl_seconds > 0),
    last_transition_at TIMESTAMPTZ NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    lease_claim_token BIGINT NOT NULL DEFAULT 0,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, project_id, environment_id, id),
    UNIQUE (provider_id, provider_handle),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, provider_id) REFERENCES dynamic_providers (org_id, project_id, id),
    FOREIGN KEY (principal_id) REFERENCES principals (id)
);
CREATE INDEX dynamic_leases_due ON dynamic_leases (state, next_attempt_at);
CREATE INDEX dynamic_leases_provider ON dynamic_leases (provider_id, state);
CREATE INDEX dynamic_leases_expiry ON dynamic_leases (state, expires_at);

-- hikyo:table dynamic_effects class=environment chain=org_id,project_id
CREATE TABLE dynamic_effects (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    lease_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('mint', 'renew', 'revoke', 'expire')),
    intent_audit_id TEXT NOT NULL UNIQUE,
    outcome_audit_id TEXT UNIQUE,
    outcome TEXT CHECK (outcome IN ('success', 'failure', 'unknown')),
    created_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    FOREIGN KEY (org_id, project_id, environment_id, lease_id) REFERENCES dynamic_leases (org_id, project_id, environment_id, id)
);
CREATE INDEX dynamic_effects_lease ON dynamic_effects (lease_id);
CREATE INDEX dynamic_effects_unknown ON dynamic_effects (outcome) WHERE outcome = 'unknown';
