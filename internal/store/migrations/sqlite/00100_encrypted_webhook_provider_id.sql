-- +goose NO TRANSACTION
-- +goose Up
-- The sealed-webhook provider id is now encrypted-webhook (#1334). Stored rows
-- are rewritten; the API still accepts the old id as a deprecated alias until
-- v2. SQLite cannot replace a CHECK constraint in place, so rebuild `adapters`
-- the same way as 00068 (legacy_alter_table keeps child foreign keys aimed at
-- the replacement table).
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

ALTER TABLE adapters RENAME TO adapters_before_encrypted_webhook_id;

CREATE TABLE adapters (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('forgejo', 'github-actions', 'encrypted-webhook', 'cloudflare', 'vault-kv', 'aws-secrets-manager', 'gitlab')),
    origin TEXT NOT NULL,
    credential_ciphertext BLOB,
    credential_set_at TEXT,
    credential_expires_at TEXT,
    authority_principal_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'moving', 'tombstoned')),
    created_at TEXT NOT NULL,
    spki_pin TEXT NOT NULL DEFAULT '',
    ca_bundle_pem TEXT NOT NULL DEFAULT '',
    allow_personal_token INTEGER NOT NULL DEFAULT 0 CHECK (allow_personal_token IN (0, 1)),
    CHECK (provider = 'gitlab' OR (spki_pin = '' AND ca_bundle_pem = '' AND allow_personal_token = 0)),
    UNIQUE (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id),
    FOREIGN KEY (authority_principal_id) REFERENCES principals (id)
);

INSERT INTO adapters (id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,allow_personal_token)
SELECT id,org_id,project_id,CASE provider WHEN 'sealed-webhook' THEN 'encrypted-webhook' ELSE provider END,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at,spki_pin,ca_bundle_pem,allow_personal_token
FROM adapters_before_encrypted_webhook_id;

DROP TABLE adapters_before_encrypted_webhook_id;

CREATE UNIQUE INDEX adapters_active_origin
    ON adapters (org_id, project_id, origin)
    WHERE state <> 'tombstoned';

COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
