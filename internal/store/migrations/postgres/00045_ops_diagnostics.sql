-- +goose Up
-- Public verification metadata only. Private escrow material is never stored.
-- hikyo:table ops_diagnostics class=instance chain=-
CREATE TABLE ops_diagnostics (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    escrow_verified_at TIMESTAMPTZ,
    escrow_instance_id TEXT NOT NULL DEFAULT '',
    escrow_incarnation TEXT NOT NULL DEFAULT '',
    escrow_root_epoch BIGINT NOT NULL DEFAULT 0 CHECK (escrow_root_epoch >= 0),
    last_reencrypt_success TIMESTAMPTZ
);
INSERT INTO ops_diagnostics (singleton) VALUES (1);
