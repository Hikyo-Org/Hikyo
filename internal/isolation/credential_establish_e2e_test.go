package isolation

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// This fixture starts with a claimed external identity, not a password that a
// test later deletes. Both protocol callbacks and both database engines run.
func socialCredentialFixture(t *testing.T, db *store.DB, kind string) (*signupHarness, service.LoginResult, func(string) (service.LoginResult, error)) {
	t.Helper()
	h := newSignupHarness(t, db)
	grants := &service.Grants{DB: db}
	invite, err := grants.InviteMember(t.Context(), service.LocalPrincipal(root), service.InviteSpec{Username: "credential-611", Delivery: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if kind == "oauth2" {
		_, f := configureGithub(t, h)
		round := func(purpose, session, proof string) (service.LoginResult, error) {
			start, err := h.auth.OAuth2Start(t.Context(), "github", purpose, "", "", "", session, proof, false)
			if err != nil {
				return service.LoginResult{}, err
			}
			u, err := url.Parse(start.AuthURL)
			if err != nil {
				t.Fatal(err)
			}
			f.challenge = u.Query().Get("code_challenge")
			done, err := h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", start.BindingCookie, session)
			return done.Login, err
		}
		login, err := round("claim", "", invite.Authority)
		if err != nil {
			t.Fatal(err)
		}
		return h, login, func(session string) (service.LoginResult, error) { return round("establish", session, "") }
	}
	configureProvider(t, h.auth, t.Context(), root, "social", service.ProviderInput{DisplayName: "Social OIDC", ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: true})
	round := func(purpose, session, proof string) (service.LoginResult, error) {
		start, err := h.auth.OIDCStart(t.Context(), "social", purpose, "", "", "", session, proof, false)
		if err != nil {
			return service.LoginResult{}, err
		}
		code, state := driveIdP(t, start.AuthURL+"&sub=credential-611")
		done, err := h.auth.OIDCCallback(t.Context(), "social", code, state, "", "", start.BindingCookie, session)
		return done.Login, err
	}
	login, err := round("claim", "", invite.Authority)
	if err != nil {
		t.Fatal(err)
	}
	return h, login, func(session string) (service.LoginResult, error) { return round("establish", session, "") }
}

func assertSocialReissue(t *testing.T, h *signupHarness, login service.LoginResult, kind string) {
	t.Helper()
	if len(login.Assurance.Factors) != 1 || login.Assurance.Factors[0] != kind {
		t.Fatalf("establish invented assurance: %+v", login.Assurance)
	}
	status, err := h.auth.TOTPStatus(t.Context(), login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if status.Establish != nil {
		t.Fatal("reissued session inherited establish stamp")
	}
	if queryInt(t, h.db, "SELECT COUNT(*) FROM credential_establish_evidence") != 0 {
		t.Fatal("spent stamp survived session deletion")
	}
	if queryInt(t, h.db, "SELECT COUNT(*) FROM reauth_windows") != 0 {
		t.Fatal("establish opened a disclosure window")
	}
}

func TestCredentialEstablishPasswordAndRecovery(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				password := "a first independent long password 611"
				if _, err := h.auth.SetPassword(t.Context(), login.SessionToken, password, ""); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("no stamp: %v", err)
				}
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				status, err := h.auth.TOTPStatus(t.Context(), verified.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				if status.Establish == nil || status.Establish.Kind != kind {
					t.Fatalf("status missing stamp: %+v", status)
				}
				ids, err := h.auth.ListIdentities(t.Context(), verified.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.auth.UnlinkIdentity(t.Context(), verified.SessionToken, ids[0].ID, ""); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("stamp authorized unlink: %v", err)
				}
				codes, reissued, err := h.auth.GenerateRecoveryCodes(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				if len(codes) != service.RecoveryBatchSize {
					t.Fatal("missing recovery batch")
				}
				assertSocialReissue(t, h, reissued, kind)
				if _, err := h.auth.Identity(t.Context(), verified.SessionToken); !isUnauth(err) {
					t.Fatalf("old session live: %v", err)
				}
				if _, _, err := h.auth.GenerateRecoveryCodes(t.Context(), reissued.SessionToken, ""); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("second recovery without roundtrip: %v", err)
				}
				verified, err = establish(reissued.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				changed, err := h.auth.SetPassword(t.Context(), verified.SessionToken, password, "")
				if err != nil {
					t.Fatal(err)
				}
				assertSocialReissue(t, h, changed, kind)
				if _, err := establish(changed.SessionToken); !errors.Is(err, service.ErrBadPurpose) {
					t.Fatalf("establish after password: %v", err)
				}
				replacement := "a replacement independent long password 611"
				changed, err = h.auth.SetPassword(t.Context(), changed.SessionToken, replacement, password)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.auth.Identity(t.Context(), changed.SessionToken); err != nil {
					t.Fatal(err)
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.password_changed' AND payload LIKE '%"first":true%'`) != 1 {
					t.Fatal("first password audit missing")
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.password_changed' AND payload LIKE '%"first":false%'`) != 1 {
					t.Fatal("password replacement audit missing")
				}
			})
		})
	}
}

func TestCredentialEstablishTOTPAndExpiry(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				now := time.Now().UTC()
				h.auth.Now = func() time.Time { return now }
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				execRaw(t, db, "UPDATE credential_establish_evidence SET expires_at = '2000-01-01T00:00:00Z'")
				if _, _, err := h.auth.GenerateRecoveryCodes(t.Context(), verified.SessionToken, ""); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("expired stamp: %v", err)
				}
				verified, err = establish(verified.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				now = now.Add(30 * time.Second)
				confirmed, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				assertSocialReissue(t, h, confirmed, kind)
				if _, err := establish(confirmed.SessionToken); !errors.Is(err, service.ErrBadPurpose) {
					t.Fatalf("establish after TOTP: %v", err)
				}
				now = now.Add(30 * time.Second)
				changed, err := h.auth.SetPassword(t.Context(), confirmed.SessionToken, "a long first password after authenticator", totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				if len(changed.Assurance.Factors) != 1 || changed.Assurance.Factors[0] != "totp" {
					t.Fatalf("wrong TOTP proof assurance: %+v", changed.Assurance)
				}
				if _, err := h.auth.Identity(t.Context(), changed.SessionToken); err != nil {
					t.Fatalf("reissued session identity: %v", err)
				}
				// The password-first policy accepts a confirmed TOTP for the first
				// password, but requires the current password for replacement.
				now = now.Add(30 * time.Second)
				replacement := "a replacement password requires the current password"
				if _, err := h.auth.SetPassword(t.Context(), changed.SessionToken, replacement, totpCode(t, uri, now)); !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatalf("TOTP replaced an existing password: %v", err)
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.password_changed'`) != 1 {
					t.Fatal("refused TOTP change emitted a password mutation")
				}
				// The exact rejected code succeeds in the existing TOTP-first
				// recovery operation, proving the password refusal was selection,
				// rather than an invalid or previously spent TOTP step.
				_, withCodes, err := h.auth.GenerateRecoveryCodes(t.Context(), changed.SessionToken, totpCode(t, uri, now))
				if err != nil {
					t.Fatalf("rejected password-change code was not valid TOTP proof: %v", err)
				}
				replaced, err := h.auth.SetPassword(t.Context(), withCodes.SessionToken, replacement, "a long first password after authenticator")
				if err != nil {
					t.Fatalf("current password could not authorize replacement: %v", err)
				}
				if len(replaced.Assurance.Factors) != 1 || replaced.Assurance.Factors[0] != "password" {
					t.Fatalf("replacement lost password proof: %+v", replaced.Assurance)
				}
				if _, err := h.auth.Identity(t.Context(), withCodes.SessionToken); !errors.Is(err, domain.ErrUnauthenticated) {
					t.Fatalf("password replacement left the former session live: %v", err)
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.password_changed' AND payload LIKE '%"first":false%' AND payload LIKE '%"authorizing_credential":"password"%'`) != 1 {
					t.Fatal("password replacement audit did not name the password proof")
				}
			})
		})
	}
}

func TestCredentialEstablishPasskeyAndPurposeWalls(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				// A second login to the same account has no stamp of its own.
				var sibling service.LoginResult
				if kind == "oidc" {
					sibling = oidcLogin(t, h.auth, t.Context(), "social", "credential-611")
				} else {
					f, ok := h.auth.OAuth2HTTPClient.Transport.(*githubFixture)
					if !ok {
						t.Fatal("missing github fixture")
					}
					done, err := githubRoundTrip(t, h, f, "sign-in")
					if err != nil {
						t.Fatal(err)
					}
					sibling = done.Login
				}
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := h.auth.GenerateRecoveryCodes(t.Context(), sibling.SessionToken, ""); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("stamp crossed session: %v", err)
				}
				if kind == "oidc" {
					_, err = h.auth.OIDCStart(t.Context(), "social", "link", "", "", "", verified.SessionToken, "", false)
				} else {
					_, err = h.auth.OAuth2Start(t.Context(), "github", "link", "", "", "", verified.SessionToken, "", false)
				}
				if !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("establish authorized link: %v", err)
				}
				// The RP uses the same origin as the configured provider callback.
				if err := h.auth.ConfigureWebAuthnRP(); err != nil {
					t.Fatal(err)
				}
				dev := webauthntest.New("hikyo.test", "https://hikyo.test")
				options, err := h.auth.EnrolPasskeyStart(t.Context(), verified.SessionToken, "", "")
				if err != nil {
					t.Fatal(err)
				}
				att, err := dev.Enrol(options)
				if err != nil {
					t.Fatal(err)
				}
				enrolled, err := h.auth.EnrolPasskeyFinish(t.Context(), verified.SessionToken, att)
				if err != nil {
					t.Fatal(err)
				}
				assertSocialReissue(t, h, enrolled, kind)
				if _, err := h.auth.Identity(t.Context(), sibling.SessionToken); !isUnauth(err) {
					t.Fatalf("sibling session survived enrollment: %v", err)
				}
				if _, err := establish(enrolled.SessionToken); !errors.Is(err, service.ErrBadPurpose) {
					t.Fatalf("establish after passkey: %v", err)
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.passkey_added' AND payload LIKE '%"authorizing_credential":"establish"%'`) != 1 {
					t.Fatal("passkey proof audit missing")
				}
			})
		})
	}
}

func TestCredentialEstablishWrongIdentityAndLivePrecondition(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				if kind == "oidc" {
					start, err := h.auth.OIDCStart(t.Context(), "social", "establish", "", "", "", login.SessionToken, "", false)
					if err != nil {
						t.Fatal(err)
					}
					code, state := driveIdP(t, start.AuthURL+"&sub=someone-else")
					if _, err := h.auth.OIDCCallback(t.Context(), "social", code, state, "", "", "", login.SessionToken); !isUnauth(err) {
						t.Fatalf("wrong OIDC pair: %v", err)
					}
				} else {
					f, ok := h.auth.OAuth2HTTPClient.Transport.(*githubFixture)
					if !ok {
						t.Fatal("missing github fixture")
					}
					start, err := h.auth.OAuth2Start(t.Context(), "github", "establish", "", "", "", login.SessionToken, "", false)
					if err != nil {
						t.Fatal(err)
					}
					u, _ := url.Parse(start.AuthURL)
					f.challenge = u.Query().Get("code_challenge")
					f.subject++
					if _, err := h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", "", login.SessionToken); !isUnauth(err) {
						t.Fatalf("wrong OAuth pair: %v", err)
					}
					f.subject--
				}
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				// A factor appearing after the callback must close the stamp at the consumer,
				// even if the session itself was not swept by a regressed concurrent writer.
				ids, err := h.auth.ListIdentities(t.Context(), verified.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				execRaw(t, db, `INSERT INTO webauthn_credentials (id,account_id,credential_id,public_key,aaguid,sign_count,transports,discoverable,backup_eligible,backup_state,label,credential_epoch,row_version,created_at) SELECT 'race-key',account_id,`+blobLit(db, []byte("race-credential"))+`,`+blobLit(db, []byte("race-public"))+`,`+blobLit(db, []byte("race-aaguid"))+`,0,'[]',1,0,0,'race',credential_epoch,1,`+ts+` FROM external_identities WHERE id='`+ids[0].ID+`'`)
				if _, _, err := h.auth.GenerateRecoveryCodes(t.Context(), verified.SessionToken, ""); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("stamp bypassed new local proof: %v", err)
				}
			})
		})
	}
}

func TestCredentialEstablishTOTPIdentityAndPasskeyFloor(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				now := time.Now().UTC()
				h.auth.Now = func() time.Time { return now }
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				now = now.Add(30 * time.Second)
				confirmed, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				ids, err := h.auth.ListIdentities(t.Context(), confirmed.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				firstID := ids[0].ID
				now = now.Add(30 * time.Second)
				if _, err := h.auth.UnlinkIdentity(t.Context(), confirmed.SessionToken, firstID, totpCode(t, uri, now)); !errors.Is(err, service.ErrLastCredential) {
					t.Fatalf("TOTP counted as login: %v", err)
				}
				var linked service.OIDCCallbackResult
				if kind == "oidc" {
					start, err := h.auth.OIDCStart(t.Context(), "social", "link", "", "", "", confirmed.SessionToken, totpCode(t, uri, now), false)
					if err != nil {
						t.Fatal(err)
					}
					code, state := driveIdP(t, start.AuthURL+"&sub=second-611")
					linked, err = h.auth.OIDCCallback(t.Context(), "social", code, state, "", "", "", confirmed.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					f, ok := h.auth.OAuth2HTTPClient.Transport.(*githubFixture)
					if !ok {
						t.Fatal("missing fixture")
					}
					f.subject = 99
					start, err := h.auth.OAuth2Start(t.Context(), "github", "link", "", "", "", confirmed.SessionToken, totpCode(t, uri, now), false)
					if err != nil {
						t.Fatal(err)
					}
					u, _ := url.Parse(start.AuthURL)
					f.challenge = u.Query().Get("code_challenge")
					linked, err = h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", "", confirmed.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(linked.Login.Assurance.Factors) != 1 || linked.Login.Assurance.Factors[0] != "totp" {
					t.Fatalf("link lost proof: %+v", linked.Login.Assurance)
				}
				ids, err = h.auth.ListIdentities(t.Context(), linked.Login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				var secondID string
				for _, id := range ids {
					if id.ID != firstID {
						secondID = id.ID
					}
				}
				now = now.Add(30 * time.Second)
				unlinked, err := h.auth.UnlinkIdentity(t.Context(), linked.Login.SessionToken, secondID, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				if err := h.auth.ConfigureWebAuthnRP(); err != nil {
					t.Fatal(err)
				}
				scratch := webauthntest.New("hikyo.test", "https://hikyo.test")
				now = now.Add(30 * time.Second)
				scratchToken := enrolPasskeyWithCode(t, h.auth, t.Context(), unlinked.SessionToken, totpCode(t, uri, now), scratch)
				keys, err := h.auth.ListPasskeys(t.Context(), scratchToken)
				if err != nil || len(keys) != 1 {
					t.Fatalf("scratch key inventory: %+v %v", keys, err)
				}
				now = now.Add(30 * time.Second)
				unlinked, err = h.auth.RemovePasskey(t.Context(), scratchToken, keys[0].ID, "", totpCode(t, uri, now))
				if err != nil {
					t.Fatalf("external login ignored by passkey removal floor: %v", err)
				}
				dev1, dev2 := webauthntest.New("hikyo.test", "https://hikyo.test"), webauthntest.New("hikyo.test", "https://hikyo.test")
				now = now.Add(30 * time.Second)
				token := enrolPasskeyWithCode(t, h.auth, t.Context(), unlinked.SessionToken, totpCode(t, uri, now), dev1)
				now = now.Add(30 * time.Second)
				token = enrolPasskeyWithCode(t, h.auth, t.Context(), token, totpCode(t, uri, now), dev2)
				now = now.Add(30 * time.Second)
				_, reissued, err := h.auth.GenerateRecoveryCodes(t.Context(), token, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				now = now.Add(30 * time.Second)
				final, err := h.auth.UnlinkIdentity(t.Context(), reissued.SessionToken, firstID, totpCode(t, uri, now))
				if err != nil {
					t.Fatalf("valid passkey-only floor ignored: %v", err)
				}
				ids, err = h.auth.ListIdentities(t.Context(), final.SessionToken)
				if err != nil || len(ids) != 0 {
					t.Fatalf("identity remains: %+v %v", ids, err)
				}
				if _, err := discoverableLogin(t, h.auth, t.Context(), dev1); err != nil {
					t.Fatalf("remaining login unavailable: %v", err)
				}
			})
		})
	}
}

// A same-account identity can establish on a session originally minted by a
// different provider. Reconfiguration of the establishing provider must still
// delete that session and stamp, including if the provider is reenabled.
func TestCredentialEstablishProviderRevocation(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				originKind := "oidc"
				if kind == "oidc" {
					originKind = "oauth2"
				}
				h, login, _ := socialCredentialFixture(t, db, originKind)
				var verified service.LoginResult
				var disable, reenable func() error
				if kind == "oauth2" {
					providers, f := configureGithub(t, h)
					execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) SELECT 'establish-other',account_id,'oauth2','https://github.com','42',(SELECT id FROM oauth2_providers WHERE slug='github'),credential_epoch,`+ts+` FROM external_identities LIMIT 1`)
					start, err := h.auth.OAuth2Start(t.Context(), "github", "establish", "", "", "", login.SessionToken, "", false)
					if err != nil {
						t.Fatal(err)
					}
					u, _ := url.Parse(start.AuthURL)
					f.challenge = u.Query().Get("code_challenge")
					done, err := h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", "", login.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
					verified = done.Login
					put := func(enabled bool) error {
						_, err := providers.Put(t.Context(), service.LocalPrincipal(root), "github", service.OAuth2ProviderInput{Profile: "github", DisplayName: "GitHub", Issuer: "https://github.com", ClientID: "client", ClientSecret: "secret", Enabled: enabled})
						return err
					}
					disable = func() error { return put(false) }
					reenable = func() error { return put(true) }
				} else {
					providers, idp := configureProvider(t, h.auth, t.Context(), root, "other", service.ProviderInput{DisplayName: "Other", ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: true})
					execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) SELECT 'establish-other',account_id,'oidc','`+idp.Issuer()+`','other-subject',(SELECT id FROM oidc_providers WHERE slug='other'),credential_epoch,`+ts+` FROM external_identities LIMIT 1`)
					start, err := h.auth.OIDCStart(t.Context(), "other", "establish", "", "", "", login.SessionToken, "", false)
					if err != nil {
						t.Fatal(err)
					}
					code, state := driveIdP(t, start.AuthURL+"&sub=other-subject")
					done, err := h.auth.OIDCCallback(t.Context(), "other", code, state, "", "", "", login.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
					verified = done.Login
					put := func(enabled bool) error {
						_, err := providers.Put(t.Context(), service.LocalPrincipal(root), "other", service.ProviderInput{DisplayName: "Other", Issuer: idp.Issuer(), ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: enabled})
						return err
					}
					disable = func() error { return put(false) }
					reenable = func() error { return put(true) }
				}
				if verified.Assurance.Method != login.Assurance.Method || strings.Join(verified.Assurance.Factors, ",") != strings.Join(login.Assurance.Factors, ",") {
					t.Fatalf("stamp changed login provenance: before=%+v after=%+v", login.Assurance, verified.Assurance)
				}
				if _, err := h.auth.TOTPStatus(t.Context(), verified.SessionToken); err != nil {
					t.Fatal(err)
				}
				if err := disable(); err != nil {
					t.Fatal(err)
				}
				if _, err := h.auth.Identity(t.Context(), verified.SessionToken); !isUnauth(err) {
					t.Fatalf("provider disable left stamp session live: %v", err)
				}
				if err := reenable(); err != nil {
					t.Fatal(err)
				}
				if _, err := h.auth.SetPassword(t.Context(), verified.SessionToken, "a stale stamp must never authorize a password", ""); !isUnauth(err) {
					t.Fatalf("reenable resurrected stamp: %v", err)
				}
				if queryInt(t, db, "SELECT COUNT(*) FROM credential_establish_evidence") != 0 {
					t.Fatal("revocation left stamp")
				}
				// A fresh A login may establish again after B is reenabled. The mutation
				// now reissues from B's actual proof, with B's provider provenance.
				var original service.LoginResult
				if originKind == "oidc" {
					original = oidcLogin(t, h.auth, t.Context(), "social", "credential-611")
				} else {
					f, ok := h.auth.OAuth2HTTPClient.Transport.(*githubFixture)
					if !ok {
						t.Fatal("missing fixture")
					}
					done, err := githubRoundTrip(t, h, f, "sign-in")
					if err != nil {
						t.Fatal(err)
					}
					original = done.Login
				}
				if kind == "oauth2" {
					f, ok := h.auth.OAuth2HTTPClient.Transport.(*githubFixture)
					if !ok {
						t.Fatal("missing fixture")
					}
					start, err := h.auth.OAuth2Start(t.Context(), "github", "establish", "", "", "", original.SessionToken, "", false)
					if err != nil {
						t.Fatal(err)
					}
					u, _ := url.Parse(start.AuthURL)
					f.challenge = u.Query().Get("code_challenge")
					done, err := h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", "", original.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
					verified = done.Login
				} else {
					start, err := h.auth.OIDCStart(t.Context(), "other", "establish", "", "", "", original.SessionToken, "", false)
					if err != nil {
						t.Fatal(err)
					}
					code, state := driveIdP(t, start.AuthURL+"&sub=other-subject")
					done, err := h.auth.OIDCCallback(t.Context(), "other", code, state, "", "", "", original.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
					verified = done.Login
				}
				if verified.Assurance.Method != original.Assurance.Method || strings.Join(verified.Assurance.Factors, ",") != strings.Join(original.Assurance.Factors, ",") {
					t.Fatal("cross-kind callback changed existing assurance")
				}
				_, reissued, err := h.auth.GenerateRecoveryCodes(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				assertSocialReissue(t, h, reissued, kind)
				if kind == "oidc" {
					if queryInt(t, db, "SELECT COUNT(*) FROM sessions s JOIN oidc_providers p ON p.id=s.provider_id WHERE s.id='"+reissued.SessionID+"' AND p.slug='other'") != 1 {
						t.Fatal("mutation lost OIDC establishing provider")
					}
				} else {
					if queryInt(t, db, "SELECT COUNT(*) FROM sessions s JOIN oauth2_providers p ON p.id=s.oauth2_provider_id WHERE s.id='"+reissued.SessionID+"' AND p.slug='github'") != 1 {
						t.Fatal("mutation lost OAuth2 establishing provider")
					}
				}
				if err := disable(); err != nil {
					t.Fatal(err)
				}
				if _, err := h.auth.Identity(t.Context(), reissued.SessionToken); !isUnauth(err) {
					t.Fatalf("establishment reissue survived provider disable: %v", err)
				}

			})
		})
	}
}

func TestCredentialEstablishIgnoresInertLocalProof(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				now := time.Now().UTC()
				h.auth.Now = func() time.Time { return now }
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				now = now.Add(30 * time.Second)
				confirmed, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				// Model restoration reconciliation of the external identity and session while
				// an earlier-epoch TOTP row remains inert in the same account's inventory.
				execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1 WHERE id=1")
				execRaw(t, db, "UPDATE external_identities SET credential_epoch=credential_epoch+1")
				execRaw(t, db, "UPDATE sessions SET credential_epoch=credential_epoch+1")
				status, err := h.auth.TOTPStatus(t.Context(), confirmed.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				if status.Confirmed || status.HasPassword || status.HasPasskey {
					t.Fatalf("inert credential advertised as usable: %+v", status)
				}
				verified, err = establish(confirmed.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				replacementURI, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatalf("inert TOTP blocked replacement: %v", err)
				}
				now = now.Add(30 * time.Second)
				replaced, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, replacementURI, now))
				if err != nil {
					t.Fatal(err)
				}
				assertSocialReissue(t, h, replaced, kind)
				now = now.Add(30 * time.Second)
				if _, err := h.auth.SetPassword(t.Context(), replaced.SessionToken, "a reconciled current local password 611", totpCode(t, replacementURI, now)); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestCredentialEstablishInertPasswordCannotAuthorizeTOTP(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				now := time.Now().UTC()
				h.auth.Now = func() time.Time { return now }
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				withPassword, err := h.auth.SetPassword(t.Context(), verified.SessionToken, "an old epoch password for this regression", "")
				if err != nil {
					t.Fatal(err)
				}
				execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1 WHERE id=1")
				execRaw(t, db, "UPDATE external_identities SET credential_epoch=credential_epoch+1")
				execRaw(t, db, "UPDATE sessions SET credential_epoch=credential_epoch+1")
				verified, err = establish(withPassword.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				execRaw(t, db, "UPDATE credential_establish_evidence SET expires_at='2000-01-01T00:00:00Z'")
				now = now.Add(30 * time.Second)
				if _, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now)); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("inert password bypassed expired stamp: %v", err)
				}
				verified, err = establish(verified.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				confirmed, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
				if err != nil {
					t.Fatal(err)
				}
				assertSocialReissue(t, h, confirmed, kind)
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.factor_enrolled' AND payload LIKE '%"authorizing_credential":"establish"%'`) != 1 {
					t.Fatal("TOTP mislabeled password proof")
				}
			})
		})
	}
}

func TestCredentialEstablishStagedPasskeyExpires(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.auth.ConfigureWebAuthnRP(); err != nil {
					t.Fatal(err)
				}
				dev := webauthntest.New("hikyo.test", "https://hikyo.test")
				options, err := h.auth.EnrolPasskeyStart(t.Context(), verified.SessionToken, "", "")
				if err != nil {
					t.Fatal(err)
				}
				att, err := dev.Enrol(options)
				if err != nil {
					t.Fatal(err)
				}
				execRaw(t, db, "UPDATE credential_establish_evidence SET expires_at='2000-01-01T00:00:00Z'")
				if _, err := h.auth.EnrolPasskeyFinish(t.Context(), verified.SessionToken, att); !errors.Is(err, service.ErrNoProofCredential) {
					t.Fatalf("staging extended stamp TTL: %v", err)
				}
				if queryInt(t, db, "SELECT COUNT(*) FROM webauthn_credentials") != 0 {
					t.Fatal("expired establishment created passkey")
				}
				verified, err = establish(verified.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				_, reissued, err := h.auth.GenerateRecoveryCodes(t.Context(), verified.SessionToken, "")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := h.auth.EnrolPasskeyFinish(t.Context(), reissued.SessionToken, att); err == nil {
					t.Fatal("reissued session inherited staged passkey authorization")
				}
				if queryInt(t, db, "SELECT COUNT(*) FROM webauthn_credentials") != 0 {
					t.Fatal("spent establishment created passkey")
				}
			})
		})
	}
}

func TestCredentialEstablishTOTPSAMLLink(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h, login, establish := socialCredentialFixture(t, db, "oauth2")
		now := time.Now().UTC()
		h.auth.Now = func() time.Time { return now }
		verified, err := establish(login.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(30 * time.Second)
		confirmed, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
		if err != nil {
			t.Fatal(err)
		}
		idp := configureSAMLProvider(t, h.auth, root)
		now = now.Add(30 * time.Second)
		start, err := h.auth.SAMLStart(t.Context(), "saml-idp", "link", "", confirmed.SessionToken, totpCode(t, uri, now))
		if err != nil {
			t.Fatal(err)
		}
		response := samlResponseForStart(t, idp, start, "response-611", "assertion-611")
		linked, err := h.auth.SAMLACS(t.Context(), "saml-idp", response, samlAuditRelayState(t, start.RedirectURL), start.InitiatorCookie)
		if err != nil {
			t.Fatal(err)
		}
		if len(linked.Assurance.Factors) != 1 || linked.Assurance.Factors[0] != "totp" {
			t.Fatalf("SAML link mislabels proof: %+v", linked.Assurance)
		}
		identities, err := h.auth.ListIdentities(t.Context(), linked.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		var samlID string
		for _, identity := range identities {
			if identity.Kind == "saml" {
				samlID = identity.ID
			}
		}
		if samlID == "" {
			t.Fatal("SAML link missing")
		}
		now = now.Add(30 * time.Second)
		if _, err := h.auth.UnlinkIdentity(t.Context(), linked.SessionToken, samlID, totpCode(t, uri, now)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCredentialEstablishCLIPolicyRefusal(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				h.auth.ReauthWindow = 5 * time.Minute
				if kind == "oauth2" {
					now := time.Now().UTC()
					h.auth.Now = func() time.Time { return now }
					verified, err := establish(login.SessionToken)
					if err != nil {
						t.Fatal(err)
					}
					uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
					if err != nil {
						t.Fatal(err)
					}
					now = now.Add(30 * time.Second)
					login, err = h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
					if err != nil {
						t.Fatal(err)
					}
				} else {
					// An OIDC MFA session is eligible for ordinary sliding disclosure, but its
					// provider ceremony cannot satisfy the adapter's exact-purpose proof.
					execRaw(t, db, `UPDATE oidc_providers SET assurance_policy='{"amr_sets":[["otp"]]}' WHERE slug='social'`)
					execRaw(t, db, `UPDATE sessions SET factors='["oidc","mfa"]' WHERE id='`+login.SessionID+`'`)
				}
				grantRead(t, db, login.Principal)
				execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_611_adapter','`+string(login.Principal)+`','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
				execRaw(t, db, `INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ('gor_611_adapter','g_611_adapter','manual','`+string(login.Principal)+`',`+ts+`)`)
				// The CLI artifact shape is fixture material. Login handoff is owned by #612;
				// the real StartCLIReauth authorization, policy and storage path runs below.
				token, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
				if err != nil {
					t.Fatal(err)
				}
				execRaw(t, db, `UPDATE sessions SET artifact='cli',verifier=`+blobLit(db, verifier)+`,csrf_verifier=NULL WHERE id='`+login.SessionID+`'`)
				intent, err := service.NewDisclosureReauthIntent(service.PurposeReveal, []string{"env_a1"}, []string{"key_611"})
				if err != nil {
					t.Fatal(err)
				}
				challenge := strings.Repeat("A", 43)
				if _, err := h.auth.StartCLIReauth(t.Context(), token, intent, challenge, "http://127.0.0.1:40124/callback"); err != nil {
					t.Fatalf("satisfiable sliding handoff refused: %v", err)
				}
				if kind == "oidc" {
					adapter, err := service.NewAdapterReauthIntent("adapter.configure", []string{"env_a1"})
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.auth.StartCLIReauth(t.Context(), token, adapter, challenge, "http://127.0.0.1:40124/callback")
					if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "cannot satisfy step-up") {
						t.Fatalf("OIDC-only adapter opened: %v", err)
					}
				}
				before := queryInt(t, db, "SELECT COUNT(*) FROM cli_reauth_handoffs")
				execRaw(t, db, "UPDATE environments SET protected=TRUE,reauth_window_seconds=0 WHERE id='env_a1'")
				_, err = h.auth.StartCLIReauth(t.Context(), token, intent, challenge, "http://127.0.0.1:40124/callback")
				if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "cannot satisfy step-up") {
					t.Fatalf("unsatisfiable zero-window handoff opened: %v", err)
				}
				if after := queryInt(t, db, "SELECT COUNT(*) FROM cli_reauth_handoffs"); after != before {
					t.Fatal("refused handoff persisted before browser")
				}
			})
		})
	}
}
