-- +goose Up
-- A pending run-now request (#148 S2b-1). Run-now records the run it asked for
-- on the rotation row and makes the rotation due now; the executor's normal start
-- path consumes the request, so a manual run goes through the same Gate, window
-- and overlap rules as a scheduled one. manual_run_id is the id the run will
-- carry (the audit row names it before the run exists); manual_skip_overlap asks
-- for a run whose revocation delay is zero. Both are cleared when the run starts
-- and whenever the rotation leaves the active state. A column pair on its own
-- rather than a rebuild: rotations is not written by any shipped release.
ALTER TABLE rotations ADD COLUMN manual_run_id TEXT;
ALTER TABLE rotations ADD COLUMN manual_skip_overlap INTEGER NOT NULL DEFAULT 0 CHECK (manual_skip_overlap IN (0, 1));
-- The adapter (sync) targets still on the superseded revision when the run
-- revoked it (#148, S2b-1): non-deleted targets mirroring either rotation key
-- whose converged revision is behind the run's published revision, counted just
-- before the revoke. Machine principals that read the keys directly are not
-- counted: no per-principal delivery record exists. NULL until a revoke decided;
-- not a live reading afterwards.
ALTER TABLE rotation_runs ADD COLUMN not_converged_adapter_targets INTEGER;
