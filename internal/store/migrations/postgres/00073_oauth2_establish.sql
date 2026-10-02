-- +goose Up
-- OAuth2 establishment is account-security evidence, never a reveal window.
-- hikyo:table credential_establish_evidence class=authn chain=-
CREATE TABLE credential_establish_evidence (
 session_id TEXT PRIMARY KEY REFERENCES sessions (id) ON DELETE CASCADE,
 identity_id TEXT NOT NULL REFERENCES external_identities (id) ON DELETE CASCADE,
 purpose TEXT NOT NULL CHECK (purpose = 'account-security'),
 expires_at TIMESTAMPTZ NOT NULL
);
