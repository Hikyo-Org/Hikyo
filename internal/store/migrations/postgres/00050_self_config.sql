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
 schema_version BIGINT NOT NULL CHECK (schema_version > 0),
 generation BIGINT NOT NULL CHECK (generation >= 1),
 desired_revision BIGINT NOT NULL CHECK (desired_revision >= 1),
 desired_snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
 previous_snapshot_id TEXT NOT NULL DEFAULT '',
 incarnation TEXT NOT NULL,
 suspended BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments(org_id, project_id, id)
);
-- hikyo:table self_config_jobs class=instance chain=-
CREATE TABLE self_config_jobs (
 id TEXT PRIMARY KEY,
 idempotency_key TEXT NOT NULL UNIQUE,
 confirm_restored_credentials BOOLEAN NOT NULL DEFAULT FALSE,
 principal_id TEXT NOT NULL,
 snapshot_id TEXT NOT NULL REFERENCES snapshots(id),
 revision BIGINT NOT NULL CHECK (revision >= 1),
 schema_version BIGINT NOT NULL CHECK (schema_version > 0),
 expected_generation BIGINT NOT NULL CHECK (expected_generation >= 1),
 generation BIGINT NOT NULL CHECK (generation >= 1),
 status TEXT NOT NULL CHECK(status IN ('preparing','applying','applied','aborted','partial','superseded')),
 error_code TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX self_config_one_open_job ON self_config_jobs((1)) WHERE status IN ('preparing','applying','partial');
CREATE INDEX self_config_jobs_generation ON self_config_jobs(generation);
-- hikyo:table self_config_nodes class=instance chain=-
CREATE TABLE self_config_nodes (
 node_id TEXT PRIMARY KEY,
 job_id TEXT NOT NULL,
 schema_version BIGINT NOT NULL,
 prepared BOOLEAN NOT NULL DEFAULT FALSE,
 active_generation BIGINT NOT NULL DEFAULT 0,
 active_revision BIGINT NOT NULL DEFAULT 0,
 incarnation TEXT NOT NULL,
 error_code TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL
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
 schema_version BIGINT NOT NULL,
 fingerprint TEXT NOT NULL,
 heartbeat_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_operation_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_operation_check
CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','self-config.adopt','self-config.apply','self-config.test'));
ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_purpose_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_purpose_check
CHECK (purpose IN ('adapter','reveal','copy','self-config'));

-- Temporary encrypted node inputs, imported into the normal project at adoption.
-- hikyo:table self_config_seed_inputs class=instance chain=-
CREATE TABLE self_config_seed_inputs (
 node_id TEXT PRIMARY KEY REFERENCES self_config_seed_attestations(node_id) ON DELETE CASCADE,
 owner_instance_id TEXT NOT NULL,
 incarnation TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 ciphertext BYTEA NOT NULL,
 dek_version INTEGER NOT NULL CHECK (dek_version > 0),
 row_version INTEGER NOT NULL DEFAULT 1 CHECK (row_version > 0)
);


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
