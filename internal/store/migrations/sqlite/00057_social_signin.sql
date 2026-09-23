-- +goose NO TRANSACTION
-- +goose Up
-- Social sign-in and open registration, the additive half (#605;
-- docs/spec/social-signin.md section 2, where it is numbered 00042: that
-- number and 00043 were consumed by MCP, so this is 00057 and the
-- registration switch is 00058). Purely additive and backward compatible:
-- new tables, nullable new columns, widened CHECKs that admit every value
-- today's writers produce, no column drops. Nothing reads the new shapes yet.
--
-- Commentary corrections (historical migrations stay untouched):
-- * 00005 "there is no self-registration, ever, and none is representable
--   here" is superseded by the registration policy below: an operator-enabled
--   policy row is now the only thing that makes self-registration reachable.
-- * 00007 "there is no email column, ever" was already superseded by 00049's
--   contact-only accounts.email. The spec's verified login email (section 2.2,
--   `accounts.email` with a partial unique index) collides with that column
--   and is NOT added here; it awaits a decision on #589. Email is still never
--   a linking key.
-- * The automatic OIDC provisioning fold of spec section 2.3 is a no-op:
--   00044 already retired the provider policy.
--
-- Directives. The new tables carry theirs below. The rebuilt tables keep the
-- directives they already have (sessions, oidc_transactions,
-- external_identities, credential_authorities, grant_origins); the rebuild
-- twins sessions_rebuilt and credential_authorities_new were declared by
-- 00020 and 00006 and a second declaration is a lint error, and the other
-- twins never outlive this file, so they need none. Postgres alters in place
-- (see its dialect), so no twin exists there to mirror.
--
-- hikyo:table registration_policies class=authn chain=-
-- hikyo:table registration_policy_domains class=authn chain=-
-- hikyo:table registration_policy_entries class=authn chain=-
-- hikyo:table registration_policy_entry_values class=authn chain=-
-- hikyo:table registration_signups class=authn chain=-
-- hikyo:table oauth2_providers class=authn chain=-
-- hikyo:table oauth2_transactions class=authn chain=-
--
-- SQLite cannot replace a CHECK in place, so five tables are recreated and
-- their rows carried across. Foreign keys are off for the run (the 00025 and
-- 00054 shape): with them on, DROP TABLE sessions would cascade into
-- cli_reauth_handoffs and silently discard live CLI handoffs. Every rebuilt
-- table keeps its current column order with the new columns appended, which
-- is the order the postgres ALTERs produce, so both engines share one shape.
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

-- OAuth2 providers precede sessions (FK) and oauth2_transactions.
CREATE TABLE oauth2_providers (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind = 'oauth2'),
    profile TEXT NOT NULL CHECK (profile IN ('github')),
    issuer TEXT NOT NULL,                                       -- canonical origin, immutable
    client_id TEXT NOT NULL,
    client_secret BLOB NOT NULL,
    redirect_uri TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    dek_version INTEGER NOT NULL,
    row_version INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX oauth2_providers_issuer_enabled ON oauth2_providers (kind, issuer) WHERE enabled = 1;

-- A sign-up always names the scope whose policy admits it: the instance, or
-- one org. One row per scope.
CREATE TABLE registration_policies (
    id TEXT PRIMARY KEY,
    org_id TEXT REFERENCES orgs (id) ON DELETE CASCADE,        -- NULL = instance scope
    authority_principal_id TEXT REFERENCES principals (id),     -- NULL only for the historical fold row
    landing TEXT NOT NULL CHECK (landing IN ('org-template', 'none', 'fresh-org')),
    template TEXT,
    local_enabled INTEGER NOT NULL CHECK (local_enabled IN (0, 1)),
    fresh_org_cap INTEGER,
    row_version INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK ((org_id IS NOT NULL AND landing = 'org-template' AND template IS NOT NULL)
        OR (org_id IS NULL AND landing IN ('none', 'fresh-org') AND template IS NULL)),
    CHECK ((landing = 'fresh-org') = (fresh_org_cap IS NOT NULL)),
    CHECK (fresh_org_cap IS NULL OR fresh_org_cap > 0)
);
CREATE UNIQUE INDEX registration_policies_org ON registration_policies (org_id) WHERE org_id IS NOT NULL;
CREATE UNIQUE INDEX registration_policies_instance ON registration_policies (landing IS NOT NULL) WHERE org_id IS NULL;

-- The local entry's domain allowlist, one row per domain (no JSON arrays: the
-- database refuses empty strings, duplicates and non-strings by shape).
CREATE TABLE registration_policy_domains (
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    domain TEXT NOT NULL CHECK (typeof(domain) = 'text' AND domain <> '' AND domain = lower(domain)),
    PRIMARY KEY (policy_id, domain)
);

CREATE TABLE registration_policy_entries (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('oidc', 'oauth2')),
    provider_id TEXT NOT NULL,                                  -- the provider row id, never the slug
    claim TEXT CHECK (claim IS NULL OR (claim <> '' AND claim <> 'email')),
    created_at TEXT NOT NULL,
    UNIQUE (policy_id, provider_kind, provider_id)
);

-- Accepted values of an entry's allowlist claim, one row per value. A row may
-- exist only for an entry with a claim; the writer refuses a claim with no
-- values and values without a claim, and a conformance test pins both.
CREATE TABLE registration_policy_entry_values (
    entry_id TEXT NOT NULL REFERENCES registration_policy_entries (id) ON DELETE CASCADE,
    value TEXT NOT NULL CHECK (typeof(value) = 'text' AND value <> ''),
    PRIMARY KEY (entry_id, value)
);

-- One live row per canonical address. Verification DELETES the row it
-- consumes (#584 d1); the reaper prunes expired rows; a request against an
-- expired row replaces it in the same transaction. policy_id is a trail
-- pointer without cascade: deleting a policy deletes its pending rows
-- explicitly, inside the delete transaction, emitting signup_expired.
CREATE TABLE registration_signups (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    token_verifier BLOB NOT NULL UNIQUE,
    policy_id TEXT NOT NULL,
    signup_scope_org_id TEXT,                                   -- NULL = instance scope
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX registration_signups_expiry ON registration_signups (expires_at);
CREATE INDEX registration_signups_policy ON registration_signups (policy_id);

-- credential_authorities (the 00037 shape) records which credential kind an
-- authority established. A recovery-issued authority only ever establishes a
-- password. Precedes oauth2_transactions and oidc_transactions (FK).
CREATE TABLE credential_authorities_new (
    id TEXT PRIMARY KEY,
    verifier BLOB NOT NULL UNIQUE,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    purpose TEXT NOT NULL CHECK (purpose IN ('establish-credential')),
    issued_by TEXT NOT NULL CHECK (issued_by IN ('bootstrap', 'credential-reset', 'break-glass', 'recovery', 'invitation')),
    established_credential_kind TEXT NOT NULL DEFAULT 'password' CHECK (established_credential_kind IN ('password', 'oidc', 'oauth2')),
    credential_epoch INTEGER NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    CHECK (issued_by <> 'recovery' OR established_credential_kind = 'password')
);
INSERT INTO credential_authorities_new
    (id, verifier, account_id, purpose, issued_by, established_credential_kind, credential_epoch, expires_at, consumed_at, created_at)
SELECT id, verifier, account_id, purpose, issued_by, established_credential_kind, credential_epoch, expires_at, consumed_at, created_at
FROM credential_authorities;
DROP TABLE credential_authorities;
ALTER TABLE credential_authorities_new RENAME TO credential_authorities;

-- oidc_transactions minus nonce (#580 d3), plus intent and signup scope
-- (#604, section 2.1) and authority_id (#582). Purposes: login | link |
-- establish | claim; reauth is refused before a row exists (#580 d6). One
-- exhaustive CHECK per purpose fixes the binding kind and the bound fields.
CREATE TABLE oauth2_transactions (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'establish', 'claim')),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BLOB,
    account_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    ceremony_id TEXT,
    browser INTEGER NOT NULL DEFAULT 0 CHECK (browser IN (0, 1)),
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((binding_kind = 'session' AND initiating_session_id IS NOT NULL AND browser_binding_verifier IS NULL)
        OR (binding_kind = 'browser-cookie' AND initiating_session_id IS NULL AND browser_binding_verifier IS NOT NULL)),
    CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie' AND intent IS NOT NULL
            AND account_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
    )
);

-- oidc_transactions: the purpose set widens to establish and claim, with
-- intent, signup scope and authority_id appended after 00033's browser bit.
-- Rows are minutes-lived and a link row may carry an empty rather than NULL
-- environment_id, which the exhaustive CHECK refuses, so in-flight
-- transactions are dropped: an interrupted OIDC sign-in simply restarts.
-- intent stays nullable here so today's login writer keeps working; 00058
-- (#607) makes it required.
DELETE FROM oidc_transactions;
CREATE TABLE oidc_transactions_new (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    nonce BLOB NOT NULL,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oidc_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'reauth', 'establish', 'claim')),
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BLOB,
    account_id TEXT,
    environment_id TEXT,
    ceremony_id TEXT,
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    browser INTEGER NOT NULL DEFAULT 0 CHECK (browser IN (0, 1)),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    CHECK ((binding_kind = 'session' AND initiating_session_id IS NOT NULL AND browser_binding_verifier IS NULL)
        OR (binding_kind = 'browser-cookie' AND initiating_session_id IS NULL AND browser_binding_verifier IS NOT NULL)),
    CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie'
            AND account_id IS NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND environment_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'reauth' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
    )
);
DROP TABLE oidc_transactions;
ALTER TABLE oidc_transactions_new RENAME TO oidc_transactions;

-- external_identities (the 00010 shape) admits the oauth2 kind.
ALTER TABLE external_identities RENAME TO external_identities_old;
CREATE TABLE external_identities (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    kind TEXT NOT NULL CHECK (kind IN ('oidc', 'saml', 'oauth2')),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    credential_epoch INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (kind, issuer, subject)
);
INSERT INTO external_identities (id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at)
SELECT id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at FROM external_identities_old;
DROP TABLE external_identities_old;

-- sessions (the 00020 shape plus 00056's enrolment_required) gains
-- oauth2_provider_id, and the one-federated-provider CHECK becomes three-way.
-- Sessions survive; every open reauth window is closed (release note), as the
-- 00020 rebuild did. CLI reauth handoffs survive: foreign keys are off.
DELETE FROM reauth_windows;
CREATE TABLE sessions_rebuilt (
    id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    verifier BLOB NOT NULL UNIQUE,
    artifact TEXT NOT NULL CHECK (artifact IN ('cli', 'browser', 'workspace')),
    session_generation INTEGER NOT NULL,
    credential_epoch INTEGER NOT NULL,
    auth_method TEXT NOT NULL,
    factors TEXT NOT NULL,
    authenticated_at TEXT NOT NULL,
    ceremony_id TEXT,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    idle_expires_at TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    source_ip TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    csrf_verifier BLOB,
    provider_id TEXT REFERENCES oidc_providers (id) ON DELETE CASCADE,
    saml_provider_id TEXT REFERENCES saml_providers (id) ON DELETE CASCADE,
    requesting_origin TEXT,
    handoff_id TEXT,
    enrolment_required INTEGER NOT NULL DEFAULT 0,
    oauth2_provider_id TEXT REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    CHECK ((provider_id IS NOT NULL) + (saml_provider_id IS NOT NULL) + (oauth2_provider_id IS NOT NULL) <= 1),
    CHECK (
        (artifact = 'workspace' AND requesting_origin IS NOT NULL AND handoff_id IS NOT NULL)
        OR (artifact <> 'workspace' AND requesting_origin IS NULL AND handoff_id IS NULL)
    )
);
INSERT INTO sessions_rebuilt
    (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at,
     ceremony_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, csrf_verifier,
     provider_id, saml_provider_id, requesting_origin, handoff_id, enrolment_required, oauth2_provider_id)
SELECT id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at,
     ceremony_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent, csrf_verifier,
     provider_id, saml_provider_id, requesting_origin, handoff_id, enrolment_required, NULL
FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_rebuilt RENAME TO sessions;
CREATE INDEX sessions_principal_idx ON sessions (principal_id);
CREATE INDEX sessions_origin_idx ON sessions (requesting_origin);

-- grant_origins admits `registration`; subject = the authority principal id.
CREATE TABLE grant_origins_new (
    id TEXT PRIMARY KEY,
    grant_id TEXT NOT NULL REFERENCES grants (id),
    kind TEXT NOT NULL CHECK (
        kind IN ('manual', 'break-glass', 'scim', 'structural', 'lockout-retention', 'registration')
    ),
    subject TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (grant_id, kind, subject)
);
INSERT INTO grant_origins_new (id, grant_id, kind, subject, created_at)
SELECT id, grant_id, kind, subject, created_at FROM grant_origins;
DROP TABLE grant_origins;
ALTER TABLE grant_origins_new RENAME TO grant_origins;
CREATE INDEX grant_origins_grant ON grant_origins (grant_id);

-- Where an org came from, and the policy that created it (#585).
ALTER TABLE orgs ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'registration'));
ALTER TABLE orgs ADD COLUMN registration_policy_id TEXT;

COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
