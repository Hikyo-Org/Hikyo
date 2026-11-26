-- +goose Up
-- The incident that contained the account's principal when a credential
-- establishment consumed an authority (#1039 S5). Release reads establishment from
-- this stamp alone and compares incident ids, never the clocks of two nodes, so
-- an establishment before containment (no stamp) cannot count however its
-- node's clock read.
ALTER TABLE credential_authorities ADD COLUMN established_in_incident TEXT;
