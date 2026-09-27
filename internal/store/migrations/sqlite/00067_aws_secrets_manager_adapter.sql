-- +goose NO TRANSACTION
-- +goose Up
-- AWS Secrets Manager is the first cloud secret-manager deployment adapter
-- (#158). It adds the provider identity and two destination kinds:
-- json-object (one secret holding a JSON object) and per-key (one secret per
-- key). SQLite cannot widen a CHECK in place, so each table whose CHECK names
-- the closed sets is rebuilt with identical columns, keys, and indexes.
-- legacy_alter_table keeps every child foreign key aimed at the replacement
-- table by name (same procedure as 00025 and 00054). No row changes.
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

ALTER TABLE adapters RENAME TO adapters_before_aws;
CREATE TABLE adapters (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('forgejo', 'github-actions', 'sealed-webhook', 'cloudflare', 'vault-kv', 'aws-secrets-manager')),
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
FROM adapters_before_aws;
DROP TABLE adapters_before_aws;
CREATE UNIQUE INDEX adapters_active_origin
    ON adapters (org_id, project_id, origin)
    WHERE state <> 'tombstoned';

ALTER TABLE adapter_targets RENAME TO adapter_targets_before_aws;
CREATE TABLE adapter_targets (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    adapter_id TEXT NOT NULL,
    destination_kind TEXT NOT NULL CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project', 'json-object', 'per-key')),
    destination_owner TEXT NOT NULL,
    destination_name TEXT NOT NULL,
    destination_environment TEXT NOT NULL DEFAULT '',
    destination_id INTEGER NOT NULL CHECK (destination_id > 0),
    repository_id INTEGER NOT NULL DEFAULT 0 CHECK (repository_id >= 0),
    visibility TEXT NOT NULL DEFAULT '' CHECK (visibility IN ('', 'all', 'private', 'selected')),
    selected_repository_ids TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(selected_repository_ids)),
    name_prefix TEXT NOT NULL,
    generation INTEGER NOT NULL CHECK (generation > 0),
    state TEXT NOT NULL CHECK (state IN ('active', 'moving', 'tombstoned')),
    sync_status TEXT NOT NULL CHECK (sync_status IN ('never', 'converging', 'converged', 'failed')),
    failure_names TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(failure_names)),
    warnings TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(warnings)),
    converged_revision INTEGER,
    active_job_id TEXT,
    provider_lease_job_id TEXT,
    provider_lease_effect_id TEXT,
    provider_lease_expires_at TEXT,
    created_at TEXT NOT NULL,
    paused_at TEXT,
    last_attempted_revision INTEGER,
    last_attempted_at TEXT,
    last_error_class TEXT
        CHECK (last_error_class IS NULL OR last_error_class IN ('auth', 'network', 'conflict', 'provider_limit', 'provider_ambiguous', 'refused')),
    drift_attention INTEGER NOT NULL DEFAULT 0 CHECK (drift_attention IN (0, 1)),
    UNIQUE (org_id, project_id, id),
    UNIQUE (org_id, project_id, environment_id, id),
    UNIQUE (adapter_id, destination_kind, destination_owner, destination_name, destination_environment, environment_id),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, adapter_id) REFERENCES adapters (org_id, project_id, id)
);
INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,generation,state,sync_status,failure_names,warnings,converged_revision,active_job_id,provider_lease_job_id,provider_lease_effect_id,provider_lease_expires_at,created_at,paused_at,last_attempted_revision,last_attempted_at,last_error_class,drift_attention)
SELECT id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,generation,state,sync_status,failure_names,warnings,converged_revision,active_job_id,provider_lease_job_id,provider_lease_effect_id,provider_lease_expires_at,created_at,paused_at,last_attempted_revision,last_attempted_at,last_error_class,drift_attention
FROM adapter_targets_before_aws;
DROP TABLE adapter_targets_before_aws;

ALTER TABLE adapter_route_move_targets RENAME TO adapter_route_move_targets_before_aws;
CREATE TABLE adapter_route_move_targets (
    move_id TEXT NOT NULL, org_id TEXT NOT NULL, project_id TEXT NOT NULL, environment_id TEXT NOT NULL, target_id TEXT NOT NULL,
    destination_kind TEXT NOT NULL CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project', 'json-object', 'per-key')),
    destination_owner TEXT NOT NULL, destination_name TEXT NOT NULL, destination_environment TEXT NOT NULL DEFAULT '',
    destination_id INTEGER NOT NULL CHECK (destination_id >= 0), repository_id INTEGER NOT NULL DEFAULT 0 CHECK (repository_id >= 0),
    visibility TEXT NOT NULL DEFAULT '' CHECK (visibility IN ('', 'all', 'private', 'selected')),
    selected_repository_ids TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(selected_repository_ids)),
    name_prefix TEXT NOT NULL, orphaned_names TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(orphaned_names)),
    PRIMARY KEY (move_id, target_id), UNIQUE (org_id, project_id, environment_id, move_id, target_id),
    FOREIGN KEY (org_id, project_id, move_id) REFERENCES adapter_route_moves (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id),
    FOREIGN KEY (org_id, project_id, environment_id, target_id) REFERENCES adapter_targets (org_id, project_id, environment_id, id)
);
INSERT INTO adapter_route_move_targets (move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,orphaned_names)
SELECT move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,orphaned_names
FROM adapter_route_move_targets_before_aws;
DROP TABLE adapter_route_move_targets_before_aws;

ALTER TABLE adapter_route_move_claims RENAME TO adapter_route_move_claims_before_aws;
CREATE TABLE adapter_route_move_claims (
    move_id TEXT NOT NULL, org_id TEXT NOT NULL, project_id TEXT NOT NULL, environment_id TEXT NOT NULL, target_id TEXT NOT NULL, key_id TEXT,
    provider_origin TEXT NOT NULL, destination_kind TEXT NOT NULL CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project', 'json-object', 'per-key')),
    destination_owner TEXT NOT NULL, destination_name TEXT NOT NULL, destination_environment TEXT NOT NULL DEFAULT '',
    surface TEXT NOT NULL CHECK (surface IN ('secret', 'variable')), effective_name TEXT NOT NULL, normalized_name TEXT NOT NULL,
    PRIMARY KEY (move_id, target_id, surface, normalized_name),
    UNIQUE (provider_origin, destination_kind, destination_owner, destination_name, destination_environment, surface, normalized_name),
    FOREIGN KEY (org_id, project_id, environment_id, move_id, target_id) REFERENCES adapter_route_move_targets (org_id, project_id, environment_id, move_id, target_id),
    FOREIGN KEY (org_id, project_id, environment_id, move_id, target_id, key_id) REFERENCES adapter_route_move_keys (org_id, project_id, environment_id, move_id, target_id, key_id) ON DELETE CASCADE
);
INSERT INTO adapter_route_move_claims (move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,surface,effective_name,normalized_name)
SELECT move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,surface,effective_name,normalized_name
FROM adapter_route_move_claims_before_aws;
DROP TABLE adapter_route_move_claims_before_aws;

ALTER TABLE adapter_ledger RENAME TO adapter_ledger_before_aws;
CREATE TABLE adapter_ledger (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    target_id TEXT NOT NULL,
    provider_origin TEXT NOT NULL,
    destination_id INTEGER NOT NULL,
    surface TEXT NOT NULL CHECK (surface IN ('secret', 'variable')),
    effective_name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('reserved', 'dispatched', 'owned', 'released')),
    updated_at TEXT NOT NULL,
    destination_kind TEXT NOT NULL DEFAULT 'repository'
        CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project', 'json-object', 'per-key')),
    repository_id INTEGER NOT NULL DEFAULT 0 CHECK (repository_id >= 0),
    missing INTEGER NOT NULL DEFAULT 0 CHECK (missing IN (0, 1)),
    UNIQUE (target_id, surface, normalized_name),
    FOREIGN KEY (org_id, project_id, environment_id, target_id) REFERENCES adapter_targets (org_id, project_id, environment_id, id)
);
INSERT INTO adapter_ledger (id,org_id,project_id,environment_id,target_id,provider_origin,destination_id,surface,effective_name,normalized_name,state,updated_at,destination_kind,repository_id,missing)
SELECT id,org_id,project_id,environment_id,target_id,provider_origin,destination_id,surface,effective_name,normalized_name,state,updated_at,destination_kind,repository_id,missing
FROM adapter_ledger_before_aws;
DROP TABLE adapter_ledger_before_aws;
CREATE UNIQUE INDEX adapter_ledger_active_provider_name
    ON adapter_ledger (provider_origin, destination_kind, repository_id, destination_id, surface, normalized_name)
    WHERE state <> 'released';

COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
