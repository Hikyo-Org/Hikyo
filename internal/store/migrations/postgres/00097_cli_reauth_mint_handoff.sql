-- +goose Up
-- A CLI reauthentication handoff for a service-account credential mint (#1516)
-- binds one environment and no keys. The purpose and operation checks also gain
-- the publish, approve, reject and bypass handoffs the service already starts
-- (00074 left them out, so their inserts failed the check); the key_set check
-- now lets 'mint' hold an empty set, like 'adapter'.
ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_purpose_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_purpose_check CHECK (purpose IN ('adapter','reveal','copy','publish','approve','reject','bypass','mint','self-config','developer-credential'));

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_operation_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_operation_check CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','value.copy-destination','approval.vote','approval.bypass','identity.credential-mint','self-config.adopt','self-config.apply','self-config.test','developer-credential.mint'));

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_key_set_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_key_set_check CHECK ((purpose IN ('adapter','mint') AND key_set = '') OR (purpose NOT IN ('adapter','mint') AND key_set <> ''));
