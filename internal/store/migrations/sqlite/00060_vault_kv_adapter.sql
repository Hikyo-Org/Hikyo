-- +goose NO TRANSACTION
-- +goose Up
-- Vault/OpenBao KV v2 is the third compiled-in deployment adapter (#162).
-- Only the provider discriminator widens; KV addressing reuses the repository
-- destination columns (owner = mount, name = path prefix). SQLite cannot
-- replace a CHECK constraint in place, so rebuild `adapters` exactly as 00054
-- left it. legacy_alter_table keeps every child foreign key aimed at the
-- replacement table (same dance as 00025 and 00054).
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

ALTER TABLE adapters RENAME TO adapters_before_vault_kv;

CREATE TABLE adapters (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('forgejo', 'github-actions', 'vault-kv')),
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
FROM adapters_before_vault_kv;

DROP TABLE adapters_before_vault_kv;

CREATE UNIQUE INDEX adapters_active_origin
    ON adapters (org_id, project_id, origin)
    WHERE state <> 'tombstoned';

COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
