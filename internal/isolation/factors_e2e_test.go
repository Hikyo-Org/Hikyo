package isolation

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The A1 factor slice end to end on a real datastore (#54, human-auth ADR §
// Factors, § Account-security mutations, § Recovery). These exercise the locked
// mechanisms the mvp-boundary A1 row names for the factor half: recovery
// single-use and its complete session-less flow, the account-security mutation
// that cannot self-authorize, and step-up elevation of an under-assured
// session.

// totpCode is the code an authenticator app would display for the seed carried
// in an otpauth URI at time at — the test playing the authenticator. Parameters
// match crypto/totp.go (30s period, 6 digits, SHA-1), so pquerna's default
// GenerateCode agrees with the server's ValidateTOTP.
func totpCode(t *testing.T, otpauthURI string, at time.Time) string {
	t.Helper()
	u, err := url.Parse(otpauthURI)
	if err != nil {
		t.Fatalf("otpauth uri %q: %v", otpauthURI, err)
	}
	code, err := totp.GenerateCode(u.Query().Get("secret"), at)
	if err != nil {
		t.Fatalf("computing a TOTP code: %v", err)
	}
	return code
}

// enrolTOTPAndStepUp runs the full first-factor ceremony for the "factor-admin"
// account: log in over the CLI artifact, enrol TOTP, confirm it 30s later, and
// step the session up 60s later. It advances the caller's clock (which auth.Now
// closes over) through the ceremony and returns the stepped-up session token.
func enrolTOTPAndStepUp(t *testing.T, auth *service.Auth, ctx context.Context, base time.Time, clk *time.Time, password string) string {
	t.Helper()
	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatalf("enrol start: %v", err)
	}
	*clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, *clk))
	if err != nil {
		t.Fatalf("enrol confirm: %v", err)
	}
	*clk = base.Add(60 * time.Second)
	stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, totpCode(t, uri, *clk))
	if err != nil {
		t.Fatalf("step-up: %v", err)
	}
	return stepped.SessionToken
}

// bootstrapFactorAdmin adds the environment read needed by factor
// reauthentication flows to the shared first-administrator fixture. One Auth
// (one root key) drives the whole flow so sealed material stays readable.
func bootstrapFactorAdmin(t *testing.T, db *store.DB) admin {
	t.Helper()
	administrator := bootstrapAdmin(t, db, adminOpts{
		username: "factor-admin", displayName: "Factor Admin",
		password: "correct horse battery staple factor",
	})
	// See bootstrapWebAuthnAdmin: the reauth routes authorize the environment
	// under `read` before inspecting its policy, and bootstrap seeds no tenant
	// capability.
	grantRead(t, db, administrator.boot.PrincipalID)
	return administrator
}

// runFactorLifecycle drives every factor audit event once, so a suite sharing a
// datastore (audit_e2e's emitter check) finds each event actually reached a
// trail. It advances an injected clock across the confirm/step-up boundary so
// the step-up code is a later, unspent time step than the one confirmation
// consumed.
func runFactorLifecycle(t *testing.T, auth *service.Auth, ctx context.Context, username, password string) {
	t.Helper()
	orig := auth.Now
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	defer func() { auth.Now = orig }()

	login, err := auth.LocalLogin(ctx, username, password, service.ArtifactCLI)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	// Profile metadata changes use existing account proof and emit their own
	// value-free audit event before the factor lifecycle replaces credentials.
	profile, err := auth.MyProfile(ctx, login.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.UpdateMyProfile(ctx, login.SessionToken, profile, password); err != nil {
		t.Fatal(err)
	}
	// Recovery: regenerate a batch (password proof), then consume one code.
	codes, _, err := auth.GenerateRecoveryCodes(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	if _, err := auth.ConsumeRecoveryCode(ctx, username, codes[0]); err != nil {
		t.Fatalf("consume recovery code: %v", err)
	}
	// Consuming revoked every session; log in afresh to enrol.
	relog, err := auth.LocalLogin(ctx, username, password, service.ArtifactCLI)
	if err != nil {
		t.Fatalf("re-login after recovery: %v", err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, relog.SessionToken, password)
	if err != nil {
		t.Fatalf("enrol start: %v", err)
	}
	// The confirming code may be the enrolment step's own — last_step seeds one
	// below the creation step, so the creation step is not pre-consumed — but
	// this lifecycle advances a window so the later step-up code is a distinct,
	// unspent step from the one confirmation consumes.
	clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, relog.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatalf("enrol confirm: %v", err)
	}
	clk = base.Add(60 * time.Second)
	stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatalf("step-up: %v", err)
	}
	if _, err := auth.RemoveTOTP(ctx, stepped.SessionToken, password); err != nil {
		t.Fatalf("remove totp: %v", err)
	}
}

func TestRecoveryCompleteFlow(t *testing.T) {
	forEngines(t, runRecoveryFlow)
}

func TestPasswordReauthEvidenceRejectsReplacedCredential(t *testing.T) {
	forEngines(t, runPasswordReauthEvidenceRejectsReplacedCredential)
}

func TestRecoveryCodeRegenerationTOTPReplay(t *testing.T) {
	forEngines(t, runRecoveryCodeRegenerationTOTPReplay)
}

func runPasswordReauthEvidenceRejectsReplacedCredential(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, boot, password := factorAdmin.auth, factorAdmin.boot, factorAdmin.password
	ctx := t.Context()
	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := auth.VerifyReauthProof(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}

	// Move the verified password row exactly as a concurrent replacement does.
	// Reusing the sealed bytes keeps this test focused on evidence liveness: the
	// row version, not the replacement password's contents, is the CAS boundary.
	err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		account, err := az.AccountByPrincipal(ctx, boot.PrincipalID)
		if err != nil {
			return err
		}
		credential, err := az.PasswordCredentialFor(ctx, account.ID)
		if err != nil {
			return err
		}
		replaced, err := az.ReplacePasswordCredential(ctx, credential, time.Now().UTC())
		if err != nil {
			return err
		}
		if !replaced {
			t.Fatal("password credential replacement lost its CAS")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return auth.ConsumeReauthEvidence(ctx, az, evidence, boot.PrincipalID)
	})
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("ConsumeReauthEvidence() after password replacement error = %v, want %v", err, domain.ErrUnauthenticated)
	}
}

func runRecoveryCodeRegenerationTOTPReplay(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	ctx := t.Context()
	now := time.Now().UTC()
	auth.Now = func() time.Time { return now }
	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, now))
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(60 * time.Second)
	code := totpCode(t, uri, now)
	_, reissued, err := auth.GenerateRecoveryCodes(ctx, confirmed.SessionToken, code)
	if err != nil {
		t.Fatalf("first regeneration: %v", err)
	}
	if _, _, err := auth.GenerateRecoveryCodes(ctx, reissued.SessionToken, code); !errors.Is(err, service.ErrTOTPCodeAlreadyUsed) {
		t.Fatalf("replayed regeneration code error = %v, want %v", err, service.ErrTOTPCodeAlreadyUsed)
	}
}

// runRecoveryFlow is the A1 recovery family: single-use, the complete
// session-less flow (code -> authority -> establish -> login), and the mid-reset
// invariant that the authority is not a session.
func runRecoveryFlow(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	ctx := t.Context()
	orgs := &service.Orgs{DB: db}

	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.GenerateRecoveryCodes(ctx, login.SessionToken, ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("missing recovery regeneration proof error = %v, want %v", err, domain.ErrUnauthenticated)
	}
	codes, _, err := auth.GenerateRecoveryCodes(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != service.RecoveryBatchSize {
		t.Fatalf("got %d recovery codes, want %d", len(codes), service.RecoveryBatchSize)
	}

	// Consume one code for a credential-establishment authority.
	rec, err := auth.ConsumeRecoveryCode(ctx, "factor-admin", codes[0])
	if err != nil {
		t.Fatalf("consuming a valid recovery code: %v", err)
	}
	if rec.Authority == "" {
		t.Fatal("recovery consumption returned no authority")
	}

	// The authority creates NO session: it does not resolve as one.
	if _, err := auth.Identity(ctx, rec.Authority); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("the recovery authority resolves as a session: %v", err)
	}
	// Mid-reset: an MFA-mandatory op attempted with the authority (which is not
	// a session) is refused — the authority carries no assurance and no session.
	if _, err := orgs.Create(ctx, service.Bearer(rec.Authority), "reveal-attempt", true, []byte(`{}`)); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("the recovery authority authorized an MFA-mandatory op: %v", err)
	}

	// Single-use: the same code cannot be consumed again.
	if _, err := auth.ConsumeRecoveryCode(ctx, "factor-admin", codes[0]); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("a recovery code was consumed twice: %v", err)
	}

	// Complete the flow: establish a new password with the authority, then log
	// in with it. The old password's sessions were revoked at consumption.
	const newPassword = "an entirely different passphrase now"
	if err := auth.EstablishCredential(ctx, rec.Authority, newPassword); err != nil {
		t.Fatalf("establishing a credential from the recovery authority: %v", err)
	}
	if _, err := auth.LocalLogin(ctx, "factor-admin", newPassword, service.ArtifactCLI); err != nil {
		t.Fatalf("login with the re-established password: %v", err)
	}
}

func TestAccountSecurityCannotSelfAuthorize(t *testing.T) {
	forEngines(t, runSelfAuthorize)
}

// runSelfAuthorize asserts an account-security mutation cannot authorize itself:
// enrolling TOTP requires the PRE-EXISTING password, and the code of the factor
// being enrolled is not that proof.
func runSelfAuthorize(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	ctx := t.Context()

	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}

	// A wrong password is refused: the pre-existing credential is the proof.
	if _, err := auth.EnrolTOTPStart(ctx, login.SessionToken, "not the password"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("enrolment start accepted a wrong password: %v", err)
	}

	// The correct password authorizes staging the seed.
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	// The new factor's own code cannot stand in for the account-security proof:
	// the proof is evaluated over credentials that predate the mutation.
	if _, err := auth.EnrolTOTPStart(ctx, login.SessionToken, totpCode(t, uri, base)); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("enrolment start accepted the new factor's own code as proof: %v", err)
	}

	clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatal(err)
	}
	// The reissued session carries ONLY the proof class — password, not totp:
	// confirming an enrolment is not presenting the factor.
	if len(confirmed.Assurance.Factors) != 1 || confirmed.Assurance.Factors[0] != "password" {
		t.Fatalf("the reissued session carries %v, want exactly [password] — enrolment must not self-elevate", confirmed.Assurance.Factors)
	}
}

func TestStepUpElevates(t *testing.T) {
	forEngines(t, runStepUpElevates)
}

// runStepUpElevates asserts a password-only session is refused an MFA-mandatory
// operation and that a TOTP step-up unlocks it.
func runStepUpElevates(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	ctx := t.Context()
	orgs := &service.Orgs{DB: db}

	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	// The admin HOLDS instance-config, but a password-only session is short of
	// the MFA-mandatory rule: refused for want of assurance, not of the grant.
	if _, err := orgs.Create(ctx, service.Bearer(login.SessionToken), "too-weak", true, []byte(`{}`)); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("a password-only session created an org: %v", err)
	}

	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatal(err)
	}
	clk = base.Add(60 * time.Second)
	stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFactor(stepped.Assurance.Factors, "totp") || !hasFactor(stepped.Assurance.Factors, "password") {
		t.Fatalf("stepped-up session carries %v, want both password and totp", stepped.Assurance.Factors)
	}
	// The elevated session now satisfies the MFA-mandatory rule.
	if _, err := orgs.Create(ctx, service.Bearer(stepped.SessionToken), "now-elevated", true, []byte(`{}`)); err != nil {
		t.Fatalf("a two-factor session was still refused: %v", err)
	}
}

func TestTOTPConfirmSameStep(t *testing.T) {
	forEngines(t, runConfirmSameStep)
}

// runConfirmSameStep asserts the code shown in the SAME time step as enrolment
// start confirms the enrolment (human-auth ADR §141: single-use is per step and
// the creation step has consumed nothing yet, so it must not be pre-consumed),
// and that a code from an earlier step is refused.
func runConfirmSameStep(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	ctx := t.Context()

	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	// A code two steps before enrolment is refused: it does not validate in the
	// skew window, so single-use never even weighs in.
	if _, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, base.Add(-60*time.Second))); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("confirm accepted a code two steps before enrolment: %v", err)
	}
	// The code of the enrolment step itself confirms — same 30-second window as
	// start, no clock advance.
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, base))
	if err != nil {
		t.Fatalf("confirm refused the enrolment-step code: %v", err)
	}
	if len(confirmed.Assurance.Factors) != 1 || confirmed.Assurance.Factors[0] != "password" {
		t.Fatalf("reissued session carries %v, want exactly [password]", confirmed.Assurance.Factors)
	}
}

func TestTOTPStepUpReplay(t *testing.T) {
	forEngines(t, runStepUpReplay)
}

// runStepUpReplay asserts single-use per (account, step): a code that stepped a
// session up cannot step it up again in the SAME time step, and the replay is
// refused by name with the already-used sentinel (human-auth ADR §141, §207 —
// the user waits for the next code), not the uniform bad-code refusal.
func runStepUpReplay(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	ctx := t.Context()

	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, base))
	if err != nil {
		t.Fatal(err)
	}

	clk = base.Add(60 * time.Second)
	code := totpCode(t, uri, clk)
	stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, code)
	if err != nil {
		t.Fatalf("first step-up: %v", err)
	}
	// Same step, same code: single-use refuses, and by name.
	if _, err := auth.StepUpTOTP(ctx, stepped.SessionToken, code); !errors.Is(err, service.ErrTOTPCodeAlreadyUsed) {
		t.Fatalf("replayed step-up code was not refused as already-used: %v", err)
	}
}

func hasFactor(factors []string, want string) bool {
	for _, f := range factors {
		if f == want {
			return true
		}
	}
	return false
}
