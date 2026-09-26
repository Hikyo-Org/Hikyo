-- +goose Up
-- Vault/OpenBao KV v2 is the third compiled-in deployment adapter (#162).
-- Only the provider discriminator widens; KV addressing reuses the repository
-- destination columns (owner = mount, name = path prefix).
ALTER TABLE adapters DROP CONSTRAINT adapters_provider_check;
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_check
    CHECK (provider IN ('forgejo', 'github-actions', 'vault-kv'));
