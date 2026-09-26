-- +goose Up
-- Transit: versioned cryptographic operations over managed keys (#156, transit
-- ADR). Roll-forward only. See the postgres copy for the rationale. Timestamps
-- are TEXT in the canonical fixed-width microsecond form; ciphertext is BLOB.

-- hikyo:table transit_keys class=environment chain=org_id,project_id
CREATE TABLE transit_keys (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    name TEXT NOT NULL,
    algorithm TEXT NOT NULL CHECK (algorithm IN ('xchacha20-poly1305', 'ed25519', 'hmac-sha256')),
    custody TEXT NOT NULL CHECK (custody IN ('software', 'external')),
    allowed_operations TEXT NOT NULL,
    exportable INTEGER NOT NULL DEFAULT 0 CHECK (exportable = 0),
    state TEXT NOT NULL CHECK (state IN ('active', 'retired', 'disabled', 'pending-deletion', 'destroyed')),
    latest_version INTEGER NOT NULL CHECK (latest_version >= 1),
    min_encrypt_version INTEGER NOT NULL,
    min_decrypt_version INTEGER NOT NULL,
    compromised_through_version INTEGER NOT NULL DEFAULT 0,
    rotation_period_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (rotation_period_seconds = 0 OR rotation_period_seconds >= 3600),
    deletion_after TEXT,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (min_decrypt_version >= 1 AND min_decrypt_version <= min_encrypt_version AND min_encrypt_version <= latest_version),
    CHECK (compromised_through_version >= 0 AND compromised_through_version <= latest_version),
    CHECK ((state = 'pending-deletion') = (deletion_after IS NOT NULL)),
    UNIQUE (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id)
        REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX transit_keys_live_name
    ON transit_keys (org_id, project_id, environment_id, name)
    WHERE state <> 'destroyed';
CREATE INDEX transit_keys_state ON transit_keys (state);

-- hikyo:table transit_key_versions class=environment chain=org_id,project_id
CREATE TABLE transit_key_versions (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1),
    material_ciphertext BLOB,
    external_ref TEXT,
    public_key BLOB,
    created_at TEXT NOT NULL,
    CHECK (material_ciphertext IS NULL OR external_ref IS NULL),
    UNIQUE (key_id, version),
    FOREIGN KEY (org_id, project_id, environment_id, key_id)
        REFERENCES transit_keys (org_id, project_id, environment_id, id) ON DELETE CASCADE
);
CREATE INDEX transit_key_versions_project ON transit_key_versions (org_id, project_id, id);

-- hikyo:table transit_key_callers class=environment chain=org_id,project_id
CREATE TABLE transit_key_callers (
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    operations TEXT NOT NULL,
    PRIMARY KEY (key_id, principal_id),
    FOREIGN KEY (org_id, project_id, environment_id, key_id)
        REFERENCES transit_keys (org_id, project_id, environment_id, id) ON DELETE CASCADE
);
