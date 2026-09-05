-- +goose Up
CREATE TABLE migration_session_probe(value TEXT NOT NULL);
INSERT INTO migration_session_probe SELECT value FROM migration_connection_marker;
