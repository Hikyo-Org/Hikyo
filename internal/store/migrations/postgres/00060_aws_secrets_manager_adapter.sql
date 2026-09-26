-- +goose Up
-- AWS Secrets Manager is the first cloud secret-manager deployment adapter
-- (#158). It adds the provider identity and two destination kinds:
-- json-object (one secret holding a JSON object) and per-key (one secret per
-- key). Only the closed CHECK sets widen; no column or row changes.
ALTER TABLE adapters DROP CONSTRAINT adapters_provider_check;
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_check
    CHECK (provider IN ('forgejo', 'github-actions', 'aws-secrets-manager'));

ALTER TABLE adapter_targets DROP CONSTRAINT adapter_targets_destination_kind_check;
ALTER TABLE adapter_targets ADD CONSTRAINT adapter_targets_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'json-object', 'per-key'));

ALTER TABLE adapter_route_move_targets DROP CONSTRAINT adapter_route_move_targets_destination_kind_check;
ALTER TABLE adapter_route_move_targets ADD CONSTRAINT adapter_route_move_targets_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'json-object', 'per-key'));

ALTER TABLE adapter_route_move_claims DROP CONSTRAINT adapter_route_move_claims_destination_kind_check;
ALTER TABLE adapter_route_move_claims ADD CONSTRAINT adapter_route_move_claims_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'json-object', 'per-key'));

ALTER TABLE adapter_ledger DROP CONSTRAINT adapter_ledger_destination_kind_check;
ALTER TABLE adapter_ledger ADD CONSTRAINT adapter_ledger_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'json-object', 'per-key'));
