-- +goose Up
-- Delivery-target condition reporting (#788, k8s-condition-reporting ADR).
-- Numbered 00059: 00057 and 00058 are claimed by the social sign-in stack.
--
-- hikyo:table delivery_target_reports class=environment chain=org_id,project_id
-- hikyo:table delivery_target_quota_notices class=project chain=org_id,project_id

-- The latest-state table (ADR D6): one row per target key, never a log. The
-- key is the reporting principal, never a credential, so overlap rotation
-- keeps one row (D3). Every text column holds a value the closed vocabulary,
-- the Kubernetes name grammar, a UID or a SemVer version admitted; there is no
-- free-text column (D4). `conditions` is the canonical JSON array of
-- {type,status,reason,observed_generation} entries from that vocabulary.
--
-- `received_at` is the server clock of the last ACCEPTED report. A refused
-- vocabulary report touches only `refusal_cause`/`refused_at` (D5). Deleting
-- the principal deletes its rows in the same transaction (D6), through the
-- cascade, the revision_pins precedent; deleting the environment does too.
CREATE TABLE delivery_target_reports (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    cluster_id TEXT NOT NULL,
    instance_uid TEXT NOT NULL,
    target_uid TEXT NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    vocabulary INTEGER NOT NULL CHECK (vocabulary >= 1),
    generation BIGINT NOT NULL CHECK (generation >= 1),
    observed_generation BIGINT NOT NULL CHECK (observed_generation >= 0),
    reported_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    report_interval_seconds INTEGER NOT NULL
        CHECK (report_interval_seconds BETWEEN 300 AND 86400),
    lifecycle TEXT NOT NULL
        CHECK (lifecycle IN ('Synced', 'Retained', 'Scrubbed', 'Refused', 'Unreconciled')),
    conditions TEXT NOT NULL,
    reporter TEXT NOT NULL CHECK (reporter IN ('kubernetes-operator')),
    reporter_version TEXT NOT NULL,
    refusal_cause TEXT CHECK (refusal_cause IN ('vocabulary')),
    refused_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    CHECK ((refusal_cause IS NULL) = (refused_at IS NULL)),
    UNIQUE (org_id, project_id, environment_id, principal_id, cluster_id, instance_uid, target_uid),
    FOREIGN KEY (org_id, project_id, environment_id)
        REFERENCES environments (org_id, project_id, id) ON DELETE CASCADE,
    FOREIGN KEY (principal_id) REFERENCES principals (id) ON DELETE CASCADE
);

-- The per-principal row quota (100) counts across environments.
CREATE INDEX delivery_target_reports_principal
    ON delivery_target_reports (org_id, project_id, principal_id);
-- The hourly 30-day purge.
CREATE INDEX delivery_target_reports_received
    ON delivery_target_reports (received_at);

-- The closed `quota-refused` notice (D5): a quota refusal has no row to
-- record on, so the principal carries its last time here instead.
CREATE TABLE delivery_target_quota_notices (
    principal_id TEXT PRIMARY KEY REFERENCES principals (id) ON DELETE CASCADE,
    org_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    refused_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (org_id, project_id) REFERENCES projects (org_id, id) ON DELETE CASCADE
);

-- The server-observed layer (D2) reads the last identity.delivery_fetched
-- event per (principal, environment). This index makes that one bounded seek
-- per principal instead of a scan of the environment's trail.
CREATE INDEX audit_tenant_events_env_actor_type_seq
    ON audit_tenant_events (org_id, project_id, env_id, actor_id, type, seq);
