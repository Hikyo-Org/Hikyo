-- +goose Up
-- Per-developer personal values (#1235, personal-values ADR). A personal value
-- attaches to (key, environment, owner) and is outside the flat value model:
-- no snapshot, revision, pending change, copy, export or adapter reads it. The
-- ciphertext is sealed under the project DEK with envelope kind 9, whose AAD
-- binds org, project, environment, key and owner. Every foreign key is
-- non-cascading, like value_entries: key, environment and account paths delete
-- these rows explicitly in their own transaction.
-- hikyo:table personal_values class=environment chain=org_id,project_id
-- hikyo:table personal_value_notices class=environment chain=org_id,project_id
-- hikyo:table key_personal_required_environments class=key chain=org_id,project_id
CREATE TABLE personal_values (
 id TEXT PRIMARY KEY,
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 environment_id TEXT NOT NULL,
 key_id TEXT NOT NULL,
 owner_principal_id TEXT NOT NULL,
 ciphertext BYTEA NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 -- When the retention sweep first saw the owner without the access
 -- predicate; cleared when it returns. 14 days later the sweep deletes the value.
 suspended_at TIMESTAMPTZ,
 UNIQUE (org_id, project_id, environment_id, key_id, owner_principal_id),
 FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
 FOREIGN KEY (org_id, project_id, key_id) REFERENCES keys (org_id, project_id, id),
 FOREIGN KEY (owner_principal_id) REFERENCES principals (id)
);
CREATE INDEX personal_values_owner ON personal_values(owner_principal_id);
-- A removal notice records why a value disappeared other than by its owner
-- deleting it. It is keyed by key NAME, so a key deleted and re-declared under
-- the same name inherits it, and hikyo run refuses the key until the owner sets
-- a new value or dismisses the notice.
CREATE TABLE personal_value_notices (
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 environment_id TEXT NOT NULL,
 key_name TEXT NOT NULL,
 owner_principal_id TEXT NOT NULL,
 cause TEXT NOT NULL CHECK (cause IN ('administrator', 'retention', 'restore', 'key_renamed', 'key_deleted', 'forbidden', 'qualification_lost')),
 created_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY (org_id, project_id, environment_id, key_name, owner_principal_id),
 FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
 FOREIGN KEY (owner_principal_id) REFERENCES principals (id)
);
CREATE INDEX personal_value_notices_owner ON personal_value_notices(owner_principal_id);
-- personal_required_in: the explicit environments where each developer must
-- hold their own personal value. Only `none` (no rows) and `explicit` exist,
-- so a later environment is never reached. Its own table rather than a third
-- `rule` in key_presence_environments, whose CHECK sqlite cannot alter.
CREATE TABLE key_personal_required_environments (
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 key_id TEXT NOT NULL,
 environment_id TEXT NOT NULL,
 PRIMARY KEY (org_id, project_id, key_id, environment_id),
 FOREIGN KEY (org_id, project_id, key_id) REFERENCES keys (org_id, project_id, id),
 FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id)
);
-- Consent to personal values is recorded per developer credential. A
-- credential minted before this column, or by a client that never showed the
-- consent, carries 0 and receives names only.
ALTER TABLE developer_credentials ADD COLUMN personal_consent INTEGER NOT NULL DEFAULT 0 CHECK (personal_consent IN (0, 1));
