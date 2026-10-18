-- +goose Up
-- #930: a completed traversal is not proof that the age ceiling was met.
-- Preserve the last successful prune while recording failures and recovery exceptions.
ALTER TABLE backup_state ADD COLUMN last_prune_attempt_at TEXT;
ALTER TABLE backup_state ADD COLUMN prune_failure_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_state ADD COLUMN retained_expired_archives BIGINT NOT NULL DEFAULT 0 CHECK (retained_expired_archives >= 0);
ALTER TABLE backup_state ADD COLUMN last_prune_attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_state ADD COLUMN last_artifact_sha256 TEXT NOT NULL DEFAULT '';
