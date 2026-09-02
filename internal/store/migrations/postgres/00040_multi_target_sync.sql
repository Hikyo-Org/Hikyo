-- +goose Up
-- Multi-target synchronization control and health (#157).
-- Roll-forward only; no Down migrations. Additive columns on adapter_targets.
-- See the sqlite copy for the column semantics.
ALTER TABLE adapter_targets
    ADD COLUMN paused_at TIMESTAMPTZ,
    ADD COLUMN last_attempted_revision BIGINT,
    ADD COLUMN last_attempted_at TIMESTAMPTZ,
    ADD COLUMN last_error_class TEXT
        CHECK (last_error_class IS NULL OR last_error_class IN ('auth', 'network', 'conflict', 'provider_limit', 'provider_ambiguous', 'refused')),
    ADD COLUMN drift_attention BOOLEAN NOT NULL DEFAULT FALSE;
