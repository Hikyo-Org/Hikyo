package isolation

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// Invitation claim by an external identity (#610; spec social-signin.md
// section 4 "Claim"), on both federated kinds and both engines: the
// authority is the proof of a purpose-claim start, checked without being
// consumed; the callback consumes it, binds the identity and signs the
// invitee in, in one write transaction.

const claimPassword = "claimed account password long enough"

// claimDriver drives purpose-claim round trips for one federated kind.
type claimDriver struct {
	t    *testing.T
	kind string // service.OIDCKind or service.OAuth2Kind
	slug string
	h    *signupHarness
	idp  *oidctest.IdP // oidc
	f    *githubFixture
	n    int
	// usernames maps an invited account id to its username.
	usernames map[string]string
}

func newClaimDriver(t *testing.T, db *store.DB, kind string) *claimDriver {
	t.Helper()
	h := newSignupHarness(t, db)
	d := &claimDriver{t: t, kind: kind, h: h, usernames: map[string]string{}}
	switch kind {
	case service.OIDCKind:
		d.slug = "google"
		d.idp = h.provider(d.slug, service.ProviderInput{})
	case service.OAuth2Kind:
		d.slug = "github"
		_, d.f = configureGithub(t, h)
		d.f.challenges = map[string]bool{}
	default:
		t.Fatalf("unknown kind %q", kind)
	}
	return d
}

// subject is a fresh provider subject (numeric, as GitHub's always is).
func (d *claimDriver) subject() string {
	d.n++
	return strconv.Itoa(61000 + d.n)
}

// invite mints an invitation: an empty account plus its authority.
func (d *claimDriver) invite(name string) service.InvitationResult {
	d.t.Helper()
	grants := &service.Grants{DB: d.h.db, Auth: d.h.auth}
	username := d.kind + "-" + name
	inv, err := grants.InviteMember(d.t.Context(), service.LocalPrincipal(root), service.InviteSpec{Username: username, Delivery: "response"})
	if err != nil {
		d.t.Fatal(err)
	}
	d.usernames[inv.AccountID] = username
	return inv
}

func (d *claimDriver) start(authority string) (service.OIDCStartResult, error) {
	ctx := d.t.Context()
	if d.kind == service.OIDCKind {
		return d.h.auth.OIDCStart(ctx, d.slug, "claim", "", "", "", "", authority, false)
	}
	start, err := d.h.auth.OAuth2Start(ctx, d.slug, "claim", "", "", "", "", authority, false)
	if err == nil {
		u, perr := url.Parse(start.AuthURL)
		if perr != nil {
			d.t.Fatal(perr)
		}
		d.f.challenges[u.Query().Get("code_challenge")] = true
	}
	return start, err
}

// claimResponse is what the provider redirected back with for one start.
type claimResponse struct {
	code, state, binding, subject string
}

// authorize runs the provider leg of a started transaction for subject.
func (d *claimDriver) authorize(start service.OIDCStartResult, subject string) claimResponse {
	d.t.Helper()
	if d.kind == service.OIDCKind {
		code, state := driveIdP(d.t, start.AuthURL+"&sub="+subject)
		return claimResponse{code: code, state: state, binding: start.BindingCookie, subject: subject}
	}
	return claimResponse{code: "oauth-code", state: start.State, binding: start.BindingCookie, subject: subject}
}

func (d *claimDriver) callback(r claimResponse, presented string) (service.OIDCCallbackResult, error) {
	ctx := d.t.Context()
	if d.kind == service.OIDCKind {
		return d.h.auth.OIDCCallback(ctx, d.slug, r.code, r.state, "", "", r.binding, presented)
	}
	d.pinSubject(r.subject)
	return d.h.auth.OAuth2Callback(ctx, d.slug, r.code, r.state, "", "", r.binding, presented)
}

// pinSubject makes the GitHub fixture answer subject. It writes only on a
// change, so callbacks running concurrently on a subject pinned beforehand
// only read the fixture (the transport reads it on every /user request).
func (d *claimDriver) pinSubject(subject string) {
	d.t.Helper()
	id, err := strconv.ParseInt(subject, 10, 64)
	if err != nil {
		d.t.Fatal(err)
	}
	if d.f.subject != id {
		d.f.subject = id
	}
}

// claim drives one whole claim round trip.
func (d *claimDriver) claim(authority, subject string) (service.OIDCCallbackResult, error) {
	d.t.Helper()
	start, err := d.start(authority)
	if err != nil {
		return service.OIDCCallbackResult{}, err
	}
	return d.callback(d.authorize(start, subject), "")
}

func (d *claimDriver) transactions() int64 {
	return d.h.count("SELECT COUNT(*) FROM " + d.kind + "_transactions")
}

// events counts audit rows of typ whose payload carries every key/value pair.
func (d *claimDriver) events(typ string, kv ...string) int64 {
	q := "SELECT COUNT(*) FROM audit_instance_events WHERE type = '" + typ + "'"
	for i := 0; i+1 < len(kv); i += 2 {
		q += " AND payload LIKE '%\"" + kv[i] + "\":\"" + kv[i+1] + "\"%'"
	}
	return d.h.count(q)
}

func (d *claimDriver) refused(cause string) int64 {
	return d.events("auth."+d.kind+"_refused", "cause", cause, "purpose", "claim")
}

func (d *claimDriver) authorityRefused(cause string) int64 {
	return d.events("auth.credential_authority_refused", "cause", cause)
}

func (d *claimDriver) consumed(authorityID string) bool {
	return d.h.count("SELECT COUNT(*) FROM credential_authorities WHERE id = '"+authorityID+"' AND consumed_at IS NOT NULL") == 1
}

func (d *claimDriver) identities(accountID string) int64 {
	return d.h.count("SELECT COUNT(*) FROM external_identities WHERE account_id = '" + accountID + "'")
}

// wantRefusedAtStart asserts a start refused by cause before any transaction
// row exists, so no provider round-trip can follow.
func (d *claimDriver) wantRefusedAtStart(label, authority, cause string) {
	d.t.Helper()
	before, refusals := d.transactions(), d.authorityRefused(cause)
	if _, err := d.start(authority); !errors.Is(err, domain.ErrUnauthenticated) {
		d.t.Fatalf("%s: start = %v, want the uniform refusal", label, err)
	}
	if d.transactions() != before {
		d.t.Fatalf("%s: a refused claim start wrote a transaction", label)
	}
	if d.authorityRefused(cause) != refusals+1 {
		d.t.Fatalf("%s: want one auth.credential_authority_refused {cause: %s}", label, cause)
	}
}

// established gives the invited account a password; returns a session token.
func (d *claimDriver) established(inv service.InvitationResult) string {
	d.t.Helper()
	ctx := d.t.Context()
	if err := d.h.auth.EstablishCredential(ctx, inv.Authority, claimPassword); err != nil {
		d.t.Fatal(err)
	}
	login, err := d.h.auth.LocalLogin(ctx, d.usernames[inv.AccountID], claimPassword, service.ArtifactCLI)
	if err != nil {
		d.t.Fatal(err)
	}
	return login.SessionToken
}

func (d *claimDriver) dropPassword(accountID string) {
	execRaw(d.t, d.h.db, "DELETE FROM password_credentials WHERE account_id = '"+accountID+"'")
}

func (d *claimDriver) reset(inv service.InvitationResult) service.ResetResult {
	d.t.Helper()
	r, err := d.h.auth.BreakGlassResetCredential(d.t.Context(), string(inv.PrincipalID), "terminal")
	if err != nil {
		d.t.Fatal(err)
	}
	return r
}

func TestClaim(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, kind := range []string{service.OIDCKind, service.OAuth2Kind} {
			t.Run(kind, func(t *testing.T) { runClaim(t, db, kind) })
		}
	})
}

func runClaim(t *testing.T, db *store.DB, kind string) {
	d := newClaimDriver(t, db, kind)
	ctx := t.Context()

	t.Run("invite then claim lands signed in", func(t *testing.T) {
		inv := d.invite("lands")
		result, err := d.claim(inv.Authority, d.subject())
		if err != nil {
			t.Fatal(err)
		}
		id, err := d.h.auth.Identity(ctx, result.Login.SessionToken)
		if err != nil || id.Principal != inv.PrincipalID {
			t.Fatalf("claimed session resolves to %v (%v), want %s", id.Principal, err, inv.PrincipalID)
		}
		if d.identities(inv.AccountID) != 1 {
			t.Fatal("claim bound no identity")
		}
		if d.h.count("SELECT COUNT(*) FROM credential_authorities WHERE id = '"+inv.AuthorityID+"' AND consumed_at IS NOT NULL AND established_credential_kind = '"+kind+"'") != 1 {
			t.Fatalf("authority not consumed as %s", kind)
		}
		if d.events("auth.credential_established", "authority_id", inv.AuthorityID, "established_credential_kind", kind, "kind", kind) != 1 {
			t.Fatal("auth.credential_established not recorded with the claimed kind")
		}
		if d.events("auth."+kind+"_login", "account_id", inv.AccountID, "purpose", "claim") != 1 {
			t.Fatalf("auth.%s_login {purpose: claim} not recorded", kind)
		}
		switch kind {
		case service.OAuth2Kind:
			if f := result.Login.Assurance.Factors; len(f) != 1 || f[0] != "oauth2" {
				t.Fatalf("oauth2 claim assurance = %v, want single-factor", f)
			}
		case service.OIDCKind:
			if f := result.Login.Assurance.Factors; len(f) != 1 || f[0] != "oidc" {
				t.Fatalf("policy-less oidc claim assurance = %v, want single-factor", f)
			}
		}
		// The authority is spent: a second claim is refused at start.
		d.wantRefusedAtStart("spent authority", inv.Authority, "consumed")
	})

	t.Run("concurrent claims of one authority", func(t *testing.T) {
		inv := d.invite("race")
		subject := d.subject()
		var responses [2]claimResponse
		for i := range responses {
			start, err := d.start(inv.Authority)
			if err != nil {
				t.Fatalf("start %d: %v", i, err)
			}
			responses[i] = d.authorize(start, subject)
		}
		consumedBefore := d.authorityRefused("consumed")
		if kind == service.OAuth2Kind {
			d.pinSubject(subject) // before the goroutines: they only read it
		}
		var (
			wg   sync.WaitGroup
			errs [2]error
		)
		for i := range responses {
			wg.Go(func() {
				_, errs[i] = d.callback(responses[i], "")
			})
		}
		wg.Wait()
		won := 0
		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case !errors.Is(err, domain.ErrUnauthenticated):
				t.Fatalf("losing callback = %v, want the uniform refusal", err)
			}
		}
		if won != 1 {
			t.Fatalf("%d callbacks won one authority, want exactly 1 (%v)", won, errs)
		}
		if d.authorityRefused("consumed") != consumedBefore+1 {
			t.Fatal("the losing claim did not record auth.credential_authority_refused {cause: consumed}")
		}
		if d.identities(inv.AccountID) != 1 {
			t.Fatal("concurrent claims bound more than one identity")
		}
	})

	t.Run("an account holding a credential refuses purpose", func(t *testing.T) {
		// Password.
		inv := d.invite("has-password")
		d.established(inv)
		d.wantRefusedAtStart("password", d.reset(inv).Authority, "purpose")

		// Confirmed TOTP, no password.
		inv = d.invite("has-totp")
		token := d.established(inv)
		uri, err := d.h.auth.EnrolTOTPStart(ctx, token, claimPassword)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.h.auth.EnrolTOTPConfirm(ctx, token, totpCode(t, uri, time.Now())); err != nil {
			t.Fatal(err)
		}
		d.dropPassword(inv.AccountID)
		d.wantRefusedAtStart("totp", d.reset(inv).Authority, "purpose")

		// Passkey, no password.
		if err := d.h.auth.ConfigureWebAuthnRP(); err != nil {
			t.Fatal(err)
		}
		inv = d.invite("has-passkey")
		token = d.established(inv)
		enrolPasskey(t, d.h.auth, ctx, token, claimPassword, webauthntest.New(waRPID, waOrigin))
		d.dropPassword(inv.AccountID)
		d.wantRefusedAtStart("passkey", d.reset(inv).Authority, "purpose")

		// A disabled passkey (here: as a clone response leaves it) proves
		// nothing, but it is still a credential the account held: a claim
		// is the first credential only, so it refuses too.
		inv = d.invite("disabled-passkey")
		token = d.established(inv)
		enrolPasskey(t, d.h.auth, ctx, token, claimPassword, webauthntest.New(waRPID, waOrigin))
		d.dropPassword(inv.AccountID)
		execRaw(t, db, "UPDATE webauthn_credentials SET disabled_at = created_at WHERE account_id = '"+inv.AccountID+"'")
		d.wantRefusedAtStart("disabled passkey", d.reset(inv).Authority, "purpose")

		// An external identity bound by an earlier claim.
		inv = d.invite("has-identity")
		if _, err := d.claim(inv.Authority, d.subject()); err != nil {
			t.Fatal(err)
		}
		d.wantRefusedAtStart("identity", d.reset(inv).Authority, "purpose")
	})

	t.Run("the precondition is re-checked at the callback", func(t *testing.T) {
		donor := d.invite("donor")
		d.established(donor)
		inv := d.invite("late-password")
		start, err := d.start(inv.Authority)
		if err != nil {
			t.Fatal(err)
		}
		// A credential arrives between start and callback.
		execRaw(t, db, "INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) "+
			"SELECT '"+inv.AccountID+"', verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at FROM password_credentials WHERE account_id = '"+donor.AccountID+"'")
		before := d.refused("purpose")
		if _, err := d.callback(d.authorize(start, d.subject()), ""); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("callback = %v, want the uniform refusal", err)
		}
		if d.refused("purpose") != before+1 {
			t.Fatalf("want one auth.%s_refused {cause: purpose, purpose: claim}", kind)
		}
		if d.consumed(inv.AuthorityID) || d.identities(inv.AccountID) != 0 {
			t.Fatal("a refused claim consumed the authority or bound the identity")
		}
	})

	t.Run("identity bound elsewhere refuses without consuming", func(t *testing.T) {
		subject := d.subject()
		first := d.invite("first-holder")
		if _, err := d.claim(first.Authority, subject); err != nil {
			t.Fatal(err)
		}
		inv := d.invite("second-holder")
		before := d.refused("identity-exists")
		if _, err := d.claim(inv.Authority, subject); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("claim of a bound identity = %v, want the uniform refusal", err)
		}
		if d.refused("identity-exists") != before+1 {
			t.Fatalf("want one auth.%s_refused {cause: identity-exists, purpose: claim}", kind)
		}
		if d.consumed(inv.AuthorityID) {
			t.Fatal("identity-exists consumed the authority")
		}
		// The authority stays spendable by the password path.
		if err := d.h.auth.EstablishCredential(ctx, inv.Authority, claimPassword); err != nil {
			t.Fatalf("password path after identity-exists: %v", err)
		}
	})

	t.Run("a reset authority on a still-empty invited account claims", func(t *testing.T) {
		inv := d.invite("reset-empty")
		reset := d.reset(inv)
		if _, err := d.claim(reset.Authority, d.subject()); err != nil {
			t.Fatal(err)
		}
		if !d.consumed(reset.AuthorityID) || d.identities(inv.AccountID) != 1 {
			t.Fatal("reset authority claim did not consume and bind")
		}
	})

	t.Run("a recovery-issued authority never claims", func(t *testing.T) {
		inv := d.invite("recovery")
		value, verifier, err := crypto.NewArtifact(crypto.ArtifactBootstrap)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			epoch, err := az.CredentialEpoch(ctx)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			return az.MintAuthority(ctx, authz.NewCredentialAuthority{
				ID: "cea_recovery_" + kind, Verifier: verifier, AccountID: inv.AccountID,
				Purpose: "establish-credential", IssuedBy: "recovery",
				CredentialEpoch: epoch, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
			})
		}); err != nil {
			t.Fatal(err)
		}
		d.wantRefusedAtStart("recovery", value, "purpose")
	})

	t.Run("malformed and unknown authorities refuse by cause", func(t *testing.T) {
		d.wantRefusedAtStart("malformed", "not-an-authority", "malformed")
		value, _, err := crypto.NewArtifact(crypto.ArtifactBootstrap)
		if err != nil {
			t.Fatal(err)
		}
		d.wantRefusedAtStart("unknown", value, "unknown")
	})

	t.Run("purpose walls", func(t *testing.T) {
		donor := d.invite("wall-donor")
		donorToken := d.established(donor)

		// A claim transaction is browser-cookie-bound: a session presented
		// without the binding cookie completes nothing.
		inv := d.invite("wall-binding")
		start, err := d.start(inv.Authority)
		if err != nil {
			t.Fatal(err)
		}
		resp := d.authorize(start, d.subject())
		resp.binding = ""
		unbound := d.refused("binding")
		if _, err := d.callback(resp, donorToken); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("unbound claim callback = %v, want the uniform refusal", err)
		}
		if d.refused("binding") != unbound+1 {
			t.Fatalf("want one auth.%s_refused {cause: binding, purpose: claim}", kind)
		}
		if d.consumed(inv.AuthorityID) || d.identities(donor.AccountID) != 0 {
			t.Fatal("an unbound claim response consumed the authority or linked the presented session's account")
		}

		// A claim's provider response paired with a login transaction
		// completes as that login (here: an unknown identity, refused), never
		// as a claim.
		inv = d.invite("wall-login")
		claimStart, err := d.start(inv.Authority)
		if err != nil {
			t.Fatal(err)
		}
		claimResp := d.authorize(claimStart, d.subject())
		var loginStart service.OIDCStartResult
		if kind == service.OIDCKind {
			loginStart, err = d.h.auth.OIDCStart(ctx, d.slug, "login", "", "", "", "", "", false)
		} else {
			loginStart, err = d.h.auth.OAuth2Start(ctx, d.slug, "login", "", "", "", "", "", false)
			if err == nil {
				u, _ := url.Parse(loginStart.AuthURL)
				d.f.challenges[u.Query().Get("code_challenge")] = true
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		crossed := claimResponse{code: claimResp.code, state: loginStart.State, binding: loginStart.BindingCookie, subject: claimResp.subject}
		if _, err := d.callback(crossed, ""); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("claim response on a login transaction = %v, want the uniform refusal", err)
		}
		if d.consumed(inv.AuthorityID) || d.identities(inv.AccountID) != 0 {
			t.Fatal("a login transaction spent a claim's authority")
		}

		// A bound claim with another account's session presented mints the
		// invitee's session and leaves the presented one untouched: it is
		// never a link, an establish or a reauth of that account.
		inv = d.invite("wall-session")
		start, err = d.start(inv.Authority)
		if err != nil {
			t.Fatal(err)
		}
		result, err := d.callback(d.authorize(start, d.subject()), donorToken)
		if err != nil {
			t.Fatal(err)
		}
		id, err := d.h.auth.Identity(ctx, result.Login.SessionToken)
		if err != nil || id.Principal != inv.PrincipalID {
			t.Fatalf("claim with a presented session minted %v (%v), want the invitee %s", id.Principal, err, inv.PrincipalID)
		}
		if _, err := d.h.auth.Identity(ctx, donorToken); err != nil {
			t.Fatalf("the presented session did not survive the claim: %v", err)
		}
		if d.identities(donor.AccountID) != 0 {
			t.Fatal("a claim linked the presented session's account")
		}
	})

	if kind == service.OAuth2Kind && d.f.emails != 0 {
		t.Fatalf("claim fetched /user/emails %d times", d.f.emails)
	}
}

// TestClaimOIDCAssurance pins that an oidc claim's session carries the
// assurance the provider row's policy yields, exactly as that identity's
// login would.
func TestClaimOIDCAssurance(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		h := newSignupHarness(t, db)
		policy := `{"acr_values":["mfa"]}`
		idp := h.provider("strict", service.ProviderInput{AssurancePolicy: &policy})
		d := &claimDriver{t: t, kind: service.OIDCKind, slug: "strict", h: h, idp: idp, usernames: map[string]string{}}
		for _, tc := range []struct {
			acr  string
			want []string
		}{
			{"", []string{"oidc"}},
			{"mfa", []string{"oidc", "oidc-mfa"}},
		} {
			idp.ACR = tc.acr
			inv := d.invite("assurance-" + fmt.Sprint(len(tc.want)))
			result, err := d.claim(inv.Authority, d.subject())
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Login.Assurance.Factors; fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("acr %q: claim factors = %v, want %v", tc.acr, got, tc.want)
			}
		}
	})
}

// runOIDCClaimLifecycle claims an invitation through the provider
// runOIDCLifecycle configured, so the shared audit_e2e datastore holds an
// oidc claim's auth.credential_established and auth.oidc_login {purpose:
// claim}, plus a claim refused at start by cause. The emitter closure counts
// types; the payload pins here are what tell a claim from a login.
func runOIDCClaimLifecycle(t *testing.T, auth *service.Auth, ctx context.Context, admin domain.PrincipalID) {
	t.Helper()
	grants := &service.Grants{DB: auth.DB, Auth: auth}
	inv, err := grants.InviteMember(ctx, service.LocalPrincipal(admin), service.InviteSpec{Username: "oidc-claim", Delivery: "response"})
	if err != nil {
		t.Fatal(err)
	}
	start, err := auth.OIDCStart(ctx, "lifecycle-idp", "claim", "", "", "", "", inv.Authority, false)
	if err != nil {
		t.Fatalf("oidc claim start: %v", err)
	}
	code, state := driveIdP(t, start.AuthURL+"&sub=lifecycle-claim")
	if _, err := auth.OIDCCallback(ctx, "lifecycle-idp", code, state, "", "", start.BindingCookie, ""); err != nil {
		t.Fatalf("oidc claim callback: %v", err)
	}
	if _, err := auth.OIDCStart(ctx, "lifecycle-idp", "claim", "", "", "", "", inv.Authority, false); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("spent claim authority: %v", err)
	}
	for _, q := range []string{
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.oidc_login' AND payload LIKE '%\"purpose\":\"claim\"%'",
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.credential_established' AND payload LIKE '%\"established_credential_kind\":\"oidc\"%'",
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.credential_authority_refused' AND payload LIKE '%\"cause\":\"consumed\"%'",
	} {
		if queryInt(t, auth.DB, q) == 0 {
			t.Fatalf("oidc claim lifecycle left no row for: %s", q)
		}
	}
}
