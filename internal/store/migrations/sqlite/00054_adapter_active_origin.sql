-- +goose NO TRANSACTION
-- +goose Up
-- #744: a tombstoned adapter must not reserve its origin forever. SQLite cannot
-- drop an inline UNIQUE, so rebuild `adapters` without the unconditional
-- UNIQUE (org_id, project_id, origin) and re-add it as a partial index that
-- ignores tombstoned rows. legacy_alter_table keeps every child foreign key
-- aimed at the replacement `adapters` table (same dance as 00025).
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

ALTER TABLE adapters RENAME TO adapters_before_active_origin;

CREATE TABLE adapters (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('forgejo', 'github-actions')),
    origin TEXT NOT NULL,
    credential_ciphertext BLOB,
    credential_set_at TEXT,
    credential_expires_at TEXT,
    authority_principal_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'moving', 'tombstoned')),
    created_at TEXT NOT NULL,
    UNIQUE (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id),
    FOREIGN KEY (authority_principal_id) REFERENCES principals (id)
);

INSERT INTO adapters (id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at)
SELECT id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at
FROM adapters_before_active_origin;

DROP TABLE adapters_before_active_origin;

CREATE UNIQUE INDEX adapters_active_origin
    ON adapters (org_id, project_id, origin)
    WHERE state <> 'tombstoned';

COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
