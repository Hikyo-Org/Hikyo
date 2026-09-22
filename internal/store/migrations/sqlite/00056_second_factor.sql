-- +goose Up
-- Enforce the second factor at sign-in (#760): the enrolment gate, the login
-- challenge, and the login-2fa WebAuthn ceremony purpose.
--
-- hikyo:table login_challenges class=authn chain=-

-- A password-assured browser session minted for an unenrolled account on an
-- instance whose second_factor policy is `required` carries this flag. The
-- authorization chokepoint confines such a session to factor enrolment,
-- recovery-code generation, whoami and logout until a factor stands. The flag
-- RESTRICTS a session that has just presented a password; it does not widen it
-- (human-auth ADR § Account-security mutations).
ALTER TABLE sessions ADD COLUMN enrolment_required INTEGER NOT NULL DEFAULT 0;

-- The login challenge: a single-use, expiring authority proving the password
-- step passed for exactly one account, issued by localLogin (202) when a factor
-- stands and a browser session was requested. No session and no cookie exist
-- until a finish operation consumes it. Same discipline as the WebAuthn
-- ceremony — single-use, expiring, account-bound. It stores no credential
-- epoch: it is an ephemeral flow token, not a credential, and its epoch safety
-- is the factor presented against it, which the finish op verifies at the LIVE
-- epoch (a restore-superseded factor is refused there).
CREATE TABLE login_challenges (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    factors TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX login_challenges_account_idx ON login_challenges (account_id);

-- The login-2fa WebAuthn sub-flow (present a passkey as the second factor
-- against a live login challenge) needs its own ceremony purpose: `login`
-- already means passkey discoverable login and reusing it would alias those
-- rows. SQLite cannot ALTER a CHECK, so the table is rebuilt (the 00020
-- pattern). webauthn_ceremonies has no indexes and no rows are dropped.
CREATE TABLE webauthn_ceremonies_rebuilt (
    id TEXT PRIMARY KEY,
    challenge_verifier BLOB NOT NULL UNIQUE,
    session_data BLOB NOT NULL,
    account_id TEXT REFERENCES accounts (id),
    session_id TEXT,
    purpose TEXT NOT NULL CHECK (purpose IN ('enrol', 'login', 'login-2fa', 'reauth', 'step-up', 'account-security')),
    operation_binding TEXT,
    environment_id TEXT,
    credential_id TEXT,
    credential_epoch INTEGER NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    CHECK (purpose <> 'account-security' OR (account_id IS NOT NULL AND session_id IS NOT NULL)),
    CHECK (purpose <> 'reauth' OR operation_binding IS NOT NULL)
);

INSERT INTO webauthn_ceremonies_rebuilt (
    id, challenge_verifier, session_data, account_id, session_id, purpose,
    operation_binding, environment_id, credential_id, credential_epoch,
    expires_at, consumed_at, created_at
)
SELECT
    id, challenge_verifier, session_data, account_id, session_id, purpose,
    operation_binding, environment_id, credential_id, credential_epoch,
    expires_at, consumed_at, created_at
FROM webauthn_ceremonies;

DROP TABLE webauthn_ceremonies;

ALTER TABLE webauthn_ceremonies_rebuilt RENAME TO webauthn_ceremonies;
