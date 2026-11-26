-- +goose Up
-- All-projects member access rules (#1446, member-access-rules ADR D3 and the
-- owner decisions of 2026-11-20 and 2026-11-25). The sqlite twin of the
-- postgres migration. A rule's project axis gains a mode: 'only' (the default,
-- every existing rule) names its projects through project items; 'all' names
-- no project item and covers every project of its org, including projects
-- created later. The mode is stored, never expanded into a project list.
--
-- An all-projects rule may narrow keys by folder path only, and a path means
-- the same in every project, so its folder items carry no project. SQLite
-- cannot drop NOT NULL in place, so rule_items is rebuilt with identical
-- columns, keys and indexes, project_id nullable for folder items only.
-- Nothing references rule_items. The composite foreign keys are MATCH SIMPLE,
-- so a NULL project_id skips them. Whether a NULL-project item belongs to an
-- all-projects rule is checked on every read (domain.Rule.Validate): a
-- mismatched row set reaches nothing.
ALTER TABLE rules ADD COLUMN project_mode TEXT NOT NULL DEFAULT 'only' CHECK (project_mode IN ('all', 'only'));

CREATE TABLE rule_items_new (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    axis TEXT NOT NULL CHECK (axis IN ('project', 'env', 'key', 'folder')),
    project_id TEXT,
    env_id TEXT,
    key_id TEXT,
    folder_path TEXT,
    CHECK (
        (axis = 'project' AND env_id IS NULL AND key_id IS NULL AND folder_path IS NULL)
        OR (axis = 'env' AND env_id IS NOT NULL AND key_id IS NULL AND folder_path IS NULL)
        OR (axis = 'key' AND env_id IS NULL AND key_id IS NOT NULL AND folder_path IS NULL)
        OR (axis = 'folder' AND env_id IS NULL AND key_id IS NULL AND folder_path IS NOT NULL)
    ),
    CHECK (project_id IS NOT NULL OR axis = 'folder'),
    FOREIGN KEY (org_id, rule_id) REFERENCES rules (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id),
    FOREIGN KEY (org_id, project_id, env_id) REFERENCES environments (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, key_id) REFERENCES keys (org_id, project_id, id)
);
INSERT INTO rule_items_new (id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path)
SELECT id, rule_id, org_id, axis, project_id, env_id, key_id, folder_path FROM rule_items;
DROP TABLE rule_items;
ALTER TABLE rule_items_new RENAME TO rule_items;

CREATE INDEX rule_items_rule ON rule_items (rule_id);
CREATE INDEX rule_items_env ON rule_items (env_id);
CREATE INDEX rule_items_key ON rule_items (key_id);
CREATE INDEX rule_items_project ON rule_items (org_id, project_id);
CREATE INDEX rules_org_project_mode ON rules (org_id, project_mode);
