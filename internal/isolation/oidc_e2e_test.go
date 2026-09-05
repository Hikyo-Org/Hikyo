package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The OIDC A1 fixture families (#54, human-auth ADR - The OIDC transaction),
// dual-engine, driven against a real test IdP (internal/oidctest) so the wire
// flow is exercised end to end: mix-up in both directions, byte-exact
// (issuer, subject) linking, the purpose walls, the transaction binding, and
// the reauth refusals.

func strptr(s string) *string { return &s }

// driveIdP follows the authorization request to the test IdP and returns the
// code and state the IdP redirected back with, WITHOUT following the redirect
// to the (non-serving) callback URL. Extra query params (e.g. sub) let a
// fixture control the minted subject.
func driveIdP(t *testing.T, authURL string) (code, state string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("driving the IdP authorize: %v", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parsing the IdP redirect %q: %v", loc, err)
	}
	return u.Query().Get("code"), u.Query().Get("state")
}

// configureProvider installs a provider under local host authority (MFA-exempt),
// returning the Providers service and the IdP.
func configureProvider(t *testing.T, auth *service.Auth, ctx context.Context, admin domain.PrincipalID, slug string, in service.ProviderInput) (*service.Providers, *oidctest.IdP) {
	t.Helper()
	idp, err := oidctest.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(idp.Close)
	if auth.ExternalOrigin == "" {
		auth.ExternalOrigin = "https://hikyo.test"
	}
	if err := idp.RegisterRedirectURI(strings.TrimRight(auth.ExternalOrigin, "/") + "/api/v1/auth/oidc/" + slug + "/callback"); err != nil {
		t.Fatal(err)
	}
	providers := &service.Providers{DB: auth.DB, Keyring: auth.Keyring, ExternalOrigin: auth.ExternalOrigin}
	in.Issuer = idp.Issuer()
	if _, err := providers.Put(ctx, service.LocalPrincipal(admin), slug, in); err != nil {
		t.Fatalf("configuring provider %q: %v", slug, err)
	}
	return providers, idp
}

// runOIDCLifecycle drives every OIDC audit event once into the shared audit_e2e
// datastore, so the emitter-closure subtest finds each reached a trail.
func runOIDCLifecycle(t *testing.T, auth *service.Auth, ctx context.Context, admin domain.PrincipalID, username, password string) {
	t.Helper()
	providers, idp := configureProvider(t, auth, ctx, admin, "lifecycle-idp", service.ProviderInput{
		DisplayName: "Lifecycle IdP", ClientID: "client", ClientSecret: "secret", Scopes: "openid",
		Enabled: true,
	})
	// provider_read (get + list).
	if _, err := providers.Get(ctx, service.LocalPrincipal(admin), "lifecycle-idp"); err != nil {
		t.Fatalf("provider get: %v", err)
	}
	if _, err := providers.List(ctx, service.LocalPrincipal(admin)); err != nil {
		t.Fatalf("provider list: %v", err)
	}

	// Link an identity to the admin account (identity_linked + session_created).
	login, err := auth.LocalLogin(ctx, username, password, service.ArtifactCLI)
	if err != nil {
		t.Fatalf("local login: %v", err)
	}
	start, err := auth.OIDCStart(ctx, "lifecycle-idp", "link", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatalf("oidc link start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=lifecycle-subject")
	if _, err := auth.OIDCCallback(ctx, "lifecycle-idp", code, state, "", "", "", login.SessionToken); err != nil {
		t.Fatalf("oidc link callback: %v", err)
	}

	// OIDC login as that identity (oidc_login + session_created).
	oidcLogin(t, auth, ctx, "lifecycle-idp", "lifecycle-subject")

	// A refusal (oidc_refused): a malformed state matches no transaction.
	if _, err := auth.OIDCCallback(ctx, "lifecycle-idp", "code", "not-a-state", "", "", "", ""); !isUnauth(err) {
		t.Fatalf("malformed state should refuse: %v", err)
	}

	// Unlink (identity_unlinked + session_created).
	relogin, err := auth.LocalLogin(ctx, username, password, service.ArtifactCLI)
	if err != nil {
		t.Fatalf("re-login for unlink: %v", err)
	}
	ids, err := auth.ListIdentities(ctx, relogin.SessionToken)
	if err != nil || len(ids) == 0 {
		t.Fatalf("list identities: %v (n=%d)", err, len(ids))
	}
	if _, err := auth.UnlinkIdentity(ctx, relogin.SessionToken, ids[0].ID, password); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	_ = idp
}

// oidcLogin drives an anonymous OIDC login for the given subject and returns
// the resulting session.
func oidcLogin(t *testing.T, auth *service.Auth, ctx context.Context, slug, subject string) service.LoginResult {
	t.Helper()
	start, err := auth.OIDCStart(ctx, slug, "login", "", "", "", false)
	if err != nil {
		t.Fatalf("oidc login start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub="+subject)
	res, err := auth.OIDCCallback(ctx, slug, code, state, "", "", start.BindingCookie, "")
	if err != nil {
		t.Fatalf("oidc login callback: %v", err)
	}
	return res.Login
}

func isUnauth(err error) bool {
	return err == domain.ErrUnauthenticated || (err != nil && err.Error() == domain.ErrUnauthenticated.Error())
}

// --- A1 fixtures ---

// oidcAdmin configures the external origin around the shared
// first-administrator fixture.
func oidcAdmin(t *testing.T, db *store.DB) admin {
	t.Helper()
	auth := authService(t, db)
	auth.ExternalOrigin = "https://hikyo.test"
	return bootstrapAdmin(t, db, adminOpts{
		username: "oidc-admin", displayName: "OIDC Admin",
		password: "correct horse battery staple oidc", auth: auth, login: true,
	})
}

func runOIDCMixup(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	_, idpA := configureProvider(t, auth, ctx, admin, "prov-a", service.ProviderInput{
		DisplayName: "A", ClientID: "ca", ClientSecret: "sa", Scopes: "openid", Enabled: true,
	})
	_, idpB := configureProvider(t, auth, ctx, admin, "prov-b", service.ProviderInput{
		DisplayName: "B", ClientID: "cb", ClientSecret: "sb", Scopes: "openid", Enabled: true,
	})

	// Begin a transaction at A; obtain a code from A's authorize.
	startA, err := auth.OIDCStart(ctx, "prov-a", "login", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	codeA, stateA := driveIdP(t, startA.AuthURL+"&sub=user")

	// Deliver A's response to B's callback path: the transaction is pinned to A,
	// so an exchange (were the slug check removed) would hit A's token endpoint,
	// never B's. Assert A's counter is untouched: the refusal precedes exchange.
	hitsA := idpA.TokenEndpointHits
	if _, err := auth.OIDCCallback(ctx, "prov-b", codeA, stateA, "", "", startA.BindingCookie, ""); !isUnauth(err) {
		t.Fatalf("mix-up A->B should refuse: %v", err)
	}
	if idpA.TokenEndpointHits != hitsA {
		t.Fatalf("mix-up A->B hit the recorded provider's token endpoint: refusal must precede exchange")
	}

	// The other direction: begin at B, deliver to A's callback. The tx is pinned
	// to B, so assert B's counter is untouched.
	startB, err := auth.OIDCStart(ctx, "prov-b", "login", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	codeB, stateB := driveIdP(t, startB.AuthURL+"&sub=user")
	hitsB := idpB.TokenEndpointHits
	if _, err := auth.OIDCCallback(ctx, "prov-a", codeB, stateB, "", "", startB.BindingCookie, ""); !isUnauth(err) {
		t.Fatalf("mix-up B->A should refuse: %v", err)
	}
	if idpB.TokenEndpointHits != hitsB {
		t.Fatalf("mix-up B->A hit the recorded provider's token endpoint: refusal must precede exchange")
	}
}

func TestOIDCMixup(t *testing.T) {
	forEngines(t, runOIDCMixup)
}

// runOIDCByteExactSubject: two subjects differing only in case are two distinct
// identities, both loginable, never merged.
func runOIDCByteExactSubject(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	configureProvider(t, auth, ctx, admin, "idp", service.ProviderInput{
		DisplayName: "IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	// Invite and link distinct accounts for case-variant subjects.
	inviteOIDCIdentity(t, auth, ctx, admin, "idp", "alice", "lower-subject")
	inviteOIDCIdentity(t, auth, ctx, admin, "idp", "Alice", "upper-subject")
	lower := oidcLogin(t, auth, ctx, "idp", "alice")
	upper := oidcLogin(t, auth, ctx, "idp", "Alice")
	if lower.AccountID == upper.AccountID {
		t.Fatalf("case-variant subjects merged into one account: %s", lower.AccountID)
	}
	// Both remain independently loginable.
	again := oidcLogin(t, auth, ctx, "idp", "alice")
	if again.AccountID != lower.AccountID {
		t.Fatalf("subject 'alice' resolved to a different account on re-login")
	}
	_ = password
}

func TestOIDCByteExactSubject(t *testing.T) {
	forEngines(t, runOIDCByteExactSubject)
}

func oidcRefusedCount(t *testing.T, db *store.DB, cause string) int64 {
	t.Helper()
	return queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.oidc_refused' AND payload LIKE '%"+cause+"%'")
}

// runOIDCBinding: the transaction binding (A2) refuses a callback that cannot
// present the binding the start recorded, and the refusal is audited by cause.
// The purpose wall is STRUCTURAL and needs no separate probe: the callback
// dispatches on the transaction's own purpose (a state resolves only its own
// transaction), so a response obtained for one purpose can never reach another
// purpose's branch.
func runOIDCBinding(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	configureProvider(t, auth, ctx, admin, "idp", service.ProviderInput{
		DisplayName: "IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	linkOn(t, auth, ctx, "idp", "user", oidcAdministrator.password)

	// Anonymous login is browser-cookie-bound (A2): a callback with the absent ob
	// cookie is refused, audited cause=binding.
	start, err := auth.OIDCStart(ctx, "idp", "login", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=user")
	before := oidcRefusedCount(t, db, "binding")
	if _, err := auth.OIDCCallback(ctx, "idp", code, state, "", "", "", ""); !isUnauth(err) {
		t.Fatalf("login callback with no binding cookie should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "binding") != before+1 {
		t.Fatalf("the binding refusal was not audited cause=binding")
	}

	// The correct binding cookie completes the same flow (positive control) - a
	// fresh transaction, since the first was consumed.
	start2, err := auth.OIDCStart(ctx, "idp", "login", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	code2, state2 := driveIdP(t, start2.AuthURL+"&sub=user")
	if _, err := auth.OIDCCallback(ctx, "idp", code2, state2, "", "", start2.BindingCookie, ""); err != nil {
		t.Fatalf("login with the correct binding cookie should succeed: %v", err)
	}

	// A link transaction is session-bound: a callback with no session fails the
	// binding, audited cause=binding.
	login, err := auth.LocalLogin(ctx, "oidc-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	lstart, err := auth.OIDCStart(ctx, "idp", "link", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatal(err)
	}
	lcode, lstate := driveIdP(t, lstart.AuthURL+"&sub=user")
	before = oidcRefusedCount(t, db, "binding")
	if _, err := auth.OIDCCallback(ctx, "idp", lcode, lstate, "", "", "", ""); !isUnauth(err) {
		t.Fatalf("link callback with no session should refuse (binding): %v", err)
	}
	if oidcRefusedCount(t, db, "binding") != before+1 {
		t.Fatalf("the session-binding refusal was not audited cause=binding")
	}
	_ = admin
}

func TestOIDCBinding(t *testing.T) {
	forEngines(t, runOIDCBinding)
}

// runOIDCReauthRefusals: OIDC reauth refuses when the environment is missing,
// when the provider has no assurance policy (A5), and when the returned token
// carries no auth_time (A7). Each cause is audited.
func runOIDCReauthRefusals(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	_, strict := configureProvider(t, auth, ctx, admin, "strict", service.ProviderInput{
		DisplayName: "Strict", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	// Link an identity on the strict provider.
	login, err := auth.LocalLogin(ctx, "oidc-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := auth.OIDCStart(ctx, "strict", "link", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatal(err)
	}
	lc, lst := driveIdP(t, ls.AuthURL+"&sub=reauth-user")
	if _, err := auth.OIDCCallback(ctx, "strict", lc, lst, "", "", "", login.SessionToken); err != nil {
		t.Fatalf("link: %v", err)
	}

	// reauth with no environment is refused loudly (would violate the tx CHECK).
	localSession, err := auth.LocalLogin(ctx, "oidc-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.OIDCStart(ctx, "strict", "reauth", "env_prod", localSession.SessionToken, "", false); !isUnauth(err) {
		t.Fatalf("local session starting OIDC reauth should refuse: %v", err)
	}
	relogin := oidcLogin(t, auth, ctx, "strict", "reauth-user")
	if _, err := auth.OIDCStart(ctx, "strict", "reauth", "", relogin.SessionToken, "", false); err != service.ErrReauthNoEnvironment {
		t.Fatalf("reauth with no environment: want ErrReauthNoEnvironment, got %v", err)
	}

	// reauth whose token carries amr=mfa but NO auth_time is refused (A7),
	// audited cause=no-auth-time. (The IdP asserts amr but leaves auth_time zero.)
	strict.AMR = []string{"mfa"}
	rs, err := auth.OIDCStart(ctx, "strict", "reauth", "env_prod", relogin.SessionToken, "", false)
	if err != nil {
		t.Fatalf("reauth start: %v", err)
	}
	rc, rst := driveIdP(t, rs.AuthURL+"&sub=reauth-user")
	before := oidcRefusedCount(t, db, "no-auth-time")
	if _, err := auth.OIDCCallback(ctx, "strict", rc, rst, "", "", "", relogin.SessionToken); !isUnauth(err) {
		t.Fatalf("reauth with no auth_time should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "no-auth-time") != before+1 {
		t.Fatalf("the auth_time refusal was not audited cause=no-auth-time")
	}

	// A present but stale auth_time is the same freshness refusal. Possession
	// and assurance both pass, isolating the five-minute bound.
	strict.AuthTime = time.Now().Add(-6 * time.Minute)
	strict.AMR = []string{"mfa", "otp"}
	staleSession := oidcLogin(t, auth, ctx, "strict", "reauth-user")
	rs, err = auth.OIDCStart(ctx, "strict", "reauth", "env_prod", staleSession.SessionToken, "", false)
	if err != nil {
		t.Fatalf("stale reauth start: %v", err)
	}
	rc, rst = driveIdP(t, rs.AuthURL+"&sub=reauth-user")
	before = oidcRefusedCount(t, db, "no-auth-time")
	if _, err := auth.OIDCCallback(ctx, "strict", rc, rst, "", "", "", staleSession.SessionToken); !isUnauth(err) {
		t.Fatalf("reauth with stale auth_time should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "no-auth-time") != before+1 {
		t.Fatalf("the stale auth_time refusal was not audited")
	}

	// A provider with NO assurance policy refuses reauth by name at start (A5).
	_, loose := configureProvider(t, auth, ctx, admin, "loose", service.ProviderInput{
		DisplayName: "Loose", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	linkOn(t, auth, ctx, "loose", "loose-user", password)
	loose.AuthTime = time.Now()
	loose.AMR = []string{"mfa", "otp"}
	looseSession := oidcLogin(t, auth, ctx, "loose", "loose-user")
	if _, err := auth.OIDCStart(ctx, "loose", "reauth", "env_prod", looseSession.SessionToken, "", false); err != service.ErrReauthNoPolicy {
		t.Fatalf("policy-less reauth: want ErrReauthNoPolicy, got %v", err)
	}
}

// runOIDCReauthZeroWindow pins the corrected server contract on both engines:
// a current-provider OIDC session still cannot open an effective-zero window,
// and the refusal is named and audited rather than silently opening authority.
func runOIDCReauthZeroWindow(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = 0
	_, idp := configureProvider(t, auth, ctx, admin, "strict", service.ProviderInput{
		DisplayName: "Strict", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "strict", "zero-user", password)
	idp.AuthTime = time.Now()
	idp.AMR = []string{"mfa", "otp"}
	session := oidcLogin(t, auth, ctx, "strict", "zero-user")

	started, err := auth.OIDCStart(ctx, "strict", "reauth", "env_prod", session.SessionToken, "", true)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, started.AuthURL+"&sub=zero-user")
	before := oidcRefusedCount(t, db, "window-zero")
	result, err := auth.OIDCCallback(ctx, "strict", code, state, "", "", "", session.SessionToken)
	if err != service.ErrReauthWindowClosed {
		t.Fatalf("OIDC at an effective-zero window = %v, want ErrReauthWindowClosed", err)
	}
	if !result.Browser || result.Purpose != "reauth" || result.State != state {
		t.Fatalf("zero-window browser metadata = %+v", result)
	}
	if oidcRefusedCount(t, db, "window-zero") != before+1 {
		t.Fatalf("the zero-window refusal was not audited cause=window-zero")
	}
}

func TestOIDCReauthZeroWindow(t *testing.T) {
	forEngines(t, runOIDCReauthZeroWindow)
}

// runOIDCBrowserOverloadMetadata proves admission refusal happens before any
// transaction lookup. Browser response shaping belongs to the HTTP-only marker
// cookie at the transport boundary, so the service returns no stored metadata.
func runOIDCBrowserOverloadMetadata(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	_, idp := configureProvider(t, auth, ctx, admin, "strict", service.ProviderInput{
		DisplayName: "Strict", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "strict", "overload-user", password)
	idp.AuthTime = time.Now()
	idp.AMR = []string{"mfa", "otp"}
	session := oidcLogin(t, auth, ctx, "strict", "overload-user")
	started, err := auth.OIDCStart(ctx, "strict", "reauth", "env_prod", session.SessionToken, "", true)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, started.AuthURL+"&sub=overload-user")

	limiter, err := admission.New(admission.Config{
		ArgonMemoryKiB: auth.KDF.MemoryKiB,
		PerIPPerMinute: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	auth.Admission = limiter
	limitedCtx := audit.WithContext(ctx, audit.Context{SourceIP: "192.0.2.195", Origin: audit.OriginAPI})
	release, err := limiter.Enter(limitedCtx, audit.FromContext(limitedCtx).SourceIP)
	if err != nil {
		t.Fatal(err)
	}
	release()

	result, err := auth.OIDCCallback(limitedCtx, "strict", code, state, "", "", "", session.SessionToken)
	if !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("overloaded browser callback = %v, want ErrOverloaded", err)
	}
	if result.Browser || result.Purpose != "" || result.State != "" || result.Login.SessionToken != "" {
		t.Fatalf("overloaded callback read transaction metadata = %+v", result)
	}
}

func TestOIDCBrowserOverloadMetadata(t *testing.T) {
	forEngines(t, runOIDCBrowserOverloadMetadata)
}

func TestOIDCReauthRefusals(t *testing.T) {
	forEngines(t, runOIDCReauthRefusals)
}

func runOIDCDisclosureAndCLIHandoff(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = 5 * time.Minute
	_, idp := configureProvider(t, auth, ctx, admin, "strict", service.ProviderInput{
		DisplayName: "Corporate IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "strict", "handoff-user", password)
	idp.AuthTime = time.Now()
	idp.AMR = []string{"mfa", "otp"}
	browser := oidcLogin(t, auth, ctx, "strict", "handoff-user")
	identity, err := auth.Identity(ctx, browser.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Assurance.Provider != "strict" {
		t.Fatalf("OIDC assurance provider = %q, want strict", identity.Assurance.Provider)
	}
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_oidc_handoff_reveal', '`+string(identity.Principal)+`', 'reveal', 'org_a', 'prj_a1', 'env_a1', `+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_oidc_handoff_read', '`+string(identity.Principal)+`', 'read', 'org_a', 'prj_a1', 'env_a1', `+ts+`)`)

	cli, err := auth.LocalLogin(ctx, "oidc-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = keyA1
	verifierHash := sha256.Sum256([]byte("oidc disclosure handoff verifier"))
	verifier := base64.RawURLEncoding.EncodeToString(verifierHash[:])
	challengeHash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeHash[:])
	intent := disclosureReauthIntent(t, service.PurposeReveal, "env_a1", []string{keyID})
	handoff, err := auth.StartCLIReauth(ctx, cli.SessionToken, intent, challenge, "http://127.0.0.1:40126/callback")
	if err != nil {
		t.Fatal(err)
	}

	started, err := auth.OIDCStart(ctx, "strict", "reauth", "env_a1", browser.SessionToken, "", true)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, started.AuthURL+"&sub=handoff-user")
	completed, err := auth.OIDCCallback(ctx, "strict", code, state, "", "", "", browser.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.Browser || completed.Purpose != "reauth" || completed.State != state {
		t.Fatalf("browser callback metadata = %+v", completed)
	}
	if got := queryString(t, db, "SELECT factor_class FROM reauth_windows WHERE session_id = '"+completed.Login.SessionID+"' AND environment_id = 'env_a1'"); got != "oidc" {
		t.Fatalf("browser reauth factor = %q, want oidc", got)
	}

	approved, err := auth.ApproveCLIReauth(ctx, service.Bearer(completed.Login.SessionToken), handoff.State)
	if err != nil {
		t.Fatal(err)
	}
	redeemed, err := auth.RedeemCLIReauth(ctx, approved.Code, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if len(redeemed.Windows) != 1 || redeemed.Windows[0].EnvironmentID != "env_a1" || redeemed.Windows[0].SingleDecision {
		t.Fatalf("redeemed OIDC windows = %+v", redeemed.Windows)
	}
	if got := queryString(t, db, "SELECT factor_class FROM reauth_windows WHERE session_id = '"+redeemed.SessionID+"' AND environment_id = 'env_a1'"); got != "oidc" {
		t.Fatalf("mirrored CLI factor = %q, want oidc", got)
	}
}

func TestOIDCDisclosureAndCLIHandoff(t *testing.T) {
	forEngines(t, runOIDCDisclosureAndCLIHandoff)
}

// runOIDCIssuerImmutable: a provider's issuer cannot change on update (A3).
func runOIDCIssuerImmutable(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	_, idp := configureProvider(t, auth, ctx, admin, "idp", service.ProviderInput{
		DisplayName: "IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
	})
	providers := &service.Providers{DB: auth.DB, Keyring: auth.Keyring, ExternalOrigin: auth.ExternalOrigin}
	// A different issuer on update is refused by name.
	other, err := oidctest.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	_, err = providers.Put(ctx, service.LocalPrincipal(admin), "idp", service.ProviderInput{
		DisplayName: "IdP2", Issuer: other.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
	})
	if err != service.ErrIssuerImmutable {
		t.Fatalf("issuer change should be refused as immutable, got %v", err)
	}
	// The same issuer updates fine (display name change).
	if _, err := providers.Put(ctx, service.LocalPrincipal(admin), "idp", service.ProviderInput{
		DisplayName: "Renamed", Issuer: idp.Issuer(), ClientID: "c2", ClientSecret: "s2", Scopes: "openid", Enabled: true,
	}); err != nil {
		t.Fatalf("same-issuer update should succeed: %v", err)
	}
}

func TestOIDCIssuerImmutable(t *testing.T) {
	forEngines(t, runOIDCIssuerImmutable)
}

// runOIDCProviderChangeSweeps: reconfiguring a provider deletes sessions
// authenticated through it (A4).
func runOIDCProviderChangeSweeps(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	providers, idp := configureProvider(t, auth, ctx, admin, "idp", service.ProviderInput{
		DisplayName: "IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	linkOn(t, auth, ctx, "idp", "user", oidcAdministrator.password)
	session := oidcLogin(t, auth, ctx, "idp", "user")
	if _, err := auth.Identity(ctx, session.SessionToken); err != nil {
		t.Fatalf("federated session should be live before the change: %v", err)
	}
	// Reconfigure (assurance policy change): the federated session is swept.
	if _, err := providers.Put(ctx, service.LocalPrincipal(admin), "idp", service.ProviderInput{
		DisplayName: "IdP", Issuer: idp.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	}); err != nil {
		t.Fatalf("reconfigure: %v", err)
	}
	if _, err := auth.Identity(ctx, session.SessionToken); !isUnauth(err) {
		t.Fatalf("federated session survived a provider change: %v", err)
	}
}

func TestOIDCProviderChangeSweeps(t *testing.T) {
	forEngines(t, runOIDCProviderChangeSweeps)
}

// --- reauth assurance fixtures (#54 cross-model review R1) ---
//
// The reauth path must be at least as strict as completeLogin: a reveal reauth
// window is opened only for evidence that carries a recognized possession
// factor, is live (current credential epoch), is recorded against the currently
// enabled provider, and is not weaker than the session it re-authorizes.

// linkOn links an external identity for the given subject on the given provider
// slug, using the admin's password as the account-security proof.
func linkOn(t *testing.T, auth *service.Auth, ctx context.Context, slug, subject, password string) {
	t.Helper()
	login, err := auth.LocalLogin(ctx, "oidc-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatalf("login for link: %v", err)
	}
	ls, err := auth.OIDCStart(ctx, slug, "link", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatalf("link start: %v", err)
	}
	lc, lst := driveIdP(t, ls.AuthURL+"&sub="+subject)
	if _, err := auth.OIDCCallback(ctx, slug, lc, lst, "", "", "", login.SessionToken); err != nil {
		t.Fatalf("link callback: %v", err)
	}
}

// reauthOn drives a reauth callback for the given subject on the given provider
// slug with the given acting session, returning the callback error.
func reauthOn(t *testing.T, auth *service.Auth, ctx context.Context, slug, subject, session string) error {
	t.Helper()
	rs, err := auth.OIDCStart(ctx, slug, "reauth", "env_prod", session, "", false)
	if err != nil {
		t.Fatalf("reauth start: %v", err)
	}
	rc, rst := driveIdP(t, rs.AuthURL+"&sub="+subject)
	_, err = auth.OIDCCallback(ctx, slug, rc, rst, "", "", "", session)
	return err
}

// runOIDCReauthPossession: a token that satisfies the assurance policy but
// carries no recognized possession amr (only "pwd") is refused (A5/B5), whether
// the policy keyed on an amr set or on acr. Policy satisfaction alone must never
// imply possession.
func runOIDCReauthPossession(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = time.Minute

	// (a) amr_sets [["pwd"]] satisfied by amr=["pwd"] — possession absent.
	_, amrIdP := configureProvider(t, auth, ctx, admin, "pwd-amr", service.ProviderInput{
		DisplayName: "pwd-amr", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["pwd"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "pwd-amr", "pwd-user", password)
	amrIdP.AuthTime = time.Now()
	amrIdP.AMR = []string{"pwd"}
	relogin := oidcLogin(t, auth, ctx, "pwd-amr", "pwd-user")
	before := oidcRefusedCount(t, db, "no-possession")
	if err := reauthOn(t, auth, ctx, "pwd-amr", "pwd-user", relogin.SessionToken); !isUnauth(err) {
		t.Fatalf("pwd-only amr reauth should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "no-possession") != before+1 {
		t.Fatalf("pwd-only amr refusal was not audited cause=no-possession")
	}

	// (b) acr-satisfied policy with amr=["pwd"] — the case evaluateAssurance
	// alone can never catch, since acr matches with no amr at all.
	_, acrIdP := configureProvider(t, auth, ctx, admin, "acr-only", service.ProviderInput{
		DisplayName: "acr-only", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"acr_values":["urn:strong"]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "acr-only", "acr-user", password)
	acrIdP.AuthTime = time.Now()
	acrIdP.ACR = "urn:strong"
	acrIdP.AMR = []string{"pwd"}
	relogin2 := oidcLogin(t, auth, ctx, "acr-only", "acr-user")
	before = oidcRefusedCount(t, db, "no-possession")
	if err := reauthOn(t, auth, ctx, "acr-only", "acr-user", relogin2.SessionToken); !isUnauth(err) {
		t.Fatalf("acr-satisfied pwd-amr reauth should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "no-possession") != before+1 {
		t.Fatalf("acr-satisfied refusal was not audited cause=no-possession")
	}

	// (c) amr_sets [["mfa"]] satisfied by amr=["mfa"] (rA-1). RFC 8176 "mfa"
	// asserts multiple factors were used but proves no possession factor, so
	// the policy is satisfied yet the reveal window is refused. A ["hwk"] or
	// ["otp"] token on the same policy passes (the downgrade fixture's positive
	// control exercises the ["hwk"] pass).
	_, mfaIdP := configureProvider(t, auth, ctx, admin, "mfa-only", service.ProviderInput{
		DisplayName: "mfa-only", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "mfa-only", "mfa-user", password)
	mfaIdP.AuthTime = time.Now()
	mfaIdP.AMR = []string{"mfa"}
	relogin3 := oidcLogin(t, auth, ctx, "mfa-only", "mfa-user")
	before = oidcRefusedCount(t, db, "no-possession")
	if err := reauthOn(t, auth, ctx, "mfa-only", "mfa-user", relogin3.SessionToken); !isUnauth(err) {
		t.Fatalf("mfa-only amr reauth should refuse (mfa is not possession): %v", err)
	}
	if oidcRefusedCount(t, db, "no-possession") != before+1 {
		t.Fatalf("mfa-only refusal was not audited cause=no-possession")
	}
}

func TestOIDCReauthPossession(t *testing.T) {
	forEngines(t, runOIDCReauthPossession)
}

// runOIDCReauthEpochInert: an epoch-inert (restored) identity is terminally
// refused, never opens a window (B2), matching completeLogin.
func runOIDCReauthEpochInert(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = time.Minute
	_, idp := configureProvider(t, auth, ctx, admin, "strict", service.ProviderInput{
		DisplayName: "strict", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "strict", "epoch-user", password)
	idp.AuthTime = time.Now()
	idp.AMR = []string{"mfa", "otp"}
	relogin := oidcLogin(t, auth, ctx, "strict", "epoch-user")
	// Restore the identity to an earlier epoch WITHOUT bumping the instance
	// epoch (which would trip the Phase-A epoch check first); this exercises the
	// reauth-branch epoch check directly.
	execRaw(t, db, "UPDATE external_identities SET credential_epoch = credential_epoch - 1 WHERE subject = 'epoch-user'")
	before := oidcRefusedCount(t, db, "epoch")
	if err := reauthOn(t, auth, ctx, "strict", "epoch-user", relogin.SessionToken); !isUnauth(err) {
		t.Fatalf("epoch-inert reauth should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "epoch") != before+1 {
		t.Fatalf("epoch-inert refusal was not audited cause=epoch")
	}
}

func TestOIDCReauthEpochInert(t *testing.T) {
	forEngines(t, runOIDCReauthEpochInert)
}

// runOIDCReauthProviderRebind: after a provider is replaced for the same
// byte-exact issuer, an identity recorded against the OLD provider must not
// authenticate through the replacement (A3, provider_id mismatch).
func runOIDCReauthProviderRebind(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = time.Minute
	idp, err := oidctest.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(idp.Close)
	for _, slug := range []string{"p1", "p2"} {
		if err := idp.RegisterRedirectURI(strings.TrimRight(auth.ExternalOrigin, "/") + "/api/v1/auth/oidc/" + slug + "/callback"); err != nil {
			t.Fatal(err)
		}
	}
	providers := &service.Providers{DB: auth.DB, Keyring: auth.Keyring, ExternalOrigin: auth.ExternalOrigin}
	policy := strptr(`{"amr_sets":[["mfa"]]}`)
	if _, err := providers.Put(ctx, service.LocalPrincipal(admin), "p1", service.ProviderInput{
		DisplayName: "p1", Issuer: idp.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: policy, Enabled: true,
	}); err != nil {
		t.Fatalf("put p1: %v", err)
	}
	linkOn(t, auth, ctx, "p1", "rebind-user", password)
	// Delete p1 (identity survives — provider_id is provenance, not an FK; the
	// tx rows cascade) then recreate the SAME issuer as a new provider p2.
	if err := providers.Delete(ctx, service.LocalPrincipal(admin), "p1"); err != nil {
		t.Fatalf("delete p1: %v", err)
	}
	if _, err := providers.Put(ctx, service.LocalPrincipal(admin), "p2", service.ProviderInput{
		DisplayName: "p2", Issuer: idp.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: policy, Enabled: true,
	}); err != nil {
		t.Fatalf("put p2: %v", err)
	}
	// Establish the acting session through p2 with a different identity on the
	// same account. The callback then returns the surviving p1 identity, which
	// isolates the external-identity provider provenance check.
	linkOn(t, auth, ctx, "p2", "p2-session-user", password)
	idp.AuthTime = time.Now()
	idp.AMR = []string{"mfa", "otp"}
	relogin := oidcLogin(t, auth, ctx, "p2", "p2-session-user")
	before := oidcRefusedCount(t, db, "reconciliation")
	if err := reauthOn(t, auth, ctx, "p2", "rebind-user", relogin.SessionToken); !isUnauth(err) {
		t.Fatalf("reauth through a replacement provider should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "reconciliation") != before+1 {
		t.Fatalf("provider-rebind refusal was not audited cause=reconciliation")
	}
}

func TestOIDCReauthProviderRebind(t *testing.T) {
	forEngines(t, runOIDCReauthProviderRebind)
}

// runOIDCReauthDowngrade: a reauth must be same-or-stronger than the session it
// re-authorizes. A phishing-resistant (WebAuthn) session cannot be
// re-authorized by federated evidence (capped at multi-factor); a single-factor
// session can (positive control).
func runOIDCReauthDowngrade(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = time.Minute
	_, idp := configureProvider(t, auth, ctx, admin, "strict", service.ProviderInput{
		DisplayName: "strict", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["hwk"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "strict", "down-user", password)
	idp.AuthTime = time.Now()
	// hwk is a true possession factor (mfa is not); the token both satisfies the
	// policy and carries possession, so it reaches the downgrade/positive legs.
	idp.AMR = []string{"hwk"}

	// A rank-2 (WebAuthn) session: OIDC evidence is rank 1, so this is a
	// downgrade. Forge the factor set directly — the rank comparison is under
	// test, not the WebAuthn ceremony.
	strong := oidcLogin(t, auth, ctx, "strict", "down-user")
	execRaw(t, db, "UPDATE sessions SET factors = '[\"webauthn\"]' WHERE id = '"+strong.SessionID+"'")
	before := oidcRefusedCount(t, db, "downgrade")
	if err := reauthOn(t, auth, ctx, "strict", "down-user", strong.SessionToken); !isUnauth(err) {
		t.Fatalf("federated reauth of a WebAuthn session should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "downgrade") != before+1 {
		t.Fatalf("downgrade refusal was not audited cause=downgrade")
	}

	// Positive control: a single-factor (password) session is re-authorized.
	weak := oidcLogin(t, auth, ctx, "strict", "down-user")
	if err := reauthOn(t, auth, ctx, "strict", "down-user", weak.SessionToken); err != nil {
		t.Fatalf("federated reauth of a single-factor session should succeed: %v", err)
	}
}

func TestOIDCReauthDowngrade(t *testing.T) {
	forEngines(t, runOIDCReauthDowngrade)
}

// runOIDCReauthProviderRace: a provider reconfigure that lands during the code
// exchange (Phase B) must make the Phase-C write refuse — the sweep always wins
// the race, so a stale policy evaluation cannot open a window (A4/TOCTOU).
func runOIDCReauthProviderRace(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin, password := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID, oidcAdministrator.password
	auth.ReauthWindow = time.Minute
	providers, idp := configureProvider(t, auth, ctx, admin, "race", service.ProviderInput{
		DisplayName: "race", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	linkOn(t, auth, ctx, "race", "race-user", password)
	idp.AuthTime = time.Now()
	idp.AMR = []string{"mfa"}
	// The acting session is provider-bound. The mid-exchange provider change
	// sweeps it, so Phase C must refuse rather than resurrecting its authority.
	relogin := oidcLogin(t, auth, ctx, "race", "race-user")
	// Tighten the provider's policy mid-exchange (bumps row_version, sweeps).
	fired := false
	idp.OnToken = func() {
		if fired {
			return
		}
		fired = true
		if _, err := providers.Put(ctx, service.LocalPrincipal(admin), "race", service.ProviderInput{
			DisplayName: "race", Issuer: idp.Issuer(), ClientID: "c", ClientSecret: "s", Scopes: "openid",
			AssurancePolicy: strptr(`{"amr_sets":[["hwk"]]}`), Enabled: true,
		}); err != nil {
			t.Errorf("mid-exchange provider Put: %v", err)
		}
	}
	before := oidcRefusedCount(t, db, "reconciliation")
	if err := reauthOn(t, auth, ctx, "race", "race-user", relogin.SessionToken); !isUnauth(err) {
		t.Fatalf("reauth racing a provider change should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "reconciliation") != before+1 {
		t.Fatalf("provider-race refusal was not audited cause=reconciliation")
	}
}

func TestOIDCReauthProviderRace(t *testing.T) {
	forEngines(t, runOIDCReauthProviderRace)
}

// runOIDCLoginProviderRace: a provider reconfigure that lands during a login's
// code exchange (Phase B) must make the Phase-C mint refuse (rA-5/TOCTOU). The
// guard row lock makes the A4 sweep deterministically win, so a live federated
// session swept mid-exchange is NOT resurrected by a fresh mint and no new
// session is created in its place.
func runOIDCLoginProviderRace(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	providers, idp := configureProvider(t, auth, ctx, admin, "race-login", service.ProviderInput{
		DisplayName: "race-login", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	linkOn(t, auth, ctx, "race-login", "race-login-user", oidcAdministrator.password)
	// A live federated session that the mid-exchange sweep must delete.
	victim := oidcLogin(t, auth, ctx, "race-login", "race-login-user")
	if _, err := auth.Identity(ctx, victim.SessionToken); err != nil {
		t.Fatalf("federated session should be live before the race: %v", err)
	}
	// A second login for the same subject; during its exchange, reconfigure the
	// provider (bumps row_version, sweeps every federated session for it).
	fired := false
	idp.OnToken = func() {
		if fired {
			return
		}
		fired = true
		if _, err := providers.Put(ctx, service.LocalPrincipal(admin), "race-login", service.ProviderInput{
			DisplayName: "race-login", Issuer: idp.Issuer(), ClientID: "c2", ClientSecret: "s", Scopes: "openid",
			Enabled: true,
		}); err != nil {
			t.Errorf("mid-exchange provider Put: %v", err)
		}
	}
	before := oidcRefusedCount(t, db, "reconciliation")
	start, err := auth.OIDCStart(ctx, "race-login", "login", "", "", "", false)
	if err != nil {
		t.Fatalf("login start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=race-login-user")
	if _, err := auth.OIDCCallback(ctx, "race-login", code, state, "", "", start.BindingCookie, ""); !isUnauth(err) {
		t.Fatalf("login racing a provider change should refuse the mint: %v", err)
	}
	if oidcRefusedCount(t, db, "reconciliation") != before+1 {
		t.Fatalf("login-race refusal was not audited cause=reconciliation")
	}
	// The swept session stays swept: the refused mint neither resurrected it nor
	// minted a replacement.
	if _, err := auth.Identity(ctx, victim.SessionToken); !isUnauth(err) {
		t.Fatalf("swept federated session was resurrected by the racing login: %v", err)
	}
}

func TestOIDCLoginProviderRace(t *testing.T) {
	forEngines(t, runOIDCLoginProviderRace)
}

// runOIDCLoginProviderDeleteRace: deleting a provider during a login's code
// exchange (Phase B) must refuse the Phase-C mint AND leave no session
// referencing the deleted provider. The delete locks the provider row before
// sweeping, and the FK cascade (A14) is the atomic backstop, so a live
// federated session and any raced mint are both gone once the provider is
// deleted — a compromised provider cannot be deleted yet keep live sessions.
func runOIDCLoginProviderDeleteRace(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	providers, idp := configureProvider(t, auth, ctx, admin, "race-del", service.ProviderInput{
		DisplayName: "race-del", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	linkOn(t, auth, ctx, "race-del", "race-del-user", oidcAdministrator.password)
	// A live federated session the mid-exchange delete's cascade must remove.
	victim := oidcLogin(t, auth, ctx, "race-del", "race-del-user")
	if _, err := auth.Identity(ctx, victim.SessionToken); err != nil {
		t.Fatalf("federated session should be live before the race: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE provider_id IS NOT NULL"); n != 1 {
		t.Fatalf("expected 1 federated session before the race, got %d", n)
	}
	// A second login for the same subject; during its exchange, delete the
	// provider. The mint's guard then finds the row gone and refuses.
	fired := false
	idp.OnToken = func() {
		if fired {
			return
		}
		fired = true
		if err := providers.Delete(ctx, service.LocalPrincipal(admin), "race-del"); err != nil {
			t.Errorf("mid-exchange provider Delete: %v", err)
		}
	}
	before := oidcRefusedCount(t, db, "reconciliation")
	start, err := auth.OIDCStart(ctx, "race-del", "login", "", "", "", false)
	if err != nil {
		t.Fatalf("login start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=race-del-user")
	if _, err := auth.OIDCCallback(ctx, "race-del", code, state, "", "", start.BindingCookie, ""); !isUnauth(err) {
		t.Fatalf("login racing a provider delete should refuse the mint: %v", err)
	}
	if oidcRefusedCount(t, db, "reconciliation") != before+1 {
		t.Fatalf("login-delete-race refusal was not audited cause=reconciliation")
	}
	// The victim is gone and no session survives referencing the deleted provider.
	if _, err := auth.Identity(ctx, victim.SessionToken); !isUnauth(err) {
		t.Fatalf("federated session survived its provider's deletion: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE provider_id IS NOT NULL"); n != 0 {
		t.Fatalf("expected 0 sessions after provider delete, got %d", n)
	}
}

func TestOIDCLoginProviderDeleteRace(t *testing.T) {
	forEngines(t, runOIDCLoginProviderDeleteRace)
}

// runOIDCProviderDeleteCascade proves the FK cascade (A14) is the atomic
// backstop, not the service sweep: deleting the provider ROW directly (raw
// SQL, bypassing SweepSessionsForProvider entirely) must still remove every
// session referencing it. On sqlite this establishes that PRAGMA
// foreign_keys=ON plus the ADD COLUMN ... REFERENCES ... ON DELETE CASCADE in
// 00007 actually enforces the cascade. Without the FK edit this test fails.
func runOIDCProviderDeleteCascade(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	configureProvider(t, auth, ctx, admin, "cascade", service.ProviderInput{
		DisplayName: "cascade", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	linkOn(t, auth, ctx, "cascade", "cascade-user", oidcAdministrator.password)
	sess := oidcLogin(t, auth, ctx, "cascade", "cascade-user")
	if _, err := auth.Identity(ctx, sess.SessionToken); err != nil {
		t.Fatalf("federated session should be live: %v", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE provider_id IS NOT NULL"); n != 1 {
		t.Fatalf("expected 1 federated session, got %d", n)
	}
	// Delete the provider row directly, NOT via the service (no sweep runs).
	execRaw(t, db, "DELETE FROM oidc_providers WHERE slug = 'cascade'")
	if n := queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE provider_id IS NOT NULL"); n != 0 {
		t.Fatalf("FK cascade did not remove sessions on provider row delete, got %d", n)
	}
	if _, err := auth.Identity(ctx, sess.SessionToken); !isUnauth(err) {
		t.Fatalf("session survived its provider's row deletion: %v", err)
	}
}

func TestOIDCProviderDeleteCascade(t *testing.T) {
	forEngines(t, runOIDCProviderDeleteCascade)
}

// runOIDCIATRejected: an ID token whose iat is in the future beyond the skew is
// refused (A validation completeness), audited cause=signature.
func runOIDCIATRejected(t *testing.T, db *store.DB) {
	ctx := t.Context()
	oidcAdministrator := oidcAdmin(t, db)
	auth, admin := oidcAdministrator.auth, oidcAdministrator.boot.PrincipalID
	_, idp := configureProvider(t, auth, ctx, admin, "idp", service.ProviderInput{
		DisplayName: "IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		Enabled: true,
	})
	// (a) iat far beyond the 2m skew.
	idp.IAT = time.Now().Add(time.Hour)
	start, err := auth.OIDCStart(ctx, "idp", "login", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=iat-user")
	before := oidcRefusedCount(t, db, "signature")
	if _, err := auth.OIDCCallback(ctx, "idp", code, state, "", "", start.BindingCookie, ""); !isUnauth(err) {
		t.Fatalf("a future-iat token should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "signature") != before+1 {
		t.Fatalf("future-iat refusal was not audited cause=signature")
	}

	// (b) iat absent entirely (go-oidc's expiry check passes it through to the
	// relying party's zero-check).
	idp.IAT = time.Time{}
	idp.OmitIAT = true
	start2, err := auth.OIDCStart(ctx, "idp", "login", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	code2, state2 := driveIdP(t, start2.AuthURL+"&sub=iat-user")
	before = oidcRefusedCount(t, db, "signature")
	if _, err := auth.OIDCCallback(ctx, "idp", code2, state2, "", "", start2.BindingCookie, ""); !isUnauth(err) {
		t.Fatalf("a missing-iat token should refuse: %v", err)
	}
	if oidcRefusedCount(t, db, "signature") != before+1 {
		t.Fatalf("missing-iat refusal was not audited cause=signature")
	}
}

func TestOIDCIATRejected(t *testing.T) {
	forEngines(t, runOIDCIATRejected)
}

// inviteOIDCIdentity provisions through the invitation service and then links
// through the real authenticated ceremony. Login itself has no write authority.
func inviteOIDCIdentity(t *testing.T, auth *service.Auth, ctx context.Context, admin domain.PrincipalID, slug, subject, username string) {
	t.Helper()
	grants := &service.Grants{DB: auth.DB, Auth: auth}
	invitation, err := grants.InviteMember(ctx, service.LocalPrincipal(admin), service.InviteSpec{Username: username, Delivery: "response"})
	if err != nil {
		t.Fatal(err)
	}
	const password = "invited subject password long enough"
	if err := auth.EstablishCredential(ctx, invitation.Authority, password); err != nil {
		t.Fatal(err)
	}
	login, err := auth.LocalLogin(ctx, username, password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	start, err := auth.OIDCStart(ctx, slug, "link", "", login.SessionToken, password, false)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub="+url.QueryEscape(subject))
	if _, err := auth.OIDCCallback(ctx, slug, code, state, "", "", "", login.SessionToken); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCUnknownIdentityNeverCreatesAccount(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		administrator := oidcAdmin(t, db)
		auth := administrator.auth
		ctx := t.Context()
		configureProvider(t, auth, ctx, administrator.boot.PrincipalID, "known-provider", service.ProviderInput{
			DisplayName: "Known provider", ClientID: "c", ClientSecret: "s", Scopes: "openid", Enabled: true,
		})
		accounts := queryInt(t, db, "SELECT COUNT(*) FROM accounts")
		identities := queryInt(t, db, "SELECT COUNT(*) FROM external_identities")
		principals := queryInt(t, db, "SELECT COUNT(*) FROM principals")
		sessions := queryInt(t, db, "SELECT COUNT(*) FROM sessions")
		for _, subject := range []string{"unknown-user", "oidc-admin", "Unknown-User"} {
			before := oidcRefusedCount(t, db, "unknown-identity")
			start, err := auth.OIDCStart(ctx, "known-provider", "login", "", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			code, state := driveIdP(t, start.AuthURL+"&sub="+subject)
			if _, err := auth.OIDCCallback(ctx, "known-provider", code, state, "", "", start.BindingCookie, ""); !isUnauth(err) {
				t.Fatalf("unknown identity %q: want uniform refusal, got %v", subject, err)
			}
			if got := oidcRefusedCount(t, db, "unknown-identity"); got != before+1 {
				t.Fatalf("unknown identity refusal audit count = %d, want %d", got, before+1)
			}
		}
		for _, check := range []struct {
			table string
			count int64
		}{{"accounts", accounts}, {"external_identities", identities}, {"principals", principals}, {"sessions", sessions}} {
			if got := queryInt(t, db, "SELECT COUNT(*) FROM "+check.table); got != check.count {
				t.Fatalf("refusal mutated %s: %d -> %d", check.table, check.count, got)
			}
		}
	})
}
