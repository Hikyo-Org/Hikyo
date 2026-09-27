-- Private PKI (#154). The issuer, profile and certificate lifecycle is
-- hand-written dialect SQL in internal/store/pki.go; only the reencrypt walk
-- lives here, beside the other instance-credential walks.

-- Reencrypt walk: pki_issuers.encrypted_private_key (retired and revoked
-- versions hold NULL, their key destroyed, and are skipped).
-- hikyo:instance-scoped
-- name: ListPkiIssuersForReencrypt :many
SELECT id, encrypted_private_key, CAST(dek_version AS INTEGER) AS dek_version, row_version FROM pki_issuers WHERE encrypted_private_key IS NOT NULL AND dek_version IS NOT NULL AND id > ? ORDER BY id LIMIT ?;
-- hikyo:instance-scoped
-- name: ReencryptPkiIssuer :execrows
UPDATE pki_issuers SET encrypted_private_key=sqlc.arg(ct), dek_version=sqlc.arg(dek_version), row_version=row_version+1 WHERE id=sqlc.arg(id) AND row_version=sqlc.arg(row_version) AND encrypted_private_key IS NOT NULL;
