-- +goose Up
-- Social sign-in and open registration, the additive half (#605;
-- docs/spec/social-signin.md section 2, numbered 00042 there). The sqlite
-- dialect carries the reasoning and the commentary corrections; this file
-- states what differs.
--
-- accounts.email and the new accounts.email_verified_at follow the sqlite
-- header exactly: only a verified email is a login identifier or a uniqueness
-- key, legacy 00049 values are kept unverified in canonical form (invalid ones
-- become NULL, duplicates stay), and the count of cleared values is not
-- reported, migrations being SQL only.
--
-- Postgres alters the five widened tables in place instead of rebuilding
-- them. A rebuild buys nothing here: postgres can drop and re-add a CHECK by
-- name, the new columns land in the same order the sqlite rebuild gives them
-- (appended), and an in-place ALTER keeps every foreign key that points at
-- sessions (reauth_windows, cli_reauth_handoffs) and credential_authorities
-- intact without dropping and restoring them. Behaviour matches sqlite
-- exactly: in-flight oidc_transactions are dropped, every open reauth window
-- is closed, and sessions and CLI handoffs survive. No twin table exists, so
-- no twin directive is declared (and sessions_rebuilt / credential_authorities_new
-- are already declared by 00020 and 00006).
--
-- hikyo:table registration_policies class=authn chain=-
-- hikyo:table registration_policy_domains class=authn chain=-
-- hikyo:table registration_policy_entries class=authn chain=-
-- hikyo:table registration_policy_entry_values class=authn chain=-
-- hikyo:table registration_signups class=authn chain=-
-- hikyo:table oauth2_providers class=authn chain=-
-- hikyo:table oauth2_transactions class=authn chain=-

CREATE TABLE oauth2_providers (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind = 'oauth2'),
    profile TEXT NOT NULL CHECK (profile IN ('github')),
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    client_secret BYTEA NOT NULL,
    redirect_uri TEXT NOT NULL,
    enabled BIGINT NOT NULL CHECK (enabled IN (0, 1)),          -- BIGINT as oidc_providers.enabled, so the partial index is verbatim
    dek_version BIGINT NOT NULL,
    row_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX oauth2_providers_issuer_enabled ON oauth2_providers (kind, issuer) WHERE enabled = 1;

CREATE TABLE registration_policies (
    id TEXT PRIMARY KEY,
    org_id TEXT REFERENCES orgs (id) ON DELETE CASCADE,
    authority_principal_id TEXT REFERENCES principals (id),
    landing TEXT NOT NULL CHECK (landing IN ('org-template', 'none', 'fresh-org')),
    template TEXT,
    local_enabled BOOLEAN NOT NULL,
    fresh_org_cap BIGINT,
    row_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CHECK ((org_id IS NOT NULL AND landing = 'org-template' AND template IS NOT NULL)
        OR (org_id IS NULL AND landing IN ('none', 'fresh-org') AND template IS NULL)),
    CHECK ((landing = 'fresh-org') = (fresh_org_cap IS NOT NULL)),
    CHECK (fresh_org_cap IS NULL OR fresh_org_cap > 0)
);
CREATE UNIQUE INDEX registration_policies_org ON registration_policies (org_id) WHERE org_id IS NOT NULL;
CREATE UNIQUE INDEX registration_policies_instance ON registration_policies ((org_id IS NULL)) WHERE org_id IS NULL;

CREATE TABLE registration_policy_domains (
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    domain TEXT NOT NULL CHECK (domain <> '' AND domain = lower(domain)),
    PRIMARY KEY (policy_id, domain)
);

CREATE TABLE registration_policy_entries (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES registration_policies (id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('oidc', 'oauth2')),
    provider_id TEXT NOT NULL,
    claim TEXT CHECK (claim IS NULL OR (claim <> '' AND claim <> 'email')),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (policy_id, provider_kind, provider_id)
);

CREATE TABLE registration_policy_entry_values (
    entry_id TEXT NOT NULL REFERENCES registration_policy_entries (id) ON DELETE CASCADE,
    value TEXT NOT NULL CHECK (value <> ''),
    PRIMARY KEY (entry_id, value)
);

CREATE TABLE registration_signups (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    token_verifier BYTEA NOT NULL UNIQUE,
    policy_id TEXT NOT NULL,
    signup_scope_org_id TEXT,
    credential_epoch BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX registration_signups_expiry ON registration_signups (expires_at);
CREATE INDEX registration_signups_policy ON registration_signups (policy_id);

-- credential_authorities: the established-credential kind widens. The CHECK
-- names are the ones 00037's rebuild left behind.
ALTER TABLE credential_authorities DROP CONSTRAINT credential_authorities_new_established_credential_kind_check1;
ALTER TABLE credential_authorities ADD CONSTRAINT credential_authorities_established_credential_kind_check
    CHECK (established_credential_kind IN ('password', 'oidc', 'oauth2'));

CREATE TABLE oauth2_transactions (
    id TEXT PRIMARY KEY,
    state_verifier BYTEA NOT NULL UNIQUE,
    pkce_verifier TEXT NOT NULL,
    provider_id TEXT NOT NULL REFERENCES oauth2_providers (id) ON DELETE CASCADE,
    issuer TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    purpose TEXT NOT NULL CHECK (purpose IN ('login', 'link', 'establish', 'claim')),
    intent TEXT CHECK (intent IN ('sign-in', 'sign-up')),
    signup_scope_org_id TEXT,
    binding_kind TEXT NOT NULL CHECK (binding_kind IN ('session', 'browser-cookie')),
    initiating_session_id TEXT,
    browser_binding_verifier BYTEA,
    account_id TEXT,
    authority_id TEXT REFERENCES credential_authorities (id),
    ceremony_id TEXT,
    browser BOOLEAN NOT NULL DEFAULT FALSE,
    credential_epoch BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
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

-- oidc_transactions: purge, append the three columns, and replace the
-- purpose CHECK and the two per-purpose CHECKs (00007's oidc_transactions_check1
-- for link, oidc_transactions_check2 for reauth) with the exhaustive one. The
-- binding CHECK (oidc_transactions_check) is unchanged.
DELETE FROM oidc_transactions;
ALTER TABLE oidc_transactions ADD COLUMN intent TEXT CHECK (intent IN ('sign-in', 'sign-up'));
ALTER TABLE oidc_transactions ADD COLUMN signup_scope_org_id TEXT;
ALTER TABLE oidc_transactions ADD COLUMN authority_id TEXT REFERENCES credential_authorities (id);
ALTER TABLE oidc_transactions DROP CONSTRAINT oidc_transactions_purpose_check;
ALTER TABLE oidc_transactions DROP CONSTRAINT oidc_transactions_check1;
ALTER TABLE oidc_transactions DROP CONSTRAINT oidc_transactions_check2;
ALTER TABLE oidc_transactions ADD CONSTRAINT oidc_transactions_purpose_check
    CHECK (purpose IN ('login', 'link', 'reauth', 'establish', 'claim'));
ALTER TABLE oidc_transactions ADD CONSTRAINT oidc_transactions_purpose_shape CHECK (
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
);

ALTER TABLE external_identities DROP CONSTRAINT external_identities_kind_check;
ALTER TABLE external_identities ADD CONSTRAINT external_identities_kind_check CHECK (kind IN ('oidc', 'saml', 'oauth2'));

-- sessions: close every open reauth window (release note; sqlite parity),
-- append oauth2_provider_id, and make the one-provider CHECK three-way.
DELETE FROM reauth_windows;
ALTER TABLE sessions ADD COLUMN oauth2_provider_id TEXT REFERENCES oauth2_providers (id) ON DELETE CASCADE;
ALTER TABLE sessions DROP CONSTRAINT sessions_one_federated_provider;
ALTER TABLE sessions ADD CONSTRAINT sessions_one_federated_provider
    CHECK (num_nonnulls(provider_id, saml_provider_id, oauth2_provider_id) <= 1);

ALTER TABLE grant_origins DROP CONSTRAINT grant_origins_kind_check;
ALTER TABLE grant_origins ADD CONSTRAINT grant_origins_kind_check CHECK (
    kind IN ('manual', 'break-glass', 'scim', 'structural', 'lockout-retention', 'registration')
);

ALTER TABLE orgs ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'registration'));
ALTER TABLE orgs ADD COLUMN registration_policy_id TEXT;

-- accounts.email: nullable, verified or unverified per email_verified_at (see
-- the sqlite header). The regex accepts exactly the set the sqlite GLOBs do,
-- and its explicit character classes already restrict the address to ASCII;
-- the octet/char length comparison is a second, encoding-based guard (UTF-8
-- databases). Kept legacy values are unverified, so duplicates among them stay.
ALTER TABLE accounts ALTER COLUMN email DROP NOT NULL;
ALTER TABLE accounts ALTER COLUMN email DROP DEFAULT;
UPDATE accounts
SET email = CASE
    WHEN octet_length(email) = char_length(email)
        AND octet_length(email) <= 254
        AND email ~ '^[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+(\.[A-Za-z0-9!#$%&''*+/=?^_`{|}~-]+)*@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*$'
    THEN split_part(email, '@', 1) || '@' || lower(split_part(email, '@', 2))
    END;
ALTER TABLE accounts ADD CONSTRAINT accounts_email_check CHECK (email IS NULL OR email <> '');
ALTER TABLE accounts ADD COLUMN email_verified_at TIMESTAMPTZ;
ALTER TABLE accounts ADD CONSTRAINT accounts_email_verified_check CHECK (email_verified_at IS NULL OR email IS NOT NULL);
CREATE UNIQUE INDEX accounts_email ON accounts (email) WHERE email_verified_at IS NOT NULL;
