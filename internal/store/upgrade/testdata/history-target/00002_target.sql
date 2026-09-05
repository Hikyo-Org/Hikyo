-- +goose Up
INSERT INTO migration_history_probe VALUES ('target-effect');

-- +goose Down
DELETE FROM migration_history_probe WHERE value='target-effect';
