-- +goose Up
-- A plan observes a provider version without reading its value. Legacy
-- artifacts have no witness and cannot authorize ambiguous Vault recovery.
ALTER TABLE adapter_conflicts ADD COLUMN observed_provider_version BIGINT CHECK (observed_provider_version IS NULL OR observed_provider_version >= 0);

-- +goose Down
ALTER TABLE adapter_conflicts DROP COLUMN observed_provider_version;
