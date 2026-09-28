-- Member access rules (member-access-rules ADR). The rule tables live on the
-- enumerated resolution surface for the same reason `grants` does: authorize()
-- reads them to mint a proof, so a rule write cannot be gated behind one.
-- Every writer takes the principal-row lock like every grant writer. ASCII
-- only: multibyte characters shift sqlite statement offsets.

-- The rule lookup authorize() makes when the principal's grants alone do not
-- satisfy a formula. It carries EXACTLY the gates ListGrantsForPrincipal
-- carries (active privacy state, reconciled up to the restore epoch) plus
-- kind = 'human': rules are human-only, and a machine row that somehow exists
-- is inert here rather than trusted. One row per selector item; every rule
-- has at least its project items, so the inner join drops nothing real.
-- hikyo:authn-resolution
-- name: ListRulesForPrincipal :many
SELECT r.id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.axis, i.project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- Every live rule that names one project, under the same gates as the
-- chokepoint's lookup, for the folder-move widening census (ADR D9): the
-- census must see exactly the rules authorize() would honour.
-- hikyo:authn-resolution
-- name: ListRulesForProject :many
SELECT r.id, r.principal_id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.axis, i.project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.org_id = sqlc.arg(org_id)
  AND r.id IN (SELECT pi.rule_id FROM rule_items AS pi WHERE pi.org_id = sqlc.arg(org_id) AND pi.project_id = sqlc.arg(project_id))
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- One rule with its items, ungated: revocation and pruning act on the stored
-- row whatever state its principal is in.
-- hikyo:authn-resolution
-- name: GetRule :many
SELECT r.id, r.principal_id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.id AS item_id, i.axis, i.project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.id = sqlc.arg(id)
ORDER BY i.id;

-- The deletion paths' censuses: which rules name an object about to go.
-- hikyo:authn-resolution
-- name: ListRuleIDsForEnvironment :many
SELECT DISTINCT rule_id FROM rule_items
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id) AND env_id = sqlc.arg(env_id)
ORDER BY rule_id;

-- hikyo:authn-resolution
-- name: ListRuleIDsForKey :many
SELECT DISTINCT rule_id FROM rule_items
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id) AND key_id = sqlc.arg(key_id)
ORDER BY rule_id;

-- hikyo:authn-resolution
-- name: ListRuleIDsForProject :many
SELECT DISTINCT rule_id FROM rule_items
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id)
ORDER BY rule_id;

-- hikyo:authn-resolution
-- name: ListRuleIDsForOrg :many
SELECT id FROM rules WHERE org_id = sqlc.arg(org_id) ORDER BY id;

-- hikyo:authn-resolution
-- name: InsertRule :exec
INSERT INTO rules (id, principal_id, capability, org_id, env_mode, key_mode, created_by, created_at)
VALUES (sqlc.arg(id), sqlc.arg(principal_id), sqlc.arg(capability), sqlc.arg(org_id),
  sqlc.arg(env_mode), sqlc.arg(key_mode), sqlc.arg(created_by), sqlc.arg(created_at));

-- hikyo:authn-resolution
-- name: InsertRuleItem :exec
INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path)
VALUES (sqlc.arg(id), sqlc.arg(rule_id), sqlc.arg(org_id), sqlc.arg(axis), sqlc.arg(project_id),
  sqlc.arg(env_id), sqlc.arg(key_id), sqlc.arg(folder_path));

-- hikyo:authn-resolution
-- name: DeleteRuleItem :execrows
DELETE FROM rule_items WHERE id = sqlc.arg(id) AND rule_id = sqlc.arg(rule_id);

-- hikyo:authn-resolution
-- name: DeleteRuleItems :execrows
DELETE FROM rule_items WHERE rule_id = sqlc.arg(rule_id);

-- hikyo:authn-resolution
-- name: DeleteRuleRow :execrows
DELETE FROM rules WHERE id = sqlc.arg(id) AND principal_id = sqlc.arg(principal_id);

-- Privacy erasure removes an erased principal's rules with their grants.
-- hikyo:authn-resolution
-- name: PrivacyEraseRuleItems :exec
DELETE FROM rule_items WHERE rule_id IN (SELECT rules.id FROM rules WHERE rules.principal_id = sqlc.arg(principal_id));

-- hikyo:authn-resolution
-- name: PrivacyEraseRules :exec
DELETE FROM rules WHERE principal_id = sqlc.arg(principal_id);

-- The key a key-aware authorization addresses, resolved from the database in
-- the authorizing transaction: the folder a rule matches on is never taken
-- from the caller. Scoped by the already-resolved chain.
-- hikyo:authn-resolution
-- name: ResolveKeyByID :one
SELECT id, folder_path FROM keys
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id) AND id = sqlc.arg(id);

-- hikyo:authn-resolution
-- name: ResolveKeyByName :one
SELECT id, folder_path FROM keys
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id) AND name = sqlc.arg(name);
