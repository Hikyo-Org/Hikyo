-- The Org aggregate. Creation, listing and counting are instance-scoped
-- operations (they are cross-tenant by definition: a create has no parent
-- tenant and an enumeration spans all of them); each is annotated and
-- content-pinned in the allowlist fixture (tenant-isolation ADR invariant 13).
--
-- The by-id statements are NOT annotated: an org row is its own tenant root
-- (scope class org, chain=id), so they carry the chain conjunct like any other
-- tenant statement and the binding layer takes `id` from the proof. That is
-- what makes an org nobody may reach indistinguishable from a missing one
-- (#48, mvp-boundary C1).

-- hikyo:instance-scoped
-- name: CreateOrg :exec
INSERT INTO orgs (id, name, active, metadata, created_at, origin, registration_policy_id)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetOrg :one
SELECT id, name, active, metadata, created_at,
       retention_mode, retention_age_seconds, retention_revision_count,
       origin, registration_policy_id,
       identity_hue, identity_mark, identity_image_version
FROM orgs WHERE id = sqlc.arg(chain_org_id);

-- LockOrg serializes retention-cap changes with project override changes, so
-- a concurrent pair cannot validate against two different effective caps.
-- name: LockOrg :one
SELECT id FROM orgs WHERE id = sqlc.arg(chain_org_id) FOR UPDATE;

-- hikyo:instance-scoped
-- name: ListOrgs :many
SELECT id, name, active, metadata, created_at,
       retention_mode, retention_age_seconds, retention_revision_count,
       origin, registration_policy_id,
       identity_hue, identity_mark, identity_image_version
FROM orgs WHERE (sqlc.arg(include_self_config) = 1 OR NOT EXISTS (SELECT 1 FROM self_config_binding b WHERE b.org_id=orgs.id)) ORDER BY name;

-- hikyo:reason Recovery classification reads only org ids, without later-migration columns or the self-config binding join, so it runs on any verified source schema.
-- hikyo:instance-scoped
-- name: ListOrgIDs :many
SELECT id FROM orgs ORDER BY id;

-- hikyo:instance-scoped
-- name: CountOrgs :one
SELECT COUNT(*) FROM orgs WHERE (sqlc.arg(include_self_config) = 1 OR NOT EXISTS (SELECT 1 FROM self_config_binding b WHERE b.org_id=orgs.id));

-- name: RenameOrg :execrows
UPDATE orgs SET name = sqlc.arg(name) WHERE id = sqlc.arg(chain_org_id);

-- name: SetOrgRetention :execrows
UPDATE orgs
SET retention_mode = sqlc.arg(retention_mode),
    retention_age_seconds = sqlc.arg(retention_age_seconds),
    retention_revision_count = sqlc.arg(retention_revision_count)
WHERE id = sqlc.arg(chain_org_id);

-- name: DeleteOrg :execrows
DELETE FROM orgs WHERE id = sqlc.arg(chain_org_id);

-- Chrome identity (hue, short mark, uploaded image). The image bytes are read
-- only by the dedicated image query (GetOrgIdentityImage in authn.sql), so
-- Get/List stay small.
-- name: SetOrgIdentity :execrows
UPDATE orgs SET identity_hue = sqlc.arg(identity_hue), identity_mark = sqlc.narg(identity_mark)
WHERE id = sqlc.arg(chain_org_id);

-- name: SetOrgIdentityImage :execrows
UPDATE orgs SET identity_image = sqlc.narg(identity_image),
    identity_image_type = sqlc.narg(identity_image_type),
    identity_image_version = sqlc.narg(identity_image_version)
WHERE id = sqlc.arg(chain_org_id);
