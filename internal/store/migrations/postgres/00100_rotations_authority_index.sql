-- +goose Up
-- The incident enumeration lists live rotations by their recorded authority
-- principal (#1471), once per selected account. Without this index each listing
-- scans every rotation in the instance.
CREATE INDEX rotations_authority ON rotations (authority_principal_id, id);
