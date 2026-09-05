-- +goose Up
CREATE TABLE build_probe (id TEXT PRIMARY KEY);

-- +goose Down
DROP TABLE build_probe;
