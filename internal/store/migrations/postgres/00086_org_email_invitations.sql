-- +goose Up
-- Email invitation of an existing account (#1207, docs/spec/email-invitations.md).
-- One pending row per (org, canonical address). The row is an invitation, not
-- membership: it is deliberately outside every principal footprint read. The
-- matched account is stored for the acceptance check only and is never listed.
-- hikyo:table org_email_invitations class=authn chain=-
CREATE TABLE org_email_invitations (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    email TEXT NOT NULL CHECK (email <> ''),
    template TEXT NOT NULL CHECK (template <> ''),
    inviter_principal_id TEXT NOT NULL,
    account_id TEXT REFERENCES accounts (id) ON DELETE CASCADE,
    token_verifier BYTEA NOT NULL UNIQUE,
    credential_epoch BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, email)
);
CREATE INDEX org_email_invitations_expiry ON org_email_invitations (expires_at);
CREATE INDEX org_email_invitations_account ON org_email_invitations (account_id);

-- +goose Down
DROP TABLE org_email_invitations;
