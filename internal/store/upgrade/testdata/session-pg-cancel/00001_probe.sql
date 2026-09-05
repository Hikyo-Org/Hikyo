-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE migration_partial_probe(id INTEGER);
SELECT pg_sleep(30);
