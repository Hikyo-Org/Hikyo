-- +goose Up
-- GitLab CI/CD variables are the third compiled-in deployment adapter (#159).
-- A GitLab project is stored as destination_kind 'repository' and a group as
-- 'organization'; destination_scope is the GitLab environment_scope.
ALTER TABLE adapters DROP CONSTRAINT adapters_provider_check;
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_check
    CHECK (provider IN ('forgejo', 'github-actions', 'sealed-webhook', 'cloudflare', 'vault-kv', 'aws-secrets-manager', 'gitlab'));
-- Adapter egress trust for self-hosted GitLab. None of these is a secret: the
-- pin is a public-key hash and the bundle holds public certificates.
ALTER TABLE adapters
    ADD COLUMN spki_pin TEXT NOT NULL DEFAULT '',
    ADD COLUMN ca_bundle_pem TEXT NOT NULL DEFAULT '',
    ADD COLUMN allow_personal_token BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_options_check
    CHECK (provider = 'gitlab' OR (spki_pin = '' AND ca_bundle_pem = '' AND NOT allow_personal_token));

ALTER TABLE adapter_targets
    ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '',
    ADD COLUMN variable_protected BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN variable_hidden BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN variable_expand BOOLEAN NOT NULL DEFAULT FALSE;

-- The same key may be owned once per GitLab environment scope.
ALTER TABLE adapter_ledger ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '';
DROP INDEX adapter_ledger_active_provider_name;
CREATE UNIQUE INDEX adapter_ledger_active_provider_name
    ON adapter_ledger (provider_origin, destination_kind, repository_id, destination_id, destination_scope, surface, normalized_name)
    WHERE state <> 'released';

-- Pending route ownership uses the same immutable GitLab environment scope.
ALTER TABLE adapter_route_move_targets ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE adapter_route_move_claims ADD COLUMN destination_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE adapter_route_move_claims DROP CONSTRAINT adapter_route_move_claims_provider_destination_name_unique;
ALTER TABLE adapter_route_move_claims ADD CONSTRAINT adapter_route_move_claims_provider_destination_name_unique
    UNIQUE (provider_origin, destination_kind, destination_owner, destination_name, destination_environment, destination_scope, surface, normalized_name);
