-- +goose Up
-- Transit gains the signing algorithm mldsa65 (#1129, post-quantum migration
-- DEC-02): a new, immutable key identity beside ed25519. Only the closed
-- algorithm CHECK widens; no column or row changes, and no existing key's
-- algorithm changes. Roll-forward only.
ALTER TABLE transit_keys DROP CONSTRAINT transit_keys_algorithm_check;
ALTER TABLE transit_keys ADD CONSTRAINT transit_keys_algorithm_check
    CHECK (algorithm IN ('xchacha20-poly1305', 'ed25519', 'hmac-sha256', 'mldsa65'));
