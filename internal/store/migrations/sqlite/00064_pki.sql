-- +goose Up
-- Private PKI: policy-bound X.509 certificate lifecycle (#154, docs/adr/pki.md).
-- Roll-forward only. ASCII only (sqlc's sqlite parser mis-slices after a
-- non-ASCII byte).
--
-- pki_issuers is one CA KEY VERSION of a named issuer. The private key is
-- PKCS#8 sealed under the instance tier-3 key (InstanceFieldAAD owner table
-- pki_issuers, field tag private_key); retire and revoke destroy it (NULL).
-- There is no export path: the column is read only by the in-process signer.
-- certificate_der, csr_der, chain_pem and crl_der are public material.
--
-- pki_profiles holds the closed issuance policy as canonical JSON; profile
-- updates may only narrow (enforced in the service, ADR D5).
-- pki_profile_bindings is the explicit reach of a profile into one project or
-- one environment of it.
-- pki_certificates is the issuance record. It never holds a private key: the
-- CSR path never sees one and the generated path discloses it once. The row is
-- also the lifecycle job: issuing rows past issuing_deadline become unknown,
-- which the CRL publishes as revoked (fail closed, ADR D6).
--
-- hikyo:table pki_issuers class=instance chain=-
-- hikyo:table pki_profiles class=instance chain=-
-- hikyo:table pki_profile_bindings class=project chain=org_id,project_id
-- hikyo:table pki_certificates class=environment chain=org_id,project_id

CREATE TABLE pki_issuers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1),
    kind TEXT NOT NULL CHECK (kind IN ('root', 'intermediate')),
    origin TEXT NOT NULL CHECK (origin IN ('generated', 'imported')),
    parent_id TEXT REFERENCES pki_issuers (id),
    state TEXT NOT NULL CHECK (state IN ('pending', 'active', 'retiring', 'retired', 'revoked')),
    key_algorithm TEXT NOT NULL,
    key_fingerprint TEXT NOT NULL,
    encrypted_private_key BLOB,
    dek_version INTEGER,
    certificate_der BLOB,
    csr_der BLOB,
    chain_pem TEXT NOT NULL DEFAULT '',
    subject_cn TEXT NOT NULL,
    subject_org TEXT NOT NULL DEFAULT '',
    not_before TEXT,
    not_after TEXT,
    crl_distribution_url TEXT NOT NULL DEFAULT '',
    restore_hold INTEGER NOT NULL DEFAULT 0 CHECK (restore_hold IN (0, 1)),
    issued_count INTEGER NOT NULL DEFAULT 0,
    crl_der BLOB,
    crl_number INTEGER NOT NULL DEFAULT 0,
    revocation_seq INTEGER NOT NULL DEFAULT 0 CHECK (revocation_seq >= 0),
    crl_revocation_seq INTEGER NOT NULL DEFAULT 0 CHECK (crl_revocation_seq >= 0),
    crl_this_update TEXT,
    crl_next_update TEXT,
    row_version INTEGER NOT NULL DEFAULT 1,
    created_by TEXT NOT NULL REFERENCES principals (id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (name, version),
    CHECK ((state IN ('retired', 'revoked')) = (encrypted_private_key IS NULL)),
    CHECK ((encrypted_private_key IS NULL) = (dek_version IS NULL)),
    CHECK (state IN ('pending', 'revoked') OR certificate_der IS NOT NULL),
    CHECK (state <> 'pending' OR csr_der IS NOT NULL)
);
CREATE UNIQUE INDEX pki_issuers_one_active ON pki_issuers (name) WHERE state = 'active';
CREATE UNIQUE INDEX pki_issuers_one_pending ON pki_issuers (name) WHERE state = 'pending';

CREATE TABLE pki_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    policy TEXT NOT NULL,
    row_version INTEGER NOT NULL DEFAULT 1,
    created_by TEXT NOT NULL REFERENCES principals (id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE pki_profile_bindings (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES pki_profiles (id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT,
    created_by TEXT NOT NULL REFERENCES principals (id),
    created_at TEXT NOT NULL,
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX pki_profile_bindings_unique
    ON pki_profile_bindings (profile_id, org_id, project_id, COALESCE(environment_id, ''));
CREATE INDEX pki_profile_bindings_scope ON pki_profile_bindings (org_id, project_id);

CREATE TABLE pki_certificates (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    issuer_id TEXT NOT NULL REFERENCES pki_issuers (id),
    serial TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('issuing', 'issued', 'renewed', 'revoked', 'expired', 'unknown', 'failed')),
    key_source TEXT NOT NULL CHECK (key_source IN ('csr', 'generated')),
    key_algorithm TEXT NOT NULL,
    key_fingerprint TEXT NOT NULL,
    common_name TEXT NOT NULL DEFAULT '',
    sans TEXT NOT NULL,
    not_before TEXT NOT NULL,
    not_after TEXT NOT NULL,
    certificate_der BLOB,
    principal_id TEXT NOT NULL REFERENCES principals (id),
    principal_class TEXT NOT NULL,
    renewed_from TEXT,
    renewed_by TEXT,
    revoked_at TEXT,
    revocation_reason TEXT CHECK (revocation_reason IN ('unspecified', 'key-compromise', 'ca-compromise', 'affiliation-changed', 'superseded', 'cessation-of-operation', 'privilege-withdrawn')),
    issuing_deadline TEXT NOT NULL,
    row_version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (issuer_id, serial),
    UNIQUE (org_id, project_id, environment_id, id),
    CHECK ((state = 'revoked') = (revoked_at IS NOT NULL)),
    CHECK ((revoked_at IS NULL) = (revocation_reason IS NULL)),
    CHECK (state IN ('issuing', 'unknown', 'failed', 'revoked') OR certificate_der IS NOT NULL),
    FOREIGN KEY (org_id, project_id, environment_id) REFERENCES environments (org_id, project_id, id)
);
CREATE INDEX pki_certificates_env ON pki_certificates (org_id, project_id, environment_id, created_at);
CREATE INDEX pki_certificates_issuer ON pki_certificates (issuer_id, state);
CREATE INDEX pki_certificates_due ON pki_certificates (state, not_after);
CREATE INDEX pki_certificates_issuing ON pki_certificates (state, issuing_deadline);
