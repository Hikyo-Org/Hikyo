-- +goose Up
-- #974: permanent grants are resolved for one principal on every authorization.
CREATE INDEX grants_principal ON grants (principal_id);

-- #986: owner equality precedes the keyset range. Keep the existing unique
-- (org, project, environment, key, owner) constraint unchanged.
CREATE INDEX pending_changes_owner_key ON pending_changes
    (org_id, project_id, environment_id, owner_id, key_id);
