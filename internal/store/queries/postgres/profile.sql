-- The authenticated account owns these fields. SCIM ownership is only a boolean
-- projection, never disclosure of another organisation's provisioning data.
-- hikyo:authn-resolution
-- name: GetAccountProfile :one
SELECT username, display_name, email, EXISTS(SELECT 1 FROM scim_users WHERE scim_users.account_id = accounts.id) AS managed, EXISTS(SELECT 1 FROM password_credentials WHERE password_credentials.account_id = accounts.id) AS has_password, EXISTS(SELECT 1 FROM totp_credentials WHERE totp_credentials.account_id = accounts.id AND confirmed_at IS NOT NULL) AS has_totp FROM accounts WHERE accounts.id = sqlc.arg(account_id);

-- hikyo:authn-resolution
-- name: UpdateAccountProfile :exec
UPDATE accounts SET username = sqlc.arg(username), display_name = sqlc.arg(display_name), email = sqlc.arg(email) WHERE id = sqlc.arg(account_id);
