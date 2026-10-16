-- +goose Up
-- #612: first-party login transports; no provider transaction changes.
-- hikyo:table cli_login_handoffs class=authn chain=-
CREATE TABLE cli_login_handoffs (
 id TEXT PRIMARY KEY,
 state_verifier BYTEA NOT NULL UNIQUE,
 code_verifier BYTEA UNIQUE,
 user_code_verifier BYTEA UNIQUE,
 transport TEXT NOT NULL CHECK(transport IN ('loopback','device')),
 pkce_challenge TEXT NOT NULL,
 redirect_uri TEXT NOT NULL,
 browser_session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
 snapshot TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 consumed_at TIMESTAMPTZ,
 last_poll_at TIMESTAMPTZ NOT NULL,
 poll_interval BIGINT NOT NULL DEFAULT 5 CHECK(poll_interval >= 5),
 CHECK(expires_at > created_at),
 CHECK((transport = 'loopback' AND user_code_verifier IS NULL AND pkce_challenge <> '' AND redirect_uri <> '')
    OR (transport = 'device' AND user_code_verifier IS NOT NULL AND code_verifier IS NOT NULL AND pkce_challenge = '' AND redirect_uri = '')),
 CHECK((browser_session_id IS NULL AND snapshot = '') OR (browser_session_id IS NOT NULL AND snapshot <> ''))
);
ALTER TABLE sessions ADD COLUMN cli_handoff_id TEXT CHECK(cli_handoff_id IS NULL OR artifact = 'cli');

-- +goose Down
ALTER TABLE sessions DROP COLUMN cli_handoff_id;
DROP TABLE cli_login_handoffs;
