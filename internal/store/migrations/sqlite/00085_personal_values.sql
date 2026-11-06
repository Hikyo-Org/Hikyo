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
CREATE TABLE personal_values (
 id TEXT PRIMARY KEY,
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 environment_id TEXT NOT NULL,
 key_id TEXT NOT NULL,
 owner_principal_id TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
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
 created_at TEXT NOT NULL,
 PRIMARY KEY (org_id, project_id, environment_id, key_name, owner_principal_id),
 FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
 FOREIGN KEY (owner_principal_id) REFERENCES principals (id)
);
CREATE INDEX personal_value_notices_owner ON personal_value_notices(owner_principal_id);
-- Consent to personal values is recorded per developer credential. A
-- credential minted before this column, or by a client that never showed the
-- consent, carries 0 and receives names only.
ALTER TABLE developer_credentials ADD COLUMN personal_consent INTEGER NOT NULL DEFAULT 0 CHECK (personal_consent IN (0, 1));
