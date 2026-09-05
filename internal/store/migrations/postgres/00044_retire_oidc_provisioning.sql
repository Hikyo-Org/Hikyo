-- +goose Up
-- Pre-1.0 retirement: OIDC login requires an existing, explicitly linked account.
-- Preserve migration history; remove the retired provider policy in place.
ALTER TABLE oidc_providers DROP COLUMN jit_policy;
