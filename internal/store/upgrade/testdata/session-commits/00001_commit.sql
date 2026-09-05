-- +goose Up
CREATE TABLE migration_commit_probe(id INTEGER PRIMARY KEY); INSERT INTO migration_commit_probe VALUES (1);

-- +goose Down
DROP TABLE migration_commit_probe;
