-- +goose Up
-- SSH user certificates (#155). Roll-forward only. Every table is
-- environment-scoped: a workload credential is granted at environment depth,
-- so a host or job holding read@environment reaches its trust material and
-- its profiles without a project grant, and a staging CA can never sign for
-- production. Design: docs/handoff/155-ssh-certificates.md.

-- hikyo:table ssh_cas class=environment chain=org_id,project_id
-- A CA is a named container of signing keys. Tombstoning is record deletion
-- only: certificates it signed stay valid on hosts that still trust its keys.
CREATE TABLE ssh_cas (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'tombstoned')),
    authority_principal_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id)
);
CREATE UNIQUE INDEX ssh_cas_live_name ON ssh_cas (org_id, project_id, environment_id, name) WHERE state = 'active';

-- hikyo:table ssh_ca_keys class=environment chain=org_id,project_id
-- One CA signing key. private_key_ciphertext is PKCS#8 DER sealed under the
-- project DEK; it is nulled the moment the key leaves `active`, so a retiring
-- key can be trusted but never sign. retire_after bounds the rotation overlap.
CREATE TABLE ssh_ca_keys (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    ca_id TEXT NOT NULL,
    algorithm TEXT NOT NULL CHECK (algorithm IN ('ed25519', 'ecdsa-p256', 'rsa-3072')),
    public_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (origin IN ('generated', 'imported')),
    private_key_ciphertext BYTEA,
    state TEXT NOT NULL CHECK (state IN ('active', 'retiring', 'retired')),
    created_at TIMESTAMPTZ NOT NULL,
    retiring_at TIMESTAMPTZ,
    retire_after TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    CHECK ((state = 'active') = (private_key_ciphertext IS NOT NULL)),
    UNIQUE (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id, ca_id) REFERENCES ssh_cas (org_id, project_id, environment_id, id)
);
CREATE UNIQUE INDEX ssh_ca_keys_one_active ON ssh_ca_keys (ca_id) WHERE state = 'active';
CREATE INDEX ssh_ca_keys_ca ON ssh_ca_keys (ca_id, state);

-- hikyo:table ssh_profiles class=environment chain=org_id,project_id
-- The signing policy. List-valued constraints are canonical JSON arrays of
-- strings validated by internal/sshca before they are written.
CREATE TABLE ssh_profiles (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    ca_id TEXT NOT NULL,
    name TEXT NOT NULL,
    principals TEXT NOT NULL,
    force_command TEXT NOT NULL DEFAULT '',
    source_addresses TEXT NOT NULL,
    extensions TEXT NOT NULL,
    key_algorithms TEXT NOT NULL,
    default_ttl_seconds BIGINT NOT NULL CHECK (default_ttl_seconds > 0),
    max_ttl_seconds BIGINT NOT NULL CHECK (max_ttl_seconds >= default_ttl_seconds),
    state TEXT NOT NULL CHECK (state IN ('enabled', 'disabled', 'tombstoned')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id, ca_id) REFERENCES ssh_cas (org_id, project_id, environment_id, id)
);
CREATE UNIQUE INDEX ssh_profiles_live_name ON ssh_profiles (org_id, project_id, environment_id, name) WHERE state <> 'tombstoned';
CREATE INDEX ssh_profiles_ca ON ssh_profiles (ca_id, state);

-- hikyo:table ssh_profile_requesters class=environment chain=org_id,project_id
-- The explicit per-profile requester list: the opt-in conjunct of issuance.
-- No foreign key to principals, deliberately: deleting a principal must not
-- be blocked by a profile naming it; the sweeper revokes its certificates.
CREATE TABLE ssh_profile_requesters (
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (profile_id, principal_id),
    FOREIGN KEY (org_id, project_id, environment_id, profile_id) REFERENCES ssh_profiles (org_id, project_id, environment_id, id)
);

-- hikyo:table ssh_certificates class=environment chain=org_id,project_id
-- One issued certificate's public record. There is no private column: a
-- generated user key is disclosed once and never stored. Stored state is only
-- issued|revoked; expiry and trust are derived at read time from valid_before
-- and the signing key's state, so the record never lies about the host's view.
-- No foreign key to principals (see ssh_profile_requesters).
CREATE TABLE ssh_certificates (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    ca_id TEXT NOT NULL,
    ca_key_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    serial BIGINT NOT NULL CHECK (serial > 0),
    key_id TEXT NOT NULL,
    principals TEXT NOT NULL,
    public_key_fingerprint TEXT NOT NULL,
    key_algorithm TEXT NOT NULL CHECK (key_algorithm IN ('ed25519', 'ecdsa-p256', 'rsa-3072')),
    key_origin TEXT NOT NULL CHECK (key_origin IN ('generated', 'supplied')),
    valid_after TIMESTAMPTZ NOT NULL,
    valid_before TIMESTAMPTZ NOT NULL,
    requester_principal_id TEXT NOT NULL,
    requester_class TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('issued', 'revoked')),
    revoked_at TIMESTAMPTZ,
    revocation_reason TEXT CHECK (revocation_reason IN ('explicit', 'authority-withdrawn', 'profile-deleted')),
    created_at TIMESTAMPTZ NOT NULL,
    CHECK ((state = 'revoked') = (revoked_at IS NOT NULL)),
    UNIQUE (org_id, project_id, environment_id, id),
    UNIQUE (ca_id, serial),
    FOREIGN KEY (org_id, project_id, environment_id, ca_key_id) REFERENCES ssh_ca_keys (org_id, project_id, environment_id, id),
    FOREIGN KEY (org_id, project_id, environment_id, profile_id) REFERENCES ssh_profiles (org_id, project_id, environment_id, id)
);
CREATE INDEX ssh_certificates_env ON ssh_certificates (org_id, project_id, environment_id, created_at);
CREATE INDEX ssh_certificates_live ON ssh_certificates (state, valid_before);
CREATE INDEX ssh_certificates_krl ON ssh_certificates (ca_id, state, valid_before);
