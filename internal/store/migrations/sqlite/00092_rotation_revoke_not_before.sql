-- +goose Up
-- The earliest time a published run's superseded credential may be revoked
-- (#148 S2a, round 9). next_attempt_at is both the wake time and, for a run in
-- the overlap wait, the revoke deadline; a wait longer than the delivered
-- credential's remaining validity needs a wake earlier than the deadline (to
-- extend the credential first), so the deadline is kept here and the revoke step
-- waits it out. NULL means the wake time is the deadline. A column on its own
-- rather than a rebuild: rotation_runs is not written by any shipped release.
ALTER TABLE rotation_runs ADD COLUMN revoke_not_before_at TEXT;
