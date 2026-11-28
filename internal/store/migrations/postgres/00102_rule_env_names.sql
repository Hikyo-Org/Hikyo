-- +goose Up
-- Environment selectors bind by NAME (#1447, member-access-rules ADR D3 and
-- the owner decision of 2026-11-26, option (c)). An environment item stops
-- naming one environment id in one project and becomes a rule-wide name,
-- matched in every project the rule covers, including environments created
-- or renamed to that name later. Name items carry no project, like the
-- folder items of an all-projects rule.
--
-- Every existing rule keeps exactly the environments it reaches today:
--
--   * A rule is CONSISTENT when every project it names holds items carrying
--     the same set of names, and each environment carrying one of those names
--     is itself one of its items there. Name matching then reaches exactly
--     what id matching reached, so the rule is converted in place: one name
--     item per distinct name. Equal name sets also keep a manager whose
--     grants cover exactly the rule's environments able to revoke it, since
--     a converted rule names every name in every project.
--   * Any other rule is SPLIT into one rule per project it names, each with
--     that project's names and key items, so each reaches exactly what the
--     old rule reached in that project. A project where the old rule reached
--     nothing (an `only` axis with no item there) gets no rule; a See rule
--     keeps its whole environment there even with no key item, because See
--     ignores keys (ADR D5). Every split is audited as rule.split with the
--     old rule id and the new rule ids.
--   * A rule that cannot be split deterministically fails closed: it keeps
--     its row, loses its environment items, reaches no environment
--     (`only` with no name) and carries a manager-facing env_fault naming
--     the reason, audited as rule.disabled. Those are environment items
--     naming a missing environment, a project outside the rule, the same
--     name twice in a project, an all-projects rule (which could not hold
--     environment items before), or a permission needing whole projects
--     (project settings, identities, adapters) that names environments,
--     which never validated and reached nothing. So do rules that did not
--     validate before the upgrade for a reason a split part or a converted
--     row would not keep (more than 500 items, among others): they reached
--     nothing, and could validate afterwards. Only a corrupted database, or
--     a row written past validation, can hold them.
--
-- Afterwards rule_items has no env_id: nothing matches environments by id.
-- env_split_from, env_split_project and env_migration are scratch columns of
-- this migration and are dropped at its end.

ALTER TABLE rules ADD COLUMN env_fault TEXT CHECK (env_fault IS NULL OR env_fault <> '');
ALTER TABLE rules ADD COLUMN env_migration TEXT;
ALTER TABLE rules ADD COLUMN env_split_from TEXT;
ALTER TABLE rules ADD COLUMN env_split_project TEXT;
ALTER TABLE rule_items ADD COLUMN env_name TEXT;
ALTER TABLE rule_items DROP CONSTRAINT rule_items_check;
ALTER TABLE rule_items DROP CONSTRAINT rule_items_project_bound;

UPDATE rule_items AS i SET env_name = e.name
FROM environments AS e
WHERE i.axis = 'env' AND e.org_id = i.org_id AND e.project_id = i.project_id AND e.id = i.env_id;

-- Fail closed: the rules that cannot be split deterministically.
UPDATE rules AS r SET env_fault = 'Disabled by the upgrade to environment names: this rule covers all projects and names environments, which it could not hold before. It reaches no environment; revoke it and create it again.'
WHERE r.project_mode = 'all'
  AND EXISTS (SELECT 1 FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'env');
UPDATE rules AS r SET env_fault = 'Disabled by the upgrade to environment names: it names an environment that no longer exists, so its reach cannot be kept exactly. It reaches no environment; revoke it and create it again.'
WHERE r.env_fault IS NULL
  AND EXISTS (SELECT 1 FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'env' AND i.env_name IS NULL);
UPDATE rules AS r SET env_fault = 'Disabled by the upgrade to environment names: it names an environment of a project the rule does not cover. It reaches no environment; revoke it and create it again.'
WHERE r.env_fault IS NULL
  AND EXISTS (SELECT 1 FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'env'
    AND NOT EXISTS (SELECT 1 FROM rule_items AS p WHERE p.rule_id = r.id AND p.axis = 'project' AND p.project_id = i.project_id));
UPDATE rules AS r SET env_fault = 'Disabled by the upgrade to environment names: it names one environment name twice in a project. It reaches no environment; revoke it and create it again.'
WHERE r.env_fault IS NULL
  AND EXISTS (SELECT 1 FROM rule_items AS a JOIN rule_items AS b
      ON b.rule_id = a.rule_id AND b.axis = 'env' AND b.project_id = a.project_id AND b.env_name = a.env_name AND b.id <> a.id
    WHERE a.rule_id = r.id AND a.axis = 'env');
-- A permission that needs whole projects (domain.ShapeProject, pinned by
-- TestRuleEnvNamesMigrationShapeProjectList) never validated with
-- environment items, so it reached nothing. Split or converted, a part
-- without items there would be a valid whole-project rule granting what the
-- old rule did not, so it fails closed before either predicate runs.
UPDATE rules AS r SET env_fault = 'Disabled by the upgrade to environment names: its permission needs whole projects but it names environments, so it reached nothing. It reaches no environment; revoke it and create it again.'
WHERE r.env_fault IS NULL
  AND r.capability IN ('manage-adapters', 'manage-identities', 'project-settings')
  AND EXISTS (SELECT 1 FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'env');
-- Rules that did not validate before this upgrade (domain.Rule.Validate,
-- and domain.CheckMachineRule for a machine's) reached nothing. A split part
-- keeps only its own project's items and could validate, granting what the
-- old rule did not, so these fail closed before either predicate runs too:
-- more than 500 items (domain.MaxRuleItems, pinned with the capability lists
-- by TestRuleEnvNamesMigrationShapeProjectList), a project named twice, an
-- item naming an empty project, environment or key id, a key or folder named
-- twice in a project, a key or folder item outside the rule's projects,
-- only-keys with no key item, Pin narrowed by keys, a whole-project
-- permission narrowed by keys, and a machine's rule over more than one
-- project. Every other rejection of Validate and CheckMachineRule holds in
-- every split part and converted row, or cannot be stored (a CHECK); the
-- handoff's conditions table maps each one.
UPDATE rules AS r SET env_fault = 'Disabled by the upgrade to environment names: it did not form a valid rule before the upgrade, so it reached nothing. It reaches no environment; revoke it and create it again.'
WHERE r.env_fault IS NULL AND r.project_mode = 'only'
  AND ((SELECT COUNT(*) FROM rule_items AS c WHERE c.rule_id = r.id) > 500
    OR EXISTS (SELECT 1 FROM rule_items AS a JOIN rule_items AS b
        ON b.rule_id = a.rule_id AND b.axis = 'project' AND b.project_id = a.project_id AND b.id <> a.id
      WHERE a.rule_id = r.id AND a.axis = 'project')
    OR EXISTS (SELECT 1 FROM rule_items AS z WHERE z.rule_id = r.id
      AND (z.project_id = '' OR z.env_id = '' OR z.key_id = ''))
    OR EXISTS (SELECT 1 FROM rule_items AS a JOIN rule_items AS b
        ON b.rule_id = a.rule_id AND b.axis = a.axis AND b.project_id = a.project_id AND b.id <> a.id
          AND ((a.axis = 'key' AND b.key_id = a.key_id) OR (a.axis = 'folder' AND b.folder_path = a.folder_path))
      WHERE a.rule_id = r.id AND a.axis IN ('key', 'folder'))
    OR EXISTS (SELECT 1 FROM rule_items AS k WHERE k.rule_id = r.id AND k.axis IN ('key', 'folder')
      AND NOT EXISTS (SELECT 1 FROM rule_items AS p
        WHERE p.rule_id = r.id AND p.axis = 'project' AND p.project_id = k.project_id))
    OR (r.key_mode = 'only' AND NOT EXISTS (SELECT 1 FROM rule_items AS k WHERE k.rule_id = r.id AND k.axis IN ('key', 'folder')))
    OR ((r.capability IN ('pin') OR r.capability IN ('manage-adapters', 'manage-identities', 'project-settings'))
      AND (r.key_mode = 'only' OR EXISTS (SELECT 1 FROM rule_items AS k WHERE k.rule_id = r.id AND k.axis IN ('key', 'folder'))))
    OR (EXISTS (SELECT 1 FROM principals AS m WHERE m.id = r.principal_id AND m.kind = 'machine')
      AND (SELECT COUNT(*) FROM rule_items AS p WHERE p.rule_id = r.id AND p.axis = 'project') > 1));

-- Inconsistent rules: some project of the rule holds an environment carrying
-- one of the rule's names that is not one of its items there.
UPDATE rules AS r SET env_migration = 'split'
WHERE r.env_fault IS NULL AND r.project_mode = 'only'
  AND EXISTS (
    SELECT 1 FROM rule_items AS p
    JOIN environments AS e ON e.org_id = r.org_id AND e.project_id = p.project_id
    WHERE p.rule_id = r.id AND p.axis = 'project'
      AND e.name IN (SELECT n.env_name FROM rule_items AS n WHERE n.rule_id = r.id AND n.axis = 'env')
      AND NOT EXISTS (SELECT 1 FROM rule_items AS x
        WHERE x.rule_id = r.id AND x.axis = 'env' AND x.project_id = p.project_id AND x.env_id = e.id));

-- Projects selecting different names: converted, each project would carry
-- names it never selected, which a manager holding only the rule's
-- environments does not cover, so the rule could no longer be revoked by
-- them. Such a rule is split per project, each part with its own names.
UPDATE rules AS r SET env_migration = 'split'
WHERE r.env_fault IS NULL AND r.project_mode = 'only' AND r.env_migration IS NULL
  AND EXISTS (
    SELECT 1 FROM rule_items AS p
    JOIN rule_items AS n ON n.rule_id = r.id AND n.axis = 'env'
    WHERE p.rule_id = r.id AND p.axis = 'project'
      AND NOT EXISTS (SELECT 1 FROM rule_items AS x
        WHERE x.rule_id = r.id AND x.axis = 'env' AND x.project_id = p.project_id AND x.env_name = n.env_name));

INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at, env_split_from, env_split_project)
SELECT 'rul_' || uuidv7(), r.principal_id, r.capability, r.org_id, 'only', r.env_mode,
  CASE WHEN r.key_mode = 'only' AND NOT EXISTS (SELECT 1 FROM rule_items AS k
      WHERE k.rule_id = r.id AND k.axis IN ('key', 'folder') AND k.project_id = p.project_id)
    THEN 'all' ELSE r.key_mode END,
  r.created_by, r.created_at, r.id, p.project_id
FROM rules AS r
JOIN rule_items AS p ON p.rule_id = r.id AND p.axis = 'project'
WHERE r.env_migration = 'split'
  AND NOT (r.env_mode = 'only' AND NOT EXISTS (SELECT 1 FROM rule_items AS x
    WHERE x.rule_id = r.id AND x.axis = 'env' AND x.project_id = p.project_id))
  AND NOT (r.key_mode = 'only' AND r.capability <> 'read' AND NOT EXISTS (SELECT 1 FROM rule_items AS k
    WHERE k.rule_id = r.id AND k.axis IN ('key', 'folder') AND k.project_id = p.project_id));

INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
SELECT 'rli_' || uuidv7(), n.id, n.org_id, 'project', n.env_split_project, NULL, NULL, NULL
FROM rules AS n WHERE n.env_split_from IS NOT NULL;
INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
SELECT 'rli_' || uuidv7(), n.id, n.org_id, 'env', NULL, i.env_name, NULL, NULL
FROM rules AS n
JOIN rule_items AS i ON i.rule_id = n.env_split_from AND i.axis = 'env' AND i.project_id = n.env_split_project
WHERE n.env_split_from IS NOT NULL;
INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
SELECT 'rli_' || uuidv7(), n.id, n.org_id, i.axis, i.project_id, NULL, i.key_id, i.folder_path
FROM rules AS n
JOIN rule_items AS i ON i.rule_id = n.env_split_from AND i.axis IN ('key', 'folder') AND i.project_id = n.env_split_project
WHERE n.env_split_from IS NOT NULL;

INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, recorded_at,
    actor_id, actor_class, actor_credential_id, authority_id, scope_class, org_id, project_id, env_id,
    object_type, object_id, outcome, correlation_id, source_ip, user_agent, origin, payload)
SELECT 'evt_' || uuidv7(), 'rule.split', 1, now(), TRUE, now(),
  NULL, 'system', NULL, NULL, 'org', r.org_id, NULL, NULL,
  'rule', r.id, 'success', NULL, NULL, NULL, 'system',
  json_build_object(
    'rule_id', r.id, 'target_principal', r.principal_id, 'capability', r.capability,
    'rule_ids', COALESCE((SELECT json_agg(n.id ORDER BY n.id) FROM rules AS n WHERE n.env_split_from = r.id), '[]'::json)
  )::text
FROM rules AS r WHERE r.env_migration = 'split';

-- env_ids and env_names keep their first 1000 entries in byte order (the
-- registry's MaxLen); env_items counts every environment item removed.
INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, recorded_at,
    actor_id, actor_class, actor_credential_id, authority_id, scope_class, org_id, project_id, env_id,
    object_type, object_id, outcome, correlation_id, source_ip, user_agent, origin, payload)
SELECT 'evt_' || uuidv7(), 'rule.disabled', 1, now(), TRUE, now(),
  NULL, 'system', NULL, NULL, 'org', r.org_id, NULL, NULL,
  'rule', r.id, 'success', NULL, NULL, NULL, 'system',
  json_build_object('rule_id', r.id, 'target_principal', r.principal_id, 'capability', r.capability, 'reason', r.env_fault,
    'env_mode', r.env_mode,
    'env_items', (SELECT COUNT(*) FROM rule_items AS i WHERE i.rule_id = r.id AND i.axis = 'env'),
    'env_ids', coalesce((SELECT json_agg(x ORDER BY x) FROM (SELECT (coalesce(i.project_id, '') || '/' || i.env_id) COLLATE "C" AS x FROM rule_items AS i
      WHERE i.rule_id = r.id AND i.axis = 'env' ORDER BY x LIMIT 1000) AS ids), '[]'::json),
    'env_names', coalesce((SELECT json_agg(x ORDER BY x) FROM (SELECT DISTINCT i.env_name COLLATE "C" AS x FROM rule_items AS i
      WHERE i.rule_id = r.id AND i.axis = 'env' AND i.env_name IS NOT NULL ORDER BY x LIMIT 1000) AS names), '[]'::json))::text
FROM rules AS r WHERE r.env_fault IS NOT NULL;

DELETE FROM rule_items WHERE rule_id IN (SELECT id FROM rules WHERE env_migration = 'split');
DELETE FROM rules WHERE env_migration = 'split';

DELETE FROM rule_items WHERE axis = 'env' AND rule_id IN (SELECT id FROM rules WHERE env_fault IS NOT NULL);
UPDATE rules SET env_mode = 'only' WHERE env_fault IS NOT NULL;

-- Consistent rules: one name item per distinct name, rule-wide.
DELETE FROM rule_items AS i
WHERE i.axis = 'env' AND i.env_id IS NOT NULL AND EXISTS (SELECT 1 FROM rule_items AS j
  WHERE j.rule_id = i.rule_id AND j.axis = 'env' AND j.env_name = i.env_name AND j.id < i.id);
UPDATE rule_items SET project_id = NULL, env_id = NULL WHERE axis = 'env';

DROP INDEX rule_items_env;
ALTER TABLE rule_items DROP COLUMN env_id;
ALTER TABLE rule_items ADD CONSTRAINT rule_items_check CHECK (
    (axis = 'project' AND project_id IS NOT NULL AND env_name IS NULL AND key_id IS NULL AND folder_path IS NULL)
    OR (axis = 'env' AND project_id IS NULL AND env_name IS NOT NULL AND key_id IS NULL AND folder_path IS NULL)
    OR (axis = 'key' AND project_id IS NOT NULL AND env_name IS NULL AND key_id IS NOT NULL AND folder_path IS NULL)
    OR (axis = 'folder' AND env_name IS NULL AND key_id IS NULL AND folder_path IS NOT NULL)
);
CREATE INDEX rule_items_env_name ON rule_items (org_id, env_name);
ALTER TABLE rules DROP COLUMN env_migration;
ALTER TABLE rules DROP COLUMN env_split_from;
ALTER TABLE rules DROP COLUMN env_split_project;
