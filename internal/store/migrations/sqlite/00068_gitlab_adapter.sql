-- +goose NO TRANSACTION
-- +goose Up
-- GitLab CI/CD variables are the third compiled-in deployment adapter (#159).
-- A GitLab project is stored as destination_kind 'repository' and a group as
-- 'organization'; destination_scope is the GitLab environment_scope.
-- SQLite cannot replace a CHECK constraint in place. legacy_alter_table keeps
-- every child foreign key aimed at the replacement `adapters` table (same
-- dance as 00025 and 00054).
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

ALTER TABLE adapters RENAME TO adapters_before_gitlab;

-- Adapter egress trust for self-hosted GitLab. None of these is a secret: the
-- pin is a public-key hash and the bundle holds public certificates.
CREATE TABLE adapters (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('forgejo', 'github-actions', 'sealed-webhook', 'cloudflare', 'vault-kv', 'aws-secrets-manager', 'gitlab')),
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

INSERT INTO adapters (id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at)
SELECT id,org_id,project_id,provider,origin,credential_ciphertext,credential_set_at,credential_expires_at,authority_principal_id,state,created_at
FROM adapters_before_gitlab;

DROP TABLE adapters_before_gitlab;

CREATE UNIQUE INDEX adapters_active_origin
    ON adapters (org_id, project_id, origin)
    WHERE state <> 'tombstoned';

ALTER TABLE adapter_targets ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE adapter_targets ADD COLUMN variable_protected INTEGER NOT NULL DEFAULT 0 CHECK (variable_protected IN (0, 1));
ALTER TABLE adapter_targets ADD COLUMN variable_hidden INTEGER NOT NULL DEFAULT 0 CHECK (variable_hidden IN (0, 1));
ALTER TABLE adapter_targets ADD COLUMN variable_expand INTEGER NOT NULL DEFAULT 0 CHECK (variable_expand IN (0, 1));

-- The same key may be owned once per GitLab environment scope.
ALTER TABLE adapter_ledger ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '';
DROP INDEX adapter_ledger_active_provider_name;
CREATE UNIQUE INDEX adapter_ledger_active_provider_name
    ON adapter_ledger (provider_origin, destination_kind, repository_id, destination_id, destination_scope, surface, normalized_name)
    WHERE state <> 'released';

-- Pending ownership is separate for each GitLab environment scope.
ALTER TABLE adapter_route_move_targets ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE adapter_route_move_claims RENAME TO adapter_route_move_claims_before_gitlab;
CREATE TABLE adapter_route_move_claims (
    move_id TEXT NOT NULL, org_id TEXT NOT NULL, project_id TEXT NOT NULL, environment_id TEXT NOT NULL, target_id TEXT NOT NULL, key_id TEXT,
    provider_origin TEXT NOT NULL, destination_kind TEXT NOT NULL CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project', 'json-object', 'per-key')),
    destination_owner TEXT NOT NULL, destination_name TEXT NOT NULL, destination_environment TEXT NOT NULL DEFAULT '', destination_scope TEXT NOT NULL DEFAULT '',
    surface TEXT NOT NULL CHECK (surface IN ('secret', 'variable')), effective_name TEXT NOT NULL, normalized_name TEXT NOT NULL,
    PRIMARY KEY (move_id, target_id, surface, normalized_name),
    UNIQUE (provider_origin, destination_kind, destination_owner, destination_name, destination_environment, destination_scope, surface, normalized_name),
    FOREIGN KEY (org_id, project_id, environment_id, move_id, target_id) REFERENCES adapter_route_move_targets (org_id, project_id, environment_id, move_id, target_id),
    FOREIGN KEY (org_id, project_id, environment_id, move_id, target_id, key_id) REFERENCES adapter_route_move_keys (org_id, project_id, environment_id, move_id, target_id, key_id) ON DELETE CASCADE
);
INSERT INTO adapter_route_move_claims (move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,surface,effective_name,normalized_name)
SELECT move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,surface,effective_name,normalized_name
FROM adapter_route_move_claims_before_gitlab;
DROP TABLE adapter_route_move_claims_before_gitlab;


COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
