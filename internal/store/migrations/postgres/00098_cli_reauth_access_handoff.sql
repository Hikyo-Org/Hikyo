-- +goose Up
-- A CLI reauthentication handoff for emergency temporary access (#1531) binds
-- one environment and no keys, like 'mint'. The purpose and operation checks
-- gain 'access' and 'access.bypass'; the key_set check lets 'access' hold an
-- empty set.
ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_purpose_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_purpose_check CHECK (purpose IN ('adapter','reveal','copy','publish','approve','reject','bypass','mint','access','self-config','developer-credential'));

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_operation_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_operation_check CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','value.copy-destination','approval.vote','approval.bypass','identity.credential-mint','access.bypass','self-config.adopt','self-config.apply','self-config.test','developer-credential.mint'));

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_key_set_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_key_set_check CHECK ((purpose IN ('adapter','mint','access') AND key_set = '') OR (purpose NOT IN ('adapter','mint','access') AND key_set <> ''));
