-- +goose Up
-- The registration switch (#607; docs/spec/social-signin.md section 2.2,
-- where it is numbered 00043: MCP consumed that number, #605 reserved 00058).
-- It ships with the code that honours it: every purpose-login OIDC
-- transaction records its intent (#604 d1, d2; absent on the wire is stored
-- as sign-in), so the login arm of the exhaustive purpose CHECK gains
-- `intent IS NOT NULL`. Nothing else changes: the retired provider policy
-- fold of spec section 2.3 is a no-op since 00044 (#617).
--
-- In-flight transactions are deleted, not backfilled: rows are minutes-lived,
-- and a login row written by the previous binary carries a NULL intent the new
-- CHECK refuses. An interrupted OIDC sign-in simply restarts.
--
-- SQLite cannot replace a CHECK in place, so the table is recreated with the
-- 00057 column order and carried across (empty after the purge). Nothing
-- references oidc_transactions, so the rebuild needs no foreign-key pragma.
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
        (purpose = 'login' AND binding_kind = 'browser-cookie' AND intent IS NOT NULL
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
INSERT INTO oidc_transactions_new
    (id, state_verifier, nonce, pkce_verifier, provider_id, issuer, redirect_uri, purpose, binding_kind,
     initiating_session_id, browser_binding_verifier, account_id, environment_id, ceremony_id, credential_epoch,
     created_at, expires_at, consumed_at, browser, intent, signup_scope_org_id, authority_id)
SELECT id, state_verifier, nonce, pkce_verifier, provider_id, issuer, redirect_uri, purpose, binding_kind,
     initiating_session_id, browser_binding_verifier, account_id, environment_id, ceremony_id, credential_epoch,
     created_at, expires_at, consumed_at, browser, intent, signup_scope_org_id, authority_id
FROM oidc_transactions;
DROP TABLE oidc_transactions;
ALTER TABLE oidc_transactions_new RENAME TO oidc_transactions;
