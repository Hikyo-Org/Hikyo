-- +goose Up
CREATE TABLE migration_history_probe(value TEXT NOT NULL);
INSERT INTO migration_history_probe VALUES ('source');
CREATE TABLE singleton_leases(name TEXT PRIMARY KEY,owner TEXT NOT NULL,fence_token BIGINT NOT NULL,acquired_at TEXT NOT NULL,expires_at TEXT NOT NULL);

-- +goose Down
DROP TABLE singleton_leases;
DROP TABLE migration_history_probe;
