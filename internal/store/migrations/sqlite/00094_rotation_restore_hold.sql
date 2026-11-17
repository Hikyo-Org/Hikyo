-- +goose Up
-- The restore hold of a rotation (#148 S2b-2). A restore brings back rotations
-- whose grants, provider credentials and served keys the operator has not
-- reconciled yet, so the restore closure marks every rotation held: a held
-- rotation starts no run and takes no pre-publish step, exactly like a paused
-- one, until `rotation release-hold` clears the flag. The flag is independent of
-- the state, so a paused or halted rotation cannot resume past it. A column on
-- its own rather than a rebuild: rotations is not written by any shipped release.
ALTER TABLE rotations ADD COLUMN restore_hold INTEGER NOT NULL DEFAULT 0 CHECK (restore_hold IN (0, 1));
