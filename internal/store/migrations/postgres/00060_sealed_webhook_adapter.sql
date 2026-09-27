-- +goose Up
-- Sealed webhook is the third compiled-in deployment adapter (#163). Its
-- endpoints are instance-admin configuration outside the database; a tenant
-- adapter row only binds to an already activated origin.
ALTER TABLE adapters DROP CONSTRAINT adapters_provider_check;
ALTER TABLE adapters ADD CONSTRAINT adapters_provider_check
    CHECK (provider IN ('forgejo', 'github-actions', 'sealed-webhook'));
