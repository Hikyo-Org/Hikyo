-- +goose Up
-- Preserve the exact AWS resource/current-version metadata reviewed by a plan.
-- Legacy artifacts have no witness and cannot authorize new AWS adoption.
ALTER TABLE adapter_conflicts ADD COLUMN observed_provider_witness TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE adapter_conflicts DROP COLUMN observed_provider_witness;
