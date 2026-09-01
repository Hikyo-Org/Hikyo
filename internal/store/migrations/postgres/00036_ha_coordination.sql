-- +goose Up
-- Multi-node HA coordination (#146). All three tables are installation-wide
-- infrastructure, not tenant data: they carry no org/project/environment
-- chain, so their scope class is instance. HA mode itself is refused on
-- sqlite, but the tables exist on both engines so a single-node instance that
-- later flips HIKYO_HA has no schema gap.

-- hikyo:table singleton_leases class=instance chain=-
-- One row per named singleton (currently 'scheduler'). A node claims a lease
-- by name when the current holder's lease has expired; fence_token increments
-- on every acquisition so a stale holder that resumes after losing the lease
-- writes under an old token and its guarded writes affect zero rows.
CREATE TABLE singleton_leases (
    name        TEXT PRIMARY KEY,
    owner       TEXT NOT NULL,
    fence_token BIGINT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);

-- hikyo:table ha_nodes class=instance chain=-
-- The live-node registry. Each serving node upserts its row on the scheduler
-- tick; health reads it for nodes_seen and the leader, and the rolling-upgrade
-- guard reads schema_version. root_key_fingerprint pins the shared root-key
-- authority: a node whose fingerprint disagrees with the installation refuses
-- to serve.
CREATE TABLE ha_nodes (
    node_id              TEXT PRIMARY KEY,
    binary_version       TEXT NOT NULL,
    schema_version       BIGINT NOT NULL,
    root_key_fingerprint TEXT NOT NULL,
    started_at           TIMESTAMPTZ NOT NULL,
    heartbeat_at         TIMESTAMPTZ NOT NULL
);

-- hikyo:table admission_counters class=instance chain=-
-- Installation-wide pre-authentication admission state so node hopping cannot
-- bypass a per-IP, per-account, or per-issuer limit. Windowed rate buckets
-- (bucket in 'ip','meta','issuer') store hits per fixed minute window; the
-- account-backoff bucket ('account') stores consecutive failures and the
-- current delay deadline in a single row with a fixed sentinel window. The
-- per-node concurrency semaphore stays in process memory (it bounds that
-- node's RAM budget) and is not stored here.
CREATE TABLE admission_counters (
    bucket       TEXT NOT NULL,
    subject      TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    hits         BIGINT NOT NULL DEFAULT 0,
    failures     BIGINT NOT NULL DEFAULT 0,
    until_at     TIMESTAMPTZ,
    PRIMARY KEY (bucket, subject, window_start)
);

-- Bounded sweep support: expired windows and stale nodes are pruned by index.
CREATE INDEX admission_counters_window ON admission_counters (window_start);
