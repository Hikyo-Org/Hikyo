-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE migration_partial_probe(id INTEGER);
WITH RECURSIVE counter(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM counter WHERE x < 1000000000) SELECT sum(x) FROM counter;
