package isolation

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The login-challenge slice end to end on a real datastore (#760): a browser
// password login on an account with an enrolled factor mints no session but a
// single-use challenge, and the second factor is presented against it before any
// cookie is set. These cover the three shapes the LocalLogin fork produces — a
// challenged enrolled account, an unenrolled account gated into setup on a
// `required` instance, and an unenrolled account waved through on `optional`.

// browserLoginWithTOTP performs a browser password login and, when the account
// is enrolled and the server answers with a challenge, completes it with a TOTP
// code generated for at. It returns the resulting session (the direct session
// for an unenrolled account, the challenge-minted one otherwise), so the
// existing enrolled-account flows keep reading `browser` as a live session.
func browserLoginWithTOTP(t *testing.T, auth *service.Auth, ctx context.Context, username, password, uri string, at time.Time) service.LoginResult {
	t.Helper()
	r, err := auth.LocalLogin(ctx, username, password, service.ArtifactBrowser)
	if err != nil {
		t.Fatalf("browser login: %v", err)
	}
	if r.Challenge == nil {
		return r
	}
	s, err := auth.LoginChallengeTOTP(ctx, r.Challenge.ID, totpCode(t, uri, at))
	if err != nil {
		t.Fatalf("login challenge totp: %v", err)
	}
	return s
}

func TestPasswordThenAuthenticator(t *testing.T) {
	forEngines(t, runPasswordThenAuthenticator)
}

// runPasswordThenAuthenticator: on the harness default (`optional`) instance, a
// browser password login on an account with a confirmed TOTP factor issues a
// challenge instead of a session; a wrong code leaves it usable, the right code
// mints a `[password, totp]` browser session, and the challenge is single-use.
func runPasswordThenAuthenticator(t *testing.T, db *store.DB) {
	adm := bootstrapAdmin(t, db, adminOpts{
		username: "challenge-admin", displayName: "Challenge Admin",
		password: "correct horse battery staple challenge",
	})
	auth, password := adm.auth, adm.password
	ctx := t.Context()
	base := time.Now().UTC()
	clock := base
	auth.Now = func() time.Time { return clock }

	// Enrol and confirm a TOTP factor. Confirmation consumes step(base).
	login, err := auth.LocalLogin(ctx, "challenge-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatalf("enrol start: %v", err)
	}
	if _, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, base)); err != nil {
		t.Fatalf("enrol confirm: %v", err)
	}

	// A browser password login now mints no session: it issues a challenge that
	// offers the enrolled factor and carries no token.
	r, err := auth.LocalLogin(ctx, "challenge-admin", password, service.ArtifactBrowser)
	if err != nil {
		t.Fatalf("browser login: %v", err)
	}
	if r.Challenge == nil {
		t.Fatal("browser login on an enrolled account minted a session, want a challenge")
	}
	if r.SessionToken != "" {
		t.Fatalf("the challenge outcome carried a session token %q, want none", r.SessionToken)
	}
	if !slices.Contains(r.Challenge.Factors, "totp") {
		t.Fatalf("challenge factors = %v, want totp offered", r.Challenge.Factors)
	}

	// A wrong code (two steps back, outside the skew window) is the uniform
	// bad-code refusal, and the challenge stays live for a retry.
	if _, err := auth.LoginChallengeTOTP(ctx, r.Challenge.ID, totpCode(t, uri, clock.Add(-60*time.Second))); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("wrong challenge code = %v, want unauthenticated", err)
	}

	// The correct code on a fresh, unspent step mints the browser session,
	// recording exactly the two factors that were presented.
	clock = base.Add(30 * time.Second)
	s, err := auth.LoginChallengeTOTP(ctx, r.Challenge.ID, totpCode(t, uri, clock))
	if err != nil {
		t.Fatalf("challenge totp: %v", err)
	}
	if s.SessionToken == "" {
		t.Fatal("a completed challenge minted no session token")
	}
	if !slices.Equal(s.Assurance.Factors, []string{"password", "totp"}) {
		t.Fatalf("session assurance factors = %v, want [password totp]", s.Assurance.Factors)
	}

	// Single-use: the consumed challenge answers the uniform nonexistent shape.
	if _, err := auth.LoginChallengeTOTP(ctx, r.Challenge.ID, totpCode(t, uri, clock)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("replayed challenge id = %v, want not-found", err)
	}
}

func TestUnenrolledIsGatedIntoSetup(t *testing.T) {
	forEngines(t, runUnenrolledIsGatedIntoSetup)
}

// runUnenrolledIsGatedIntoSetup: on a `required` instance, a password login on
// an account with no factor mints a session flagged enrolment_required (surfaced
// on the resolved identity, the whoami source). The gate still admits the
// enrolment ceremony in-process, and the session reissued once a factor stands
// no longer carries the flag. The chokepoint REFUSAL of a non-allowlisted
// operation is proved where it actually runs — against a network operation
// context — in internal/authz (TestAdmitOperationEnrolmentGate).
func runUnenrolledIsGatedIntoSetup(t *testing.T, db *store.DB) {
	required := authService(t, db)
	required.SecondFactorRequired = true
	adm := bootstrapAdmin(t, db, adminOpts{
		username: "gated-admin", displayName: "Gated Admin",
		password: "correct horse battery staple gated", auth: required,
	})
	auth, password := adm.auth, adm.password
	ctx := t.Context()
	base := time.Now().UTC()
	clock := base
	auth.Now = func() time.Time { return clock }

	login, err := auth.LocalLogin(ctx, "gated-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	gated, err := auth.Identity(ctx, login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if !gated.EnrolmentRequired {
		t.Fatal("an unenrolled required-instance session is not flagged enrolment_required")
	}

	// The gate must still admit enrolment, or the account can never escape it.
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatalf("the gated session could not start enrolment: %v", err)
	}
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, base))
	if err != nil {
		t.Fatalf("the gated session could not confirm enrolment: %v", err)
	}

	// The session reissued once a factor stands is no longer gated.
	lifted, err := auth.Identity(ctx, confirmed.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if lifted.EnrolmentRequired {
		t.Fatal("the session reissued after enrolment is still flagged enrolment_required")
	}
}

func TestUnenrolledAllowedByPolicy(t *testing.T) {
	forEngines(t, runUnenrolledAllowedByPolicy)
}

// runUnenrolledAllowedByPolicy: on the harness default (`optional`) instance, an
// unenrolled account's browser login mints a real session directly — no
// challenge, and no enrolment gate.
func runUnenrolledAllowedByPolicy(t *testing.T, db *store.DB) {
	adm := bootstrapAdmin(t, db, adminOpts{
		username: "optional-admin", displayName: "Optional Admin",
		password: "correct horse battery staple optional",
	})
	auth, password := adm.auth, adm.password
	ctx := t.Context()

	r, err := auth.LocalLogin(ctx, "optional-admin", password, service.ArtifactBrowser)
	if err != nil {
		t.Fatalf("browser login: %v", err)
	}
	if r.Challenge != nil {
		t.Fatal("an unenrolled account under the optional policy was challenged")
	}
	if r.SessionToken == "" {
		t.Fatal("an unenrolled optional-policy login minted no session")
	}
	id, err := auth.Identity(ctx, r.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if id.EnrolmentRequired {
		t.Fatal("an optional-policy session is flagged enrolment_required")
	}
}
