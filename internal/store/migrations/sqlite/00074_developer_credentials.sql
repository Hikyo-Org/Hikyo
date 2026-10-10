-- +goose Up
-- Bounded developer delegation (#807); authentication metadata only.
-- hikyo:table developer_credentials class=authn chain=-
-- hikyo:table developer_credential_policy class=authn chain=-
CREATE TABLE developer_credentials (
 id TEXT PRIMARY KEY,
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id),
 authority_principal_id TEXT NOT NULL,
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 env_id TEXT NOT NULL,
 verifier BLOB NOT NULL UNIQUE,
 prefix_hint TEXT NOT NULL,
 parent_session_id TEXT NOT NULL,
 provider_id TEXT NOT NULL,
 oauth2_provider_id TEXT NOT NULL,
 saml_provider_id TEXT NOT NULL,
 auth_method TEXT NOT NULL,
 authority_generation BIGINT NOT NULL,
 credential_epoch BIGINT NOT NULL,
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 revoked_at TEXT,
 CHECK (expires_at > created_at)
);
CREATE INDEX developer_credentials_authority ON developer_credentials(authority_principal_id);
CREATE TABLE developer_credential_policy (
 id INTEGER PRIMARY KEY CHECK(id=1),
 max_lifetime_seconds BIGINT NOT NULL CHECK(max_lifetime_seconds BETWEEN 1 AND 28800)
);
INSERT INTO developer_credential_policy VALUES(1,28800);

CREATE TABLE cli_reauth_handoffs_new (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    code_verifier BLOB UNIQUE,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    purpose TEXT NOT NULL DEFAULT 'adapter' CHECK (purpose IN ('adapter', 'reveal', 'copy', 'self-config', 'developer-credential')),
    operation TEXT NOT NULL CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','self-config.adopt','self-config.apply','self-config.test','developer-credential.mint')),
    environment_set TEXT NOT NULL,
    key_set TEXT NOT NULL DEFAULT '',
    pkce_challenge TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    approved_windows TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(approved_windows)),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((purpose = 'adapter' AND key_set = '') OR (purpose <> 'adapter' AND key_set <> ''))
);

INSERT INTO cli_reauth_handoffs_new
(id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at)
SELECT id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at FROM cli_reauth_handoffs;
DROP TABLE cli_reauth_handoffs;
ALTER TABLE cli_reauth_handoffs_new RENAME TO cli_reauth_handoffs;
