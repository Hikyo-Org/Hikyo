-- +goose Up
-- The sealed-webhook provider id is now encrypted-webhook (#1334). Stored rows
-- are rewritten; the API still accepts the old id as a deprecated alias until
-- v2.
ALTER TABLE adapters DROP CONSTRAINT adapters_provider_check;
UPDATE adapters SET provider = 'encrypted-webhook' WHERE provider = 'sealed-webhook';
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_check
    CHECK (provider IN ('forgejo', 'github-actions', 'encrypted-webhook', 'cloudflare', 'vault-kv', 'aws-secrets-manager', 'gitlab'));
