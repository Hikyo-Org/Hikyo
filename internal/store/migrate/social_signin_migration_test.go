package migrate

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// TestSocialSigninMigrationSQLite verifies the 00057 upgrade on SQLite.
func TestSocialSigninMigrationSQLite(t *testing.T) {
	testSocialSigninMigration(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "social.db")})
}

// TestSocialSigninMigrationPostgres verifies the same upgrade on PostgreSQL.
func TestSocialSigninMigrationPostgres(t *testing.T) {
	testSocialSigninMigration(t, postgresTestConfig(t, "social_signin"))
}

type socialEmailFixture struct{ name, before, after string }

// socialEmailFixtures are 00049 contact values and what 00057 keeps, always
// unverified: the canonical form of a provably valid address, otherwise NULL.
// Duplicates are kept; unverified values are contact data, not a unique key.
func socialEmailFixtures() []socialEmailFixture {
	local64 := strings.Repeat("l", 64)
	domain189 := strings.Repeat("d", 63) + "." + strings.Repeat("d", 63) + "." + strings.Repeat("d", 61)
	return []socialEmailFixture{
		{"empty", "", ""},
		{"mixed-case domain", "Keep.Me+tag@Example.COM", "Keep.Me+tag@example.com"},
		{"plain", "plain@example.org", "plain@example.org"},
		{"254 bytes", local64 + "@" + domain189, local64 + "@" + domain189},
		{"255 bytes", local64 + "@" + domain189 + "d", ""},
		{"display name", "Name <name@example.com>", ""},
		{"no at sign", "no-at-sign", ""},
		{"two at signs", "a@b@example.com", ""},
		{"trailing dot", "dot@example.com.", ""},
		{"non-ASCII domain", "user@bücher.example", ""},
		{"quoted local", `"quoted"@example.com`, ""},
		{"leading dot", ".lead@example.com", ""},
		{"double dot", "a..b@example.com", ""},
		{"empty local", "@example.com", ""},
		{"empty domain", "nodomain@", ""},
		{"space", "sp ace@example.com", ""},
		{"duplicate pair 1", "dup@example.com", "dup@example.com"},
		{"duplicate pair 2", "dup@example.com", "dup@example.com"},
		{"case-variant domain 1", "Case@Example.com", "Case@example.com"},
		{"case-variant domain 2", "Case@EXAMPLE.COM", "Case@example.com"},
		{"local case is significant 1", "Local@example.net", "Local@example.net"},
		{"local case is significant 2", "local@example.net", "local@example.net"},
	}
}

// socialSQL rewrites the engine-neutral placeholders: {bN} is a distinct
// one-byte blob, {t}/{f} a boolean literal for a BOOLEAN column on postgres.
func socialSQL(cfg store.Config, stmt string) string {
	pairs := []string{}
	for i := 1; i <= 40; i++ {
		blob := fmt.Sprintf("X'%02x'", i)
		if cfg.Engine == store.EnginePostgres {
			blob = fmt.Sprintf("decode('%02x','hex')", i)
		}
		pairs = append(pairs, fmt.Sprintf("{b%d}", i), blob)
	}
	if cfg.Engine == store.EnginePostgres {
		pairs = append(pairs, "{t}", "TRUE", "{f}", "FALSE")
	} else {
		pairs = append(pairs, "{t}", "1", "{f}", "0")
	}
	return strings.NewReplacer(pairs...).Replace(stmt)
}

// sqlValue renders a nullable string literal for the shared SQL fixtures.
func sqlValue(v string) string {
	if v == "" {
		return "NULL"
	}
	return "'" + v + "'"
}

const socialTS = "'2026-01-01T00:00:00Z'"

// socialPolicy builds a policy row with the supplied landing and org scope.
func socialPolicy(id, orgID, landing, template, freshOrgCap string) string {
	capValue := "NULL"
	if freshOrgCap != "" {
		capValue = freshOrgCap
	}
	return "INSERT INTO registration_policies (id,org_id,authority_principal_id,landing,template,local_enabled,fresh_org_cap,row_version,created_at,updated_at) VALUES (" +
		sqlValue(id) + "," + sqlValue(orgID) + ",'prn_social'," + sqlValue(landing) + "," + sqlValue(template) + ",{f}," + capValue + ",1," + socialTS + "," + socialTS + ")"
}

// socialTx builds an oidc_transactions (table "oidc") or oauth2_transactions
// (table "oauth2") row; the binding columns follow bindingKind so only the
// field under test varies.
func socialTx(table, id, blob, purpose, intent, scope, bindingKind, account, environment, ceremony, authority string) string {
	session, binding := "NULL", "NULL"
	if bindingKind == "session" {
		session = "'ses_social'"
	} else {
		binding = "{b" + blob + "}"
	}
	if table == "oauth2" {
		return "INSERT INTO oauth2_transactions (id,state_verifier,pkce_verifier,provider_id,issuer,redirect_uri,purpose,intent,signup_scope_org_id,binding_kind,initiating_session_id,browser_binding_verifier,account_id,authority_id,ceremony_id,credential_epoch,created_at,expires_at) VALUES (" +
			sqlValue(id) + ",{b" + blob + "},'pkce','prv_oauth2','https://github.com','https://hikyo.test/cb'," + sqlValue(purpose) + "," + sqlValue(intent) + "," + sqlValue(scope) + "," +
			sqlValue(bindingKind) + "," + session + "," + binding + "," + sqlValue(account) + "," + sqlValue(authority) + "," + sqlValue(ceremony) + ",1," + socialTS + "," + socialTS + ")"
	}
	return "INSERT INTO oidc_transactions (id,state_verifier,nonce,pkce_verifier,provider_id,issuer,redirect_uri,purpose,intent,signup_scope_org_id,binding_kind,initiating_session_id,browser_binding_verifier,account_id,environment_id,ceremony_id,authority_id,browser,credential_epoch,created_at,expires_at) VALUES (" +
		sqlValue(id) + ",{b" + blob + "},{b1},'pkce','prv_oidc','https://idp.test','https://hikyo.test/cb'," + sqlValue(purpose) + "," + sqlValue(intent) + "," + sqlValue(scope) + "," +
		sqlValue(bindingKind) + "," + session + "," + binding + "," + sqlValue(account) + "," + sqlValue(environment) + "," + sqlValue(ceremony) + "," + sqlValue(authority) + ",{f},1," + socialTS + "," + socialTS + ")"
}

// socialSession builds a session row to exercise the provider one-of CHECK.
func socialSession(id, blob, oidc, saml, oauth2 string) string {
	return "INSERT INTO sessions (id,principal_id,verifier,artifact,session_generation,credential_epoch,auth_method,factors,authenticated_at,created_at,last_seen_at,idle_expires_at,absolute_expires_at,source_ip,user_agent,provider_id,saml_provider_id,oauth2_provider_id) VALUES (" +
		sqlValue(id) + ",'prn_social',{b" + blob + "},'browser',1,1,'password','[\"password\"]'," + socialTS + "," + socialTS + "," + socialTS + "," + socialTS + "," + socialTS + ",'192.0.2.1','agent'," +
		sqlValue(oidc) + "," + sqlValue(saml) + "," + sqlValue(oauth2) + ")"
}

// socialAuthority builds a credential authority row with a chosen issuer.
func socialAuthority(id, blob, issuedBy, kind string) string {
	return "INSERT INTO credential_authorities (id,verifier,account_id,purpose,issued_by,established_credential_kind,credential_epoch,expires_at,created_at) VALUES (" +
		sqlValue(id) + ",{b" + blob + "},'acc_social','establish-credential'," + sqlValue(issuedBy) + "," + sqlValue(kind) + ",1," + socialTS + "," + socialTS + ")"
}

// testSocialSigninMigration upgrades a populated 00056 database to only 00057,
// preserving existing rows while checking legacy writers and new constraints.
func testSocialSigninMigration(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 56); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	exec := func(label, stmt string) error {
		_, err := db.ExecContext(ctx, socialSQL(cfg, stmt))
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return nil
	}
	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, socialSQL(cfg, query)).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}

	// The state 00057 inherits: a live oidc session carrying 00056's enrolment
	// flag, an open reauth window and a CLI handoff hanging off it, a pending
	// invitation authority, a granted origin, a linked identity, and an
	// in-flight link transaction.
	for i, stmt := range []string{
		"INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_social','social',{t},'{}'," + socialTS + ")",
		"INSERT INTO principals (id,kind,created_at) VALUES ('prn_social','human'," + socialTS + ")",
		"INSERT INTO accounts (id,principal_id,username,display_name,created_at) VALUES ('acc_social','prn_social','social','Social'," + socialTS + ")",
		"INSERT INTO oidc_providers (id,slug,display_name,kind,issuer,client_id,client_secret,scopes,redirect_uri,enabled,dek_version,row_version,created_at,updated_at) VALUES ('prv_oidc','corp','Corp','oidc','https://idp.test','client',{b2},'openid','https://hikyo.test/cb',1,1,1," + socialTS + "," + socialTS + ")",
		"INSERT INTO sessions (id,principal_id,verifier,artifact,session_generation,credential_epoch,auth_method,factors,authenticated_at,created_at,last_seen_at,idle_expires_at,absolute_expires_at,source_ip,user_agent,provider_id,enrolment_required) VALUES ('ses_social','prn_social',{b3},'browser',1,1,'oidc:https://idp.test','[\"federated\"]'," + socialTS + "," + socialTS + "," + socialTS + "," + socialTS + "," + socialTS + ",'192.0.2.1','agent','prv_oidc',{t})",
		"INSERT INTO reauth_windows (id,session_id,environment_id,ceremony_id,factor_class,single_decision,authenticated_at,window_expires_at,hard_expires_at,credential_epoch,created_at) VALUES ('rw_social','ses_social','env_x','cer_x','oidc',0," + socialTS + "," + socialTS + "," + socialTS + ",1," + socialTS + ")",
		"INSERT INTO cli_reauth_handoffs (id,state_verifier,session_id,principal_id,purpose,operation,environment_set,key_set,pkce_challenge,redirect_uri,created_at,expires_at) VALUES ('clh_social',{b4},'ses_social','prn_social','adapter','adapter.sync','[]','','challenge','http://127.0.0.1/cb'," + socialTS + "," + socialTS + ")",
		"INSERT INTO credential_authorities (id,verifier,account_id,purpose,issued_by,credential_epoch,expires_at,created_at) VALUES ('cra_social',{b5},'acc_social','establish-credential','invitation',1," + socialTS + "," + socialTS + ")",
		"INSERT INTO grants (id,principal_id,capability,created_at) VALUES ('grt_social','prn_social','read'," + socialTS + ")",
		"INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gro_social','grt_social','manual','prn_social'," + socialTS + ")",
		"INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('eid_social','acc_social','oidc','https://idp.test','Subject','prv_oidc',1," + socialTS + ")",
		"INSERT INTO oidc_transactions (id,state_verifier,nonce,pkce_verifier,provider_id,issuer,redirect_uri,purpose,binding_kind,initiating_session_id,account_id,environment_id,ceremony_id,credential_epoch,created_at,expires_at) VALUES ('otx_old',{b6},{b1},'pkce','prv_oidc','https://idp.test','https://hikyo.test/cb','link','session','ses_social','acc_social','','cer_old',1," + socialTS + "," + socialTS + ")",
	} {
		if err := exec(fmt.Sprintf("seed %d", i), stmt); err != nil {
			t.Fatal(err)
		}
	}

	// 00049's contact emails, which 00057 keeps as unverified contact data.
	for i, c := range socialEmailFixtures() {
		stmt := fmt.Sprintf("INSERT INTO principals (id,kind,created_at) VALUES ('prn_e%d','human',%s)", i, socialTS)
		if err := exec("email principal", stmt); err != nil {
			t.Fatal(err)
		}
		stmt = fmt.Sprintf("INSERT INTO accounts (id,principal_id,username,display_name,created_at,email) VALUES ('acc_e%d','prn_e%d','e%d','E',%s,'%s')",
			i, i, i, socialTS, strings.ReplaceAll(c.before, "'", "''"))
		if err := exec("email account "+c.before, stmt); err != nil {
			t.Fatal(err)
		}
	}

	// Pinned to 00057: 00058 (#607) then makes a login's intent required,
	// which this file checks at its end against the rows accepted here.
	if err := RunUpTo(ctx, cfg, 57); err != nil {
		t.Fatal(err)
	}

	for i, c := range socialEmailFixtures() {
		var got sql.NullString
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT email FROM accounts WHERE id = 'acc_e%d'", i)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.Valid != (c.after != "") || got.String != c.after {
			t.Errorf("%s: email %q became %v, want %q", c.name, c.before, got, c.after)
			continue
		}
		// Whatever the SQL keeps, the Go canonicalizer agrees with.
		if got.Valid {
			if canonical, err := domain.CanonicalEmail(c.before); err != nil || canonical != got.String {
				t.Errorf("%s: SQL kept %q, CanonicalEmail(%q) = %q, %v", c.name, got.String, c.before, canonical, err)
			}
		}
	}
	for label, stmt := range map[string]string{
		"the pre-existing account has no email": "SELECT COUNT(*) FROM accounts WHERE id='acc_social' AND email IS NULL",
	} {
		if n := count(stmt); n != 1 {
			t.Fatalf("%s: %d", label, n)
		}
	}
	// No legacy value is verified: none is a login identifier or unique key.
	if n := count("SELECT COUNT(*) FROM accounts WHERE email_verified_at IS NOT NULL"); n != 0 {
		t.Fatalf("00057 marked %d legacy emails verified", n)
	}
	// Uniqueness is over the canonical form of verified values only; NULLs and
	// unverified duplicates coexist; '' is not a value; verified needs an email.
	for i, stmt := range []string{
		"INSERT INTO principals (id,kind,created_at) VALUES ('prn_n1','human'," + socialTS + ")",
		"INSERT INTO principals (id,kind,created_at) VALUES ('prn_n2','human'," + socialTS + ")",
		"INSERT INTO principals (id,kind,created_at) VALUES ('prn_n3','human'," + socialTS + ")",
		"INSERT INTO accounts (id,principal_id,username,display_name,created_at) VALUES ('acc_n1','prn_n1','n1','N'," + socialTS + ")",
		"INSERT INTO accounts (id,principal_id,username,display_name,created_at,email) VALUES ('acc_n2','prn_n2','n2','N'," + socialTS + ",NULL)",
	} {
		if err := exec(fmt.Sprintf("null email %d", i), stmt); err != nil {
			t.Fatal(err)
		}
	}
	if n := count("SELECT COUNT(*) FROM accounts WHERE id IN ('acc_n1','acc_n2') AND email IS NULL"); n != 2 {
		t.Fatalf("new accounts default to NULL email: %d of 2", n)
	}
	canonical, err := domain.CanonicalEmail("Keep.Me+tag@EXAMPLE.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"n4", "n5", "n6"} {
		if err := exec("principal "+id, "INSERT INTO principals (id,kind,created_at) VALUES ('prn_"+id+"','human',"+socialTS+")"); err != nil {
			t.Fatal(err)
		}
	}
	account := func(id, email, verifiedAt string) string {
		return "INSERT INTO accounts (id,principal_id,username,display_name,created_at,email,email_verified_at) VALUES ('acc_" + id + "','prn_" + id + "','" + id + "','N'," + socialTS + "," + email + "," + verifiedAt + ")"
	}
	// Another unverified holder of a legacy address is accepted: unverified
	// values are not a uniqueness key.
	if err := exec("unverified duplicate", account("n3", "'"+canonical+"'", "NULL")); err != nil {
		t.Errorf("a second unverified holder of %q was refused: %v", canonical, err)
	}
	// Nobody is blocked by the unverified holders: the first verified holder of
	// the address is accepted beside them.
	if err := exec("verified beside unverified", account("n4", "'"+canonical+"'", socialTS)); err != nil {
		t.Errorf("an unverified holder blocked verifying %q: %v", canonical, err)
	}
	// A second verified holder, arriving in another case of the domain and
	// canonicalized as every writer must, is refused.
	again, err := domain.CanonicalEmail("Keep.Me+tag@example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if exec("verified canonical duplicate", account("n5", "'"+again+"'", socialTS)) == nil {
		t.Error("a second account with the same verified canonical email was accepted")
	}
	if err := exec("verified without email", account("n5", "NULL", socialTS)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check constraint") {
		t.Errorf("a verification time without an email was not refused by a CHECK: %v", err)
	}
	if err := exec("empty email", account("n6", "''", "NULL")); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check constraint") {
		t.Errorf("an empty email was not refused by a CHECK: %v", err)
	}

	for query, want := range map[string]int{
		// Sessions survive the rebuild with every column carried forward.
		"SELECT COUNT(*) FROM sessions WHERE id='ses_social' AND enrolment_required={t} AND provider_id='prv_oidc' AND oauth2_provider_id IS NULL AND user_agent='agent'": 1,
		// Every open reauth window is closed (release note), handoffs are not.
		"SELECT COUNT(*) FROM reauth_windows":                                    0,
		"SELECT COUNT(*) FROM cli_reauth_handoffs WHERE session_id='ses_social'": 1,
		"SELECT COUNT(*) FROM credential_authorities WHERE id='cra_social' AND issued_by='invitation' AND established_credential_kind='password'": 1,
		"SELECT COUNT(*) FROM grant_origins WHERE id='gro_social' AND kind='manual'":                                                              1,
		"SELECT COUNT(*) FROM external_identities WHERE id='eid_social' AND kind='oidc' AND subject='Subject'":                                    1,
		// Minutes-lived transactions are purged (an empty environment_id on a
		// link row would violate the exhaustive CHECK).
		"SELECT COUNT(*) FROM oidc_transactions": 0,
		"SELECT COUNT(*) FROM orgs WHERE id='org_social' AND origin='manual' AND registration_policy_id IS NULL": 1,
	} {
		if got := count(query); got != want {
			t.Fatalf("%s = %d, want %d", query, got, want)
		}
	}

	// The sqlite rebuild runs with foreign keys off; nothing may dangle after.
	if cfg.Engine == store.EngineSQLite {
		rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		dangling := rows.Next()
		_ = rows.Close()
		if dangling {
			t.Fatal("foreign_key_check reported a violation after 00057")
		}
	}

	// Fixtures the new tables reference.
	for i, stmt := range []string{
		"INSERT INTO oauth2_providers (id,slug,display_name,kind,profile,issuer,client_id,client_secret,redirect_uri,enabled,dek_version,row_version,created_at,updated_at) VALUES ('prv_oauth2','github','GitHub','oauth2','github','https://github.com','client',{b7},'https://hikyo.test/oauth2/cb',1,1,1," + socialTS + "," + socialTS + ")",
		"INSERT INTO saml_providers (id,slug,display_name,kind,entity_id,acs_url,sso_redirect_url,signing_certificates,allow_email_nameid,force_sign_requests,metadata_want_authn_requests_signed,metadata_source,metadata_signed,enabled,row_version,created_at,updated_at) VALUES ('prv_saml','saml','SAML','saml','https://saml.test','https://hikyo.test/acs','https://saml.test/sso',{b8},0,0,0,'file',0,1,1," + socialTS + "," + socialTS + ")",
	} {
		if err := exec(fmt.Sprintf("fixture %d", i), stmt); err != nil {
			t.Fatal(err)
		}
	}

	// Every refusal is a CHECK refusal on an otherwise valid row: the message
	// is asserted so a foreign-key or uniqueness failure cannot pass for one.
	refused := []struct{ label, stmt string }{
		{"org policy without template", socialPolicy("rp_r1", "org_social", "org-template", "", "")},
		{"instance policy with template", socialPolicy("rp_r2", "", "none", "member", "")},
		{"fresh_org_cap without fresh-org landing", socialPolicy("rp_r3", "", "none", "", "5")},
		{"fresh-org landing without fresh_org_cap", socialPolicy("rp_r4", "", "fresh-org", "", "")},
		{"non-positive fresh_org_cap", socialPolicy("rp_r5", "", "fresh-org", "", "0")},
		{"intent on a link transaction", socialTx("oidc", "otx_r1", "10", "link", "sign-in", "", "session", "acc_social", "", "cer_r1", "")},
		{"intent on a reauth transaction", socialTx("oidc", "otx_r2", "11", "reauth", "sign-up", "", "session", "acc_social", "env_x", "", "")},
		{"signup scope on a link transaction", socialTx("oidc", "otx_r3", "12", "link", "", "org_social", "session", "acc_social", "", "cer_r3", "")},
		{"signup scope on a sign-in login", socialTx("oidc", "otx_r4", "13", "login", "sign-in", "org_social", "browser-cookie", "", "", "", "")},
		{"login bound to a session", socialTx("oidc", "otx_r5", "14", "login", "", "", "session", "", "", "", "")},
		{"link without ceremony", socialTx("oidc", "otx_r6", "15", "link", "", "", "session", "acc_social", "", "", "")},
		{"authority on a link", socialTx("oidc", "otx_r7", "16", "link", "", "", "session", "acc_social", "", "cer_r7", "cra_social")},
		{"authority on a login", socialTx("oidc", "otx_r8", "17", "login", "", "", "browser-cookie", "", "", "", "cra_social")},
		{"claim without authority", socialTx("oidc", "otx_r9", "18", "claim", "", "", "browser-cookie", "acc_social", "", "", "")},
		{"environment on a link", socialTx("oidc", "otx_r10", "19", "link", "", "", "session", "acc_social", "env_x", "cer_r10", "")},
		{"oauth2 login without intent", socialTx("oauth2", "o2x_r1", "20", "login", "", "", "browser-cookie", "", "", "", "")},
		{"oauth2 login bound to a session", socialTx("oauth2", "o2x_r2", "21", "login", "sign-in", "", "session", "", "", "", "")},
		{"oauth2 reauth", socialTx("oauth2", "o2x_r3", "22", "reauth", "", "", "session", "acc_social", "", "", "")},
		{"oauth2 authority without claim", socialTx("oauth2", "o2x_r4", "23", "establish", "", "", "session", "acc_social", "", "", "cra_social")},
		{"oidc and oauth2 provider on one session", socialSession("ses_r1", "24", "prv_oidc", "", "prv_oauth2")},
		{"saml and oauth2 provider on one session", socialSession("ses_r2", "25", "", "prv_saml", "prv_oauth2")},
		{"oidc and saml provider on one session", socialSession("ses_r3", "26", "prv_oidc", "prv_saml", "")},
		{"recovery authority establishing oidc", socialAuthority("cra_r1", "27", "recovery", "oidc")},
		{"unknown established credential kind", socialAuthority("cra_r2", "28", "invitation", "saml")},
		{"unknown grant origin", "INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gro_r1','grt_social','bogus','x'," + socialTS + ")"},
		{"unknown identity kind", "INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('eid_r1','acc_social','github','https://github.com','1','prv_oauth2',1," + socialTS + ")"},
		{"unknown org origin", "INSERT INTO orgs (id,name,active,metadata,created_at,origin) VALUES ('org_r1','r1',{t},'{}'," + socialTS + ",'imported')"},
		{"uppercase allowlist domain", "INSERT INTO registration_policy_domains (policy_id,domain) VALUES ('rp_ok_instance','Example.com')"},
		{"email as an allowlist claim", "INSERT INTO registration_policy_entries (id,policy_id,provider_kind,provider_id,claim,created_at) VALUES ('rpe_r1','rp_ok_instance','oidc','prv_oidc','email'," + socialTS + ")"},
		{"empty claim value", "INSERT INTO registration_policy_entry_values (entry_id,value) VALUES ('rpe_ok','')"},
	}
	accepted := []struct{ label, stmt string }{
		{"org policy", socialPolicy("rp_ok_org", "org_social", "org-template", "member", "")},
		{"instance fresh-org policy", socialPolicy("rp_ok_instance", "", "fresh-org", "", "5")},
		{"allowlist domain", "INSERT INTO registration_policy_domains (policy_id,domain) VALUES ('rp_ok_instance','example.com')"},
		{"policy entry", "INSERT INTO registration_policy_entries (id,policy_id,provider_kind,provider_id,claim,created_at) VALUES ('rpe_ok','rp_ok_instance','oidc','prv_oidc','groups'," + socialTS + ")"},
		{"entry value", "INSERT INTO registration_policy_entry_values (entry_id,value) VALUES ('rpe_ok','staff')"},
		{"pending signup", "INSERT INTO registration_signups (id,email,token_verifier,policy_id,signup_scope_org_id,credential_epoch,created_at,expires_at) VALUES ('rsu_ok','user@example.com',{b29},'rp_ok_org','org_social',1," + socialTS + "," + socialTS + ")"},
		{"today's login writer (no intent)", socialTx("oidc", "otx_ok1", "30", "login", "", "", "browser-cookie", "", "", "", "")},
		{"sign-up login with org scope", socialTx("oidc", "otx_ok2", "31", "login", "sign-up", "org_social", "browser-cookie", "", "", "", "")},
		{"today's link writer", socialTx("oidc", "otx_ok3", "32", "link", "", "", "session", "acc_social", "", "cer_ok3", "")},
		{"today's reauth writer", socialTx("oidc", "otx_ok4", "33", "reauth", "", "", "session", "acc_social", "env_x", "", "")},
		{"establish", socialTx("oidc", "otx_ok5", "34", "establish", "", "", "session", "acc_social", "", "", "")},
		{"claim", socialTx("oidc", "otx_ok6", "35", "claim", "", "", "browser-cookie", "acc_social", "", "", "cra_social")},
		{"oauth2 sign-in login", socialTx("oauth2", "o2x_ok1", "36", "login", "sign-in", "", "browser-cookie", "", "", "", "")},
		{"oauth2 claim", socialTx("oauth2", "o2x_ok2", "37", "claim", "", "", "browser-cookie", "acc_social", "", "", "cra_social")},
		{"oauth2 session", socialSession("ses_ok1", "38", "", "", "prv_oauth2")},
		{"invitation authority establishing oidc", socialAuthority("cra_ok1", "39", "invitation", "oidc")},
		{"recovery authority establishing a password", socialAuthority("cra_ok2", "40", "recovery", "password")},
		{"registration grant origin", "INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gro_ok','grt_social','registration','prn_social'," + socialTS + ")"},
		{"oauth2 identity", "INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('eid_ok','acc_social','oauth2','https://github.com','1','prv_oauth2',1," + socialTS + ")"},
		{"registration org", "INSERT INTO orgs (id,name,active,metadata,created_at,origin,registration_policy_id) VALUES ('org_ok','ok',{t},'{}'," + socialTS + ",'registration','rp_ok_instance')"},
	}
	for _, c := range accepted {
		if err := exec(c.label, c.stmt); err != nil {
			t.Fatalf("valid row refused: %v", err)
		}
	}
	for _, c := range refused {
		err := exec(c.label, c.stmt)
		if err == nil {
			t.Errorf("%s: accepted, want a CHECK refusal", c.label)
			continue
		}
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "check constraint") {
			t.Errorf("%s: refused by something other than a CHECK: %v", c.label, err)
		}
	}

	// One instance policy, one policy per org.
	if exec("second instance policy", socialPolicy("rp_dup", "", "none", "", "")) == nil {
		t.Error("a second instance policy was accepted")
	}
	if exec("second org policy", socialPolicy("rp_dup2", "org_social", "org-template", "member", "")) == nil {
		t.Error("a second policy for one org was accepted")
	}

	// 00058, the registration switch (#607): in-flight transactions are
	// purged (the accepted rows above include a NULL-intent login), and a
	// login must now record its intent. Every other purpose is unchanged.
	if n := count("SELECT COUNT(*) FROM oidc_transactions"); n == 0 {
		t.Fatal("the 00058 fixture needs in-flight transactions to purge")
	}
	if err := RunUpTo(ctx, cfg, 58); err != nil {
		t.Fatal(err)
	}
	if n := count("SELECT COUNT(*) FROM oidc_transactions"); n != 0 {
		t.Fatalf("00058 left %d in-flight OIDC transactions", n)
	}
	if n := count("SELECT COUNT(*) FROM oauth2_transactions"); n == 0 {
		t.Fatal("00058 touched oauth2_transactions")
	}
	for _, c := range []struct{ label, stmt string }{
		{"sign-in login", socialTx("oidc", "otx_s1", "30", "login", "sign-in", "", "browser-cookie", "", "", "", "")},
		{"sign-up login with org scope", socialTx("oidc", "otx_s2", "31", "login", "sign-up", "org_social", "browser-cookie", "", "", "", "")},
		{"link", socialTx("oidc", "otx_s3", "32", "link", "", "", "session", "acc_social", "", "cer_s3", "")},
		{"reauth", socialTx("oidc", "otx_s4", "33", "reauth", "", "", "session", "acc_social", "env_x", "", "")},
		{"establish", socialTx("oidc", "otx_s5", "34", "establish", "", "", "session", "acc_social", "", "", "")},
		{"claim", socialTx("oidc", "otx_s6", "35", "claim", "", "", "browser-cookie", "acc_social", "", "", "cra_social")},
	} {
		if err := exec(c.label, c.stmt); err != nil {
			t.Fatalf("after 00058, valid row refused: %v", err)
		}
	}
	for _, c := range []struct{ label, stmt string }{
		{"login without intent", socialTx("oidc", "otx_s7", "36", "login", "", "", "browser-cookie", "", "", "", "")},
		{"signup scope on a sign-in login", socialTx("oidc", "otx_s8", "37", "login", "sign-in", "org_social", "browser-cookie", "", "", "", "")},
		{"intent on a link", socialTx("oidc", "otx_s9", "38", "link", "sign-in", "", "session", "acc_social", "", "cer_s9", "")},
	} {
		err := exec(c.label, c.stmt)
		if err == nil {
			t.Errorf("after 00058, %s: accepted, want a CHECK refusal", c.label)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "check constraint") {
			t.Errorf("after 00058, %s: refused by something other than a CHECK: %v", c.label, err)
		}
	}
}

// TestSocialSigninEmailValidityParitySQLite exercises the email conversion
// against SQLite's GLOB-based rules.
func TestSocialSigninEmailValidityParitySQLite(t *testing.T) {
	testSocialSigninEmailValidityParity(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "parity.db")})
}

// TestSocialSigninEmailValidityParityPostgres exercises the same conversion
// against PostgreSQL's regex-based rules.
func TestSocialSigninEmailValidityParityPostgres(t *testing.T) {
	testSocialSigninEmailValidityParity(t, postgresTestConfig(t, "email_parity"))
}

// testSocialSigninEmailValidityParity verifies both engines keep the same
// legacy contact values, matching domain.CanonicalEmail for every kept value.
func testSocialSigninEmailValidityParity(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	cases := []struct {
		in   string
		want string // canonical kept value, "" = NULL
	}{
		{"back`tick@example.com", "back`tick@example.com"},
		{"{|}~@example.com", "{|}~@example.com"},
		{"!#$%&'*+/=?^_-@example.com", "!#$%&'*+/=?^_-@example.com"},
		{"dollar$sign@example.com", "dollar$sign@example.com"},
		{"caret^hat@example.com", "caret^hat@example.com"},
		{"under_score@example.com", "under_score@example.com"},
		{"dash-start@-example.com", "dash-start@-example.com"},
		{"dash-end@example-.com", "dash-end@example-.com"},
		{"digits@123.456", "digits@123.456"},
		{"single@Label", "single@label"},
		{"Upper@EXAMPLE.COM", "Upper@example.com"},
		{"a.b.c@x.y.z", "a.b.c@x.y.z"},
		{"domain-underscore@ex_ample.com", ""}, // Go admits it; the SQL stays conservative
		{"literal@[192.0.2.1]", ""},
		{"paren(s)@example.com", ""},
		{`back\slash@example.com`, ""},
		{"comma,s@example.com", ""},
		{"semi;colon@example.com", ""},
		{"colon:s@example.com", ""},
		{"angle<s@example.com", ""},
		{"tab\t@example.com", ""},
		{"space in@example.com", ""},
		{"dom@exa mple.com", ""},
		{"dom@example..com", ""},
		{"trail.@example.com", ""},
		{".lead2@example.com", ""},
		{"ümlaut@example.com", ""},
		{"idn@bücher.example", ""},
		{"two@@example.com", ""},
		{"dom@.", ""},
		{"dom@example.com.", ""},
		{"plus+tag@example.com", "plus+tag@example.com"},
	}
	if err := RunUpTo(ctx, cfg, 56); err != nil {
		t.Fatal(err)
	}
	db := migrationFixtureSQL(t, cfg)
	for i, c := range cases {
		for _, stmt := range []string{
			fmt.Sprintf("INSERT INTO principals (id,kind,created_at) VALUES ('prn_p%d','human',%s)", i, socialTS),
			fmt.Sprintf("INSERT INTO accounts (id,principal_id,username,display_name,created_at,email) VALUES ('acc_p%d','prn_p%d','p%d','P',%s,'%s')",
				i, i, i, socialTS, strings.ReplaceAll(c.in, "'", "''")),
		} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("seed %q: %v", c.in, err)
			}
		}
	}
	if err := RunUpTo(ctx, cfg, 57); err != nil {
		t.Fatal(err)
	}
	var verified int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE email_verified_at IS NOT NULL").Scan(&verified); err != nil || verified != 0 {
		t.Fatalf("%s: %d legacy emails became verified: %v", cfg.Engine, verified, err)
	}
	for i, c := range cases {
		var got sql.NullString
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT email FROM accounts WHERE id = 'acc_p%d'", i)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.Valid != (c.want != "") || got.String != c.want {
			t.Errorf("%s: %q became %v, want %q", cfg.Engine, c.in, got, c.want)
			continue
		}
		if got.Valid {
			if canonical, err := domain.CanonicalEmail(c.in); err != nil || canonical != got.String {
				t.Errorf("%s: SQL kept %q but CanonicalEmail(%q) = %q, %v", cfg.Engine, got.String, c.in, canonical, err)
			}
		}
	}
}
