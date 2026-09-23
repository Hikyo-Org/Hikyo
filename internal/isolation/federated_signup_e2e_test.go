package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// Federated sign-up on the OIDC kind (#607; docs/spec/social-signin.md
// section 4 and the #604 scenarios as reordered by section 14.2), driven end
// to end through a real test IdP on both engines: intent at start, the
// callback's intent-blind resolution, the uncharged admission gate, the
// `signup` budget, the verified-email assertion, the allowlist, the landings
// under the authority principal, and the audit each leg writes.

func TestFederatedSignup(t *testing.T)         { forEngines(t, runFederatedSignup) }
func TestFederatedSignupLandings(t *testing.T) { forEngines(t, runFederatedSignupLandings) }
func TestFederatedSignupOneQueryPath(t *testing.T) {
	forEngines(t, runFederatedSignupOneQueryPath)
}
func TestOIDCProviderIssuerDepartures(t *testing.T) { forEngines(t, runOIDCProviderIssuerDepartures) }
func TestOrgRenameFormula(t *testing.T)             { forEngines(t, runOrgRenameFormula) }
func TestFederatedSignupRetryRefundsCharge(t *testing.T) {
	forEngines(t, runFederatedSignupRetryRefundsCharge)
}

// runFederatedSignupRetryRefundsCharge pins the charge per attempt: a
// sign-up attempt that charged the `signup` budget and then rolled back for a
// serialization retry must not stay counted. The retried attempt charges once
// and commits, so exactly BudgetSignupPerHour admitted sign-ups fit in the
// window, the retried one included, and the next overflows.
func runFederatedSignupRetryRefundsCharge(t *testing.T, db *store.DB) {
	ctx := t.Context()
	h := newSignupHarness(t, db)
	h.provider("google", service.ProviderInput{})
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	induced := 0
	restore := authn.SetMutationFailureObserver(func(query string) error {
		if induced == 0 && strings.Contains(query, "-- name: InsertExternalIdentity ") {
			induced++
			return fmt.Errorf("%w: induced serialization retry", store.ErrRetrySerialization)
		}
		return nil
	})
	_, err := h.login("google", h.fresh(), "sign-up", "", googleClaims("retry@acme.example"))
	restore()
	if err != nil || induced != 1 {
		t.Fatalf("retried sign-up = %v (induced %d), want success after one retry", err, induced)
	}
	for i := 1; i < service.BudgetSignupPerHour; i++ {
		if _, err := h.login("google", h.fresh(), "sign-up", "", googleClaims("more@acme.example")); err != nil {
			t.Fatalf("admitted sign-up %d/%d after the retried one: %v", i+1, service.BudgetSignupPerHour, err)
		}
	}
	if _, err := h.login("google", h.fresh(), "sign-up", "", googleClaims("late@acme.example")); !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("sign-up past the budget = %v, want the shared 429", err)
	}
}

// signupHarness is one engine's auth surface with the registration policy and
// the `signup` budget wired, as internal/app wires them.
type signupHarness struct {
	t    *testing.T
	db   *store.DB
	auth *service.Auth
	reg  *service.Registration
	idps map[string]*oidctest.IdP
	n    int
}

func newSignupHarness(t *testing.T, db *store.DB) *signupHarness {
	t.Helper()
	auth := authService(t, db)
	auth.ExternalOrigin = "https://hikyo.test"
	reg := newRegistration(t, service.RegistrationConfig{DB: db, Auth: auth, PublicOriginExplicit: true})
	if err := auth.EnableSignup(reg, service.NewBudget()); err != nil {
		t.Fatal(err)
	}
	return &signupHarness{t: t, db: db, auth: auth, reg: reg, idps: map[string]*oidctest.IdP{}}
}

// provider configures an OIDC provider row (openid + email, the #598 write-
// time requirement) against its own fixture IdP.
func (h *signupHarness) provider(slug string, in service.ProviderInput) *oidctest.IdP {
	h.t.Helper()
	in.DisplayName, in.ClientID, in.ClientSecret, in.Enabled = slug, "client-"+slug, "secret", true
	if in.Scopes == "" {
		in.Scopes = "openid email"
	}
	_, idp := configureProvider(h.t, h.auth, h.t.Context(), root, slug, in)
	h.idps[slug] = idp
	return idp
}

// login drives one purpose-login round trip for subject with the given
// intent and sign-up scope, the token carrying claims.
func (h *signupHarness) login(slug, subject, intent, org string, claims map[string]any) (service.OIDCCallbackResult, error) {
	h.t.Helper()
	ctx := h.t.Context()
	start, err := h.auth.OIDCStart(ctx, slug, "login", intent, org, "", "", "", false)
	if err != nil {
		h.t.Fatalf("oidc start (%s, %q, %q): %v", slug, intent, org, err)
	}
	h.idps[slug].Claims = claims
	code, state := driveIdP(h.t, start.AuthURL+"&sub="+subject)
	return h.auth.OIDCCallback(ctx, slug, code, state, "", "", start.BindingCookie, "")
}

// fresh returns a subject no account holds yet.
func (h *signupHarness) fresh() string {
	h.n++
	return fmt.Sprintf("subject-%d", h.n)
}

func googleClaims(email string) map[string]any {
	return map[string]any{"email": email, "email_verified": true}
}

func entraClaims(email string) map[string]any {
	return map[string]any{"email": email, "xms_edov": true}
}

func (h *signupHarness) count(q string) int64 {
	h.t.Helper()
	return queryInt(h.t, h.db, q)
}

// refusedCount is the number of registration.signup_refused rows with cause.
func (h *signupHarness) refusedCount(cause string) int64 {
	return h.count("SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_refused' " +
		"AND payload LIKE '%\"cause\":\"" + cause + "\"%'")
}

func (h *signupHarness) accounts() int64 { return h.count("SELECT COUNT(*) FROM accounts") }

func wantUniformRefusal(t *testing.T, label string, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("%s: %v, want the uniform refusal", label, err)
	}
}

func runFederatedSignup(t *testing.T, db *store.DB) {
	ctx := t.Context()
	h := newSignupHarness(t, db)
	h.provider("google", service.ProviderInput{})
	h.provider("entra", service.ProviderInput{})

	// Intent is a login-only field; a scope needs a sign-up intent (#604 d1,
	// spec section 4). Both refuse at start in the ErrBadPurpose shape, before
	// any transaction row exists.
	before := h.count("SELECT COUNT(*) FROM oidc_transactions")
	for _, c := range []struct {
		purpose, intent, org string
	}{
		{"link", "sign-in", ""},
		{"link", "sign-up", ""},
		{"reauth", "sign-up", ""},
		{"login", "sign-in", string(orgA)},
		{"login", "", string(orgA)},
		{"login", "join", ""},
	} {
		_, err := h.auth.OIDCStart(ctx, "google", c.purpose, c.intent, c.org, "", "", "", false)
		if !errors.Is(err, service.ErrBadPurpose) {
			t.Fatalf("start %+v = %v, want ErrBadPurpose", c, err)
		}
	}
	if n := h.count("SELECT COUNT(*) FROM oidc_transactions"); n != before {
		t.Fatalf("refused starts wrote %d transaction rows", n-before)
	}
	// Absent intent is stored as sign-in; a sign-up scope rides the row.
	if _, err := h.auth.OIDCStart(ctx, "google", "login", "", "", "", "", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.auth.OIDCStart(ctx, "google", "login", "sign-up", string(orgA), "", "", "", false); err != nil {
		t.Fatal(err)
	}
	if n := h.count("SELECT COUNT(*) FROM oidc_transactions WHERE purpose = 'login' AND intent = 'sign-in' AND signup_scope_org_id IS NULL"); n != 1 {
		t.Fatalf("absent intent stored as sign-in: %d rows, want 1", n)
	}
	if n := h.count("SELECT COUNT(*) FROM oidc_transactions WHERE intent = 'sign-up' AND signup_scope_org_id = 'org_a'"); n != 1 {
		t.Fatalf("sign-up scope rows: %d, want 1", n)
	}

	// Closed instance: an unknown identity under sign-up refuses `closed`,
	// uncharged; an unknown org is the same closed door; under sign-in it is
	// `unknown-identity` and no registration path is entered at all.
	accounts := h.accounts()
	_, err := h.login("google", h.fresh(), "sign-up", "", googleClaims("a@acme.example"))
	wantUniformRefusal(t, "sign-up on a closed instance", err)
	_, err = h.login("google", h.fresh(), "sign-up", "org_nope", googleClaims("a@acme.example"))
	wantUniformRefusal(t, "sign-up into an unknown org", err)
	if n := h.refusedCount("closed"); n != 2 {
		t.Fatalf("closed refusals = %d, want 2", n)
	}
	registrationEvents := h.count("SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'registration.%'")
	_, err = h.login("google", h.fresh(), "sign-in", "", googleClaims("a@acme.example"))
	wantUniformRefusal(t, "unknown identity under sign-in", err)
	_, err = h.login("google", h.fresh(), "", "", googleClaims("a@acme.example"))
	wantUniformRefusal(t, "unknown identity under an absent intent", err)
	if n := h.count("SELECT COUNT(*) FROM audit_instance_events WHERE type LIKE 'registration.%'"); n != registrationEvents {
		t.Fatalf("a sign-in miss wrote %d registration events", n-registrationEvents)
	}
	if n := h.count("SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.oidc_refused' " +
		"AND payload LIKE '%\"cause\":\"unknown-identity\"%' AND payload LIKE '%\"intent\":\"sign-in\"%'"); n != 2 {
		t.Fatalf("unknown-identity refusals carrying intent sign-in = %d, want 2", n)
	}

	// An instance policy admitting Google only (landing none, zero grants).
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	// An unadmitted provider refuses `predicate` before the budget. Twenty of
	// them, the whole hourly `signup` budget: had any been charged, the
	// admitted sign-up after them would overflow.
	for range service.BudgetSignupPerHour {
		_, err := h.login("entra", h.fresh(), "sign-up", "", entraClaims("b@acme.example"))
		wantUniformRefusal(t, "unadmitted provider", err)
	}
	if n := h.refusedCount("predicate"); n != int64(service.BudgetSignupPerHour) {
		t.Fatalf("predicate refusals = %d", n)
	}
	charged := 0
	signedUp := h.fresh()
	first, err := h.login("google", signedUp, "sign-up", "", googleClaims("first@acme.example"))
	charged++
	if err != nil || first.Login.SessionToken == "" {
		t.Fatalf("admitted Google-shaped sign-up: %+v, %v", first, err)
	}
	if n := h.accounts(); n != accounts+1 {
		t.Fatalf("accounts after one admitted sign-up: +%d, want +1", n-accounts)
	}
	if n := h.count("SELECT COUNT(*) FROM accounts a JOIN external_identities e ON e.account_id = a.id " +
		"WHERE e.subject = '" + signedUp + "' AND a.username LIKE 'oidc-acc_%' AND a.email IS NULL AND a.email_verified_at IS NULL"); n != 1 {
		t.Fatalf("the signed-up account is not the handle-named, email-less shape (%d)", n)
	}
	if n := h.count("SELECT COUNT(*) FROM grants g JOIN accounts a ON a.principal_id = g.principal_id " +
		"JOIN external_identities e ON e.account_id = a.id WHERE e.subject = '" + signedUp + "'"); n != 0 {
		t.Fatalf("a landing-none sign-up holds %d grants", n)
	}
	for _, q := range []string{
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_admitted' AND actor_class = 'unauthenticated' " +
			"AND payload LIKE '%\"verified_by\":\"email_verified\"%' AND payload LIKE '%first@acme.example%'",
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_completed' AND actor_class = 'human' " +
			"AND payload LIKE '%\"landing\":\"none\"%'",
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.oidc_login' AND payload LIKE '%\"intent\":\"sign-up\"%'",
	} {
		if n := h.count(q); n != 1 {
			t.Fatalf("%s = %d, want 1", q, n)
		}
	}

	// A known identity signs in under either intent, uncharged, and never
	// enters the registration path.
	for _, intent := range []string{"sign-in", "sign-up", ""} {
		res, err := h.login("google", signedUp, intent, "", map[string]any{})
		if err != nil || res.Login.SessionToken == "" {
			t.Fatalf("known identity under intent %q: %v", intent, err)
		}
	}
	if n := h.accounts(); n != accounts+1 {
		t.Fatal("a known identity's login created an account")
	}

	// Verified-email fixtures (#598): refused `no-verified-email` with
	// verified_by none, charged (after the budget), evaluated before the
	// allowlist. The string "true" is not true; a present `false` refuses
	// whatever the other recognised claim says.
	for label, claims := range map[string]map[string]any{
		"missing claim":         {"email": "x@acme.example"},
		"false":                 {"email": "x@acme.example", "email_verified": false},
		"string true":           {"email": "x@acme.example", "email_verified": "true"},
		"empty email":           {"email": "", "email_verified": true},
		"no email":              {"email_verified": true},
		"non-string email":      {"email": 7, "email_verified": true},
		"false beside xms_edov": {"email": "x@acme.example", "email_verified": false, "xms_edov": true},
		"xms_edov string":       {"email": "x@acme.example", "xms_edov": "true"},
	} {
		_, err := h.login("google", h.fresh(), "sign-up", "", claims)
		charged++
		wantUniformRefusal(t, label, err)
	}
	if n := h.count("SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_refused' " +
		"AND payload LIKE '%\"cause\":\"no-verified-email\"%' AND payload LIKE '%\"verified_by\":\"none\"%'"); n != 8 {
		t.Fatalf("no-verified-email refusals with verified_by none = %d, want 8", n)
	}

	// The Entra-shaped token admits through xms_edov once Entra is an entry;
	// the allowlist (one string claim) refuses `predicate`, after the
	// verified-email check.
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{
			oidcEntry("google"),
			{Provider: domain.ProviderRef{Kind: domain.ProviderOIDC, Slug: "entra"}, Claim: "tid", Values: []string{"tenant-a"}},
		},
		Landing: service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	withTenant := func(claims map[string]any, tid any) map[string]any {
		claims["tid"] = tid
		return claims
	}
	if _, err := h.login("entra", h.fresh(), "sign-up", "", withTenant(entraClaims("e@acme.example"), "tenant-a")); err != nil {
		t.Fatalf("admitted Entra-shaped sign-up: %v", err)
	}
	charged++
	predicate := h.refusedCount("predicate")
	for label, tid := range map[string]any{"other tenant": "tenant-b", "non-string": 1, "absent": nil} {
		claims := entraClaims("e@acme.example")
		if tid != nil {
			claims["tid"] = tid
		}
		_, err := h.login("entra", h.fresh(), "sign-up", "", claims)
		charged++
		wantUniformRefusal(t, "allowlist "+label, err)
	}
	if n := h.refusedCount("predicate"); n != predicate+3 {
		t.Fatalf("allowlist misses = %d, want 3", n-predicate)
	}
	noVerified := h.refusedCount("no-verified-email")
	_, err = h.login("entra", h.fresh(), "sign-up", "", map[string]any{"email": "e@acme.example", "tid": "tenant-b"})
	charged++
	wantUniformRefusal(t, "unverified and not allowlisted", err)
	if h.refusedCount("no-verified-email") != noVerified+1 || h.refusedCount("predicate") != predicate+3 {
		t.Fatal("verified-email is not evaluated before the allowlist")
	}

	// The budget: every charged leg above counted, the known logins did not.
	// Exhaust it exactly; the next admitted identity overflows to the shared
	// 429 with a `budget` refusal and no account.
	for charged < service.BudgetSignupPerHour {
		_, err := h.login("google", h.fresh(), "sign-up", "", map[string]any{"email": "x@acme.example"})
		charged++
		wantUniformRefusal(t, "charging", err)
	}
	accounts = h.accounts()
	_, err = h.login("google", h.fresh(), "sign-up", "", googleClaims("late@acme.example"))
	if !errors.Is(err, admission.ErrOverloaded) {
		t.Fatalf("sign-up past the budget = %v, want the shared 429", err)
	}
	if h.refusedCount("budget") != 1 || h.accounts() != accounts {
		t.Fatal("budget overflow must refuse `budget` before any write")
	}
	// A known identity still signs in with the budget exhausted.
	if _, err := h.login("google", signedUp, "sign-up", "", map[string]any{}); err != nil {
		t.Fatalf("known identity with the budget exhausted: %v", err)
	}
	// Gate refusals stay uncharged and uniform even with the budget gone.
	_, err = h.login("google", h.fresh(), "sign-up", "org_nope", googleClaims("a@acme.example"))
	wantUniformRefusal(t, "closed with the budget exhausted", err)

	// The identity-exists race (spec section 9): the UNIQUE key refusing the
	// identity insert leaves nothing of the sign-up (no account, no org) and
	// records `identity-exists`. Induced at the insert on both engines.
	if err := h.auth.EnableSignup(h.reg, service.NewBudget()); err != nil {
		t.Fatal(err)
	}
	accounts = h.accounts()
	restore := authn.SetMutationFailureObserver(func(query string) error {
		if strings.Contains(query, "-- name: InsertExternalIdentity ") {
			return fmt.Errorf("%w: induced identity race", domain.ErrConflict)
		}
		return nil
	})
	_, err = h.login("google", h.fresh(), "sign-up", "", googleClaims("race@acme.example"))
	restore()
	wantUniformRefusal(t, "identity race", err)
	if h.refusedCount("identity-exists") != 1 || h.accounts() != accounts {
		t.Fatal("an identity race must refuse `identity-exists` and leave no account")
	}
}

func runFederatedSignupLandings(t *testing.T, db *store.DB) {
	ctx := t.Context()
	h := newSignupHarness(t, db)
	policyJSON := `{"acr_values":["mfa"]}`
	idp := h.provider("google", service.ProviderInput{AssurancePolicy: &policyJSON})

	// Org landing: orgAdmin's policy lands the signer in org A with the
	// template, grant origin registration, subject = the authority.
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(orgAdmin), orgAReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google")}, Landing: orgTemplateLanding(),
	}, ""); err != nil {
		t.Fatal(err)
	}
	member := h.fresh()
	if _, err := h.login("google", member, "sign-up", string(orgA), googleClaims("m@acme.example")); err != nil {
		t.Fatalf("org sign-up: %v", err)
	}
	principal := queryStrings(t, db, "SELECT a.principal_id FROM accounts a JOIN external_identities e ON e.account_id = a.id WHERE e.subject = '"+member+"'")
	if n := h.count("SELECT COUNT(*) FROM grants g JOIN grant_origins o ON o.grant_id = g.id WHERE g.principal_id = '" + principal +
		"' AND g.org_id = 'org_a' AND g.capability = 'read' AND o.kind = 'registration' AND o.subject = 'usr_orgadmin'"); n != 1 {
		t.Fatalf("org-landing template grant with origin registration(orgAdmin) = %d, want 1", n)
	}
	// Org A's own trail shows who joined: the template grant lines carry the
	// registration origin and the new principal (the outcome itself is on the
	// instance trail, where the display name's source is recorded).
	if n := h.count("SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.created' AND org_id = 'org_a' " +
		"AND payload LIKE '%\"origin_kind\":\"registration\"%' AND payload LIKE '%" + principal + "%'"); n == 0 {
		t.Fatal("org_a's tenant trail does not show the sign-up's grants")
	}
	if n := h.count("SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_completed' " +
		"AND actor_id = '" + principal + "' AND payload LIKE '%\"display_name_from\":\"handle\"%'"); n != 1 {
		t.Fatalf("signup_completed recording the display name's source = %d, want 1", n)
	}
	// An administrator's revoke releases a registration origin like a manual
	// one (permission-model 2026-09-03 (b)).
	grants := &service.Grants{DB: db}
	if err := grants.Revoke(ctx, service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: domain.PrincipalID(principal), Capability: domain.CapRead, Scope: domain.Scope{Org: orgA},
	}); err != nil {
		t.Fatalf("revoking a registration-origin grant: %v", err)
	}

	// Authority lost: the org policy's authority no longer holds the grant
	// it hands out; the sign-up refuses `authority-lost`, uncharged.
	execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = 'usr_orgadmin' AND capability = 'manage-members')")
	execRaw(t, db, "DELETE FROM grants WHERE principal_id = 'usr_orgadmin' AND capability = 'manage-members'")
	_, err := h.login("google", h.fresh(), "sign-up", string(orgA), googleClaims("m2@acme.example"))
	wantUniformRefusal(t, "authority lost", err)
	if h.refusedCount("authority-lost") != 1 {
		t.Fatal("a lost authority must refuse `authority-lost`")
	}

	// Fresh org: root's instance policy with a cap of one.
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google")},
		Landing:  service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 1},
	}, ""); err != nil {
		t.Fatal(err)
	}
	policyID := queryStrings(t, db, "SELECT id FROM registration_policies WHERE org_id IS NULL")
	// The identity row precedes the org row (spec section 9): an identity
	// race refuses `identity-exists` with no org minted and no cap slot spent.
	orgsBefore, accountsBefore := h.count("SELECT COUNT(*) FROM orgs"), h.accounts()
	restore := authn.SetMutationFailureObserver(func(query string) error {
		if strings.Contains(query, "-- name: InsertExternalIdentity ") {
			return fmt.Errorf("%w: induced identity race", domain.ErrConflict)
		}
		return nil
	})
	_, err = h.login("google", h.fresh(), "sign-up", "", googleClaims("race@acme.example"))
	restore()
	wantUniformRefusal(t, "fresh-org identity race", err)
	if h.refusedCount("identity-exists") != 1 || h.count("SELECT COUNT(*) FROM orgs") != orgsBefore || h.accounts() != accountsBefore {
		t.Fatal("a fresh-org identity race must refuse `identity-exists` and mint neither org nor account")
	}
	founder := h.fresh()
	signup, err := h.login("google", founder, "sign-up", "", googleClaims("f@acme.example"))
	if err != nil {
		t.Fatalf("fresh-org sign-up: %v", err)
	}
	orgID := queryStrings(t, db, "SELECT id FROM orgs WHERE registration_policy_id = '"+policyID+"'")
	if orgID == "" || h.count("SELECT COUNT(*) FROM orgs WHERE id = '"+orgID+"' AND name = 'org-"+orgID+"' AND origin = 'registration'") != 1 {
		t.Fatalf("fresh org %q: not named org-<id> with origin registration", orgID)
	}
	founderPrincipal := queryStrings(t, db, "SELECT a.principal_id FROM accounts a JOIN external_identities e ON e.account_id = a.id WHERE e.subject = '"+founder+"'")
	if n := h.count("SELECT COUNT(*) FROM grants g JOIN grant_origins o ON o.grant_id = g.id WHERE g.principal_id = '" + founderPrincipal +
		"' AND g.org_id = '" + orgID + "' AND g.capability = 'manage-members' AND o.kind = 'registration' AND o.subject = 'usr_root'"); n != 1 {
		t.Fatalf("fresh-org admin template grant with origin registration(root) = %d, want 1", n)
	}
	for _, q := range []string{
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'settings.org_created' AND actor_id = 'usr_root' " +
			"AND payload LIKE '%\"origin\":\"registration\"%' AND payload LIKE '%\"policy_id\":\"" + policyID + "\"%'",
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'registration.signup_completed' AND actor_id = '" + founderPrincipal +
			"' AND payload LIKE '%\"org_id\":\"" + orgID + "\"%'",
	} {
		if n := h.count(q); n != 1 {
			t.Fatalf("%s = %d, want 1", q, n)
		}
	}
	// The first administrator is single-factor: manage-members is refused
	// until the session carries adequate assurance (here, the provider's
	// assurance policy satisfied; a local factor arrives with #611).
	if _, err := h.reg.Get(ctx, service.Bearer(signup.Login.SessionToken), service.OrgRegistrationScope(domain.OrgID(orgID))); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("single-factor founder reaching manage-members: %v, want ErrUnauthorized", err)
	}
	idp.ACR = "mfa"
	strong, err := h.login("google", founder, "sign-in", "", map[string]any{})
	idp.ACR = ""
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.reg.Get(ctx, service.Bearer(strong.Login.SessionToken), service.OrgRegistrationScope(domain.OrgID(orgID))); err != nil {
		t.Fatalf("multi-factor founder reaching manage-members: %v", err)
	}

	// The cap: a second fresh org overflows `cap` (the identity resolved
	// first; nothing minted); an operator delete frees the slot.
	orgs := h.count("SELECT COUNT(*) FROM orgs")
	_, err = h.login("google", h.fresh(), "sign-up", "", googleClaims("g@acme.example"))
	wantUniformRefusal(t, "cap reached", err)
	if h.refusedCount("cap") != 1 || h.count("SELECT COUNT(*) FROM orgs") != orgs {
		t.Fatal("cap overflow must refuse `cap` and mint no org")
	}
	if err := (&service.Orgs{DB: db}).Delete(ctx, service.LocalPrincipal(root), domain.OrgID(orgID)); err != nil {
		t.Fatalf("operator delete of the self-served org: %v", err)
	}
	if _, err := h.login("google", h.fresh(), "sign-up", "", googleClaims("h@acme.example")); err != nil {
		t.Fatalf("sign-up after the delete freed a slot: %v", err)
	}

	// Precondition: a disabled entry provider renders the policy inactive;
	// the sign-up refuses `precondition`, uncharged.
	providers := &service.Providers{DB: db, Keyring: h.auth.Keyring, ExternalOrigin: h.auth.ExternalOrigin, FederationPolicy: h.auth.FederationPolicy}
	h.provider("other", service.ProviderInput{})
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google"), oidcEntry("other")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := providers.Put(ctx, service.LocalPrincipal(root), "other", service.ProviderInput{
		DisplayName: "other", Issuer: h.idps["other"].Issuer(), ClientID: "client-other", ClientSecret: "secret",
		Scopes: "openid email", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	_, err = h.login("google", h.fresh(), "sign-up", "", googleClaims("p@acme.example"))
	wantUniformRefusal(t, "inactive policy", err)
	if h.refusedCount("precondition") != 1 {
		t.Fatal("an inactive policy must refuse `precondition`")
	}
}

// runFederatedSignupOneQueryPath pins the tenant-isolation third member
// (amendment 2026-09-03): the instance-wide (kind, issuer, subject)
// resolution is one query path whether the identity is known (login) or
// fresh (sign-up). The ordered query traces agree up to and including the
// deciding lookup, at the same index, under the same sign-up intent.
func runFederatedSignupOneQueryPath(t *testing.T, db *store.DB) {
	ctx := t.Context()
	h := newSignupHarness(t, db)
	h.provider("google", service.ProviderInput{})
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	known := h.fresh()
	if _, err := h.login("google", known, "sign-up", "", googleClaims("k@acme.example")); err != nil {
		t.Fatal(err)
	}
	trace := func(subject, intent string) []string {
		t.Helper()
		start, err := h.auth.OIDCStart(ctx, "google", "login", intent, "", "", "", "", false)
		if err != nil {
			t.Fatal(err)
		}
		h.idps["google"].Claims = googleClaims(subject + "@acme.example")
		code, state := driveIdP(t, start.AuthURL+"&sub="+subject)
		var seen []string
		restore := authn.SetQueryObserver(func(sql string) { seen = append(seen, queryIdentity(sql)) })
		_, err = h.auth.OIDCCallback(ctx, "google", code, state, "", "", start.BindingCookie, "")
		restore()
		if err != nil {
			t.Fatalf("callback for %q: %v", subject, err)
		}
		return seen
	}
	// The banner compares "a known-identity login and a fresh sign-up": the
	// login leg is the ordinary sign-in (tx.Write); the sign-up leg is a
	// known identity under sign-up intent (WriteSerialized) and a fresh one.
	signInTrace := trace(known, "sign-in")
	knownTrace := trace(known, "sign-up")
	freshTrace := trace(h.fresh(), "sign-up")
	const decider = "GetExternalIdentity"
	at := func(trace []string) int {
		for i, q := range trace {
			if q == decider {
				return i
			}
		}
		t.Fatalf("no %s in the trace: %v", decider, trace)
		return -1
	}
	if len(knownTrace) < 5 || len(freshTrace) < 5 {
		t.Fatalf("the traces are too short to be real: known=%v fresh=%v", knownTrace, freshTrace)
	}
	f := at(freshTrace)
	for label, other := range map[string][]string{"known sign-in": signInTrace, "known sign-up": knownTrace} {
		k := at(other)
		if k != f || strings.Join(other[:k+1], "\n") != strings.Join(freshTrace[:f+1], "\n") {
			t.Fatalf("the %s and fresh sign-up legs diverge at or before the resolution:\n  %s: %v\n  fresh: %v", label, label, other[:k+1], freshTrace[:f+1])
		}
	}
	// And the known legs are one conversation to the end: intent never
	// changes what a known identity's login does.
	if strings.Join(signInTrace, "\n") != strings.Join(knownTrace, "\n") {
		t.Fatalf("a known identity's login differs by intent:\n  sign-in: %v\n  sign-up: %v", signInTrace, knownTrace)
	}
}

// runOIDCProviderIssuerDepartures covers #588 on the provider surface and
// at start: an Entra `common` authority is refused naming the `{tenantid}`
// placeholder its document publishes, a domain-name authority naming its
// tenant GUID; a client_id change is refused on a pairwise-
// subject row with linked identities and allowed otherwise; a reauth on a
// policy-less row is refused by name at start with no round-trip.
func runOIDCProviderIssuerDepartures(t *testing.T, db *store.DB) {
	ctx := t.Context()
	h := newSignupHarness(t, db)
	providers := &service.Providers{DB: db, Keyring: h.auth.Keyring, ExternalOrigin: h.auth.ExternalOrigin}

	// An Entra authority that is not tenant-specific is refused by discovery,
	// naming the issuer its document carries (#588 d1): the `common` and
	// `organizations` documents publish the literal `{tenantid}` placeholder;
	// a domain-name authority's document carries the tenant GUID to use.
	providers.FederationPolicy = h.auth.FederationPolicy
	providers.FederationPolicy.Development = true
	for label, published := range map[string]string{
		"common":      "https://login.microsoftonline.com/{tenantid}/v2.0",
		"domain-name": "https://login.microsoftonline.com/72f988bf-86f1-41af-91ab-2d7cd011db47/v2.0",
	} {
		authority, err := oidctest.New()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(authority.Close)
		authority.IssuerOverride = published
		_, err = providers.Put(ctx, service.LocalPrincipal(root), "entra-"+label, service.ProviderInput{
			DisplayName: "Contoso", Issuer: authority.Server.URL, ClientID: "c", ClientSecret: "s", Scopes: "openid email", Enabled: true,
		})
		var sd interface{ SafeDetail() string }
		if !errors.Is(err, service.ErrProviderDiscovery) || !errors.As(err, &sd) || !strings.Contains(sd.SafeDetail(), published) {
			t.Fatalf("%s authority = %v, want a discovery refusal naming %q", label, err, published)
		}
	}

	// Pairwise subjects: the guard bites only with linked identities.
	pairwise := h.provider("entra", service.ProviderInput{})
	pairwise.SubjectTypes = []string{"pairwise"}
	put := func(clientID string) error {
		_, err := providers.Put(ctx, service.LocalPrincipal(root), "entra", service.ProviderInput{
			DisplayName: "entra", Issuer: pairwise.Issuer(), ClientID: clientID, ClientSecret: "secret",
			Scopes: "openid email", Enabled: true,
		})
		return err
	}
	if err := put("rotated-before-identities"); err != nil {
		t.Fatalf("client_id change with no linked identities: %v", err)
	}
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("entra")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	pairwiseSubject := h.fresh()
	if _, err := h.login("entra", pairwiseSubject, "sign-up", "", entraClaims("e@acme.example")); err != nil {
		t.Fatal(err)
	}
	if err := put("rotated-after-identities"); !errors.Is(err, service.ErrPairwiseClientID) {
		t.Fatalf("client_id change on a pairwise row with identities = %v, want ErrPairwiseClientID", err)
	}
	if err := put("rotated-before-identities"); err != nil {
		t.Fatalf("a PUT that keeps client_id on a pairwise row: %v", err)
	}
	public := h.provider("google", service.ProviderInput{})
	if _, err := h.reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{
		External: []service.RegistrationExternalEntry{oidcEntry("google")},
		Landing:  service.RegistrationLanding{Kind: service.LandingNone},
	}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.login("google", h.fresh(), "sign-up", "", googleClaims("g@acme.example")); err != nil {
		t.Fatal(err)
	}
	if _, err := providers.Put(ctx, service.LocalPrincipal(root), "google", service.ProviderInput{
		DisplayName: "google", Issuer: public.Issuer(), ClientID: "rotated", ClientSecret: "secret",
		Scopes: "openid email", Enabled: true,
	}); err != nil {
		t.Fatalf("client_id change on a public-subject row with identities: %v", err)
	}

	// Reauth on a policy-less row: refused by name at start, remedy named,
	// no transaction row written and no round-trip made.
	session, err := h.login("entra", pairwiseSubject, "sign-in", "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var sd interface{ SafeDetail() string }
	hits, rows := pairwise.TokenEndpointHits, h.count("SELECT COUNT(*) FROM oidc_transactions")
	_, err = h.auth.OIDCStart(ctx, "entra", "reauth", "", "", "env_prod", session.Login.SessionToken, "", false)
	if !errors.Is(err, service.ErrReauthNoPolicy) || !errors.As(err, &sd) || !strings.Contains(sd.SafeDetail(), "enrol WebAuthn or TOTP") {
		t.Fatalf("reauth start on a policy-less row = %v, want ErrReauthNoPolicy naming the remedy", err)
	}
	if pairwise.TokenEndpointHits != hits || h.count("SELECT COUNT(*) FROM oidc_transactions") != rows {
		t.Fatal("a refused reauth start made a round-trip or wrote a transaction")
	}
}

// runOrgRenameFormula pins #585 d7 as shipped by #617: an org administrator
// (manage-members at the org) renames the org; a bare instance-config
// principal does not. Delete stays instance administration.
func runOrgRenameFormula(t *testing.T, db *store.DB) {
	ctx := t.Context()
	orgs := &service.Orgs{DB: db}
	if _, err := orgs.Rename(ctx, service.LocalPrincipal(orgAdmin), orgA, "Renamed by its admin"); err != nil {
		t.Fatalf("org admin rename: %v", err)
	}
	ts := `'2026-01-01T00:00:00.000000Z'`
	if db.PG() != nil {
		ts = `'2026-01-01T00:00:00Z'`
	}
	execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_icfg', 'human', `+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_icfg', 'usr_icfg', 'instance-config', NULL, NULL, NULL, `+ts+`)`)
	execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) VALUES ('go_icfg', 'g_icfg', 'manual', 'usr_root', `+ts+`)`)
	if _, err := orgs.Rename(ctx, service.LocalPrincipal("usr_icfg"), orgA, "Renamed by config"); err == nil {
		t.Fatal("a bare instance-config principal renamed an org")
	}
	if err := orgs.Delete(ctx, service.LocalPrincipal(orgAdmin), orgB); err == nil {
		t.Fatal("an org administrator deleted an org")
	}
}
