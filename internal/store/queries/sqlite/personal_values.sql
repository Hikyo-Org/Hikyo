-- Personal values (#1235, personal-values ADR). Tenant-scoped statements: the
-- reserved chain_* parameters are bound by the store's binding layer from proof
-- fields only, and owner_principal_id on the owner's own statements is bound
-- from the proof's authorized principal, never from a caller argument. There is
-- no statement that returns another principal's ciphertext: the only
-- ciphertext reads are keyed on the owner, and the holder reads return
-- metadata only.

-- name: InsertPersonalValue :exec
INSERT INTO personal_values (
    id, org_id, project_id, environment_id, key_id, owner_principal_id, ciphertext, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(chain_org_id), sqlc.arg(chain_project_id), sqlc.arg(chain_env_id),
    sqlc.arg(key_id), sqlc.arg(owner_principal_id), sqlc.arg(ciphertext), sqlc.arg(created_at), sqlc.arg(updated_at)
);

-- DeletePersonalValueForCell destroys one row: replacing a value deletes the
-- old row and inserts a new one, so no superseded ciphertext survives.
-- name: DeletePersonalValueForCell :execrows
DELETE FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND key_id = sqlc.arg(key_id)
  AND owner_principal_id = sqlc.arg(owner_principal_id);

-- name: GetPersonalValueForOwner :one
SELECT id, key_id, owner_principal_id, ciphertext, created_at, updated_at FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND key_id = sqlc.arg(key_id)
  AND owner_principal_id = sqlc.arg(owner_principal_id);

-- name: ListPersonalValuesForOwner :many
SELECT id, key_id, owner_principal_id, ciphertext, created_at, updated_at FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND owner_principal_id = sqlc.arg(owner_principal_id)
ORDER BY key_id;

-- ListPersonalValueHolders is metadata only: who holds a value for one key in
-- the environment and when it changed. It never selects ciphertext.
-- name: ListPersonalValueHolders :many
SELECT owner_principal_id, updated_at FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND key_id = sqlc.arg(key_id)
ORDER BY owner_principal_id;

-- name: CountPersonalValuesForProject :one
SELECT COUNT(*) FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id);

-- ListPersonalValueCellsForKey is the key cascade's read: which owners in
-- which environments lose a value, metadata only, so each gets a notice.
-- name: ListPersonalValueCellsForKey :many
SELECT environment_id, owner_principal_id FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND key_id = sqlc.arg(key_id)
ORDER BY environment_id, owner_principal_id;

-- name: DeletePersonalValuesForKey :execrows
DELETE FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND key_id = sqlc.arg(key_id);

-- name: DeletePersonalValuesForEnvironment :many
DELETE FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id)
RETURNING owner_principal_id;

-- name: ListPersonalValuesForReencrypt :many
SELECT id, environment_id, key_id, owner_principal_id, ciphertext FROM personal_values
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id > sqlc.arg(cursor)
ORDER BY id LIMIT sqlc.arg(page_limit);

-- name: ReencryptPersonalValue :execrows
UPDATE personal_values SET ciphertext = sqlc.arg(new_ciphertext)
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND id = sqlc.arg(id) AND ciphertext = sqlc.arg(old_ciphertext);

-- A notice records why a value disappeared. Recording one replaces any
-- earlier notice on the same name: the store deletes it first, then inserts.
-- name: DeletePersonalValueNoticeAt :execrows
DELETE FROM personal_value_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(environment_id) AND key_name = sqlc.arg(key_name)
  AND owner_principal_id = sqlc.arg(owner_principal_id);

-- name: InsertPersonalValueNotice :exec
INSERT INTO personal_value_notices (org_id, project_id, environment_id, key_name, owner_principal_id, cause, created_at)
VALUES (sqlc.arg(chain_org_id), sqlc.arg(chain_project_id), sqlc.arg(environment_id), sqlc.arg(key_name),
    sqlc.arg(owner_principal_id), sqlc.arg(cause), sqlc.arg(created_at));

-- name: DeletePersonalValueNotice :execrows
DELETE FROM personal_value_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND key_name = sqlc.arg(key_name)
  AND owner_principal_id = sqlc.arg(owner_principal_id);

-- name: ListPersonalValueNoticesForOwner :many
SELECT key_name, cause, created_at FROM personal_value_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND owner_principal_id = sqlc.arg(owner_principal_id)
ORDER BY key_name;

-- name: ListPersonalValueNoticesForKeyName :many
SELECT owner_principal_id, cause, created_at FROM personal_value_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id) AND key_name = sqlc.arg(key_name)
ORDER BY owner_principal_id;

-- name: DeletePersonalValueNoticesForEnvironment :execrows
DELETE FROM personal_value_notices
WHERE org_id = sqlc.arg(chain_org_id) AND project_id = sqlc.arg(chain_project_id)
  AND environment_id = sqlc.arg(chain_env_id);

-- hikyo:reason Losing qualification through a credential change must delete the owner's values in that same authentication transaction, across every project, before any new factor can be enrolled (personal-values ADR, Which accounts qualify); the statement is keyed on the account's own principal and copies names only.
-- hikyo:authn-resolution
-- name: RecordPersonalValueNoticesForOwner :exec
INSERT INTO personal_value_notices (org_id, project_id, environment_id, key_name, owner_principal_id, cause, created_at)
SELECT personal_values.org_id, personal_values.project_id, personal_values.environment_id, keys.name,
    personal_values.owner_principal_id, sqlc.arg(cause), sqlc.arg(created_at)
FROM personal_values JOIN keys ON keys.org_id = personal_values.org_id
    AND keys.project_id = personal_values.project_id AND keys.id = personal_values.key_id
WHERE personal_values.owner_principal_id = sqlc.arg(owner_principal_id)
ON CONFLICT (org_id, project_id, environment_id, key_name, owner_principal_id)
DO UPDATE SET cause = excluded.cause, created_at = excluded.created_at;

-- hikyo:reason Losing qualification through a credential change must delete the owner's values in that same authentication transaction, across every project, before any new factor can be enrolled (personal-values ADR, Which accounts qualify); the statement is keyed on the account's own principal and reads no ciphertext.
-- hikyo:authn-resolution
-- name: DeletePersonalValuesForOwner :execrows
DELETE FROM personal_values WHERE owner_principal_id = sqlc.arg(owner_principal_id);
