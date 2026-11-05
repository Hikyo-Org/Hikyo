-- +goose NO TRANSACTION
-- +goose Up
-- Transit gains the signing algorithm mldsa65 (#1129, post-quantum migration
-- DEC-02): a new, immutable key identity beside ed25519. Only the closed
-- algorithm CHECK widens; no column or row changes, and no existing key's
-- algorithm changes. SQLite cannot widen a CHECK in place, so transit_keys is
-- rebuilt with identical columns, keys and indexes. legacy_alter_table keeps
-- the child foreign keys (transit_key_versions, transit_key_callers) aimed at
-- the replacement table by name (same procedure as 00067). Roll-forward only.
PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;
BEGIN IMMEDIATE;

ALTER TABLE transit_keys RENAME TO transit_keys_before_mldsa65;
CREATE TABLE transit_keys (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    name TEXT NOT NULL,
    algorithm TEXT NOT NULL CHECK (algorithm IN ('xchacha20-poly1305', 'ed25519', 'hmac-sha256', 'mldsa65')),
    custody TEXT NOT NULL CHECK (custody IN ('software', 'external')),
    allowed_operations TEXT NOT NULL,
    exportable INTEGER NOT NULL DEFAULT 0 CHECK (exportable = 0),
    state TEXT NOT NULL CHECK (state IN ('active', 'retired', 'disabled', 'pending-deletion', 'destroyed')),
    latest_version INTEGER NOT NULL CHECK (latest_version >= 1),
    min_encrypt_version INTEGER NOT NULL,
    min_decrypt_version INTEGER NOT NULL,
    -- min_available_version is the trim floor: every version below it is
    -- trimmed or being trimmed (its external material may already be gone),
    -- so min_decrypt_version can never be lowered past it.
    min_available_version INTEGER NOT NULL DEFAULT 1,
    compromised_through_version INTEGER NOT NULL DEFAULT 0,
    rotation_period_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (rotation_period_seconds = 0 OR rotation_period_seconds >= 3600),
    purge_started INTEGER NOT NULL DEFAULT 0 CHECK (purge_started IN (0, 1)),
    deletion_after TEXT,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (min_decrypt_version >= 1 AND min_decrypt_version <= min_encrypt_version AND min_encrypt_version <= latest_version),
    CHECK (min_available_version >= 1 AND min_available_version <= min_decrypt_version),
    CHECK (compromised_through_version >= 0 AND compromised_through_version <= latest_version),
    CHECK ((state = 'pending-deletion') = (deletion_after IS NOT NULL)),
    UNIQUE (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id)
        REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE
);
INSERT INTO transit_keys (id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,exportable,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,purge_started,deletion_after,created_by,created_at,updated_at)
SELECT id,org_id,project_id,environment_id,name,algorithm,custody,allowed_operations,exportable,state,latest_version,min_encrypt_version,min_decrypt_version,min_available_version,compromised_through_version,rotation_period_seconds,purge_started,deletion_after,created_by,created_at,updated_at
FROM transit_keys_before_mldsa65;
DROP TABLE transit_keys_before_mldsa65;
CREATE UNIQUE INDEX transit_keys_live_name
    ON transit_keys (org_id, project_id, environment_id, name)
    WHERE state <> 'destroyed';
CREATE INDEX transit_keys_state ON transit_keys (state);

COMMIT;
PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
