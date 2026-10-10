-- +goose Up
-- Bounded developer delegation (#807); authentication metadata only.
-- hikyo:table developer_credentials class=authn chain=-
-- hikyo:table developer_credential_policy class=authn chain=-
CREATE TABLE developer_credentials (
 id TEXT PRIMARY KEY,
 principal_id TEXT NOT NULL UNIQUE REFERENCES principals(id),
 authority_principal_id TEXT NOT NULL,
 org_id TEXT NOT NULL,
 project_id TEXT NOT NULL,
 env_id TEXT NOT NULL,
 verifier BYTEA NOT NULL UNIQUE,
 prefix_hint TEXT NOT NULL,
 parent_session_id TEXT NOT NULL,
 provider_id TEXT NOT NULL,
 oauth2_provider_id TEXT NOT NULL,
 saml_provider_id TEXT NOT NULL,
 auth_method TEXT NOT NULL,
 authority_generation BIGINT NOT NULL,
 credential_epoch BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 revoked_at TIMESTAMPTZ,
 CHECK (expires_at > created_at)
);
CREATE INDEX developer_credentials_authority ON developer_credentials(authority_principal_id);
CREATE TABLE developer_credential_policy (
 id INTEGER PRIMARY KEY CHECK(id=1),
 max_lifetime_seconds BIGINT NOT NULL CHECK(max_lifetime_seconds BETWEEN 1 AND 28800)
);
INSERT INTO developer_credential_policy VALUES(1,28800);

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_purpose_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_purpose_check CHECK (purpose IN ('adapter','reveal','copy','self-config','developer-credential'));

ALTER TABLE cli_reauth_handoffs DROP CONSTRAINT cli_reauth_handoffs_operation_check;
ALTER TABLE cli_reauth_handoffs ADD CONSTRAINT cli_reauth_handoffs_operation_check CHECK (operation IN ('adapter.configure','adapter.credential-set','adapter.adopt','adapter.sync','value.reveal','value.copy-source','self-config.adopt','self-config.apply','self-config.test','developer-credential.mint'));
