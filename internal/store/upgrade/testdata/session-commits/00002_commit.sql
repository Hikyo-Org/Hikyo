-- +goose Up
INSERT INTO migration_commit_probe VALUES (2);

-- +goose Down
DELETE FROM migration_commit_probe WHERE id=2;
