-- Member access rules (member-access-rules ADR). The rule tables live on the
-- enumerated resolution surface for the same reason `grants` does: authorize()
-- reads them to mint a proof, so a rule write cannot be gated behind one.
-- Every writer takes the principal-row lock like every grant writer. ASCII
-- only: multibyte characters shift sqlite statement offsets.

-- The rule lookup authorize() makes when the principal's grants alone do not
-- satisfy a formula. It carries EXACTLY the gates ListGrantsForPrincipal
-- carries (active privacy state, reconciled up to the restore epoch) plus
-- kind = 'human'. Machine rules have their own lookup below, so a machine row
-- is never read as a person's. One row per selector item; every rule
-- names its projects through project items, but an all-projects rule
-- (project_mode = 'all', #1446) may hold no item at all, so the items are
-- LEFT JOINed and an item-less row reads axis ''.
-- hikyo:authn-resolution
-- name: ListRulesForPrincipal :many
SELECT r.id, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_name, '') AS TEXT) AS env_name,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND r.env_fault IS NULL
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.containment_state = 'none'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- The machine twin of ListRulesForPrincipal: the same gates with kind =
-- 'machine'. A separate query keeps every human-only reader human-only. The
-- caller applies the machine class's live rule check to every row.
-- hikyo:reason Machine access rules resolve the principal's own rule rows during authentication, before an authorization proof exists; the machine kind, privacy state, containment state and restore-epoch gates stay and the caller applies the machine class's live rule check to every row.
-- hikyo:authn-resolution
-- name: ListMachineRulesForPrincipal :many
SELECT r.id, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_name, '') AS TEXT) AS env_name,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND r.env_fault IS NULL
  AND p.kind = 'machine'
  AND p.privacy_state = 'active'
  AND p.containment_state = 'none'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- Every rule that names one project, or covers every project of the org (an
-- all-projects rule names every project), with its principal's kind, for the
-- widening censuses: the folder-move census (ADR D9) reads the human rules,
-- the environment rename, create and delete census (#1447) reads every rule,
-- a machine's unchecked by its class's live rule check (the machine widening
-- gate re-reads them through it). The census must be a SUPERSET of what
-- authorize() could ever honour, so it deliberately omits the chokepoint's
-- privacy and restore-epoch gates: a principal restricted today whose rule
-- gains on this move would otherwise regain that access later with nobody
-- having confirmed it.
-- hikyo:authn-resolution
-- name: ListRulesForProject :many
SELECT r.id, r.principal_id, p.kind AS principal_kind, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_name, '') AS TEXT) AS env_name,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.org_id = sqlc.arg(org_id)
  AND (r.project_mode = 'all'
    OR r.id IN (SELECT pi.rule_id FROM rule_items AS pi WHERE pi.org_id = sqlc.arg(org_id) AND pi.project_id = sqlc.arg(project_id)))
  AND r.env_fault IS NULL
ORDER BY r.id, i.id;

-- One rule with its items, ungated: revocation and pruning act on the stored
-- row whatever state its principal is in.
-- hikyo:authn-resolution
-- name: GetRule :many
SELECT r.id, r.principal_id, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.id, '') AS TEXT) AS item_id,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_name, '') AS TEXT) AS env_name,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.id = sqlc.arg(id)
ORDER BY i.id;

-- The deletion paths' censuses: which rules name an object about to go. An
-- environment is named by name, which outlives the environment, so its
-- deletion has none (#1447).

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
INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at)
VALUES (sqlc.arg(id), sqlc.arg(principal_id), sqlc.arg(capability), sqlc.arg(org_id),
  sqlc.arg(project_mode), sqlc.arg(env_mode), sqlc.arg(key_mode), sqlc.arg(created_by), sqlc.arg(created_at));

-- hikyo:authn-resolution
-- name: InsertRuleItem :exec
INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
VALUES (sqlc.arg(id), sqlc.arg(rule_id), sqlc.arg(org_id), sqlc.arg(axis), sqlc.arg(project_id),
  sqlc.arg(env_name), sqlc.arg(key_id), sqlc.arg(folder_path));

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

-- The rule listing (the rules half of the membership surface), ungated like
-- the grant listing: a member manager sees every stored rule in the org.
-- hikyo:authn-resolution
-- name: ListRuleLinesInOrg :many
SELECT r.id, r.principal_id, r.capability, r.project_mode, r.env_mode, r.key_mode, r.created_by, r.created_at,
  CAST(COALESCE(r.env_fault, '') AS TEXT) AS env_fault,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_name, '') AS TEXT) AS env_name,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.org_id = sqlc.arg(org_id)
ORDER BY r.id, i.id;

-- The project rule listing. A project member manager authorizes for ONE
-- project, so only that project's items are read (never a sibling project's
-- environments, keys or folders); other_items counts, without naming, the
-- items a rule holds elsewhere so the listing can say the rule spans more.
-- An all-projects rule names the project without an item: it is listed with
-- its project-less folder items, and the service decides other_projects from
-- the org's projects. Project-less items are rule-wide: a rule's environment
-- names (#1447) select in every project it covers, this one included, so
-- they are listed too; the service binds them to this project's
-- environments only.
-- hikyo:authn-resolution
-- name: ListRuleLinesInProject :many
SELECT r.id, r.principal_id, r.capability, r.project_mode, r.env_mode, r.key_mode, r.created_by, r.created_at,
  CAST(COALESCE(r.env_fault, '') AS TEXT) AS env_fault,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_name, '') AS TEXT) AS env_name,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path,
  (SELECT COUNT(*) FROM rule_items AS o WHERE o.rule_id = r.id AND o.project_id <> sqlc.arg(project_id)) AS other_items
FROM rules AS r
LEFT JOIN rule_items AS i ON i.rule_id = r.id AND i.org_id = sqlc.arg(org_id)
  AND (i.project_id = sqlc.arg(project_id) OR i.project_id IS NULL)
WHERE r.org_id = sqlc.arg(org_id)
  AND (r.project_mode = 'all'
    OR EXISTS (SELECT 1 FROM rule_items AS pi WHERE pi.rule_id = r.id AND pi.org_id = sqlc.arg(org_id) AND pi.project_id = sqlc.arg(project_id)))
ORDER BY r.id, i.id;

-- The all-projects rules of one org with their stored folder and
-- environment-name item counts: the
-- last-project deletion refusal names them, because a v1 rule response never
-- carries an empty project list, and a project creation that would list one
-- past the v1 bounds names them too.
-- hikyo:reason Project deletion and creation read the org's all-projects rules only after OpProjectDelete or OpProjectCreate authorized the caller in that org; ids and counts only, scoped to the org.
-- hikyo:authn-resolution
-- name: ListAllProjectsRuleItemCountsInOrg :many
SELECT r.id,
  (SELECT COUNT(*) FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'folder') AS folders,
  (SELECT COUNT(*) FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'env') AS names
FROM rules AS r WHERE r.org_id = sqlc.arg(org_id) AND r.project_mode = 'all' ORDER BY r.id;

-- Resolve only the addressed draft's key metadata for key-scoped Publish.
-- hikyo:authn-resolution
-- hikyo:reason authorize() resolves only the caller-owned draft key metadata within the resolved environment before minting a key-scoped Publish proof; ciphertext remains proof-gated.
-- name: ResolveRulePendingKey :one
SELECT k.id, k.folder_path
FROM pending_changes AS p
JOIN keys AS k ON k.org_id = p.org_id AND k.project_id = p.project_id AND k.id = p.key_id
WHERE p.org_id = sqlc.arg(org_id) AND p.project_id = sqlc.arg(project_id)
 AND p.environment_id = sqlc.arg(env_id) AND p.owner_id = sqlc.arg(owner_id) AND p.id = sqlc.arg(id);

-- Resolve only the pinned approval key ids before key-scoped Publish auth.
-- hikyo:authn-resolution
-- hikyo:reason authorize() resolves pinned approval key metadata within the resolved environment before minting a key-scoped proof; request details and ciphertext remain proof-gated.
-- name: ResolveRuleApprovalKeys :one
SELECT key_ids FROM approval_requests
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id)
 AND environment_id = sqlc.arg(env_id) AND id = sqlc.arg(id);

-- Metadata-only schema proofs cannot alter a key's existing value delivery.
-- hikyo:authn-resolution
-- hikyo:reason authorize() checks target-key value absence before minting an internal metadata-only schema snapshot proof; ciphertext is never selected.
-- name: RuleKeyValueCount :one
SELECT COUNT(*) FROM value_entries
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id)
 AND environment_id = sqlc.arg(env_id) AND key_id = sqlc.arg(key_id);

-- Initial drafts cannot replace another owner's work or an existing-value draft.
-- hikyo:authn-resolution
-- hikyo:reason authorize() checks only draft ownership and absent-baseline metadata before minting an initial-cell draft proof; ciphertext is never selected.
-- name: RuleKeyConflictingDraftCount :one
SELECT COUNT(*) FROM pending_changes
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id)
 AND environment_id = sqlc.arg(env_id) AND key_id = sqlc.arg(key_id)
 AND (owner_id <> sqlc.arg(owner_id) OR staged_from_entry <> '');

-- The org's current projects, ids only: what an all-projects rule covers
-- today. Authority over such a rule is proved in each of them (and in a
-- future-project witness), and a v1 rule response lists them at read time.
-- hikyo:reason A member access rule over all projects is authorized per current org project, so its create and revoke read these ids to choose the scopes to authorize, never returning them before that authorization; listings read them after it. Ids only.
-- hikyo:authn-resolution
-- name: ListRuleOrgProjectIDs :many
SELECT id FROM projects WHERE org_id = sqlc.arg(org_id) ORDER BY id;

-- The same list, row-locked for the creating transaction (FOR SHARE). Write
-- transactions are SERIALIZABLE, which already aborts one of a rule create
-- and a last-project deletion (each reads what the other writes); the locks
-- make them queue instead of failing late: a deletion takes every org project
-- FOR UPDATE (LockOrgProjectIDsForDelete) before it counts the org's
-- all-projects rules. A project create takes the same FOR UPDATE lock, so it
-- queues behind a rule create whose expansion bound it could break.
-- hikyo:reason An all-projects rule create locks the org's current project ids it is proved against; runs after rule.create authorized the caller in each of them, ids only, scoped to the org.
-- hikyo:authn-resolution
-- name: LockRuleOrgProjectIDs :many
SELECT id FROM projects WHERE org_id = sqlc.arg(org_id) ORDER BY id FOR SHARE;

-- The org's projects for a project deletion or creation. FOR UPDATE, not FOR
-- SHARE: two deletions of an org's last two projects, or two creations at an
-- all-projects rule's expansion bound, queue on each other rather than
-- relying on a serialization failure, and it conflicts with a rule create's
-- FOR SHARE.
-- hikyo:reason Project deletion and creation lock the org's project ids to keep an all-projects rule from outliving the last project or outgrowing the v1 listing bound; runs after OpProjectDelete or OpProjectCreate authorized the caller, ids only.
-- hikyo:authn-resolution
-- name: LockOrgProjectIDsForDelete :many
SELECT id FROM projects WHERE org_id = sqlc.arg(org_id) ORDER BY id FOR UPDATE;

-- The environments of an org, or of one project, by id and name: a rule's
-- environment items are names (#1447), bound to the environments carrying
-- them in each project the rule covers at every read, so an environment
-- created or renamed later is matched with no rule rewrite.
-- hikyo:reason authorize() binds the principal's own environment-name rule items to the environments of the rule's org while minting a proof, and a rule create binds the names it was sent before it authorizes the caller, returning nothing of them until the caller is proved; ids and names only, scoped to the org.
-- hikyo:authn-resolution
-- name: ListRuleEnvRefsInOrg :many
SELECT project_id, id, name FROM environments WHERE org_id = sqlc.arg(org_id) ORDER BY project_id, id;

-- hikyo:reason authorize() binds the caller's own environment-name rule items, and a rule target's selector, to the chain project's environments before it decides whether to mint a proof, so it runs even when authorization is then refused and returns nothing beyond that decision; the rule listing and the widening censuses bind rule items to one project's environments after the caller was authorized in that project. Ids and names only, scoped to the org and project.
-- hikyo:authn-resolution
-- name: ListRuleEnvRefsInProject :many
SELECT project_id, id, name FROM environments WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id) ORDER BY id;

-- Historical recovery of source schema 99 (#1447): rules.project_mode exists
-- but environment items name one environment id in one project and there is
-- no rules.env_fault. The caller reads every such item as an unknown axis, so
-- a rule holding one is dropped (fails closed) rather than guessed by name.
-- hikyo:reason Historical recovery of source schema 99 reads the principal's member access rules with environment items by id, which that schema stores; the human kind, privacy, containment and restore-epoch gates stay and an environment item drops its rule.
-- hikyo:authn-resolution
-- name: RecoveryListRulesBeforeEnvNames :many
SELECT r.id, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.containment_state = 'none'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- hikyo:reason Historical recovery of source schema 99 reads a machine principal's member access rules with environment items by id, which that schema stores; the machine kind, privacy, containment and restore-epoch gates stay, an environment item drops its rule and the caller applies the machine class's live rule check to every row.
-- hikyo:authn-resolution
-- name: RecoveryListMachineRulesBeforeEnvNames :many
SELECT r.id, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND p.kind = 'machine'
  AND p.privacy_state = 'active'
  AND p.containment_state = 'none'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- hikyo:reason Historical recovery of source schema 99 reads the folder-move census with environment items by id, which that schema stores; like ListRulesForProject it is deliberately ungated beyond human kind so the census stays a superset.
-- hikyo:authn-resolution
-- name: RecoveryListRulesForProjectBeforeEnvNames :many
SELECT r.id, r.principal_id, r.capability, r.org_id, r.project_mode, r.env_mode, r.key_mode,
  CAST(COALESCE(i.axis, '') AS TEXT) AS axis,
  CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
LEFT JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.org_id = sqlc.arg(org_id)
  AND (r.project_mode = 'all'
    OR r.id IN (SELECT pi.rule_id FROM rule_items AS pi WHERE pi.org_id = sqlc.arg(org_id) AND pi.project_id = sqlc.arg(project_id)))
  AND p.kind = 'human'
ORDER BY r.id, i.id;

-- Historical recovery of source schemas 89 through 98 (#1446): they hold the
-- containment columns but no rules.project_mode, and every rule_items row
-- names a project. The three readers below are ListRulesForPrincipal,
-- ListMachineRulesForPrincipal and ListRulesForProject without the column;
-- the caller reads every rule they return as project-bound ('only').
-- hikyo:reason Historical recovery of source schema 89 through 98 reads the principal's member access rules without rules.project_mode, which that schema does not have; the human kind, privacy, containment and restore-epoch gates stay.
-- hikyo:authn-resolution
-- name: RecoveryListRulesBeforeAllProjects :many
SELECT r.id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.axis, CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND p.kind = 'human'
  AND p.privacy_state = 'active'
  AND p.containment_state = 'none'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- hikyo:reason Historical recovery of source schema 89 through 98 reads a machine principal's member access rules without rules.project_mode, which that schema does not have; the machine kind, privacy, containment and restore-epoch gates stay and the caller applies the machine class's live rule check to every row.
-- hikyo:authn-resolution
-- name: RecoveryListMachineRulesBeforeAllProjects :many
SELECT r.id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.axis, CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.principal_id = sqlc.arg(principal_id)
  AND p.kind = 'machine'
  AND p.privacy_state = 'active'
  AND p.containment_state = 'none'
  AND p.reconciled_epoch >= (SELECT restore_epoch FROM auth_instance_state WHERE auth_instance_state.id = 1)
ORDER BY r.id, i.id;

-- hikyo:reason Historical recovery of source schema 89 through 98 reads the folder-move census without rules.project_mode, which that schema does not have; like ListRulesForProject it is deliberately ungated beyond human kind so the census stays a superset.
-- hikyo:authn-resolution
-- name: RecoveryListRulesForProjectBeforeAllProjects :many
SELECT r.id, r.principal_id, r.capability, r.org_id, r.env_mode, r.key_mode,
  i.axis, CAST(COALESCE(i.project_id, '') AS TEXT) AS project_id,
  CAST(COALESCE(i.env_id, '') AS TEXT) AS env_id,
  CAST(COALESCE(i.key_id, '') AS TEXT) AS key_id,
  CAST(COALESCE(i.folder_path, '') AS TEXT) AS folder_path
FROM rules AS r
JOIN principals AS p ON p.id = r.principal_id
JOIN rule_items AS i ON i.rule_id = r.id
WHERE r.org_id = sqlc.arg(org_id)
  AND r.id IN (SELECT pi.rule_id FROM rule_items AS pi WHERE pi.org_id = sqlc.arg(org_id) AND pi.project_id = sqlc.arg(project_id))
  AND p.kind = 'human'
ORDER BY r.id, i.id;
