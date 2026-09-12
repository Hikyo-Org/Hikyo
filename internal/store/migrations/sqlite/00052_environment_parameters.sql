-- +goose Up
-- Public config declarations and immutable delivery contracts, issue #723.
ALTER TABLE environments ADD COLUMN parameters_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE snapshots ADD COLUMN parameter_contract TEXT NOT NULL DEFAULT '{}';
