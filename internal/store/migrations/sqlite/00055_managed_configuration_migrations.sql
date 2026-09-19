-- +goose Up
-- Versioned encrypted managed-data migrations run under the upgrade session,
-- after schema application and before candidate health. SQL cannot seal values.
ALTER TABLE self_config_binding ADD COLUMN migration_version BIGINT NOT NULL DEFAULT 0 CHECK (migration_version >= 0);
