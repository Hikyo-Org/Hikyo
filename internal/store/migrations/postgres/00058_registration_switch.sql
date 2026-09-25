-- +goose Up
-- The registration switch (#607; see the sqlite dialect's header). The login
-- arm of oidc_transactions_purpose_shape gains `intent IS NOT NULL`; in-flight
-- transactions are deleted rather than backfilled (minutes-lived rows, and a
-- login row from the previous binary carries a NULL intent). Postgres replaces
-- the named CHECK in place.
DELETE FROM oidc_transactions;
ALTER TABLE oidc_transactions DROP CONSTRAINT oidc_transactions_purpose_shape;
ALTER TABLE oidc_transactions ADD CONSTRAINT oidc_transactions_purpose_shape CHECK (
        (purpose = 'login' AND binding_kind = 'browser-cookie' AND intent IS NOT NULL
            AND account_id IS NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL
            AND (intent = 'sign-up' OR signup_scope_org_id IS NULL))
     OR (purpose = 'link' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND ceremony_id IS NOT NULL AND environment_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'reauth' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NOT NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'establish' AND binding_kind = 'session' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NULL)
     OR (purpose = 'claim' AND binding_kind = 'browser-cookie' AND intent IS NULL AND signup_scope_org_id IS NULL
            AND account_id IS NOT NULL AND environment_id IS NULL AND ceremony_id IS NULL AND authority_id IS NOT NULL)
);
