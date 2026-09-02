-- +goose Up
-- Disaster-recovery operating state (#145, ops-spec section 11). One row of
-- instance operational state beside retention_runtime: the latest successful
-- export, the latest failure, the latest prune, and the latest restore drill.
-- It is health, not audit: the durable record of each act is its audit event
-- (backup.exported, backup.export_failed, restore.drill_completed); this row is
-- what doctor, the instance health read and /metrics read to answer "is the
-- recovery point objective being met right now". Nothing here names a
-- recipient, an identity or a key: archive names and versions only.
--
-- hikyo:table backup_state class=instance chain=-
CREATE TABLE backup_state (
    id BIGINT PRIMARY KEY CHECK (id = 1),
    last_success_at TIMESTAMPTZ,
    last_artifact_name TEXT NOT NULL DEFAULT '',
    last_artifact_bytes BIGINT NOT NULL DEFAULT 0,
    last_failure_at TIMESTAMPTZ,
    last_failure_reason TEXT NOT NULL DEFAULT '',
    last_prune_at TIMESTAMPTZ,
    last_drill_at TIMESTAMPTZ,
    last_drill_ok BOOLEAN NOT NULL DEFAULT FALSE,
    last_drill_archive TEXT NOT NULL DEFAULT '',
    last_drill_elapsed_ms BIGINT NOT NULL DEFAULT 0,
    last_drill_binary_version TEXT NOT NULL DEFAULT '',
    last_drill_schema_version BIGINT NOT NULL DEFAULT 0
);

INSERT INTO backup_state (id) VALUES (1);
