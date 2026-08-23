-- +goose Up
-- Browser-started OIDC transactions return through the SPA done page. The
-- explicit bit is independent of the binding kind: reauth and link stay bound
-- to their initiating session while still using a browser redirect.
ALTER TABLE oidc_transactions ADD COLUMN browser INTEGER NOT NULL DEFAULT 0 CHECK (browser IN (0, 1));
