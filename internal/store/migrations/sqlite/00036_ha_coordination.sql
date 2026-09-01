-- +goose Up
-- Multi-node HA coordination (#146). See the postgres copy for the rationale.
-- These tables exist on sqlite for schema parity only: HA mode is refused at
-- boot on sqlite, so the scheduler lease and admission counters here are only
-- ever exercised by the single local node. Timestamps are TEXT in the
-- canonical RFC3339 microsecond form the rest of the sqlite schema uses.

-- hikyo:table singleton_leases class=instance chain=-
CREATE TABLE singleton_leases (
    name        TEXT PRIMARY KEY,
    owner       TEXT NOT NULL,
    fence_token INTEGER NOT NULL,
    acquired_at TEXT NOT NULL,
    expires_at  TEXT NOT NULL
);

-- hikyo:table ha_nodes class=instance chain=-
CREATE TABLE ha_nodes (
    node_id              TEXT PRIMARY KEY,
    binary_version       TEXT NOT NULL,
    schema_version       INTEGER NOT NULL,
    root_key_fingerprint TEXT NOT NULL,
    started_at           TEXT NOT NULL,
    heartbeat_at         TEXT NOT NULL
);

-- hikyo:table admission_counters class=instance chain=-
CREATE TABLE admission_counters (
    bucket       TEXT NOT NULL,
    subject      TEXT NOT NULL,
    window_start TEXT NOT NULL,
    hits         INTEGER NOT NULL DEFAULT 0,
    failures     INTEGER NOT NULL DEFAULT 0,
    until_at     TEXT,
    PRIMARY KEY (bucket, subject, window_start)
);

CREATE INDEX admission_counters_window ON admission_counters (window_start);
