-- +goose Up
-- Member invitation (#568). A `manage-members` holder invites a human by
-- creating the account and minting a credential-establishment authority for
-- it, exactly as credential-reset does, with `invitation` as the recorded
-- issuer. The issuer taxonomy is a CHECK, and sqlite cannot alter a CHECK,
-- so the table is recreated and its rows carried across (the 00006 shape).
CREATE TABLE credential_authorities_new (
    id TEXT PRIMARY KEY,
    verifier BLOB NOT NULL UNIQUE,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    purpose TEXT NOT NULL CHECK (purpose IN ('establish-credential')),
    issued_by TEXT NOT NULL CHECK (issued_by IN ('bootstrap', 'credential-reset', 'break-glass', 'recovery', 'invitation')),
    established_credential_kind TEXT NOT NULL DEFAULT 'password' CHECK (established_credential_kind IN ('password')),
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
