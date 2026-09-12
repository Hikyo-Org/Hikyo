-- +goose Up
-- Trust anchors belong to one machine federation issuer, never the host.
ALTER TABLE federation_issuers ADD COLUMN ca_bundle_pem TEXT NOT NULL DEFAULT ''
    CHECK (ca_bundle_pem = '' OR jwks_mode = 'discovery');
