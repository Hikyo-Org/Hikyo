-- +goose Up
-- The restore hold of a rotation (#148 S2b-2). A restore brings back rotations
-- whose grants, provider credentials and served keys the operator has not
-- reconciled yet, so the restore closure marks every rotation held: a held
-- rotation starts no run and takes no pre-publish step, exactly like a paused
-- one, until `rotation release-hold` clears the flag. The flag is independent of
-- the state, so a paused or halted rotation cannot resume past it. A column on
-- its own rather than a rebuild: rotations is not written by any shipped release.
ALTER TABLE rotations ADD COLUMN restore_hold INTEGER NOT NULL DEFAULT 0 CHECK (restore_hold IN (0, 1));
-- deferred_from keeps the first due time of a wait that datastore faults keep deferring,
-- so the overdue gauge cannot be defeated by the deferral; the index serves the
-- expiring gauge's latest-published-run lookup without scanning every run.
ALTER TABLE rotations ADD COLUMN deferred_from TEXT;
CREATE INDEX rotation_runs_env_published ON rotation_runs (org_id, project_id, environment_id, published_revision DESC, id DESC) WHERE published_revision IS NOT NULL;
-- A superseded role is found by the handle its minting run recorded (the run list's
-- expiry, the revoke step's role lookup), so that handle is indexed within the chain.
CREATE INDEX rotation_runs_minter ON rotation_runs (org_id, project_id, environment_id, candidate_handle);
