-- +goose Up
-- Bound the principal footprint read, which reaches scim_users through the
-- account alone; the existing index leads with binding_id.
CREATE INDEX scim_users_account_only ON scim_users (account_id);

-- +goose Down
DROP INDEX scim_users_account_only;
