-- +goose Up
-- Directory lifecycle events for OIDC providers (human-auth ADR, 2026-11-17
-- bring-your-own user directory amendment). A provider row may name a
-- lifecycle profile from a closed code-defined enum (v1: clerk) and carry the
-- signing secret that authenticates the directory's webhook. The secret is
-- envelope-encrypted under the instance DEK with its own field AAD
-- (oidc_providers / lifecycle_secret), distinct from the client secret's. Both
-- columns are NULL together or set together. Neither is login-path material.
ALTER TABLE oidc_providers ADD COLUMN lifecycle_profile TEXT CHECK (lifecycle_profile IN ('clerk'));
ALTER TABLE oidc_providers ADD COLUMN lifecycle_secret BYTEA CHECK ((lifecycle_profile IS NULL) = (lifecycle_secret IS NULL));
