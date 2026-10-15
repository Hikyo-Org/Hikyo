-- hikyo:reason pre-authentication login transport owns its state and code verifiers before a caller exists
-- hikyo:authn-resolution
-- name: InsertCLILogin :exec
INSERT INTO cli_login_handoffs
(id, state_verifier, code_verifier, user_code_verifier, transport, pkce_challenge, redirect_uri, created_at, expires_at, last_poll_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- hikyo:reason opaque state resolves only the first-party CLI login approval transaction
-- hikyo:authn-resolution
-- name: CLILoginByState :one
SELECT * FROM cli_login_handoffs WHERE state_verifier = ?;

-- hikyo:reason opaque authorization or device code resolves only its first-party login transaction
-- hikyo:authn-resolution
-- name: CLILoginByCode :one
SELECT * FROM cli_login_handoffs WHERE code_verifier = ?;

-- hikyo:reason rate-limited human code resolves only its browser approval transaction
-- hikyo:authn-resolution
-- name: CLILoginByUserCode :one
SELECT * FROM cli_login_handoffs WHERE user_code_verifier = ?;

-- hikyo:reason a live browser session approves only the resolved unconsumed first-party login transaction
-- hikyo:authn-resolution
-- name: ApproveCLILogin :execrows
UPDATE cli_login_handoffs SET browser_session_id = ?, snapshot = ?, code_verifier = ?
WHERE id = ? AND browser_session_id IS NULL AND consumed_at IS NULL;

-- hikyo:reason durable per-transaction device polling timestamps and intervals enforce RFC 8628 slowdown
-- hikyo:authn-resolution
-- name: PollCLILogin :exec
UPDATE cli_login_handoffs SET last_poll_at = ?, poll_interval = ? WHERE id = ?;

-- hikyo:reason atomic single-use redemption of an approved first-party login transaction gates session issuance
-- hikyo:authn-resolution
-- name: ConsumeCLILogin :execrows
UPDATE cli_login_handoffs SET consumed_at = ? WHERE id = ? AND consumed_at IS NULL AND browser_session_id IS NOT NULL;

-- hikyo:reason expired first-party login transaction metadata is removed opportunistically at issuance
-- hikyo:authn-resolution
-- name: PruneCLILogins :exec
DELETE FROM cli_login_handoffs WHERE expires_at <= ?;
