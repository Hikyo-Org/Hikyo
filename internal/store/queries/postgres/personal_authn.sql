-- Personal values on the authentication surface (personal-values ADR).

-- hikyo:reason A restore purges every personal value with a restore notice (personal-values ADR, restore); listing the distinct owners lets that purge visit only principals holding one, and reads no ciphertext.
-- hikyo:authn-resolution
-- name: ListPersonalValueOwners :many
SELECT DISTINCT owner_principal_id FROM personal_values ORDER BY owner_principal_id;

-- hikyo:reason Erasing a privacy subject destroys every personal-value notice the subject owns, across every project, in the erasure transaction, so no key name survives for an erased owner; the statement is keyed on the subject's own principal.
-- hikyo:authn-resolution
-- name: DeletePersonalValueNoticesForOwner :exec
DELETE FROM personal_value_notices WHERE owner_principal_id = sqlc.arg(owner_principal_id);
