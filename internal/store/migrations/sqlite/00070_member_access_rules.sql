-- +goose Up
-- Member access rules (member-access-rules ADR, D1 to D9). The sqlite twin of
-- the postgres migration: TEXT timestamps where postgres stores TIMESTAMPTZ.
-- ASCII only, matching every other migration.
--
-- Rules live in their OWN tables, never as columns on `grants`: every
-- coverage predicate that reads `grants` (delivery holds, the lockout census,
-- the grantor bound, the stranded-reveal census, SCIM and privacy reads,
-- OrgsForPrincipal) stays blind to rules and therefore conservative. Only
-- authorize() reads rules, and only for human principals.
--
-- A rule row is ONE capability for ONE human principal inside ONE org, with a
-- `where` of projects, environments and keys. Rows are immutable: an edit is a
-- revoke plus a create, so session invalidation is the grant surface's.
-- Nothing in these tables can widen by itself: every selector item is bound to
-- an id by a RESTRICT foreign key (a deletion path that forgets to prune a rule
-- fails loudly instead of leaving an id a later object could match), and an
-- `only` axis with no items reaches nothing.

-- hikyo:table rules class=authn chain=-
-- env_mode / key_mode: 'all' reads the axis's items as exceptions, 'only' as
-- the complete list. created_by is the granting principal (the manual origin);
-- a second origin kind gets its own table when one exists.
CREATE TABLE rules (
    id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    capability TEXT NOT NULL,
    org_id TEXT NOT NULL REFERENCES orgs (id),
    env_mode TEXT NOT NULL CHECK (env_mode IN ('all', 'only')),
    key_mode TEXT NOT NULL CHECK (key_mode IN ('all', 'only')),
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (org_id, id)
);

CREATE INDEX rules_principal ON rules (principal_id);

-- hikyo:table rule_items class=authn chain=-
-- One selector item. Every item names the project it belongs to; exactly the
-- column its axis needs is set, and the composite foreign keys (MATCH SIMPLE:
-- enforced only when every column is non-null) keep each id inside the rule's
-- own org and project. folder_path is a plain string like keys.folder_path and
-- matches a key's folder exactly.
CREATE TABLE rule_items (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    org_id TEXT NOT NULL,
    axis TEXT NOT NULL CHECK (axis IN ('project', 'env', 'key', 'folder')),
    project_id TEXT NOT NULL,
    env_id TEXT,
    key_id TEXT,
    folder_path TEXT,
    CHECK (
        (axis = 'project' AND env_id IS NULL AND key_id IS NULL AND folder_path IS NULL)
        OR (axis = 'env' AND env_id IS NOT NULL AND key_id IS NULL AND folder_path IS NULL)
        OR (axis = 'key' AND env_id IS NULL AND key_id IS NOT NULL AND folder_path IS NULL)
        OR (axis = 'folder' AND env_id IS NULL AND key_id IS NULL AND folder_path IS NOT NULL)
    ),
    FOREIGN KEY (org_id, rule_id) REFERENCES rules (org_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id),
    FOREIGN KEY (org_id, project_id, env_id) REFERENCES environments (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, key_id) REFERENCES keys (org_id, project_id, id)
);

CREATE INDEX rule_items_rule ON rule_items (rule_id);
CREATE INDEX rule_items_env ON rule_items (env_id);
CREATE INDEX rule_items_key ON rule_items (key_id);
CREATE INDEX rule_items_project ON rule_items (org_id, project_id);

-- +goose Down
DROP TABLE rule_items;
DROP TABLE rules;
