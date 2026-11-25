-- +goose Up
-- A CLI reauthentication handoff for emergency temporary access (#1531) binds
-- one environment and no keys, like 'mint'. The purpose and operation checks
-- gain 'access' and 'access.bypass'; the key_set check lets 'access' hold an
-- empty set.
CREATE TABLE cli_reauth_handoffs_new (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    code_verifier BLOB UNIQUE,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    purpose TEXT NOT NULL DEFAULT 'adapter' CHECK (purpose IN ('adapter', 'reveal', 'copy', 'publish', 'approve', 'reject', 'bypass', 'mint', 'access', 'self-config', 'developer-credential')),
    operation TEXT NOT NULL CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','value.copy-destination','approval.vote','approval.bypass','identity.credential-mint','access.bypass','self-config.adopt','self-config.apply','self-config.test','developer-credential.mint')),
    environment_set TEXT NOT NULL,
    key_set TEXT NOT NULL DEFAULT '',
    pkce_challenge TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    approved_windows TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(approved_windows)),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((purpose IN ('adapter', 'mint', 'access') AND key_set = '') OR (purpose NOT IN ('adapter', 'mint', 'access') AND key_set <> ''))
);

INSERT INTO cli_reauth_handoffs_new
(id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at)
SELECT id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at FROM cli_reauth_handoffs;
DROP TABLE cli_reauth_handoffs;
ALTER TABLE cli_reauth_handoffs_new RENAME TO cli_reauth_handoffs;
