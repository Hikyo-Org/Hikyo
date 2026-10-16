package isolation

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

const loginRedirect = "http://127.0.0.1:41234/callback"

func cliLoginState() string { return strings.Repeat("A", 43) }
func loginStart(t *testing.T, auth *service.Auth, state string) (service.CLILoginStart, string) {
	t.Helper()
	verifier, challenge := pkcePair("cli-login-" + state)
	start, err := auth.StartCLILogin(t.Context(), "loopback", state, challenge, loginRedirect)
	if err != nil {
		t.Fatal(err)
	}
	return start, verifier
}
func loginApprove(t *testing.T, auth *service.Auth, browser, state string) service.CLIReauthApproval {
	t.Helper()
	approval, err := auth.ApproveCLILogin(t.Context(), service.Bearer(browser), state)
	if err != nil {
		t.Fatal(err)
	}
	return approval
}
func assertCLISnapshot(t *testing.T, auth *service.Auth, db *store.DB, browser, cli service.LoginResult) {
	t.Helper()
	ctx := t.Context()
	if cli.Artifact != service.ArtifactCLI || cli.SessionID == browser.SessionID || cli.SessionToken == browser.SessionToken {
		t.Fatal("handoff did not mint a distinct CLI session")
	}
	before, err := auth.Identity(ctx, browser.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	// Approval snapshots the durable browser identity. Session timestamps are
	// stored at microsecond precision, while the original mint result may carry
	// nanoseconds supplied by the host clock.
	want := browser.Assurance
	want.AuthenticatedAt = before.Assurance.AuthenticatedAt
	if !reflect.DeepEqual(cli.Assurance, want) {
		t.Fatalf("snapshot differs: got %+v want %+v", cli.Assurance, want)
	}
	after, err := auth.Identity(ctx, cli.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if before.Principal != after.Principal || sessionProviderProvenance(t, db, browser.SessionID) != sessionProviderProvenance(t, db, cli.SessionID) {
		t.Fatalf("provenance mismatch: browser %+v cli %+v", before, after)
	}
	if !reflect.DeepEqual(before.Assurance, after.Assurance) {
		t.Fatalf("durable assurance differs: browser %+v cli %+v", before.Assurance, after.Assurance)
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM credential_establish_evidence WHERE session_id='"+cli.SessionID+"'") != 0 {
		t.Fatal("CLI inherited establish evidence")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM reauth_windows WHERE session_id='"+cli.SessionID+"'") != 0 {
		t.Fatal("CLI inherited reauthentication windows")
	}
	if queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE id='"+cli.SessionID+"' AND cli_handoff_id IS NOT NULL AND requesting_origin IS NULL AND handoff_id IS NULL") != 1 {
		t.Fatal("issuing transaction was not recorded on CLI session")
	}
	if cli.AbsExpires.Sub(cli.CreatedAt) != service.CLISessionAbsolute || cli.IdleExpires.Sub(cli.CreatedAt) != service.CLISessionIdle {
		t.Fatal("CLI clocks were inherited")
	}
}
func TestCLILoginLoopback(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		browser, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		for _, redirect := range []string{"http://localhost:41234/callback", "http://127.0.0.2:41234/callback", "https://127.0.0.1:41234/callback", "http://127.0.0.1:41234/callback?x=1", "http://127.0.0.1:41234/callback#x", "http://example.com:41234/callback"} {
			if _, err := auth.StartCLILogin(ctx, "loopback", cliLoginState(), strings.Repeat("A", 43), redirect); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("accepted redirect %q: %v", redirect, err)
			}
		}
		if _, err := auth.StartCLILogin(ctx, "loopback", "short", strings.Repeat("A", 43), loginRedirect); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("short state accepted")
		}
		start, verifier := loginStart(t, auth, cliLoginState())
		if start.ExpiresAt.Sub(time.Now()) > service.CLILoginLifetime {
			t.Fatal("wrong handoff lifetime")
		}
		if _, err := auth.CLILoginTransaction(ctx, service.Bearer(admin.token), start.State); !isUnauth(err) {
			t.Fatalf("CLI inspected browser approval: %v", err)
		}
		summary, err := auth.CLILoginTransaction(ctx, service.Bearer(browser.SessionToken), start.State)
		if err != nil || summary.Transport != "loopback" || summary.RedirectURI != loginRedirect {
			t.Fatalf("summary %+v %v", summary, err)
		}
		// A browser window exists, but no window may cross this login join point.
		err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			epoch, e := az.CredentialEpoch(ctx)
			if e != nil {
				return e
			}
			now := time.Now().UTC()
			return az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: "cli-login-browser-window", SessionID: browser.SessionID, EnvironmentID: "env_prod", CeremonyID: "cli-login-browser-proof", FactorClass: "totp", AuthenticatedAt: now, WindowExpiresAt: now.Add(time.Minute), HardExpiresAt: now.Add(time.Minute), CredentialEpoch: epoch, CreatedAt: now})
		})
		if err != nil {
			t.Fatal(err)
		}
		approved := loginApprove(t, auth, browser.SessionToken, start.State)
		if _, err := auth.ApproveCLILogin(ctx, service.Bearer(browser.SessionToken), start.State); !errors.Is(err, service.ErrCLILoginInvalid) {
			t.Fatalf("reapproval: %v", err)
		}
		for _, tc := range []struct{ verifier, redirect string }{{strings.Repeat("Z", 43), loginRedirect}, {verifier, "http://127.0.0.1:41235/callback"}, {verifier, "http://[::1]:41234/callback"}} {
			if _, err := auth.RedeemCLILogin(ctx, approved.Code, tc.verifier, tc.redirect); !errors.Is(err, service.ErrCLILoginInvalid) {
				t.Fatalf("wrong proof accepted: %v", err)
			}
		}
		cli, err := auth.RedeemCLILogin(ctx, approved.Code, verifier, loginRedirect)
		if err != nil {
			t.Fatal(err)
		}
		assertCLISnapshot(t, auth, db, browser, cli)
		if _, err := auth.RedeemCLILogin(ctx, approved.Code, verifier, loginRedirect); !errors.Is(err, service.ErrCLILoginInvalid) {
			t.Fatalf("replayed code: %v", err)
		}
		// Concurrent redemption publishes exactly one committed artifact.
		next, v := loginStart(t, auth, strings.Repeat("E", 43))
		a := loginApprove(t, auth, browser.SessionToken, next.State)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { _, e := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); results <- e })
		}
		wg.Wait()
		close(results)
		wins := 0
		for e := range results {
			if e == nil {
				wins++
			} else if !errors.Is(e, service.ErrCLILoginInvalid) {
				t.Fatal(e)
			}
		}
		if wins != 1 {
			t.Fatalf("concurrent successful redemptions=%d", wins)
		}
	})
}
func TestCLILoginDevice(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		clock := time.Now().UTC().Truncate(time.Microsecond).Add(123 * time.Nanosecond)
		auth.Now = func() time.Time { return clock }
		browser, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		start, err := auth.StartCLILogin(ctx, "device", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(start.UserCode) != 8 || start.DeviceCode == start.UserCode || start.ExpiresAt.Sub(clock) != service.CLIDeviceLifetime || start.Interval != 5 || !strings.HasPrefix(start.VerificationURI, auth.ExternalOrigin) {
			t.Fatalf("device start %+v", start)
		}
		for _, want := range []int{10, 15, 20, 25, 30, 35, 40} {
			got, e := auth.PollCLIDevice(ctx, start.DeviceCode)
			if e != nil || got.Status != "slow_down" || got.Interval != want {
				t.Fatalf("slowdown %+v %v, want %d", got, e, want)
			}
		}
		clock = clock.Add(40 * time.Second)
		if got, e := auth.PollCLIDevice(ctx, start.DeviceCode); e != nil || got.Status != "authorization_pending" {
			t.Fatalf("pending %+v %v", got, e)
		}
		if _, err := auth.ApproveCLILogin(ctx, service.Bearer(admin.token), start.UserCode); !isUnauth(err) {
			t.Fatalf("CLI approved device: %v", err)
		}
		approved := loginApprove(t, auth, browser.SessionToken, start.UserCode)
		if approved.Code != "" || approved.RedirectURI != "" {
			t.Fatal("device secret escaped approval")
		}
		clock = clock.Add(40 * time.Second)
		got, err := auth.PollCLIDevice(ctx, start.DeviceCode)
		if err != nil || got.Status != "" {
			t.Fatalf("redeem %+v %v", got, err)
		}
		assertCLISnapshot(t, auth, db, browser, got.Login)
		if got, e := auth.PollCLIDevice(ctx, start.DeviceCode); e != nil || got.Status != "expired_token" {
			t.Fatalf("device replay %+v %v", got, e)
		}
		fresh, err := auth.StartCLILogin(ctx, "device", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(service.CLIDeviceLifetime)
		if got, e := auth.PollCLIDevice(ctx, fresh.DeviceCode); e != nil || got.Status != "expired_token" {
			t.Fatalf("expired device %+v %v", got, e)
		}
		if _, err := auth.ApproveCLILogin(ctx, service.Bearer(browser.SessionToken), fresh.UserCode); !errors.Is(err, service.ErrCLILoginInvalid) {
			t.Fatalf("expired approval: %v", err)
		}
		if _, err := auth.StartCLILogin(ctx, "device", "", "", ""); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM cli_login_handoffs"); n != 1 {
			t.Fatalf("expired transactions not pruned: %d", n)
		}
	})
}
func TestCLILoginPasskey(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := bootstrapWebAuthnAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		dev := webauthntest.New(waRPID, waOrigin)
		enrolPasskey(t, auth, ctx, admin.token, waPassword, dev)
		browser, err := discoverableLogin(t, auth, ctx, dev)
		if err != nil {
			t.Fatal(err)
		}
		start, v := loginStart(t, auth, cliLoginState())
		a := loginApprove(t, auth, browser.SessionToken, start.State)
		cli, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect)
		if err != nil {
			t.Fatal(err)
		}
		assertCLISnapshot(t, auth, db, browser, cli)
		if cli.Assurance.Method != service.MethodLocalPasskey {
			t.Fatal("passkey assurance lost")
		}
	})
}
func TestCLILoginFederated(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newSignupHarness(t, db)
		ctx := t.Context()
		h.provider("cli-oidc", service.ProviderInput{})
		entry := service.RegistrationExternalEntry{Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: "cli-oidc"}}
		if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{entry}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		claims := map[string]any{"email": "cli@example.com", "email_verified": true}
		if _, err := h.login("cli-oidc", "known", "sign-up", "", claims); err != nil {
			t.Fatal(err)
		}
		before := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'registration.%'")
		if _, err := h.login("cli-oidc", "unknown", "sign-in", "", claims); !isUnauth(err) {
			t.Fatalf("unknown OIDC: %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'registration.%'"); n != before {
			t.Fatal("unknown sign-in entered registration")
		}
		oidc, err := h.login("cli-oidc", "known", "sign-in", "", claims)
		if err != nil {
			t.Fatal(err)
		}
		start, v := loginStart(t, h.auth, cliLoginState())
		a := loginApprove(t, h.auth, oidc.Login.SessionToken, start.State)
		cli, err := h.auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect)
		if err != nil {
			t.Fatal(err)
		}
		assertCLISnapshot(t, h.auth, db, oidc.Login, cli)
		p, f := configureGithub(t, h)
		entry.Provider = domain.ProviderRef{Kind: domain.ProviderOAuth2, Slug: "github"}
		if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{External: []service.RegistrationExternalEntry{entry}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := githubRoundTrip(t, h, f, "sign-up"); err != nil {
			t.Fatal(err)
		}
		oauth, err := githubRoundTrip(t, h, f, "sign-in")
		if err != nil {
			t.Fatal(err)
		}
		establish, err := h.auth.OAuth2Start(ctx, "github", "establish", "", "", "", oauth.Login.SessionToken, "", false)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(establish.AuthURL)
		if err != nil {
			t.Fatal(err)
		}
		f.challenge = u.Query().Get("code_challenge")
		established, err := h.auth.OAuth2Callback(ctx, "github", "oauth-code", establish.State, "", "", "", oauth.Login.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		oauth.Login = established.Login
		if queryInt(t, db, "SELECT COUNT(*) FROM credential_establish_evidence WHERE session_id='"+oauth.Login.SessionID+"'") != 1 {
			t.Fatal("browser establishment fixture missing")
		}
		start, v = loginStart(t, h.auth, strings.Repeat("I", 43))
		a = loginApprove(t, h.auth, oauth.Login.SessionToken, start.State)
		cli, err = h.auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect)
		if err != nil {
			t.Fatal(err)
		}
		assertCLISnapshot(t, h.auth, db, oauth.Login, cli)
		pending, pendingVerifier := loginStart(t, h.auth, strings.Repeat("M", 43))
		pendingApproval := loginApprove(t, h.auth, oauth.Login.SessionToken, pending.State)
		if _, err := p.Put(ctx, service.LocalPrincipal(root), "github", service.OAuth2ProviderInput{Profile: "github", DisplayName: "GitHub", Issuer: "https://github.com", ClientID: "client", ClientSecret: "secret", Enabled: false}); err != nil {
			t.Fatal(err)
		}
		if result, err := h.auth.RedeemCLILogin(ctx, pendingApproval.Code, pendingVerifier, loginRedirect); !errors.Is(err, service.ErrCLILoginInvalid) || result.SessionToken != "" {
			t.Fatalf("disabled provider pending approval: %v", err)
		}
		if _, err := h.auth.Identity(ctx, cli.SessionToken); !isUnauth(err) {
			t.Fatalf("provider disable did not sweep CLI snapshot: %v", err)
		}
	})
}

func sessionProviderProvenance(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	return queryString(t, db, "SELECT COALESCE(provider_id,'')||'|'||COALESCE(oauth2_provider_id,'')||'|'||COALESCE(saml_provider_id,'') FROM sessions WHERE id='"+id+"'")
}

func TestCLILoginApprovalRevocation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		browser, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		start, v := loginStart(t, auth, cliLoginState())
		a := loginApprove(t, auth, browser.SessionToken, start.State)
		if err := auth.Logout(ctx, browser.SessionToken); err != nil {
			t.Fatal(err)
		}
		if result, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); !errors.Is(err, service.ErrCLILoginInvalid) || result.SessionToken != "" {
			t.Fatalf("logout approval minted token: %v", err)
		}
		browser, err = auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		start, v = loginStart(t, auth, strings.Repeat("E", 43))
		a = loginApprove(t, auth, browser.SessionToken, start.State)
		execRaw(t, db, "UPDATE principals SET session_generation=session_generation+1 WHERE id='"+string(browser.Principal)+"'")
		if result, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); !isUnauth(err) || result.SessionToken != "" {
			t.Fatalf("generation drift approval minted token: %v", err)
		}
		browser, err = auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		start, v = loginStart(t, auth, strings.Repeat("I", 43))
		a = loginApprove(t, auth, browser.SessionToken, start.State)
		err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.AdvanceRestoreEpoch(ctx, time.Now().UTC())
		})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); (!isUnauth(err) && !errors.Is(err, upgrade.ErrConflict)) || result.SessionToken != "" {
			t.Fatalf("restore epoch approval minted token: %v", err)
		}
	})
}

func TestCLILoginExpiryAndConcurrentDevice(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		browser, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		start, v := loginStart(t, auth, cliLoginState())
		a := loginApprove(t, auth, browser.SessionToken, start.State)
		// The browser has only thirty seconds of idle lifetime left at approval.
		expiry := clock.Add(30 * time.Second).Format("2006-01-02T15:04:05.000000Z")
		execRaw(t, db, "UPDATE sessions SET idle_expires_at='"+expiry+"' WHERE id='"+browser.SessionID+"'")
		clock = clock.Add(time.Minute)
		if result, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); !isUnauth(err) || result.SessionToken != "" {
			t.Fatalf("expired browser approval: %v", err)
		}
		browser, err = auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		start, v = loginStart(t, auth, strings.Repeat("E", 43))
		a = loginApprove(t, auth, browser.SessionToken, start.State)
		clock = clock.Add(service.CLILoginLifetime)
		if result, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); !errors.Is(err, service.ErrCLILoginInvalid) || result.SessionToken != "" {
			t.Fatalf("expired code: %v", err)
		}
		device, err := auth.StartCLILogin(ctx, "device", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan service.CLIDeviceAttempt, 2)
		failures := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { r, e := auth.PollCLIDevice(ctx, device.DeviceCode); results <- r; failures <- e })
		}
		wg.Wait()
		close(results)
		close(failures)
		for e := range failures {
			if e != nil {
				t.Fatal(e)
			}
		}
		intervals := []int{}
		for result := range results {
			if result.Status != "slow_down" {
				t.Fatalf("concurrent poll %+v", result)
			}
			intervals = append(intervals, result.Interval)
		}
		sort.Ints(intervals)
		if !reflect.DeepEqual(intervals, []int{10, 15}) {
			t.Fatalf("lost durable slowdown: %v", intervals)
		}
		// Origin labels and verification URLs come only from valid server config.
		for _, origin := range []string{"", "https://user:secret@example.com", "https://example.com/path", "http://example.com"} {
			auth.ExternalOrigin = origin
			if _, err := auth.StartCLILogin(ctx, "device", "", "", ""); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("invalid configured origin %q accepted: %v", origin, err)
			}
		}
	})
}
func TestCLILoginSAML(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		idp := configureSAMLProvider(t, auth, admin.boot.PrincipalID)
		local, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		link, err := auth.SAMLStart(ctx, "saml-idp", "link", "", local.SessionToken, admin.password)
		if err != nil {
			t.Fatal(err)
		}
		response := samlResponseForStart(t, idp, link, "_cli_link_response", "_cli_link_assertion")
		if _, err := auth.SAMLACS(ctx, "saml-idp", response, samlAuditRelayState(t, link.RedirectURL), link.InitiatorCookie); err != nil {
			t.Fatal(err)
		}
		start, err := auth.SAMLStart(ctx, "saml-idp", "login", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		response = samlResponseForStart(t, idp, start, "_cli_login_response", "_cli_login_assertion")
		browser, err := auth.SAMLACS(ctx, "saml-idp", response, samlAuditRelayState(t, start.RedirectURL), start.InitiatorCookie)
		if err != nil {
			t.Fatal(err)
		}
		handoff, v := loginStart(t, auth, cliLoginState())
		approval := loginApprove(t, auth, browser.SessionToken, handoff.State)
		cli, err := auth.RedeemCLILogin(ctx, approval.Code, v, loginRedirect)
		if err != nil {
			t.Fatal(err)
		}
		assertCLISnapshot(t, auth, db, browser, cli)
	})
}

func TestCLILoginFailureAudit(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		admin := oidcAdmin(t, db)
		auth := admin.auth
		ctx := t.Context()
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		browser, err := auth.LocalLogin(ctx, "oidc-admin", admin.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		failures := func() int64 {
			return queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='auth.login' AND outcome='failure'")
		}
		start, v := loginStart(t, auth, cliLoginState())
		a := loginApprove(t, auth, browser.SessionToken, start.State)
		before := failures()
		wrong, _ := pkcePair("wrong-login-proof")
		if _, err := auth.RedeemCLILogin(ctx, a.Code, wrong, loginRedirect); !errors.Is(err, service.ErrCLILoginInvalid) {
			t.Fatal(err)
		}
		if _, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect+"?changed=1"); !errors.Is(err, service.ErrCLILoginInvalid) {
			t.Fatal(err)
		}
		if got := failures(); got != before+2 {
			t.Fatalf("rollback login failures not durable: got%d want%d", got, before+2)
		}
		payload := queryString(t, db, "SELECT payload FROM audit_instance_events WHERE type='auth.login' AND outcome='failure' ORDER BY seq DESC LIMIT 1")
		for _, secret := range []string{a.Code, v, start.State, browser.SessionToken} {
			if strings.Contains(payload, secret) {
				t.Fatal("login audit contains a flow secret")
			}
		}
		if !strings.Contains(payload, `"method":"local-password"`) || !strings.Contains(payload, `"cause":"redirect-mismatch"`) {
			t.Fatalf("failure lost method/cause: %s", payload)
		}
		if _, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.RedeemCLILogin(ctx, a.Code, v, loginRedirect); !errors.Is(err, service.ErrCLILoginInvalid) {
			t.Fatal(err)
		}
		if got := failures(); got != before+3 {
			t.Fatalf("replay failure not durable: %d", got)
		}
		device, err := auth.StartCLILogin(ctx, "device", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := auth.PollCLIDevice(ctx, device.DeviceCode); err != nil || got.Status != "slow_down" {
			t.Fatalf("slowdown %+v %v", got, err)
		}
		clock = clock.Add(10 * time.Second)
		if got, err := auth.PollCLIDevice(ctx, device.DeviceCode); err != nil || got.Status != "authorization_pending" {
			t.Fatalf("pending %+v %v", got, err)
		}
		if got := failures(); got != before+3 {
			t.Fatal("normal device polling recorded a failed login")
		}
		loginApprove(t, auth, browser.SessionToken, device.UserCode)
		execRaw(t, db, "UPDATE principals SET session_generation=session_generation+1 WHERE id='"+string(browser.Principal)+"'")
		clock = clock.Add(10 * time.Second)
		if got, err := auth.PollCLIDevice(ctx, device.DeviceCode); err != nil || got.Status != "access_denied" {
			t.Fatalf("revoked device %+v %v", got, err)
		}
		if got := failures(); got != before+4 {
			t.Fatal("device denial failure not durable")
		}
		expired, err := auth.StartCLILogin(ctx, "device", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(service.CLIDeviceLifetime)
		if got, err := auth.PollCLIDevice(ctx, expired.DeviceCode); err != nil || got.Status != "expired_token" {
			t.Fatalf("expired device %+v %v", got, err)
		}
		if got := failures(); got != before+5 {
			t.Fatal("expired device failure not durable")
		}
	})
}
