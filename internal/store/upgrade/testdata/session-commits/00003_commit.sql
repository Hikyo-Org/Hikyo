-- +goose Up
INSERT INTO migration_commit_probe VALUES (3);

-- +goose Down
DELETE FROM migration_commit_probe WHERE id=3;
