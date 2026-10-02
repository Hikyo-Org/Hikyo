package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The fixture enforces PKCE rather than merely accepting its parameters. The
// production profile still names github.com/api.github.com; only HTTP is replaced.
type githubFixture struct {
	challenge  string
	subject    int64
	emails     int
	userError  bool
	emailError bool
	verified   bool
	onUser     func()
	onEmail    func()
}

func (f *githubFixture) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	status := http.StatusOK
	switch req.URL.Path {
	case "/login/oauth/access_token":
		b, _ := io.ReadAll(req.Body)
		q, _ := url.ParseQuery(string(b))
		hash := sha256.Sum256([]byte(q.Get("code_verifier")))
		if q.Get("redirect_uri") == "" || base64.RawURLEncoding.EncodeToString(hash[:]) != f.challenge || q.Get("code") != "oauth-code" || req.Header.Get("Accept") != "application/json" {
			status = 400
			body = `{"error":"invalid_grant"}`
		} else {
			body = `{"access_token":"callback-token","token_type":"bearer","refresh_token":"discard-me"}`
		}
	case "/user":
		if f.onUser != nil {
			f.onUser()
		}
		if req.Header.Get("Authorization") != "Bearer callback-token" {
			status = 401
		}
		if f.userError {
			body = `{"login":"mutable","node_id":"opaque"}`
		} else {
			b, _ := json.Marshal(map[string]any{"id": f.subject, "login": "mutable", "node_id": "opaque", "two_factor_authentication": true, "name": "GitHub user"})
			body = string(b)
		}
	case "/user/emails":
		f.emails++
		if f.onEmail != nil {
			f.onEmail()
		}
		if f.emailError {
			status = 500
			body = `{}`
		} else {
			b, _ := json.Marshal([]map[string]any{{"email": "primary@example.com", "primary": true, "verified": f.verified}, {"email": "other@example.com", "primary": false, "verified": true}})
			body = string(b)
		}
	default:
		return nil, errors.New("unexpected profile request")
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}
func (f *githubFixture) start(t *testing.T, auth *service.Auth, intent string) service.OIDCStartResult {
	t.Helper()
	start, err := auth.OAuth2Start(t.Context(), "github", "login", intent, "", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(start.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("scope") != "user:email" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("unpinned auth URL: %s", u)
	}
	f.challenge = u.Query().Get("code_challenge")
	return start
}
func configureGithub(t *testing.T, h *signupHarness) (*service.OAuth2Providers, *githubFixture) {
	t.Helper()
	f := &githubFixture{subject: 42, verified: true}
	h.auth.OAuth2HTTPClient = &http.Client{Transport: f}
	p := &service.OAuth2Providers{DB: h.db, Keyring: h.auth.Keyring, ExternalOrigin: "https://hikyo.test"}
	_, err := p.Put(t.Context(), service.LocalPrincipal(root), "github", service.OAuth2ProviderInput{Profile: "github", DisplayName: "GitHub", Issuer: "https://github.com", ClientID: "client", ClientSecret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return p, f
}
func githubRoundTrip(t *testing.T, h *signupHarness, f *githubFixture, intent string) (service.OIDCCallbackResult, error) {
	t.Helper()
	start := f.start(t, h.auth, intent)
	return h.auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", start.BindingCookie, "")
}
func TestOAuth2Identity(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newSignupHarness(t, db)
		p, f := configureGithub(t, h)
		ctx := t.Context()
		if _, err := githubRoundTrip(t, h, f, "sign-in"); !isUnauth(err) {
			t.Fatalf("unknown sign-in: %v", err)
		}
		if f.emails != 0 {
			t.Fatal("sign-in fetched email")
		}
		entry := service.RegistrationExternalEntry{Provider: domain.ProviderRef{Kind: domain.ProviderOAuth2, Slug: "github"}}
		if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{entry}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		result, err := githubRoundTrip(t, h, f, "sign-up")
		if err != nil {
			t.Fatal(err)
		}
		if result.Login.Assurance.Method != "oauth2:https://github.com" || len(result.Login.Assurance.Factors) != 1 || result.Login.Assurance.Factors[0] != "oauth2" {
			t.Fatalf("wrong assurance: %+v", result.Login.Assurance)
		}
		if f.emails != 1 {
			t.Fatalf("signup email requests: %d", f.emails)
		}
		for _, intent := range []string{"sign-in", "sign-up"} {
			if _, err := githubRoundTrip(t, h, f, intent); err != nil {
				t.Fatal(err)
			}
		}
		if f.emails != 1 {
			t.Fatal("known login fetched email")
		}
		identity, err := h.auth.Identity(ctx, result.Login.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		for _, cap := range []string{"read", "reveal"} {
			execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_oauth2_`+cap+`','`+string(identity.Principal)+`','`+cap+`','org_a','prj_a1','env_a1',`+ts+`)`)
		}
		values := &service.Values{DB: db, Keyring: h.auth.Keyring}
		if _, err := values.List(ctx, service.Bearer(result.Login.SessionToken), domain.Scope{Org: "org_a", Project: "prj_a1", Env: "env_a1"}, true); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("GitHub upstream 2FA conferred reveal: %v", err)
		}
		ids, err := h.auth.ListIdentities(ctx, result.Login.SessionToken)
		if err != nil || len(ids) != 1 || ids[0].Subject != "42" || ids[0].Profile != "github" {
			t.Fatalf("numeric identity: %+v %v", ids, err)
		}
		if _, err := h.auth.OAuth2Start(ctx, "github", "reauth", "", "", "env", result.Login.SessionToken, "", false); !errors.Is(err, service.ErrOAuth2Reauth) {
			t.Fatalf("reauth: %v", err)
		}
		bad := f.start(t, h.auth, "sign-in")
		f.challenge = "wrong"
		if _, err := h.auth.OAuth2Callback(ctx, "github", "oauth-code", bad.State, "", "", bad.BindingCookie, ""); !isUnauth(err) {
			t.Fatalf("bad PKCE: %v", err)
		}
		bad = f.start(t, h.auth, "sign-in")
		if _, err := h.auth.OAuth2Callback(ctx, "other", "oauth-code", bad.State, "", "", bad.BindingCookie, ""); !isUnauth(err) {
			t.Fatalf("mixup: %v", err)
		}
		configureProvider(t, h.auth, ctx, root, "oidc-mixup", service.ProviderInput{DisplayName: "OIDC", ClientID: "client", ClientSecret: "secret", Scopes: "openid", Enabled: true})
		oidcStart, err := h.auth.OIDCStart(ctx, "oidc-mixup", "login", "sign-in", "", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		code, state := driveIdP(t, oidcStart.AuthURL)
		if _, err := h.auth.OAuth2Callback(ctx, "github", code, state, "", "", oidcStart.BindingCookie, ""); !isUnauth(err) {
			t.Fatalf("OIDC state accepted at OAuth2 callback: %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.oauth2_refused' AND payload LIKE '%"cause":"mixup"%'`) < 2 {
			t.Fatal("protocol mixup did not reach its audit cause")
		}
		f.subject = 43
		f.verified = false
		if _, err := githubRoundTrip(t, h, f, "sign-up"); !isUnauth(err) {
			t.Fatalf("unverified primary: %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.signup_refused' AND payload LIKE '%"cause":"no-verified-email"%'`) != 1 {
			t.Fatal("missing registration no-verified-email refusal")
		}
		f.verified = true
		f.emailError = true
		if _, err := githubRoundTrip(t, h, f, "sign-up"); !isUnauth(err) {
			t.Fatalf("failed emails: %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.oauth2_refused' AND payload LIKE '%"cause":"userinfo-error"%' AND payload LIKE '%"intent":"sign-up"%' AND payload LIKE '%"purpose":"login"%'`) != 1 {
			t.Fatal("email network refusal lost its cause or signup context")
		}
		f.emailError = false
		f.userError = true
		if _, err := githubRoundTrip(t, h, f, "sign-in"); !isUnauth(err) {
			t.Fatalf("mutable subject: %v", err)
		}
		if _, err := p.Put(ctx, service.LocalPrincipal(root), "github", service.OAuth2ProviderInput{Profile: "github", DisplayName: "GitHub", Issuer: "https://github.com", ClientID: "client", ClientSecret: "secret", Enabled: false}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.auth.Identity(ctx, result.Login.SessionToken); !isUnauth(err) {
			t.Fatalf("disabled session live: %v", err)
		}
	})
}

// runOAuth2Lifecycle reaches both registered OAuth2 events in the closure fixture.
func runOAuth2Lifecycle(t *testing.T, auth *service.Auth, ctx context.Context, admin domain.PrincipalID, username, password string) {
	t.Helper()
	h := &signupHarness{t: t, db: auth.DB, auth: auth}
	_, f := configureGithub(t, h)
	login, err := auth.LocalLogin(ctx, username, password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	start, err := auth.OAuth2Start(ctx, "github", "link", "", "", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start.AuthURL)
	f.challenge = u.Query().Get("code_challenge")
	if _, err := auth.OAuth2Callback(ctx, "github", "oauth-code", start.State, "", "", "", login.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := githubRoundTrip(t, h, f, "sign-in"); err != nil {
		t.Fatal(err)
	}
	if f.emails != 0 {
		t.Fatal("link/login fetched emails")
	}
	if _, err := auth.OAuth2Callback(ctx, "github", "code", "invalid", "", "", "", ""); !isUnauth(err) {
		t.Fatal(err)
	}
}

func runOAuth2EstablishLifecycle(t *testing.T, auth *service.Auth) {
	t.Helper()
	h := &signupHarness{t: t, db: auth.DB, auth: auth}
	f := &githubFixture{subject: 60901, verified: true}
	auth.OAuth2HTTPClient = &http.Client{Transport: f}
	grants := &service.Grants{DB: auth.DB}
	invite, err := grants.InviteMember(t.Context(), service.LocalPrincipal(root), service.InviteSpec{Username: "oauth2-claim", Delivery: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := auth.OAuth2Start(t.Context(), "github", "claim", "", "", "", "", invite.Authority, false)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(start.AuthURL)
	f.challenge = u.Query().Get("code_challenge")
	claimed, err := auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", start.BindingCookie, "")
	if err != nil {
		t.Fatal(err)
	}
	start, err = auth.OAuth2Start(t.Context(), "github", "establish", "", "", "", claimed.Login.SessionToken, "", false)
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(start.AuthURL)
	f.challenge = u.Query().Get("code_challenge")
	established, err := auth.OAuth2Callback(t.Context(), "github", "oauth-code", start.State, "", "", "", claimed.Login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Identity(t.Context(), established.Login.SessionToken); err != nil {
		t.Fatal(err)
	}
	if f.emails != 0 {
		t.Fatal("claim/establish fetched email")
	}
	if queryInt(t, h.db, "SELECT COUNT(*) FROM credential_establish_evidence") != 1 {
		t.Fatal("establish stamp missing")
	}
	if _, err := auth.OAuth2Start(t.Context(), "github", "claim", "", "", "", "", invite.Authority, false); !isUnauth(err) {
		t.Fatalf("consumed claim: %v", err)
	}
}

func TestOAuth2EpochFence(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, phase := range []string{"user", "emails"} {
			t.Run(phase, func(t *testing.T) {
				h := newSignupHarness(t, db)
				_, f := configureGithub(t, h)
				entry := service.RegistrationExternalEntry{Provider: domain.ProviderRef{Kind: domain.ProviderOAuth2, Slug: "github"}}
				if _, err := h.reg.Put(t.Context(), service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{entry}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
					t.Fatal(err)
				}
				bump := func() {
					execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch = credential_epoch + 1 WHERE id = 1")
				}
				if phase == "user" {
					f.onUser = bump
				} else {
					f.onEmail = bump
				}
				before := queryInt(t, db, "SELECT COUNT(*) FROM accounts")
				if _, err := githubRoundTrip(t, h, f, "sign-up"); !isUnauth(err) {
					t.Fatalf("epoch race: %v", err)
				}
				if queryInt(t, db, "SELECT COUNT(*) FROM accounts") != before {
					t.Fatal("epoch race minted account")
				}
				if phase == "user" && f.emails != 0 {
					t.Fatal("epoch refusal fetched email")
				}
			})
		}
	})
}

func TestOAuth2ProviderAdministration(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newSignupHarness(t, db)
		p, _ := configureGithub(t, h)
		ctx := t.Context()
		input := service.OAuth2ProviderInput{Profile: "github", DisplayName: "GitHub", Issuer: "https://github.com", ClientID: "client", ClientSecret: "secret", Enabled: true}
		for _, origin := range []string{"https://github.enterprise.test", "https://github.com/", "http://github.com"} {
			bad := input
			bad.Issuer = origin
			if _, err := p.Put(ctx, service.LocalPrincipal(root), "other", bad); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("origin %q: %v", origin, err)
			}
		}
		bad := input
		bad.Profile = "arbitrary"
		if _, err := p.Put(ctx, service.LocalPrincipal(root), "other", bad); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unknown profile: %v", err)
		}
		if _, err := p.Put(ctx, service.LocalPrincipal(root), "other", input); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("duplicate enabled origin: %v", err)
		}
		input.Enabled = false
		if _, err := p.Put(ctx, service.LocalPrincipal(root), "other", input); err != nil {
			t.Fatal(err)
		}
		input.Enabled = true
		if _, err := p.Put(ctx, service.LocalPrincipal(root), "other", input); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("duplicate enable: %v", err)
		}
		view, err := p.Get(ctx, service.LocalPrincipal(root), "github")
		if err != nil {
			t.Fatal(err)
		}
		serialized, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(serialized), "secret") {
			t.Fatal("provider view exposed secret")
		}
		var sealed []byte
		if db.Engine() == store.EnginePostgres {
			err = db.PG().QueryRow(ctx, "SELECT client_secret FROM oauth2_providers WHERE slug='github'").Scan(&sealed)
		} else {
			err = db.SQLiteRead().QueryRowContext(ctx, "SELECT client_secret FROM oauth2_providers WHERE slug='github'").Scan(&sealed)
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(sealed) == 0 || strings.Contains(string(sealed), "secret") {
			t.Fatal("provider secret is not sealed")
		}
		if providers, err := p.List(ctx, service.LocalPrincipal(root)); err != nil || len(providers) != 2 {
			t.Fatalf("provider list: %+v %v", providers, err)
		}
		for _, event := range []string{"auth.provider_changed", "auth.provider_read"} {
			if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='`+event+`' AND payload LIKE '%"kind":"oauth2"%'`) == 0 {
				t.Fatalf("provider event %s omitted OAuth2 kind", event)
			}
		}
		if err := p.Delete(ctx, service.LocalPrincipal(root), "other"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Get(ctx, service.LocalPrincipal(root), "other"); !errors.Is(err, service.ErrProviderNotFound) {
			t.Fatalf("deleted provider visible: %v", err)
		}
	})
}

func TestOAuth2ClaimAllowlistRefused(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newSignupHarness(t, db)
		_, f := configureGithub(t, h)
		entry := service.RegistrationExternalEntry{Provider: domain.ProviderRef{Kind: domain.ProviderOAuth2, Slug: "github"}, Claim: "name", Values: []string{"GitHub user"}}
		input := service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{entry}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}
		if _, err := h.reg.Put(t.Context(), service.LocalPrincipal(root), instanceReg, input, ""); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("editable claim accepted: %v", err)
		}
		input.External[0].Claim = ""
		input.External[0].Values = nil
		if _, err := h.reg.Put(t.Context(), service.LocalPrincipal(root), instanceReg, input, ""); err != nil {
			t.Fatal(err)
		}
		// Simulate an older or restored policy that bypassed input validation.
		execRaw(t, db, `UPDATE registration_policy_entries SET claim='name' WHERE provider_kind='oauth2'`)
		execRaw(t, db, `INSERT INTO registration_policy_entry_values(entry_id,value) SELECT id,'GitHub user' FROM registration_policy_entries WHERE provider_kind='oauth2'`)
		if _, err := githubRoundTrip(t, h, f, "sign-up"); !isUnauth(err) {
			t.Fatalf("stored OAuth2 claim admitted: %v", err)
		}
	})
}

func TestOAuth2ProviderWriteVersion(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newSignupHarness(t, db)
		p, _ := configureGithub(t, h)
		in := service.OAuth2ProviderInput{Profile: "github", DisplayName: "Changed", Issuer: "https://github.com", ClientID: "client", ClientSecret: "replacement", Enabled: false, CreateOnly: true}
		if _, err := p.Put(t.Context(), service.LocalPrincipal(root), "github", in); !errors.Is(err, service.ErrOAuth2ProviderExists) {
			t.Fatalf("create overwrote provider: %v", err)
		}
		view, err := p.Get(t.Context(), service.LocalPrincipal(root), "github")
		if err != nil {
			t.Fatal(err)
		}
		if view.RowVersion != 1 || !view.Enabled {
			t.Fatalf("create refusal changed row: %+v", view)
		}
		in.CreateOnly = false
		in.ExpectedRowVersion = &view.RowVersion
		updated, err := p.Put(t.Context(), service.LocalPrincipal(root), "github", in)
		if err != nil {
			t.Fatal(err)
		}
		if updated.RowVersion != 2 || updated.Enabled {
			t.Fatalf("versioned update: %+v", updated)
		}
		in.Enabled = true
		if _, err := p.Put(t.Context(), service.LocalPrincipal(root), "github", in); !errors.Is(err, service.ErrProviderRace) {
			t.Fatalf("stale update admitted: %v", err)
		}
		current, err := p.Get(t.Context(), service.LocalPrincipal(root), "github")
		if err != nil || current.Enabled || current.RowVersion != 2 {
			t.Fatalf("stale update changed row: %+v %v", current, err)
		}
		if err := p.Delete(t.Context(), service.LocalPrincipal(root), "github"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Put(t.Context(), service.LocalPrincipal(root), "github", in); !errors.Is(err, service.ErrProviderRace) {
			t.Fatalf("deleted update recreated row: %v", err)
		}
	})
}
