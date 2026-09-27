-- +goose Up
-- Cloudflare Workers and Pages is a compiled-in deployment adapter
-- (#161). It adds the cloudflare provider and the workers-script and
-- pages-project destination kinds.
ALTER TABLE adapters DROP CONSTRAINT adapters_provider_check;
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_check
    CHECK (provider IN ('forgejo', 'github-actions', 'sealed-webhook', 'cloudflare'));

ALTER TABLE adapter_targets DROP CONSTRAINT adapter_targets_destination_kind_check;
ALTER TABLE adapter_targets ADD CONSTRAINT adapter_targets_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project'));

ALTER TABLE adapter_route_move_targets DROP CONSTRAINT adapter_route_move_targets_destination_kind_check;
ALTER TABLE adapter_route_move_targets ADD CONSTRAINT adapter_route_move_targets_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project'));

ALTER TABLE adapter_route_move_claims DROP CONSTRAINT adapter_route_move_claims_destination_kind_check;
ALTER TABLE adapter_route_move_claims ADD CONSTRAINT adapter_route_move_claims_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project'));

ALTER TABLE adapter_ledger DROP CONSTRAINT adapter_ledger_destination_kind_check;
ALTER TABLE adapter_ledger ADD CONSTRAINT adapter_ledger_destination_kind_check
    CHECK (destination_kind IN ('repository', 'organization', 'environment', 'workers-script', 'pages-project'));
