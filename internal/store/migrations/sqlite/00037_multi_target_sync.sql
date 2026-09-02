-- +goose Up
-- Multi-target synchronization control and health (#157).
-- Roll-forward only; no Down migrations. Additive columns on adapter_targets.
--
-- sync_status keeps its four stored outcomes (never, converging, converged,
-- failed). The operator-facing health (pending, degraded, paused) is derived
-- at read time from these columns and the active outbox job, so pausing a
-- target never erases the last recorded outcome.
--
-- paused_at: set while an operator has paused the target. A paused target's
-- queued jobs are never claimed and publishes do not enqueue for it; the
-- ownership ledger is retained so resume needs no re-adoption.
-- last_attempted_*: the revision and time of the most recent converge attempt,
-- whether or not it succeeded. converged_revision keeps the last success.
-- last_error_class: the bounded cause of the last failed attempt; NULL after a
-- successful attempt. Never a provider response body.
-- drift_attention: set when the destination disagrees with the ledger in a way
-- only an operator can settle (unowned name in the way, destination identity
-- moved, orphaned names); cleared by the next successful converge.
ALTER TABLE adapter_targets ADD COLUMN paused_at TEXT;
ALTER TABLE adapter_targets ADD COLUMN last_attempted_revision INTEGER;
ALTER TABLE adapter_targets ADD COLUMN last_attempted_at TEXT;
ALTER TABLE adapter_targets ADD COLUMN last_error_class TEXT
    CHECK (last_error_class IS NULL OR last_error_class IN ('auth', 'network', 'conflict', 'provider_limit', 'provider_ambiguous', 'refused'));
ALTER TABLE adapter_targets ADD COLUMN drift_attention INTEGER NOT NULL DEFAULT 0 CHECK (drift_attention IN (0, 1));
