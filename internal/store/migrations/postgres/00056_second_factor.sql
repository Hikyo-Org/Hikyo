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
ALTER TABLE sessions ADD COLUMN enrolment_required BOOLEAN NOT NULL DEFAULT FALSE;

-- The login challenge: a single-use, expiring authority proving the password
-- step passed for exactly one account, issued by localLogin (202) when a factor
-- stands and a browser session was requested. No session and no cookie exist
-- until a finish operation consumes it. The id is a high-entropy prefixed
-- UUIDv7 (74 random bits) named in the finish URL — it grants no authority on
-- its own: the second factor is still required, so it is a continuation handle,
-- not a bearer secret. Single-use, expiring, account-bound. It stores no
-- credential epoch: it is an ephemeral flow token, not a credential, and its
-- epoch safety is the factor presented against it, which the finish op verifies
-- at the LIVE epoch (a restore-superseded factor is refused there).
CREATE TABLE login_challenges (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts (id),
    factors TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX login_challenges_account_idx ON login_challenges (account_id);

-- The login-2fa WebAuthn sub-flow (present a passkey as the second factor
-- against a live login challenge) needs its own ceremony purpose: `login`
-- already means passkey discoverable login and reusing it would alias those
-- rows, blurring the audit line and the purpose check.
ALTER TABLE webauthn_ceremonies DROP CONSTRAINT webauthn_ceremonies_purpose_check;
ALTER TABLE webauthn_ceremonies
    ADD CONSTRAINT webauthn_ceremonies_purpose_check
    CHECK (purpose IN ('enrol', 'login', 'login-2fa', 'reauth', 'step-up', 'account-security'));
