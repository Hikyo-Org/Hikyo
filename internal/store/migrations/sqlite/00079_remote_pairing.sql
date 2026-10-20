-- +goose Up
-- Manually approved remote pairing. These rows resolve authentication only.
-- hikyo:table remote_pairing_invites class=authn chain=-
-- hikyo:table remote_pairing_exchanges class=authn chain=-
-- hikyo:table remote_paired_connections class=authn chain=-
-- hikyo:table remote_pairing_replays class=authn chain=-
CREATE TABLE remote_pairing_invites (
 id TEXT PRIMARY KEY,
 label TEXT NOT NULL,
 origin TEXT NOT NULL,
 spki_pin TEXT NOT NULL,
 capability_verifier BLOB NOT NULL UNIQUE,
 pake_sealed BLOB NOT NULL,
 attempts BIGINT NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 8),
 credential_epoch BIGINT NOT NULL,
 created_by TEXT NOT NULL,
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 credential_expires_at TEXT NOT NULL,
 revoked_at TEXT,
 consumed_at TEXT,
 CHECK (expires_at > created_at),
 CHECK (credential_expires_at > expires_at)
);
CREATE INDEX remote_pairing_invites_expiry ON remote_pairing_invites(expires_at);
CREATE TABLE remote_pairing_exchanges (
 id TEXT PRIMARY KEY,
 invite_id TEXT NOT NULL REFERENCES remote_pairing_invites(id) ON DELETE CASCADE,
 state_sealed BLOB NOT NULL,
 credential_epoch BIGINT NOT NULL,
 created_at TEXT NOT NULL,
 expires_at TEXT NOT NULL,
 consumed_at TEXT,
 CHECK (expires_at > created_at)
);
CREATE INDEX remote_pairing_exchanges_expiry ON remote_pairing_exchanges(expires_at);
CREATE INDEX remote_pairing_exchanges_invite ON remote_pairing_exchanges(invite_id);
CREATE TABLE remote_paired_connections (
 connection_id TEXT PRIMARY KEY REFERENCES instance_connections(id) ON DELETE CASCADE,
 public_jwk TEXT NOT NULL,
 thumbprint TEXT NOT NULL,
 origin TEXT NOT NULL
);
CREATE TABLE remote_pairing_replays (
 thumbprint TEXT NOT NULL,
 jti_verifier BLOB NOT NULL,
 expires_at TEXT NOT NULL,
 PRIMARY KEY(thumbprint,jti_verifier)
);
CREATE INDEX remote_pairing_replays_expiry ON remote_pairing_replays(expires_at);
