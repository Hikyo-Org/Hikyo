-- +goose Up
-- A CLI reauthentication handoff for a service-account credential mint (#1516)
-- binds one environment and no keys. The purpose and operation checks also gain
-- the publish, approve, reject and bypass handoffs the service already starts
-- (00074 left them out, so their inserts failed the check); the key_set check
-- now lets 'mint' hold an empty set, like 'adapter'.
CREATE TABLE cli_reauth_handoffs_new (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    code_verifier BLOB UNIQUE,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    purpose TEXT NOT NULL DEFAULT 'adapter' CHECK (purpose IN ('adapter', 'reveal', 'copy', 'publish', 'approve', 'reject', 'bypass', 'mint', 'self-config', 'developer-credential')),
    operation TEXT NOT NULL CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','value.copy-destination','approval.vote','approval.bypass','identity.credential-mint','self-config.adopt','self-config.apply','self-config.test','developer-credential.mint')),
    environment_set TEXT NOT NULL,
    key_set TEXT NOT NULL DEFAULT '',
    pkce_challenge TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    approved_windows TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(approved_windows)),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((purpose IN ('adapter', 'mint') AND key_set = '') OR (purpose NOT IN ('adapter', 'mint') AND key_set <> ''))
);

INSERT INTO cli_reauth_handoffs_new
(id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at)
SELECT id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at FROM cli_reauth_handoffs;
DROP TABLE cli_reauth_handoffs;
ALTER TABLE cli_reauth_handoffs_new RENAME TO cli_reauth_handoffs;
