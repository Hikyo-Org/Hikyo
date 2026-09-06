-- +goose Up
-- Owner-local runtime configuration. Values remain in normal encrypted snapshots.
-- The singleton is identity-bound; independent remote instances never share it.
-- hikyo:table self_config_binding class=instance chain=-
CREATE TABLE self_config_binding (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 owner_instance_id TEXT NOT NULL,
 adoption_key TEXT NOT NULL,
 adopted_by TEXT NOT NULL,
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 environment_id TEXT NOT NULL,
 schema_version INTEGER NOT NULL CHECK (schema_version > 0),
 generation INTEGER NOT NULL CHECK (generation >= 1),
 desired_revision INTEGER NOT NULL CHECK (desired_revision >= 1),
 desired_snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
 previous_snapshot_id TEXT NOT NULL DEFAULT '',
 incarnation TEXT NOT NULL,
 suspended INTEGER NOT NULL DEFAULT 0 CHECK(suspended IN (0,1)),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments(org_id, project_id, id)
);
-- hikyo:table self_config_jobs class=instance chain=-
CREATE TABLE self_config_jobs (
 id TEXT PRIMARY KEY,
 idempotency_key TEXT NOT NULL UNIQUE,
 confirm_restored_credentials INTEGER NOT NULL DEFAULT 0 CHECK(confirm_restored_credentials IN (0,1)),
 principal_id TEXT NOT NULL,
 snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
 revision INTEGER NOT NULL CHECK (revision >= 1),
 schema_version INTEGER NOT NULL CHECK (schema_version > 0),
 expected_generation INTEGER NOT NULL CHECK (expected_generation >= 1),
 generation INTEGER NOT NULL CHECK (generation >= 1),
 status TEXT NOT NULL CHECK(status IN ('preparing','applying','applied','aborted','partial','superseded')),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX self_config_one_open_job ON self_config_jobs((1)) WHERE status IN ('preparing','applying','partial');
CREATE INDEX self_config_jobs_generation ON self_config_jobs(generation);
-- hikyo:table self_config_nodes class=instance chain=-
CREATE TABLE self_config_nodes (
 node_id TEXT PRIMARY KEY,
 job_id TEXT NOT NULL,
 schema_version INTEGER NOT NULL,
 prepared INTEGER NOT NULL DEFAULT 0 CHECK(prepared IN (0,1)),
 active_generation INTEGER NOT NULL DEFAULT 0,
 active_revision INTEGER NOT NULL DEFAULT 0,
 incarnation TEXT NOT NULL,
 error_code TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL
);
-- Each slot references one snapshot. Three slots structurally bound retention.
-- hikyo:table self_config_retention class=instance chain=-
CREATE TABLE self_config_retention (
 slot TEXT PRIMARY KEY CHECK(slot IN ('desired','previous','candidate')),
 snapshot_id TEXT NOT NULL REFERENCES snapshots(id)
);

-- Metadata-only, keyed comparison of effective config before explicit adoption.
-- hikyo:table self_config_seed_attestations class=instance chain=-
CREATE TABLE self_config_seed_attestations (
 node_id TEXT PRIMARY KEY,
 schema_version INTEGER NOT NULL,
 fingerprint TEXT NOT NULL,
 heartbeat_at TEXT NOT NULL
);

-- Temporary encrypted node inputs, imported into the normal project at adoption.
-- hikyo:table self_config_seed_inputs class=instance chain=-
CREATE TABLE self_config_seed_inputs (
 node_id TEXT PRIMARY KEY REFERENCES self_config_seed_attestations(node_id) ON DELETE CASCADE,
 owner_instance_id TEXT NOT NULL,
 incarnation TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 dek_version INTEGER NOT NULL CHECK (dek_version > 0),
 row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0)
);

-- Preserve outstanding ceremonies while extending the closed purpose set.
CREATE TABLE cli_reauth_handoffs_new (
    id TEXT PRIMARY KEY,
    state_verifier BLOB NOT NULL UNIQUE,
    code_verifier BLOB UNIQUE,
    session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    purpose TEXT NOT NULL DEFAULT 'adapter' CHECK (purpose IN ('adapter', 'reveal', 'copy', 'self-config')),
    operation TEXT NOT NULL CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','self-config.adopt','self-config.apply','self-config.test')),
    environment_set TEXT NOT NULL,
    key_set TEXT NOT NULL DEFAULT '',
    pkce_challenge TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    approved_windows TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(approved_windows)),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    CHECK ((purpose = 'adapter' AND key_set = '') OR (purpose <> 'adapter' AND key_set <> ''))
);

INSERT INTO cli_reauth_handoffs_new
(id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at)
SELECT id,state_verifier,code_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,approved_windows,created_at,expires_at,consumed_at FROM cli_reauth_handoffs;
DROP TABLE cli_reauth_handoffs;
ALTER TABLE cli_reauth_handoffs_new RENAME TO cli_reauth_handoffs;


-- Rollout records contain only signed source aliases and secret-free receipts.
-- hikyo:table self_config_rollouts class=instance chain=-
CREATE TABLE self_config_rollouts (
 job_id TEXT PRIMARY KEY REFERENCES self_config_jobs(id),
 enrollment_id TEXT NOT NULL,
 incarnation TEXT NOT NULL,
 plan_digest TEXT NOT NULL DEFAULT '',
 command_json TEXT NOT NULL,
 response_json TEXT NOT NULL DEFAULT '',
 external_phase TEXT NOT NULL DEFAULT '' CHECK (external_phase IN ('','applied','restored')),
 sequence BIGINT NOT NULL CHECK (sequence > 0),
 row_version BIGINT NOT NULL DEFAULT 1 CHECK (row_version > 0)
);
-- hikyo:table self_config_rollout_sequences class=instance chain=-
CREATE TABLE self_config_rollout_sequences (
 enrollment_id TEXT PRIMARY KEY,
 sequence BIGINT NOT NULL CHECK (sequence > 0)
);
