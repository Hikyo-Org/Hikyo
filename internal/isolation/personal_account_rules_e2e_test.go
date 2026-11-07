package isolation

import (
	"encoding/base64"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/samlsp"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// Account rules for personal values (personal-values ADR, "Which accounts
// qualify" and "A factor-holding account needs nothing more").

const acctNewPassword = "personal-account-rules-new-password"

func acctEpoch(t *testing.T, db *store.DB) string {
	t.Helper()
	return queryString(t, db, `SELECT CAST(credential_epoch AS TEXT) FROM auth_instance_state WHERE id = 1`)
}

func acctAccountID(t *testing.T, db *store.DB, username string) string {
	t.Helper()
	return queryString(t, db, `SELECT id FROM accounts WHERE username='`+username+`'`)
}

// acctLinkOIDC links an identity under the given OIDC provider slug directly,
// as a completed link or claim would have recorded it.
func acctLinkOIDC(t *testing.T, db *store.DB, id, accountID, issuer, slug string) {
	t.Helper()
	execRaw(t, db, `INSERT INTO external_identities (id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at) VALUES ('`+
		id+`', '`+accountID+`', 'oidc', '`+issuer+`', 'subject-`+id+`', (SELECT id FROM oidc_providers WHERE slug='`+slug+`'), `+acctEpoch(t, db)+`, `+ts+`)`)
}

// acctResetter grants the ceremony owner credential-reset over org A and
// returns them as the network reset actor.
func acctResetter(t *testing.T, db *store.DB, e *personalEnv) service.Actor {
	t.Helper()
	if _, err := (&service.Grants{DB: db}).Create(t.Context(), service.LocalPrincipal(root), service.GrantSpec{
		Target: e.admin.boot.PrincipalID, Capability: domain.CapCredentialReset, Scope: domain.Scope{Org: orgA},
	}); err != nil {
		t.Fatal(err)
	}
	return service.LocalPrincipal(e.admin.boot.PrincipalID)
}

func acctProvider(t *testing.T, e *personalEnv, slug string, policy *string, enabled bool) string {
	t.Helper()
	_, idp := configureProvider(t, e.admin.auth, t.Context(), e.admin.boot.PrincipalID, slug, service.ProviderInput{
		DisplayName: "Corporate IdP " + slug, ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: policy, Enabled: enabled,
	})
	return idp.Issuer()
}

// Network reset refuses an account with a linked identity at mint, even under
// a disabled provider, with the same answer an unknown account gets. The
// refusal mints nothing, leaves the target's sessions alive and audits the
// cause.
func TestPersonalAccountNetworkResetRefusesLinkedIdentityAtMint(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-reset-mint")
		ctx := t.Context()
		target := acctAccountID(t, db, "pv-reset-mint-admin")
		principal := queryString(t, db, `SELECT principal_id FROM accounts WHERE id='`+target+`'`)
		issuer := acctProvider(t, &e, "corp-off", strptr(`{"amr_sets":[["mfa"]]}`), false)
		acctLinkOIDC(t, db, "eid_reset_mint", target, issuer, "corp-off")
		resetter := acctResetter(t, db, &e)

		_, unknownErr := e.admin.auth.ResetCredential(ctx, resetter, "nobody-at-all", "response")
		_, err := e.admin.auth.ResetCredential(ctx, resetter, principal, "response")
		if !errors.Is(err, service.ErrNoResetTarget) || unknownErr == nil || err.Error() != unknownErr.Error() {
			t.Fatalf("linked-identity reset = %v, unknown = %v", err, unknownErr)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id='`+target+`' AND issued_by='credential-reset'`); n != 0 {
			t.Fatalf("refused reset minted %d authorities", n)
		}
		if _, err := e.admin.auth.Identity(ctx, e.adminTok); err != nil {
			t.Fatalf("refused reset revoked the target's session: %v", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.credential_reset_issued' AND outcome='failure' AND payload LIKE '%linked-identity%'`); n != 1 {
			t.Fatalf("linked-identity refusal audited %d times", n)
		}
		// Break-glass is the remaining path for such an account.
		if _, err := e.admin.auth.BreakGlassResetCredential(ctx, principal, "response"); err != nil {
			t.Fatalf("break-glass reset of a linked account: %v", err)
		}
	})
}

// An identity linked after the mint is caught again at consumption: the
// uniform establish refusal, no password written, the authority unspent.
func TestPersonalAccountNetworkResetRefusesLinkedIdentityAtConsumption(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-reset-consume")
		ctx := t.Context()
		target := acctAccountID(t, db, "pv-reset-consume-admin")
		principal := queryString(t, db, `SELECT principal_id FROM accounts WHERE id='`+target+`'`)
		reset, err := e.admin.auth.ResetCredential(ctx, acctResetter(t, db, &e), principal, "response")
		if err != nil {
			t.Fatal(err)
		}
		issuer := acctProvider(t, &e, "corp-late", strptr(`{"amr_sets":[["mfa"]]}`), true)
		acctLinkOIDC(t, db, "eid_reset_consume", target, issuer, "corp-late")
		before := queryString(t, db, `SELECT CAST(row_version AS TEXT) FROM password_credentials WHERE account_id='`+target+`'`)

		if err := e.admin.auth.EstablishCredential(ctx, reset.Authority, acctNewPassword); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("consuming a network reset of a linked account = %v", err)
		}
		if after := queryString(t, db, `SELECT CAST(row_version AS TEXT) FROM password_credentials WHERE account_id='`+target+`'`); after != before {
			t.Fatal("a refused reset replaced the password")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE id='`+reset.AuthorityID+`' AND consumed_at IS NULL`); n != 1 {
			t.Fatal("a refused reset spent its authority")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.credential_authority_refused' AND payload LIKE '%linked-identity%'`); n != 1 {
			t.Fatalf("consumption refusal audited %d times", n)
		}
		if _, err := e.admin.auth.LocalLogin(ctx, "pv-reset-consume-admin", acctNewPassword, service.ArtifactCLI); err == nil {
			t.Fatal("the refused password signs in")
		}
		// A break-glass authority for the same account still establishes.
		glass, err := e.admin.auth.BreakGlassResetCredential(ctx, principal, "response")
		if err != nil {
			t.Fatal(err)
		}
		if err := e.admin.auth.EstablishCredential(ctx, glass.Authority, acctNewPassword); err != nil {
			t.Fatalf("break-glass establish: %v", err)
		}
	})
}

// A factor-holding account keeps its personal values through a network
// reset, and the new password alone reaches none of them.
func TestPersonalAccountFactorHolderKeepsValuesAfterReset(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-reset-keep")
		ctx := t.Context()
		auth := e.admin.auth
		e.adminTok = personalReauth(t, auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Set(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, personalPlain); err != nil {
			t.Fatal(err)
		}
		target := acctAccountID(t, db, "pv-reset-keep-admin")
		principal := queryString(t, db, `SELECT principal_id FROM accounts WHERE id='`+target+`'`)
		reset, err := auth.ResetCredential(ctx, acctResetter(t, db, &e), principal, "response")
		if err != nil {
			t.Fatal(err)
		}
		if err := auth.EstablishCredential(ctx, reset.Authority, acctNewPassword); err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices`) != 0 {
			t.Fatal("a reset of a factor-holding account removed the personal value")
		}

		// The password alone reaches no session that could set or get: the
		// CLI refuses a password login while a factor stands, and the browser
		// answers with a second-factor challenge rather than a session.
		if _, err := auth.LocalLogin(ctx, "pv-reset-keep-admin", acctNewPassword, service.ArtifactCLI); err == nil {
			t.Fatal("a password-only CLI login succeeded on a factor-holding account")
		}
		if browser, err := auth.LocalLogin(ctx, "pv-reset-keep-admin", acctNewPassword, service.ArtifactBrowser); err == nil && browser.SessionToken != "" {
			t.Fatal("a password-only browser login minted a session on a factor-holding account")
		}

		// With the factor, the same value is still there.
		login, err := discoverableLogin(t, auth, ctx, e.adminDev)
		if err != nil {
			t.Fatal(err)
		}
		token := personalReauth(t, auth, login.SessionToken, e.adminDev, e.keyA)
		got, err := e.pv.Reveal(ctx, service.Bearer(token), envScope(envA1), ceremonySecretA)
		if err != nil || got.Value != personalPlain {
			t.Fatalf("owner reveal after reset: err=%v", err)
		}
	})
}

// acctFederated makes the ceremony owner a passwordless, factorless account
// whose only proof is the given OIDC identity: the passkey is disabled and
// the password row moved to an earlier, inert credential epoch.
type acctFederated struct {
	t       *testing.T
	e       *personalEnv
	db      *store.DB
	account string
}

func (f acctFederated) passkey(enabled bool) {
	if enabled {
		execRaw(f.t, f.db, `UPDATE webauthn_credentials SET disabled_at=NULL WHERE account_id='`+f.account+`'`)
		return
	}
	execRaw(f.t, f.db, `UPDATE webauthn_credentials SET disabled_at=`+ts+` WHERE account_id='`+f.account+`'`)
}

func (f acctFederated) passwordCurrent(current bool) {
	epoch := acctEpoch(f.t, f.db)
	if !current {
		epoch += " - 1"
	}
	execRaw(f.t, f.db, `UPDATE password_credentials SET credential_epoch=`+epoch+` WHERE account_id='`+f.account+`'`)
}

// window opens a passkey window for keyA, then presents it as one the "corp"
// identity provider opened, with the passkey hidden again.
func (f acctFederated) window(t *testing.T) {
	t.Helper()
	f.windowVia(t, "corp")
}

// windowVia presents the window as opened by the OIDC provider with the given
// slug: an OIDC reauthentication is only ever opened on a session of that
// same provider, so the session records it.
func (f acctFederated) windowVia(t *testing.T, slug string) {
	t.Helper()
	f.passkey(true)
	f.e.admin.token = personalReauth(t, f.e.admin.auth, f.e.admin.token, f.e.device, f.e.keyA)
	f.passkey(false)
	execRaw(t, f.db, `UPDATE reauth_windows SET factor_class='oidc' WHERE environment_id='env_a1'`)
	execRaw(t, f.db, `UPDATE sessions SET provider_id=(SELECT id FROM oidc_providers WHERE slug='`+slug+
		`') WHERE id IN (SELECT session_id FROM reauth_windows WHERE environment_id='env_a1')`)
}

func newAcctFederated(t *testing.T, db *store.DB, username string) (acctFederated, string) {
	t.Helper()
	e := personalFixture(t, db, username)
	f := acctFederated{t: t, e: &e, db: db, account: e.admin.accountID}
	issuer := acctProvider(t, &e, "corp", strptr(`{"amr_sets":[["mfa"]]}`), true)
	acctLinkOIDC(t, db, "eid_"+username, f.account, issuer, "corp")
	return f, issuer
}

// An account qualifies through its identity provider only while it holds no
// password in the current epoch and no standing factor, and only through an
// OIDC provider with an assurance policy. SAML does not qualify while no SAML
// window can open a personal value.
func TestPersonalAccountFederatedQualification(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := newAcctFederated(t, db, "pv-federated")
		e := f.e
		ctx := t.Context()
		set := func() error {
			_, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain)
			return err
		}

		// A factor holder must use the factor, whatever its identity provider.
		f.passwordCurrent(false)
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		execRaw(t, db, `UPDATE reauth_windows SET factor_class='oidc' WHERE environment_id='env_a1'`)
		if err := set(); !errors.Is(err, service.ErrPersonalFactorRequired) {
			t.Fatalf("factor holder with an identity-provider window: %v", err)
		}

		// Passwordless and factorless: the identity-provider window opens set and get.
		f.window(t)
		if err := set(); err != nil {
			t.Fatalf("qualifying federated set: %v", err)
		}
		f.window(t)
		if got, err := e.pv.Reveal(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA); err != nil || got.Value != personalPlain {
			t.Fatalf("qualifying federated reveal: err=%v", err)
		}

		// Each refusal below is the account not qualifying.
		refused := func(name string) {
			t.Helper()
			f.window(t)
			if err := set(); !errors.Is(err, service.ErrPersonalUnqualified) {
				t.Fatalf("%s: set = %v", name, err)
			}
			if _, err := e.pv.Reveal(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA); !errors.Is(err, service.ErrPersonalUnqualified) {
				t.Fatalf("%s: reveal = %v", name, err)
			}
		}
		f.passwordCurrent(true)
		refused("password in the current epoch")
		f.passwordCurrent(false)

		execRaw(t, db, `UPDATE oidc_providers SET assurance_policy=NULL WHERE slug='corp'`)
		refused("provider without an assurance policy")
		execRaw(t, db, `UPDATE oidc_providers SET assurance_policy='{"amr_sets":[["mfa"]]}' WHERE slug='corp'`)

		execRaw(t, db, `UPDATE oidc_providers SET enabled=0 WHERE slug='corp'`)
		refused("disabled provider")
		execRaw(t, db, `UPDATE oidc_providers SET enabled=1 WHERE slug='corp'`)

		execRaw(t, db, `UPDATE external_identities SET kind='oauth2' WHERE account_id='`+f.account+`'`)
		refused("OAuth2 identity")

		// A SAML identity on an enabled provider with an assurance policy and a
		// subject it accepts would pass the provider's gate, yet does not qualify.
		nameID, err := samlsp.EncodeNameID(samlsp.NameID{Value: []byte("pv-saml-subject")})
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `INSERT INTO saml_providers (id, slug, display_name, kind, entity_id, acs_url, sso_redirect_url, signing_certificates,
			assurance_policy, allow_email_nameid, force_sign_requests, metadata_want_authn_requests_signed, metadata_source, metadata_signed,
			enabled, row_version, created_at, updated_at) VALUES ('prv_pv_saml', 'pv-saml', 'PV SAML', 'saml', 'https://saml.pv.test',
			'https://hikyo.test/acs', 'https://saml.pv.test/sso', '', '{"amr_sets":[["mfa"]]}', 0, 0, 0, 'file', 0, 1, 1, `+ts+`, `+ts+`)`)
		execRaw(t, db, `UPDATE external_identities SET kind='saml', issuer='https://saml.pv.test', provider_id='prv_pv_saml', subject='`+
			base64.RawStdEncoding.EncodeToString(nameID)+`' WHERE account_id='`+f.account+`'`)
		refused("SAML identity")

		// None of these were credential writes: the value is suspended, not deleted.
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("a qualification refusal deleted the value")
		}
	})
}

// A password established on a passwordless account that qualified through
// its identity provider deletes its values in that transaction.
func TestPersonalAccountPasswordOnFederatedAccountPurges(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := newAcctFederated(t, db, "pv-federated-pw")
		e := f.e
		ctx := t.Context()
		f.passwordCurrent(false)
		f.window(t)
		if _, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain); err != nil {
			t.Fatal(err)
		}
		glass, err := e.admin.auth.BreakGlassResetCredential(ctx, string(e.admin.boot.PrincipalID), "response")
		if err != nil {
			t.Fatal(err)
		}
		if err := e.admin.auth.EstablishCredential(ctx, glass.Authority, acctNewPassword); err != nil {
			t.Fatal(err)
		}
		acctPurged(t, db, ceremonySecretA)
	})
}

// Setting a password on a factorless account deletes its values too, here an
// account whose factor vanished without a credential write (suspended).
func TestPersonalAccountSetPasswordOnFactorlessAccountPurges(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-setpw")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		execRaw(t, db, `DELETE FROM webauthn_credentials WHERE account_id='`+e.admin.accountID+`'`)
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("setup: suspension deleted the value")
		}
		if _, err := e.admin.auth.SetPassword(ctx, e.admin.token, acctNewPassword, ceremonyPassword); err != nil {
			t.Fatal(err)
		}
		acctPurged(t, db, ceremonySecretA)
	})
}

func acctPurged(t *testing.T, db *store.DB, keyName string) {
	t.Helper()
	if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values`); n != 0 {
		t.Fatalf("%d personal values survived", n)
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE cause='qualification_lost' AND key_name='`+keyName+`'`) != 1 {
		t.Fatal("no qualification_lost notice")
	}
	if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='personal_value.deleted' AND payload LIKE '%qualification_lost%'`) != 1 {
		t.Fatal("purge not audited")
	}
}

// Removing a passkey while TOTP stands keeps the values; removing TOTP, the
// last factor, then deletes them.
func TestPersonalAccountTOTPRemovalPurges(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-totp")
		ctx := t.Context()
		auth := e.admin.auth
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		clk := time.Now().UTC()
		auth.Now = func() time.Time { return clk }
		defer func() { auth.Now = nil }()
		uri, err := auth.EnrolTOTPStart(ctx, e.admin.token, ceremonyPassword)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := url.Parse(uri); err != nil {
			t.Fatal(err)
		}
		clk = clk.Add(30 * time.Second)
		confirmed, err := auth.EnrolTOTPConfirm(ctx, e.admin.token, totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		keys, err := auth.ListPasskeys(ctx, confirmed.SessionToken)
		if err != nil || len(keys) != 1 {
			t.Fatalf("passkeys: err=%v n=%d", err, len(keys))
		}
		clk = clk.Add(30 * time.Second)
		removed, err := auth.RemovePasskey(ctx, confirmed.SessionToken, keys[0].ID, "", totpCode(t, uri, clk))
		if err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("removing a passkey while TOTP stands deleted the value")
		}
		if _, err := auth.RemoveTOTP(ctx, removed.SessionToken, ceremonyPassword); err != nil {
			t.Fatal(err)
		}
		acctPurged(t, db, ceremonySecretA)
	})
}

// A cloned passkey, when it was the only factor, takes the values with it.
func TestPersonalAccountClonedPasskeyPurges(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-clone")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		execRaw(t, db, `UPDATE webauthn_credentials SET sign_count=1000000, backup_eligible=0 WHERE account_id='`+e.admin.accountID+`'`)
		intent, err := service.NewDisclosureReauthIntent(service.PurposeReveal, []string{string(envA1)}, []string{e.keyA})
		if err != nil {
			t.Fatal(err)
		}
		opts, err := e.admin.auth.ReauthPasskeyStart(ctx, e.admin.token, intent)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := e.device.Assert(opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.auth.ReauthPasskeyFinish(ctx, e.admin.token, resp); err == nil {
			t.Fatal("a cloned passkey reauthenticated")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM webauthn_credentials WHERE account_id='`+e.admin.accountID+`' AND disabled_at IS NOT NULL`) != 1 {
			t.Fatal("setup: the clone was not detected")
		}
		acctPurged(t, db, ceremonySecretA)
	})
}

// A planted recovery code destroys the values and never reads them: after
// consuming it, establishing a password and enrolling a new passkey, the
// holder finds a notice and nothing to reveal.
func TestPersonalAccountPlantedRecoveryCodeNeverReads(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-planted")
		ctx := t.Context()
		auth := e.admin.auth
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		codes, _, err := auth.GenerateRecoveryCodes(ctx, e.admin.token, ceremonyPassword)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := auth.ConsumeRecoveryCode(ctx, "pv-planted", codes[0])
		if err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("recovery consumption kept the value")
		}
		if err := auth.EstablishCredential(ctx, recovered.Authority, acctNewPassword); err != nil {
			t.Fatal(err)
		}
		login, err := auth.LocalLogin(ctx, "pv-planted", acctNewPassword, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		planted := webauthntest.New(ceremonyRPID, ceremonyOrigin)
		token := enrolPasskeyAndStepUp(t, auth, ctx, login.SessionToken, acctNewPassword, planted)
		token = personalReauth(t, auth, token, planted, e.keyA)
		if _, err := e.pv.Reveal(ctx, service.Bearer(token), envScope(envA1), ceremonySecretA); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("reveal after a planted recovery = %v", err)
		}
		entries, err := e.pv.List(ctx, service.Bearer(token), envScope(envA1))
		if err != nil || len(entries) != 1 || entries[0].Set || entries[0].Notice != service.PersonalCauseQualificationLost {
			t.Fatalf("owner list after a planted recovery: err=%v entries=%d", err, len(entries))
		}
	})
}

// A federated window is bound to the provider that opened it: a second linked
// provider the account does not qualify through cannot borrow the
// qualification the first one gives, nor can a window whose opener the
// session does not name.
func TestPersonalAccountFederatedWindowBoundToOpener(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := newAcctFederated(t, db, "pv-federated-opener")
		e := f.e
		ctx := t.Context()
		weak := acctProvider(t, e, "corp-weak", nil, true)
		acctLinkOIDC(t, db, "eid_pv_opener_weak", f.account, weak, "corp-weak")
		f.passwordCurrent(false)
		set := func() error {
			_, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain)
			return err
		}

		f.windowVia(t, "corp-weak")
		if err := set(); !errors.Is(err, service.ErrPersonalFactorRequired) {
			t.Fatalf("window opened by a non-qualifying provider: set = %v", err)
		}
		f.window(t)
		execRaw(t, db, `UPDATE sessions SET provider_id=NULL WHERE id IN (SELECT session_id FROM reauth_windows WHERE environment_id='env_a1')`)
		if err := set(); !errors.Is(err, service.ErrPersonalFactorRequired) {
			t.Fatalf("window whose session names no provider: set = %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("a refused window stored a value")
		}
		f.window(t)
		if err := set(); err != nil {
			t.Fatalf("window opened by the qualifying provider: set = %v", err)
		}
	})
}

// Unlinking an identity re-checks qualification in the same transaction, as
// every other credential write does. Here the account's passkey vanished
// without a credential write, so its value is suspended; the unlink deletes it.
func TestPersonalAccountUnlinkPurgesUnqualified(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-unlink")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		issuer := acctProvider(t, &e, "corp", strptr(`{"amr_sets":[["mfa"]]}`), true)
		acctLinkOIDC(t, db, "eid_pv_unlink", e.admin.accountID, issuer, "corp")
		execRaw(t, db, `DELETE FROM webauthn_credentials WHERE account_id='`+e.admin.accountID+`'`)
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("setup: suspension deleted the value")
		}
		if _, err := e.admin.auth.UnlinkIdentity(ctx, e.admin.token, "eid_pv_unlink", ceremonyPassword); err != nil {
			t.Fatal(err)
		}
		acctPurged(t, db, ceremonySecretA)
	})
}

// A passwordless, factorless account that qualifies through its identity
// provider cannot unlink that identity at all: the unlink needs a password or
// TOTP proof, which such an account does not hold. Its value stays.
func TestPersonalAccountFederatedUnlinkNeedsProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f, _ := newAcctFederated(t, db, "pv-federated-unlink")
		e := f.e
		ctx := t.Context()
		f.passwordCurrent(false)
		f.window(t)
		if _, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain); err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.auth.UnlinkIdentity(ctx, e.admin.token, "eid_pv-federated-unlink", ceremonyPassword); !errors.Is(err, service.ErrNoProofCredential) {
			t.Fatalf("federated unlink = %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM external_identities WHERE account_id='`+f.account+`'`) != 1 ||
			queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("a refused unlink removed the identity or the value")
		}
	})
}

// Erasing a privacy subject deletes the subject's personal values and notices
// in the erasure transaction and leaves no notice behind; another owner's
// value and notice are untouched.
func TestPersonalAccountPrivacyEraseDeletesValuesAndNotices(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-erase")
		ctx := t.Context()
		auth := e.admin.auth
		subject := string(e.admin.boot.PrincipalID)
		other := queryString(t, db, `SELECT principal_id FROM accounts WHERE username='pv-erase-admin'`)
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		e.adminTok = personalReauth(t, auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Set(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, personalPlain); err != nil {
			t.Fatal(err)
		}
		for _, owner := range []string{subject, other} {
			execRaw(t, db, `INSERT INTO personal_value_notices (org_id, project_id, environment_id, key_name, owner_principal_id, cause, created_at) VALUES ('org_a', 'prj_a1', 'env_a1', 'PV_ERASE_NOTICE', '`+
				owner+`', '`+service.PersonalCauseAdministrator+`', `+ts+`)`)
		}

		if _, err := auth.ApplyPrivacySubject(ctx, subject, "erase", ""); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE owner_principal_id='`+subject+`'`); n != 0 {
			t.Fatalf("%d personal values survived the erasure", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE owner_principal_id='`+subject+`'`); n != 0 {
			t.Fatalf("%d personal value notices survived the erasure", n)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='personal_value.deleted' AND payload LIKE '%"cause":"erasure"%' AND payload LIKE '%"count":1%'`) != 1 {
			t.Fatal("the erasure did not audit the personal value it deleted")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE owner_principal_id='`+other+`'`) != 1 ||
			queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE owner_principal_id='`+other+`'`) != 1 {
			t.Fatal("erasure touched another owner's value or notice")
		}
	})
}
