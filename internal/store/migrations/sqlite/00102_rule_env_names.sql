-- +goose Up
-- The sqlite twin of the postgres migration. Environment selectors bind by NAME (#1447, member-access-rules ADR D3 and
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
--     name twice in a project, or an all-projects rule (which could not hold
--     environment items before). Only a corrupted database can hold them.
--
-- Afterwards rule_items has no env_id: nothing matches environments by id.
-- env_split_from, env_split_project and env_migration are scratch columns of
-- this migration and are dropped at its end.
--
-- SQLite cannot drop a constraint in place, so rule_items is rebuilt twice:
-- first into an unconstrained working copy the conversion edits, then into
-- the final table with its checks, foreign keys and indexes. Nothing
-- references rule_items.

ALTER TABLE rules ADD COLUMN env_fault TEXT CHECK (env_fault IS NULL OR env_fault <> '');
ALTER TABLE rules ADD COLUMN env_migration TEXT;
ALTER TABLE rules ADD COLUMN env_split_from TEXT;
ALTER TABLE rules ADD COLUMN env_split_project TEXT;
CREATE TABLE rule_items_work (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    axis TEXT NOT NULL,
    project_id TEXT,
    env_id TEXT,
    env_name TEXT,
    key_id TEXT,
    folder_path TEXT
);
INSERT INTO rule_items_work (id, rule_id, org_id, axis, project_id, env_id, env_name, key_id, folder_path)
SELECT id, rule_id, org_id, axis, project_id, env_id, NULL, key_id, folder_path FROM rule_items;
DROP TABLE rule_items;
ALTER TABLE rule_items_work RENAME TO rule_items;
-- The conversion below looks rule items up by rule; the rebuild dropped
-- rule_items_rule, and the second rebuild drops this index with its table.
CREATE INDEX rule_items_work_rule ON rule_items (rule_id);

UPDATE rule_items SET env_name = (SELECT e.name FROM environments AS e
  WHERE e.org_id = rule_items.org_id AND e.project_id = rule_items.project_id AND e.id = rule_items.env_id)
WHERE axis = 'env';

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

-- Consistent rules that could not be saved again once converted: a rule lists
-- one environment item per name in each project it covers, so projects x
-- (1 + names) + key items must stay within the 500 a rule carries
-- (domain.MaxRuleItems). Such a rule is split per project instead, where each
-- part lists at most what the rule listed there before.
-- A guard for rows that bypassed the item cap: with equal name sets, a saved
-- rule lists as many items by name as by id.
UPDATE rules AS r SET env_migration = 'split'
WHERE r.env_fault IS NULL AND r.project_mode = 'only' AND r.env_migration IS NULL
  AND (SELECT COUNT(*) FROM rule_items AS p WHERE p.rule_id = r.id AND p.axis = 'project')
      * (1 + (SELECT COUNT(DISTINCT n.env_name) FROM rule_items AS n WHERE n.rule_id = r.id AND n.axis = 'env'))
    + (SELECT COUNT(*) FROM rule_items AS k WHERE k.rule_id = r.id AND k.axis IN ('key', 'folder')) > 500;

INSERT INTO rules (id, principal_id, capability, org_id, project_mode, env_mode, key_mode, created_by, created_at, env_split_from, env_split_project)
SELECT 'rul_' || lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-7' || substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + abs(random()) % 4, 1) || substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))), r.principal_id, r.capability, r.org_id, 'only', r.env_mode,
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
SELECT 'rli_' || lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-7' || substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + abs(random()) % 4, 1) || substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))), n.id, n.org_id, 'project', n.env_split_project, NULL, NULL, NULL
FROM rules AS n WHERE n.env_split_from IS NOT NULL;
INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
SELECT 'rli_' || lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-7' || substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + abs(random()) % 4, 1) || substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))), n.id, n.org_id, 'env', NULL, i.env_name, NULL, NULL
FROM rules AS n
JOIN rule_items AS i ON i.rule_id = n.env_split_from AND i.axis = 'env' AND i.project_id = n.env_split_project
WHERE n.env_split_from IS NOT NULL;
INSERT INTO rule_items (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
SELECT 'rli_' || lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-7' || substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + abs(random()) % 4, 1) || substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))), n.id, n.org_id, i.axis, i.project_id, NULL, i.key_id, i.folder_path
FROM rules AS n
JOIN rule_items AS i ON i.rule_id = n.env_split_from AND i.axis IN ('key', 'folder') AND i.project_id = n.env_split_project
WHERE n.env_split_from IS NOT NULL;

INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, recorded_at,
    actor_id, actor_class, actor_credential_id, authority_id, scope_class, org_id, project_id, env_id,
    object_type, object_id, outcome, correlation_id, source_ip, user_agent, origin, payload)
SELECT 'evt_' || lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-7' || substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + abs(random()) % 4, 1) || substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))), 'rule.split', 1, strftime('%Y-%m-%dT%H:%M:%f000Z', 'now'), 1, strftime('%Y-%m-%dT%H:%M:%f000Z', 'now'),
  NULL, 'system', NULL, NULL, 'org', r.org_id, NULL, NULL,
  'rule', r.id, 'success', NULL, NULL, NULL, 'system',
  json_object(
    'rule_id', r.id, 'target_principal', r.principal_id, 'capability', r.capability,
    'rule_ids', json((SELECT json_group_array(s.id) FROM (SELECT n.id FROM rules AS n WHERE n.env_split_from = r.id ORDER BY n.id) AS s))
  )
FROM rules AS r WHERE r.env_migration = 'split';

INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, recorded_at,
    actor_id, actor_class, actor_credential_id, authority_id, scope_class, org_id, project_id, env_id,
    object_type, object_id, outcome, correlation_id, source_ip, user_agent, origin, payload)
SELECT 'evt_' || lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-7' || substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + abs(random()) % 4, 1) || substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))), 'rule.disabled', 1, strftime('%Y-%m-%dT%H:%M:%f000Z', 'now'), 1, strftime('%Y-%m-%dT%H:%M:%f000Z', 'now'),
  NULL, 'system', NULL, NULL, 'org', r.org_id, NULL, NULL,
  'rule', r.id, 'success', NULL, NULL, NULL, 'system',
  json_object('rule_id', r.id, 'target_principal', r.principal_id, 'capability', r.capability, 'reason', r.env_fault,
    'env_mode', r.env_mode,
    'env_ids', json((SELECT json_group_array(x) FROM (SELECT coalesce(i.project_id, '') || '/' || i.env_id AS x FROM rule_items AS i
      WHERE i.rule_id = r.id AND i.axis = 'env' ORDER BY x))),
    'env_names', json((SELECT json_group_array(x) FROM (SELECT DISTINCT i.env_name AS x FROM rule_items AS i
      WHERE i.rule_id = r.id AND i.axis = 'env' AND i.env_name IS NOT NULL ORDER BY x))))
FROM rules AS r WHERE r.env_fault IS NOT NULL;

DELETE FROM rule_items WHERE rule_id IN (SELECT id FROM rules WHERE env_migration = 'split');
DELETE FROM rules WHERE env_migration = 'split';

DELETE FROM rule_items WHERE axis = 'env' AND rule_id IN (SELECT id FROM rules WHERE env_fault IS NOT NULL);
UPDATE rules SET env_mode = 'only' WHERE env_fault IS NOT NULL;

-- Consistent rules: one name item per distinct name, rule-wide.
DELETE FROM rule_items
WHERE axis = 'env' AND env_id IS NOT NULL AND EXISTS (SELECT 1 FROM rule_items AS j
  WHERE j.rule_id = rule_items.rule_id AND j.axis = 'env' AND j.env_name = rule_items.env_name AND j.id < rule_items.id);
UPDATE rule_items SET project_id = NULL, env_id = NULL WHERE axis = 'env';

CREATE TABLE rule_items_new (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    axis TEXT NOT NULL CHECK (axis IN ('project', 'env', 'key', 'folder')),
    project_id TEXT,
    env_name TEXT,
    key_id TEXT,
    folder_path TEXT,
    CHECK (
        (axis = 'project' AND project_id IS NOT NULL AND env_name IS NULL AND key_id IS NULL AND folder_path IS NULL)
        OR (axis = 'env' AND project_id IS NULL AND env_name IS NOT NULL AND key_id IS NULL AND folder_path IS NULL)
        OR (axis = 'key' AND project_id IS NOT NULL AND env_name IS NULL AND key_id IS NOT NULL AND folder_path IS NULL)
        OR (axis = 'folder' AND env_name IS NULL AND key_id IS NULL AND folder_path IS NOT NULL)
    ),
    FOREIGN KEY (org_id, rule_id) REFERENCES rules (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id),
    FOREIGN KEY (org_id, project_id, key_id) REFERENCES keys (org_id, project_id, id)
);
INSERT INTO rule_items_new (id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path)
SELECT id, rule_id, org_id, axis, project_id, env_name, key_id, folder_path FROM rule_items;
DROP TABLE rule_items;
ALTER TABLE rule_items_new RENAME TO rule_items;

CREATE INDEX rule_items_rule ON rule_items (rule_id);
CREATE INDEX rule_items_env_name ON rule_items (org_id, env_name);
CREATE INDEX rule_items_key ON rule_items (key_id);
CREATE INDEX rule_items_project ON rule_items (org_id, project_id);
ALTER TABLE rules DROP COLUMN env_migration;
ALTER TABLE rules DROP COLUMN env_split_from;
ALTER TABLE rules DROP COLUMN env_split_project;
