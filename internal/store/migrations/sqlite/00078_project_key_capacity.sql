-- +goose Up
ALTER TABLE projects ADD COLUMN key_limit_override INTEGER
    CHECK (key_limit_override BETWEEN 1 AND 10000);
ALTER TABLE projects ADD COLUMN key_limit_revision INTEGER NOT NULL DEFAULT 0
    CHECK (key_limit_revision >= 0);
