package isolation

import (
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/samltest"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// Two real linked identities and a confirmed TOTP leave a local proof session
// alive when an administrator disables either external sign-in provider.
func linkedCredentialAvailabilityFixture(t *testing.T, db *store.DB, kind string) (*signupHarness, service.LoginResult, string, string, *time.Time, func(bool)) {
	t.Helper()
	originKind := "oauth2"
	if kind == "oauth2" {
		originKind = "oidc"
	}
	h, login, establish := socialCredentialFixture(t, db, originKind)
	now := time.Now().UTC()
	h.auth.Now = func() time.Time { return now }
	verified, err := establish(login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	identities, err := h.auth.ListIdentities(t.Context(), verified.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	originalID := identities[0].ID
	uri, err := h.auth.EnrolTOTPStart(t.Context(), verified.SessionToken, "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	confirmed, err := h.auth.EnrolTOTPConfirm(t.Context(), verified.SessionToken, totpCode(t, uri, now))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	var linked service.LoginResult
	var toggle func(bool)
	switch kind {
	case "oidc":
		providers, idp := configureProvider(t, h.auth, t.Context(), root, "survivor", service.ProviderInput{DisplayName: "Surviving OIDC", ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: true})
		start, err := h.auth.OIDCStart(t.Context(), "survivor", "link", "", "", "", confirmed.SessionToken, totpCode(t, uri, now), false)
		if err != nil {
			t.Fatal(err)
		}
		code, state := driveIdP(t, start.AuthURL+"&sub=surviving-identity")
		done, err := h.auth.OIDCCallback(t.Context(), "survivor", code, state, "", "", "", confirmed.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		linked = done.Login
		toggle = func(enabled bool) {
			t.Helper()
			_, err := providers.Put(t.Context(), service.LocalPrincipal(root), "survivor", service.ProviderInput{DisplayName: "Surviving OIDC", Issuer: idp.Issuer(), ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: enabled})
			if err != nil {
				t.Fatal(err)
			}
		}
	case "oauth2":
		providers, f := configureGithub(t, h)
		start, err := h.auth.OAuth2Start(t.Context(), "github", "link", "", "", "", confirmed.SessionToken, totpCode(t, uri, now), false)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(start.AuthURL)
		if err != nil {
			t.Fatal(err)
		}
		f.challenge = u.Query().Get("code_challenge")
		done, err := h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", "", confirmed.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		linked = done.Login
		toggle = func(enabled bool) {
			t.Helper()
			_, err := providers.Put(t.Context(), service.LocalPrincipal(root), "github", service.OAuth2ProviderInput{Profile: "github", DisplayName: "GitHub", Issuer: "https://github.com", ClientID: "client", ClientSecret: "secret", Enabled: enabled})
			if err != nil {
				t.Fatal(err)
			}
		}
	case "saml":
		idp := configureSAMLProvider(t, h.auth, root)
		start, err := h.auth.SAMLStart(t.Context(), "saml-idp", "link", "", confirmed.SessionToken, totpCode(t, uri, now))
		if err != nil {
			t.Fatal(err)
		}
		response := samlResponseForStart(t, idp, start, "response-availability", "assertion-availability")
		linked, err = h.auth.SAMLACS(t.Context(), "saml-idp", response, samlAuditRelayState(t, start.RedirectURL), start.InitiatorCookie)
		if err != nil {
			t.Fatal(err)
		}
		providers := &service.SAMLProviders{DB: db, Keyring: h.auth.Keyring, ExternalOrigin: h.auth.ExternalOrigin}
		toggle = func(enabled bool) {
			t.Helper()
			metadata, err := idp.SignedMetadata(time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			input := service.SAMLProviderInput{DisplayName: "SAML test IdP", EntityID: samltest.EntityID, MetadataSource: "file", MetadataDocument: metadata, Enabled: enabled}
			preview, err := providers.Put(t.Context(), service.LocalPrincipal(root), "saml-idp", input)
			if err != nil {
				t.Fatal(err)
			}
			if !preview.Applied {
				input.ConfirmedFingerprints = preview.RequiredFingerprints
				input.ConfirmedEndpoints = preview.RequiredEndpoints
				result, err := providers.Put(t.Context(), service.LocalPrincipal(root), "saml-idp", input)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Applied {
					t.Fatal("provider toggle not applied")
				}
			}
		}
	default:
		t.Fatalf("unsupported fixture kind %q", kind)
	}
	return h, linked, originalID, uri, &now, toggle
}

func TestUnavailableExternalIdentityCannotAuthorizeUnlink(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2", "saml"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, originalID, uri, now, toggle := linkedCredentialAvailabilityFixture(t, db, kind)
				toggle(false)
				if _, err := h.auth.Identity(t.Context(), login.SessionToken); err != nil {
					t.Fatalf("local-proof session lost during provider disable: %v", err)
				}
				*now = now.Add(30 * time.Second)
				code := totpCode(t, uri, *now)
				if _, err := h.auth.UnlinkIdentity(t.Context(), login.SessionToken, originalID, code); !errors.Is(err, service.ErrLastCredential) {
					t.Fatalf("disabled provider counted as surviving login: %v", err)
				}
				identities, err := h.auth.ListIdentities(t.Context(), login.SessionToken)
				if err != nil || len(identities) != 2 {
					t.Fatalf("refused unlink changed inventory: %+v %v", identities, err)
				}
				toggle(true)
				var remaining service.ExternalIdentityView
				for _, identity := range identities {
					if identity.ID != originalID {
						remaining = identity
					}
				}
				execRaw(t, db, "UPDATE external_identities SET issuer=issuer||'-mismatch' WHERE id='"+remaining.ID+"'")
				if _, err := h.auth.UnlinkIdentity(t.Context(), login.SessionToken, originalID, code); !errors.Is(err, service.ErrLastCredential) {
					t.Fatalf("mismatched provider issuer counted as login: %v", err)
				}
				execRaw(t, db, "UPDATE external_identities SET issuer='"+remaining.Issuer+"' WHERE id='"+remaining.ID+"'")
				if kind != "saml" {
					execRaw(t, db, "UPDATE external_identities SET provider_id='retired-provider' WHERE id='"+remaining.ID+"'")
					if _, err := h.auth.UnlinkIdentity(t.Context(), login.SessionToken, originalID, code); !errors.Is(err, service.ErrLastCredential) {
						t.Fatalf("unreconciled provider identity counted as login: %v", err)
					}
					execRaw(t, db, "UPDATE external_identities SET provider_id='"+remaining.ProviderID+"' WHERE id='"+remaining.ID+"'")
				} else {
					expiry := queryString(t, db, "SELECT COALESCE(CAST(metadata_valid_until AS TEXT),'') FROM saml_providers WHERE slug='saml-idp'")
					execRaw(t, db, "UPDATE saml_providers SET metadata_valid_until='2000-01-01T00:00:00Z' WHERE slug='saml-idp'")
					if _, err := h.auth.UnlinkIdentity(t.Context(), login.SessionToken, originalID, code); !errors.Is(err, service.ErrLastCredential) {
						t.Fatalf("expired SAML metadata counted as login: %v", err)
					}
					restoredExpiry := "NULL"
					if expiry != "" {
						restoredExpiry = "'" + expiry + "'"
					}
					execRaw(t, db, "UPDATE saml_providers SET metadata_valid_until="+restoredExpiry+" WHERE slug='saml-idp'")
					// Recreate the same SAML entity with a new provider row.
					// Its login callback can rebind the retained identity, so
					// this supported sign-in must still satisfy the predicate.
					providers := &service.SAMLProviders{DB: db, Keyring: h.auth.Keyring, ExternalOrigin: h.auth.ExternalOrigin}
					if err := providers.Delete(t.Context(), service.LocalPrincipal(root), "saml-idp"); err != nil {
						t.Fatal(err)
					}
					configureSAMLProvider(t, h.auth, root)
					if id := queryString(t, db, "SELECT id FROM saml_providers WHERE slug='saml-idp'"); id == remaining.ProviderID {
						t.Fatal("SAML recreation reused the historical provider row")
					}
					if id := queryString(t, db, "SELECT provider_id FROM external_identities WHERE id='"+remaining.ID+"'"); id != remaining.ProviderID {
						t.Fatal("fixture unexpectedly reconciled SAML identity before checking eligibility")
					}
				}
				if _, err := h.auth.UnlinkIdentity(t.Context(), login.SessionToken, originalID, code); err != nil {
					t.Fatalf("enabled provider did not count as surviving login: %v", err)
				}
			})
		})
	}
}

func TestUnavailableExternalIdentityCannotAuthorizePasskeyRemoval(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2", "saml"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, originalID, uri, now, toggle := linkedCredentialAvailabilityFixture(t, db, kind)
				if err := h.auth.ConfigureWebAuthnRP(); err != nil {
					t.Fatal(err)
				}
				dev := webauthntest.New("hikyo.test", "https://hikyo.test")
				*now = now.Add(30 * time.Second)
				token := enrolPasskeyWithCode(t, h.auth, t.Context(), login.SessionToken, totpCode(t, uri, *now), dev)
				*now = now.Add(30 * time.Second)
				unlinked, err := h.auth.UnlinkIdentity(t.Context(), token, originalID, totpCode(t, uri, *now))
				if err != nil {
					t.Fatal(err)
				}
				keys, err := h.auth.ListPasskeys(t.Context(), unlinked.SessionToken)
				if err != nil || len(keys) != 1 {
					t.Fatalf("passkey inventory: %+v %v", keys, err)
				}
				toggle(false)
				*now = now.Add(30 * time.Second)
				code := totpCode(t, uri, *now)
				if _, err := h.auth.RemovePasskey(t.Context(), unlinked.SessionToken, keys[0].ID, "", code); !errors.Is(err, service.ErrPasskeyOnlyViolation) {
					t.Fatalf("disabled provider justified removing last passkey: %v", err)
				}
				keys, err = h.auth.ListPasskeys(t.Context(), unlinked.SessionToken)
				if err != nil || len(keys) != 1 {
					t.Fatalf("refused removal changed passkeys: %+v %v", keys, err)
				}
				toggle(true)
				if _, err := h.auth.RemovePasskey(t.Context(), unlinked.SessionToken, keys[0].ID, "", code); err != nil {
					t.Fatalf("enabled provider did not justify removal: %v", err)
				}
			})
		})
	}
}

func TestRestoredPasswordEstablishmentAuditsFirstCurrentCredential(t *testing.T) {
	for _, kind := range []string{"oidc", "oauth2"} {
		t.Run(kind, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, establish := socialCredentialFixture(t, db, kind)
				verified, err := establish(login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				first, err := h.auth.SetPassword(t.Context(), verified.SessionToken, "a password before the credential epoch changes", "")
				if err != nil {
					t.Fatal(err)
				}
				version := queryInt(t, db, "SELECT row_version FROM password_credentials")
				execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1 WHERE id=1")
				execRaw(t, db, "UPDATE external_identities SET credential_epoch=credential_epoch+1")
				execRaw(t, db, "UPDATE sessions SET credential_epoch=credential_epoch+1")
				verified, err = establish(first.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				currentPassword := "a password reconciled into the current credential epoch"
				established, err := h.auth.SetPassword(t.Context(), verified.SessionToken, currentPassword, "")
				if err != nil {
					t.Fatal(err)
				}
				if queryInt(t, db, "SELECT COUNT(*) FROM password_credentials") != 1 || queryInt(t, db, "SELECT row_version FROM password_credentials") != version+1 {
					t.Fatal("restored verifier must be replaced by CAS in its existing row")
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.password_changed' AND payload LIKE '%"first":true%' AND payload LIKE '%"authorizing_credential":"establish"%'`) != 2 {
					t.Fatal("establishing the first current-epoch password was audited as replacement")
				}
				if _, err := h.auth.SetPassword(t.Context(), established.SessionToken, "a later replacement in the same live epoch", currentPassword); err != nil {
					t.Fatal(err)
				}
				if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.password_changed' AND payload LIKE '%"first":false%' AND payload LIKE '%"authorizing_credential":"password"%'`) != 1 {
					t.Fatal("current-epoch replacement must retain first false")
				}
			})
		})
	}
}

func putSAMLAvailabilityPolicy(t *testing.T, h *signupHarness, idp *samltest.IdP, allowEmail bool) {
	t.Helper()
	metadata, err := idp.SignedMetadata(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	providers := &service.SAMLProviders{DB: h.db, Keyring: h.auth.Keyring, ExternalOrigin: h.auth.ExternalOrigin}
	input := service.SAMLProviderInput{DisplayName: "SAML test IdP", EntityID: samltest.EntityID, MetadataSource: "file", MetadataDocument: metadata, Enabled: true, AllowEmailNameID: allowEmail}
	preview, err := providers.Put(t.Context(), service.LocalPrincipal(root), "saml-idp", input)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Applied {
		input.ConfirmedFingerprints = preview.RequiredFingerprints
		input.ConfirmedEndpoints = preview.RequiredEndpoints
		result, err := providers.Put(t.Context(), service.LocalPrincipal(root), "saml-idp", input)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Applied {
			t.Fatal("SAML NameID policy not applied")
		}
	}
}

func TestSAMLNameIDPolicyCannotJustifyCredentialRemoval(t *testing.T) {
	for _, operation := range []string{"unlink", "passkey-removal"} {
		t.Run(operation, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				h, login, originalID, uri, now, _ := linkedCredentialAvailabilityFixture(t, db, "saml")
				identities, err := h.auth.ListIdentities(t.Context(), login.SessionToken)
				if err != nil {
					t.Fatal(err)
				}
				var persistentID string
				for _, identity := range identities {
					if identity.Kind == "saml" {
						persistentID = identity.ID
					}
				}
				providers := &service.SAMLProviders{DB: db, Keyring: h.auth.Keyring, ExternalOrigin: h.auth.ExternalOrigin}
				if err := providers.Delete(t.Context(), service.LocalPrincipal(root), "saml-idp"); err != nil {
					t.Fatal(err)
				}
				idp := configureSAMLProvider(t, h.auth, root)
				putSAMLAvailabilityPolicy(t, h, idp, true)
				*now = now.Add(30 * time.Second)
				start, err := h.auth.SAMLStart(t.Context(), "saml-idp", "link", "", login.SessionToken, totpCode(t, uri, *now))
				if err != nil {
					t.Fatal(err)
				}
				request, err := samltest.DecodeRequest(start.RedirectURL)
				if err != nil {
					t.Fatal(err)
				}
				response, err := idp.SignedResponse(samltest.Response{RequestID: request.ID, ResponseID: "response-email-policy", AssertionID: "assertion-email-policy", ACSURL: "https://hikyo.test/api/v1/auth/saml/saml-idp/acs", SPEntityID: "https://hikyo.test/api/v1/auth/saml", NameID: "email-identity@example.test", NameIDFormat: "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress", Now: time.Now().UTC()})
				if err != nil {
					t.Fatal(err)
				}
				linked, err := h.auth.SAMLACS(t.Context(), "saml-idp", response, samlAuditRelayState(t, start.RedirectURL), start.InitiatorCookie)
				if err != nil {
					t.Fatal(err)
				}
				*now = now.Add(30 * time.Second)
				linked, err = h.auth.UnlinkIdentity(t.Context(), linked.SessionToken, persistentID, totpCode(t, uri, *now))
				if err != nil {
					t.Fatal(err)
				}
				var keyID string
				if operation == "passkey-removal" {
					if err := h.auth.ConfigureWebAuthnRP(); err != nil {
						t.Fatal(err)
					}
					*now = now.Add(30 * time.Second)
					token := enrolPasskeyWithCode(t, h.auth, t.Context(), linked.SessionToken, totpCode(t, uri, *now), webauthntest.New("hikyo.test", "https://hikyo.test"))
					*now = now.Add(30 * time.Second)
					linked, err = h.auth.UnlinkIdentity(t.Context(), token, originalID, totpCode(t, uri, *now))
					if err != nil {
						t.Fatal(err)
					}
					keys, err := h.auth.ListPasskeys(t.Context(), linked.SessionToken)
					if err != nil || len(keys) != 1 {
						t.Fatalf("key inventory: %+v %v", keys, err)
					}
					keyID = keys[0].ID
				}
				putSAMLAvailabilityPolicy(t, h, idp, false)
				*now = now.Add(30 * time.Second)
				code := totpCode(t, uri, *now)
				if operation == "unlink" {
					if _, err := h.auth.UnlinkIdentity(t.Context(), linked.SessionToken, originalID, code); !errors.Is(err, service.ErrLastCredential) {
						t.Fatalf("disabled email NameID justified unlink: %v", err)
					}
				} else {
					if _, err := h.auth.RemovePasskey(t.Context(), linked.SessionToken, keyID, "", code); !errors.Is(err, service.ErrPasskeyOnlyViolation) {
						t.Fatalf("disabled email NameID justified passkey removal: %v", err)
					}
				}
				putSAMLAvailabilityPolicy(t, h, idp, true)
				if operation == "unlink" {
					if _, err := h.auth.UnlinkIdentity(t.Context(), linked.SessionToken, originalID, code); err != nil {
						t.Fatalf("admitted email NameID did not count: %v", err)
					}
				} else {
					if _, err := h.auth.RemovePasskey(t.Context(), linked.SessionToken, keyID, "", code); err != nil {
						t.Fatalf("admitted email NameID did not count: %v", err)
					}
				}
			})
		})
	}
}
