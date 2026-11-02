package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

const callbackSource = "192.0.2.90"

// callbackRig drives the production router, service and authorizer, with one
// limiter behind both the API and the service as in the running server.
type callbackRig struct {
	db      *store.DB
	auth    *service.Auth
	idp     *oidctest.IdP
	github  *githubFixture
	boot    admin
	seconds atomic.Int64
	limiter *admission.Limiter
	budget  *service.Budget
	handler http.Handler
	serial  int
}

func newCallbackRig(t *testing.T, db *store.DB) *callbackRig {
	t.Helper()
	boot := oidcAdmin(t, db)
	r := &callbackRig{db: db, auth: boot.auth, boot: boot}
	_, r.idp = configureProvider(t, boot.auth, t.Context(), boot.boot.PrincipalID, "idp", service.ProviderInput{
		DisplayName: "IdP", ClientID: "c", ClientSecret: "s", Scopes: "openid",
		AssurancePolicy: strptr(`{"amr_sets":[["mfa"]]}`), Enabled: true,
	})
	_, r.github = configureGithub(t, &signupHarness{t: t, db: db, auth: boot.auth})
	return r
}

func (r *callbackRig) now() time.Time {
	return time.Date(2026, 11, 2, 12, 0, 30, 0, time.UTC).Add(time.Duration(r.seconds.Load()) * time.Second)
}

// fresh gives each scenario its own full allowance.
func (r *callbackRig) fresh(t *testing.T) {
	t.Helper()
	limiter, err := admission.New(admission.Config{ArgonMemoryKiB: r.auth.KDF.MemoryKiB, Now: r.now})
	if err != nil {
		t.Fatal(err)
	}
	r.limiter, r.budget = limiter, service.NewBudgetWithClock(r.now)
	r.auth.Admission = limiter
	r.handler = server.New(&server.API{Auth: r.auth, Admission: limiter, RequestBudget: r.budget}, nil)
}

func (r *callbackRig) login(t *testing.T) service.LoginResult {
	t.Helper()
	login, err := r.auth.LocalLogin(t.Context(), "oidc-admin", r.boot.password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	return login
}

func (r *callbackRig) exhaustSource(t *testing.T) {
	t.Helper()
	for range admission.MetaPerIPPerMinute {
		if err := r.limiter.AdmitDiscovery(callbackSource); err != nil {
			t.Fatal(err)
		}
	}
}

func (r *callbackRig) exhaustBudget(t *testing.T, session service.LoginResult) {
	t.Helper()
	for range service.BudgetAuthenticatedAPIBurst {
		if err := r.budget.AdmitAuthenticatedAPI(session.SessionID); err != nil {
			t.Fatal(err)
		}
	}
}

// callbackCookie names a per-transaction cookie as the server derives it.
func callbackCookie(prefix, state, value string) *http.Cookie {
	suffix := state[strings.LastIndex(state, "_")+1:]
	if len(suffix) > 24 {
		suffix = suffix[:24]
	}
	return &http.Cookie{Name: prefix + suffix, Value: value}
}

type callbackCeremony struct{ flow, slug, code, state string }

// sessionBound starts a link ceremony, which binds the transaction to session.
func (r *callbackRig) sessionBound(t *testing.T, flow, session string) callbackCeremony {
	t.Helper()
	r.serial++
	subject := "callback-user-" + strconv.Itoa(r.serial)
	if flow == "oidc" {
		start, err := r.auth.OIDCStart(t.Context(), "idp", "link", "", "", "", session, r.boot.password, false)
		if err != nil {
			t.Fatal(err)
		}
		code, state := driveIdP(t, start.AuthURL+"&sub="+subject)
		return callbackCeremony{flow: flow, slug: "idp", code: code, state: state}
	}
	start, err := r.auth.OAuth2Start(t.Context(), "github", "link", "", "", "", session, r.boot.password, false)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(start.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	r.github.challenge = u.Query().Get("code_challenge")
	r.github.subject = int64(70000 + r.serial)
	return callbackCeremony{flow: flow, slug: "github", code: "oauth-code", state: start.State}
}

func (r *callbackRig) callback(ctx context.Context, c callbackCeremony, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	query := url.Values{"code": {c.code}, "state": {c.state}}.Encode()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, api.PathPrefix+"/auth/"+c.flow+"/"+c.slug+"/callback?"+query, nil)
	req.RemoteAddr = callbackSource + ":1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}

func (r *callbackRig) spent(t *testing.T, flow string) int64 {
	t.Helper()
	return queryInt(t, r.db, "SELECT COUNT(*) FROM "+flow+"_transactions WHERE consumed_at IS NOT NULL")
}

func (r *callbackRig) bindingRefusals(t *testing.T, flow string) int64 {
	t.Helper()
	return queryInt(t, r.db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth."+flow+"_refused' AND payload LIKE '%binding%'")
}

func TestCallbackAdmission(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		r := newCallbackRig(t, db)
		for _, flow := range []string{"oidc", "oauth2"} {
			t.Run(flow, func(t *testing.T) {
				t.Run("exhausted_source_is_a_retryable_429", func(t *testing.T) {
					r.throttledIsRetryable(t, flow, func(service.LoginResult) { r.exhaustSource(t) }, 61*time.Second)
				})
				t.Run("exhausted_session_budget_is_a_retryable_429", func(t *testing.T) {
					r.throttledIsRetryable(t, flow, func(session service.LoginResult) { r.exhaustBudget(t, session) }, 5*time.Second)
				})
				t.Run("overload_is_independent_of_state", func(t *testing.T) { r.overloadIndependentOfState(t, flow) })
				t.Run("mismatch_stays_uniform", func(t *testing.T) { r.mismatchStaysUniform(t, flow) })
				t.Run("fault_resolving_the_credential", func(t *testing.T) { r.faultIsNotBinding(t, flow) })
				t.Run("dead_credential_is_not_the_outcome_of_a_failed_login", func(t *testing.T) { r.deadCredentialProbe(t, flow) })
				t.Run("browser_callback_returns_to_the_spa", func(t *testing.T) { r.browserOverload(t, flow) })
				t.Run("login_without_a_credential_is_not_charged", func(t *testing.T) { r.loginUncharged(t, flow) })
			})
		}
		t.Run("fault_at_oidc_reauth_is_not_binding", r.reauthFault)
		t.Run("fault_at_federated_establish_is_not_binding", r.establishFault)
		t.Run("fault_at_saml_initiator_is_not_a_mismatch", r.samlFault)
		t.Run("cli_reauth_does_not_audit_overload_or_fault_as_unauthenticated", r.cliReauth)
	})
}

// A limit reached with a session-bound state answers 429 and leaves the ceremony
// to complete once the limit reopens.
func (r *callbackRig) throttledIsRetryable(t *testing.T, flow string, exhaust func(service.LoginResult), reopen time.Duration) {
	r.fresh(t)
	session := r.login(t)
	c := r.sessionBound(t, flow, session.SessionToken)
	exhaust(session)
	spent, refusals := r.spent(t, flow), r.bindingRefusals(t, flow)

	rec := r.callback(t.Context(), c, session.SessionToken)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("over the limit = %d %q Retry-After=%q, want 429 with Retry-After", rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
	}
	if got := r.spent(t, flow); got != spent {
		t.Errorf("throttled callback spent the ceremony: consumed %d -> %d", spent, got)
	}
	if got := r.bindingRefusals(t, flow); got != refusals {
		t.Errorf("throttled callback was audited as a binding refusal: %d -> %d", refusals, got)
	}

	r.seconds.Add(int64(reopen / time.Second))
	if retry := r.callback(t.Context(), c, session.SessionToken); retry.Code != http.StatusOK {
		t.Errorf("retry inside the ceremony lifetime = %d %q, want 200", retry.Code, retry.Body.String())
	}
}

// The answer to an over-limit caller must not depend on the state it presents,
// or a stolen state could be probed for its purpose and lifecycle.
func (r *callbackRig) overloadIndependentOfState(t *testing.T, flow string) {
	states := func(t *testing.T, owner service.LoginResult) []callbackCeremony {
		t.Helper()
		slug, code := "idp", "unused-code"
		var loginBound string
		if flow == "oidc" {
			start, err := r.auth.OIDCStart(t.Context(), "idp", "login", "", "", "", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(start.AuthURL)
			if err != nil {
				t.Fatal(err)
			}
			loginBound = u.Query().Get("state")
		} else {
			slug = "github"
			start, err := r.auth.OAuth2Start(t.Context(), "github", "login", "sign-in", "", "", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			loginBound = start.State
		}
		unknown, _, err := crypto.NewArtifact(crypto.ArtifactOIDCState)
		if err != nil {
			t.Fatal(err)
		}
		used := r.sessionBound(t, flow, owner.SessionToken)
		if rec := r.callback(t.Context(), used, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("spending a ceremony = %d", rec.Code)
		}
		return []callbackCeremony{
			r.sessionBound(t, flow, owner.SessionToken),
			used,
			{flow: flow, slug: slug, code: code, state: loginBound},
			{flow: flow, slug: slug, code: code, state: unknown},
		}
	}
	assertUniform := func(t *testing.T, ceremonies []callbackCeremony, bearers ...string) {
		t.Helper()
		spent := r.spent(t, flow)
		var transactionReads atomic.Int64
		restore := authn.SetQueryObserver(func(q string) {
			if strings.Contains(q, "_transactions") {
				transactionReads.Add(1)
			}
		})
		defer restore()
		var first *httptest.ResponseRecorder
		for _, bearer := range bearers {
			for i, c := range ceremonies {
				rec := r.callback(t.Context(), c, bearer)
				if first == nil {
					first = rec
				}
				if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
					t.Errorf("state %d = %d %q Retry-After=%q, want 429 with Retry-After", i, rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
				}
				if rec.Body.String() != first.Body.String() || rec.Header().Get("Retry-After") != first.Header().Get("Retry-After") {
					t.Errorf("state %d answered %q Retry-After=%q, first answered %q Retry-After=%q", i, rec.Body.String(), rec.Header().Get("Retry-After"), first.Body.String(), first.Header().Get("Retry-After"))
				}
			}
		}
		if got := transactionReads.Load(); got != 0 {
			t.Errorf("over-limit callbacks touched transactions %d times, want none", got)
		}
		restore()
		if got := r.spent(t, flow); got != spent {
			t.Errorf("over-limit callbacks spent ceremonies: %d -> %d", spent, got)
		}
	}

	t.Run("source", func(t *testing.T) {
		r.fresh(t)
		owner := r.login(t)
		ceremonies := states(t, owner)
		r.exhaustSource(t)
		unknownBearer, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		assertUniform(t, ceremonies, owner.SessionToken, unknownBearer)
	})
	t.Run("session_budget", func(t *testing.T) {
		r.fresh(t)
		owner := r.login(t)
		ceremonies := states(t, owner)
		r.exhaustBudget(t, owner)
		assertUniform(t, ceremonies, owner.SessionToken)
	})
}

// A real binding failure keeps one refusal, whatever the presented credential
// turned out to be, and still spends the ceremony.
func (r *callbackRig) mismatchStaysUniform(t *testing.T, flow string) {
	r.fresh(t)
	owner := r.login(t)
	other := r.login(t)
	gated := r.login(t)
	execRaw(t, r.db, "UPDATE sessions SET enrolment_required = (1=1) WHERE id = '"+gated.SessionID+"'")
	unknown, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, tc := range []struct{ name, bearer string }{
		{"different_live_session", other.SessionToken},
		{"unknown_artifact", unknown},
		{"no_credential", ""},
		{"class_refused_session", gated.SessionToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := r.sessionBound(t, flow, owner.SessionToken)
			spent, refusals := r.spent(t, flow), r.bindingRefusals(t, flow)
			denials := queryInt(t, r.db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.artifact_class_refused'")
			rec := r.callback(t.Context(), c, tc.bearer)
			if rec.Code != http.StatusUnauthorized || rec.Header().Get("Retry-After") != "" {
				t.Errorf("binding mismatch = %d Retry-After=%q, want a uniform 401", rec.Code, rec.Header().Get("Retry-After"))
			}
			if body == "" {
				body = rec.Body.String()
			}
			if rec.Body.String() != body {
				t.Errorf("binding mismatch body %q differs from %q", rec.Body.String(), body)
			}
			if got := r.spent(t, flow); got != spent+1 {
				t.Errorf("binding mismatch must spend the ceremony: %d -> %d", spent, got)
			}
			if got := r.bindingRefusals(t, flow); got != refusals+1 {
				t.Errorf("binding mismatch must be audited as binding: %d -> %d", refusals, got)
			}
			wantDenials := denials
			if tc.name == "class_refused_session" {
				wantDenials++
			}
			if got := queryInt(t, r.db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.artifact_class_refused'"); got != wantDenials {
				t.Errorf("class denials %d -> %d, want %d", denials, got, wantDenials)
			}
		})
	}
}

// breakSessionRow makes reading this session fail while the rest of the datastore
// keeps working, so the refusal path a wrong caller would take could still commit.
// It returns the repair.
func breakSessionRow(t *testing.T, db *store.DB, sessionID string) func() {
	t.Helper()
	where := " WHERE id = '" + sessionID + "'"
	if db.Engine() == store.EnginePostgres {
		method := queryString(t, db, "SELECT auth_method FROM sessions"+where)
		execRaw(t, db, "ALTER TABLE sessions ALTER COLUMN auth_method DROP NOT NULL")
		execRaw(t, db, "UPDATE sessions SET auth_method = NULL"+where)
		return func() {
			execRaw(t, db, "UPDATE sessions SET auth_method = '"+method+"'"+where)
			execRaw(t, db, "ALTER TABLE sessions ALTER COLUMN auth_method SET NOT NULL")
		}
	}
	created := queryString(t, db, "SELECT created_at FROM sessions"+where)
	execRaw(t, db, "UPDATE sessions SET created_at = 'not-a-time'"+where)
	return func() { execRaw(t, db, "UPDATE sessions SET created_at = '"+created+"'"+where) }
}

// A fault while resolving the session is neither a binding refusal nor a spent
// ceremony.
func (r *callbackRig) faultIsNotBinding(t *testing.T, flow string) {
	r.fresh(t)
	session := r.login(t)
	c := r.sessionBound(t, flow, session.SessionToken)
	spent, refusals := r.spent(t, flow), r.bindingRefusals(t, flow)
	repair := breakSessionRow(t, r.db, session.SessionID)
	rec := r.callback(t.Context(), c, session.SessionToken)
	repair()
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("fault = %d %q, want 500", rec.Code, rec.Body.String())
	}
	if got := r.spent(t, flow); got != spent {
		t.Errorf("fault spent the ceremony: %d -> %d", spent, got)
	}
	if got := r.bindingRefusals(t, flow); got != refusals {
		t.Errorf("fault was audited as a binding refusal: %d -> %d", refusals, got)
	}
	if retry := r.callback(t.Context(), c, session.SessionToken); retry.Code != http.StatusOK {
		t.Errorf("retry after a fault = %d %q, want 200", retry.Code, retry.Body.String())
	}
}

// A browser cannot read a 429 at the callback URL, so overload keeps returning
// to the SPA with the error code, and the ceremony stays unspent.
func (r *callbackRig) browserOverload(t *testing.T, flow string) {
	r.fresh(t)
	session := r.login(t)
	c := r.sessionBound(t, flow, session.SessionToken)
	r.exhaustSource(t)
	spent := r.spent(t, flow)
	rec := r.callback(t.Context(), c, session.SessionToken, callbackCookie("__Host-hikyo-oidc-browser-", c.state, "link"))
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusSeeOther || location.Path != "/auth/oidc/done" || location.Query().Get("error") != "too_many_requests" || location.Query().Get("purpose") != "link" {
		t.Errorf("browser overload = %d Location=%q", rec.Code, location)
	}
	if got := r.spent(t, flow); got != spent {
		t.Errorf("browser overload spent the ceremony: %d -> %d", spent, got)
	}
}

// browserBoundLogin starts a login for an identity already linked to the admin,
// returning the ceremony and its binding cookie.
func (r *callbackRig) browserBoundLogin(t *testing.T, flow string) (callbackCeremony, *http.Cookie) {
	t.Helper()
	if flow == "oidc" {
		r.serial++
		subject := "callback-login-" + strconv.Itoa(r.serial)
		linkOn(t, r.auth, t.Context(), "idp", subject, r.boot.password)
		start, err := r.auth.OIDCStart(t.Context(), "idp", "login", "", "", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		code, state := driveIdP(t, start.AuthURL+"&sub="+subject)
		return callbackCeremony{flow: flow, slug: "idp", code: code, state: state}, callbackCookie("__Host-hikyo-oidc-tx-", state, start.BindingCookie)
	}
	session := r.login(t)
	link := r.sessionBound(t, flow, session.SessionToken)
	if _, err := r.auth.OAuth2Callback(t.Context(), "github", link.code, link.state, "", "", "", session.SessionToken); err != nil {
		t.Fatal(err)
	}
	start := r.github.start(t, r.auth, "sign-in")
	return callbackCeremony{flow: flow, slug: "github", code: "oauth-code", state: start.State}, callbackCookie("__Host-hikyo-oidc-tx-", start.State, start.BindingCookie)
}

// Only a presented credential is charged: a browser-bound login with no session
// still completes on an exhausted source, as it did before.
func (r *callbackRig) loginUncharged(t *testing.T, flow string) {
	r.fresh(t)
	c, binding := r.browserBoundLogin(t, flow)
	r.exhaustSource(t)
	if rec := r.callback(t.Context(), c, "", binding); rec.Code != http.StatusOK {
		t.Errorf("login with no credential on an exhausted source = %d %q, want 200", rec.Code, rec.Body.String())
	}
}

// A stale credential on a browser-bound login is incidental, so a failure of that
// login for another reason must not be signalled as an artifact refusal.
func (r *callbackRig) deadCredentialProbe(t *testing.T, flow string) {
	r.fresh(t)
	c, binding := r.browserBoundLogin(t, flow)
	dead, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.New(&server.API{Auth: r.auth, Admission: r.limiter, RequestBudget: r.budget, Refusals: service.NewRefusalSignals(r.db)}, nil)
	before := queryString(t, r.db, "SELECT state FROM auth_refusal_window WHERE id = 1")
	consume := map[string]string{"oidc": "ConsumeOIDCTransaction", "oauth2": "ConsumeOAuth2Transaction"}[flow]
	var failed atomic.Bool
	restore := authn.SetMutationFailureObserver(func(q string) error {
		if strings.Contains(q, "-- name: "+consume+" ") {
			failed.Store(true)
			return errors.New("injected consume fault")
		}
		return nil
	})
	defer restore()
	r.handler = handler
	rec := r.callback(t.Context(), c, dead, binding)
	restore()
	if rec.Code != http.StatusInternalServerError || !failed.Load() {
		t.Errorf("fault = %d %q fired=%v, want 500", rec.Code, rec.Body.String(), failed.Load())
	}
	if got := queryString(t, r.db, "SELECT state FROM auth_refusal_window WHERE id = 1"); got != before {
		t.Errorf("dead credential was signalled as a refusal: %s -> %s", before, got)
	}
}

func (r *callbackRig) reauthFault(t *testing.T) {
	r.fresh(t)
	ctx := t.Context()
	linkOn(t, r.auth, ctx, "idp", "callback-reauth", r.boot.password)
	r.idp.AuthTime = time.Now()
	r.idp.AMR = []string{"mfa", "otp"}
	session := oidcLogin(t, r.auth, ctx, "idp", "callback-reauth")
	start, err := r.auth.OIDCStart(ctx, "idp", "reauth", "", "", "env_prod", session.SessionToken, "", false)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=callback-reauth")
	refusals := r.bindingRefusals(t, "oidc")
	// The IdP answers on its own goroutine, so the row breaks on this one while
	// the exchange waits.
	reached, proceed := make(chan struct{}), make(chan struct{})
	r.idp.OnToken = func() {
		close(reached)
		<-proceed
	}
	// Runs before the IdP's cleanup, which would otherwise wait on this handler.
	var release sync.Once
	proceedOnce := func() { release.Do(func() { close(proceed) }) }
	defer func() {
		proceedOnce()
		r.idp.OnToken = nil
	}()
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- r.callback(ctx, callbackCeremony{flow: "oidc", slug: "idp", code: code, state: state}, session.SessionToken)
	}()
	var rec *httptest.ResponseRecorder
	select {
	case <-reached:
		repair := breakSessionRow(t, r.db, session.SessionID)
		proceedOnce()
		rec = <-answered
		repair()
	case rec = <-answered:
		t.Fatalf("callback answered %d before the exchange", rec.Code)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("fault = %d %q, want 500", rec.Code, rec.Body.String())
	}
	if got := r.bindingRefusals(t, "oidc"); got != refusals {
		t.Errorf("fault was audited as a binding refusal: %d -> %d", refusals, got)
	}
}

func (r *callbackRig) establishFault(t *testing.T) {
	r.fresh(t)
	ctx := t.Context()
	r.github.subject = 60950
	invite, err := (&service.Grants{DB: r.db}).InviteMember(ctx, service.LocalPrincipal(root), service.InviteSpec{Username: "callback-claim", Delivery: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	begin := func(purpose, session, proof string) service.OIDCStartResult {
		start, err := r.auth.OAuth2Start(ctx, "github", purpose, "", "", "", session, proof, false)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(start.AuthURL)
		if err != nil {
			t.Fatal(err)
		}
		r.github.challenge = u.Query().Get("code_challenge")
		return start
	}
	claim := begin("claim", "", invite.Authority)
	claimed, err := r.auth.OAuth2Callback(ctx, "github", "oauth-code", claim.State, "", "", claim.BindingCookie, "")
	if err != nil {
		t.Fatal(err)
	}
	session := claimed.Login
	establish := begin("establish", session.SessionToken, "")
	refusals := r.bindingRefusals(t, "oauth2")
	var repair func()
	r.github.onUser = func() { repair = breakSessionRow(t, r.db, session.SessionID) }
	defer func() { r.github.onUser = nil }()
	rec := r.callback(ctx, callbackCeremony{flow: "oauth2", slug: "github", code: "oauth-code", state: establish.State}, session.SessionToken)
	if repair == nil {
		t.Fatal("the exchange never ran")
	}
	repair()
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("fault = %d %q, want 500", rec.Code, rec.Body.String())
	}
	if got := r.bindingRefusals(t, "oauth2"); got != refusals {
		t.Errorf("fault was audited as a binding refusal: %d -> %d", refusals, got)
	}
}

func (r *callbackRig) samlFault(t *testing.T) {
	r.fresh(t)
	ctx := t.Context()
	idp := configureSAMLProvider(t, r.auth, r.boot.boot.PrincipalID)
	session := r.login(t)
	start, err := r.auth.SAMLStart(ctx, "saml-idp", "link", "", session.SessionToken, r.boot.password)
	if err != nil {
		t.Fatal(err)
	}
	response := samlResponseForStart(t, idp, start, "_response_callback", "_assertion_callback")
	relay := samlAuditRelayState(t, start.RedirectURL)
	mismatches := func() int64 {
		return queryInt(t, r.db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.saml_login' AND payload LIKE '%initiator-mismatch%'")
	}
	before, spent := mismatches(), r.spent(t, "saml")
	repair := breakSessionRow(t, r.db, session.SessionID)
	_, err = r.auth.SAMLACS(ctx, "saml-idp", response, relay, start.InitiatorCookie)
	repair()
	if err == nil || isUnauth(err) {
		t.Errorf("fault = %v, want the fault itself", err)
	}
	if got := mismatches(); got != before {
		t.Errorf("fault was audited as an initiator mismatch: %d -> %d", before, got)
	}
	if got := r.spent(t, "saml"); got != spent {
		t.Errorf("fault spent the ceremony: %d -> %d", spent, got)
	}
	if _, err := r.auth.SAMLACS(ctx, "saml-idp", response, relay, start.InitiatorCookie); err != nil {
		t.Errorf("retry after a fault: %v", err)
	}
}

func (r *callbackRig) cliReauth(t *testing.T) {
	r.fresh(t)
	ctx := t.Context()
	r.auth.ReauthWindow = 5 * time.Minute
	linkOn(t, r.auth, ctx, "idp", "callback-handoff", r.boot.password)
	r.idp.AuthTime = time.Now()
	r.idp.AMR = []string{"mfa", "otp"}
	browser := oidcLogin(t, r.auth, ctx, "idp", "callback-handoff")
	identity, err := r.auth.Identity(ctx, browser.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{"reveal", "read"} {
		execRaw(t, r.db, "INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_callback_"+capability+"', '"+string(identity.Principal)+"', '"+capability+"', 'org_a', 'prj_a1', 'env_a1', "+ts+")")
	}
	cliSession := r.login(t)
	verifierHash := sha256.Sum256([]byte("callback admission handoff verifier"))
	verifier := base64.RawURLEncoding.EncodeToString(verifierHash[:])
	challengeHash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeHash[:])
	intent := disclosureReauthIntent(t, service.PurposeReveal, "env_a1", []string{keyA1})
	const redirect = "http://127.0.0.1:40127/callback"
	failures := func() int64 {
		return queryInt(t, r.db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.cli_reauth_handoff' AND outcome = 'failure' AND CAST(payload AS TEXT) LIKE '%unauthenticated%'")
	}
	overloaded := operation.WithPreauthenticationAdmission(ctx, func() error { return admission.ErrOverloaded })
	before := failures()

	if _, err := r.auth.StartCLIReauth(overloaded, cliSession.SessionToken, intent, challenge, redirect); !errors.Is(err, admission.ErrOverloaded) {
		t.Errorf("overloaded start = %v", err)
	}
	handoff, err := r.auth.StartCLIReauth(ctx, cliSession.SessionToken, intent, challenge, redirect)
	if err != nil {
		t.Fatal(err)
	}
	start, err := r.auth.OIDCStart(ctx, "idp", "reauth", "", "", "env_a1", browser.SessionToken, "", true)
	if err != nil {
		t.Fatal(err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=callback-handoff")
	completed, err := r.auth.OIDCCallback(ctx, "idp", code, state, "", "", "", browser.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.auth.ApproveCLIReauth(overloaded, service.Bearer(completed.Login.SessionToken), handoff.State); !errors.Is(err, admission.ErrOverloaded) {
		t.Errorf("overloaded approval = %v", err)
	}
	approved, err := r.auth.ApproveCLIReauth(ctx, service.Bearer(completed.Login.SessionToken), handoff.State)
	if err != nil {
		t.Fatal(err)
	}
	repair := breakSessionRow(t, r.db, cliSession.SessionID)
	_, err = r.auth.RedeemCLIReauth(ctx, approved.Code, verifier)
	repair()
	if err == nil || isUnauth(err) {
		t.Errorf("fault at redemption = %v, want the fault itself", err)
	}
	if got := failures(); got != before {
		t.Errorf("overload or fault was audited as unauthenticated: %d -> %d", before, got)
	}
	if _, err := r.auth.RedeemCLIReauth(ctx, approved.Code, verifier); err != nil {
		t.Errorf("redemption retry after a fault: %v", err)
	}
}
