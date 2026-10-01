-- name: ListAdapterManifestKeys :many
SELECT id,name,classification FROM keys
WHERE org_id=sqlc.arg(chain_org) AND project_id=sqlc.arg(chain_project) AND id=ANY(sqlc.arg(key_ids)::text[]) ORDER BY id;
