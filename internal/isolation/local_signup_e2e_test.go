package isolation

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestLocalSignup(t *testing.T) { forEngines(t, runLocalSignup) }

func signupMailFragment(t *testing.T, message mailtest.Message) url.Values {
	t.Helper()
	parsed, err := mail.ReadMessage(strings.NewReader(message.Data))
	if err != nil {
		t.Fatal(err)
	}
	var reader io.Reader = parsed.Body
	if strings.EqualFold(parsed.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
		reader = quotedprintable.NewReader(reader)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://hikyo.test/signup/verify#") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatal(err)
			}
			fragment, err := url.ParseQuery(u.Fragment)
			if err != nil {
				t.Fatal(err)
			}
			return fragment
		}
	}
	t.Fatalf("verification link absent from mail body")
	return nil
}

func localSignupHarness(t *testing.T, db *store.DB) (*service.Auth, *service.Registration, *mailtest.Sink) {
	t.Helper()
	sink := mailtest.New(t, "implicit")
	bundle, err := runtimeconfig.Prepare(map[string]string{"HIKYO_MAIL_ADDR": sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	auth := authService(t, db)
	auth.ExternalOrigin = "https://hikyo.test"
	reg := newRegistration(t, service.RegistrationConfig{DB: db, Auth: auth, PublicOriginExplicit: true, MailConfigured: func(context.Context) (bool, error) { return true, nil }})
	if err = auth.EnableSignup(reg, service.NewBudget()); err != nil {
		t.Fatal(err)
	}
	auth.EnableLocalSignup(func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil })
	return auth, reg, sink
}

func runLocalSignup(t *testing.T, db *store.DB) {
	auth, reg, sink := localSignupHarness(t, db)
	ctx := t.Context()
	now := time.Now()
	auth.Now = func() time.Time { return now }
	input := service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{Domains: []string{"example.test"}}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}
	if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, input, ""); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"bad", "miss@else.test"} {
		if err := auth.Signup(ctx, email, ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.Messages()) != 0 {
		t.Fatal("refused request sent mail")
	}
	if err := auth.Signup(ctx, "User@EXAMPLE.test", ""); err != nil {
		t.Fatal(err)
	}
	first := signupMailFragment(t, sink.Messages()[0])
	var initial authz.RegistrationSignup
	if err := tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var e error
		initial, e = az.RegistrationSignupByEmail(ctx, "User@example.test")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if !initial.ExpiresAt.Equal(store.CanonTime(now.Add(24 * time.Hour))) {
		t.Fatalf("expiry %v", initial.ExpiresAt)
	}
	if err := auth.Signup(ctx, "User@example.test", ""); err != nil {
		t.Fatal(err)
	}
	second := signupMailFragment(t, sink.Messages()[1])
	if first.Get("token") == second.Get("token") {
		t.Fatal("resend reused token")
	}
	if err := tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		row, e := az.RegistrationSignupByEmail(ctx, "User@example.test")
		if row.ID != initial.ID {
			t.Error("live resend replaced row")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	password := "LongSignUpPassword42!"
	verify := service.SignupVerification{Token: first.Get("token"), DisplayName: "Test User", Password: password, Landing: second.Get("landing")}
	if err := auth.VerifySignup(ctx, verify); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("superseded token %v", err)
	}
	verify.Token = second.Get("token")
	// Live policy excludes the address without spending its pending token.
	input.Local.Domains = []string{"else.test"}
	if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, input, ""); err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifySignup(ctx, verify); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("policy change %v", err)
	}
	input.Local.Domains = []string{"example.test"}
	if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, input, ""); err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifySignup(ctx, verify); err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifySignup(ctx, verify); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("replay %v", err)
	}
	if err := tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		_, e := az.RegistrationSignupByEmail(ctx, "User@example.test")
		if !errors.Is(e, domain.ErrNotFound) {
			t.Errorf("consumed row %v", e)
		}
		_, e = az.AccountByEmail(ctx, "User@example.test")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// No session exists until an explicit email login, whose domain is canonicalized.
	if n := queryInt(t, db, "SELECT COUNT(*) FROM sessions WHERE principal_id IN (SELECT principal_id FROM accounts WHERE email='User@example.test')"); n != 0 {
		t.Fatalf("verification minted %d sessions", n)
	}
	if _, err := auth.LocalLogin(ctx, "User@EXAMPLE.test", password, service.ArtifactCLI); err != nil {
		t.Fatalf("email login %v", err)
	}
	execRaw(t, db, "UPDATE principals SET privacy_state='restricted' WHERE id IN (SELECT principal_id FROM accounts WHERE email='User@example.test')")
	if err := auth.Signup(ctx, "User@example.test", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink.Messages()[2].Data, "signup/verify") || strings.Contains(sink.Messages()[2].Data, "hik_1_su_") {
		t.Fatal("existing address email contains link")
	}
	execRaw(t, db, "UPDATE principals SET privacy_state='active' WHERE id IN (SELECT principal_id FROM accounts WHERE email='User@example.test')")
	if err := auth.Signup(ctx, "expired@example.test", ""); err != nil {
		t.Fatal(err)
	}
	expired := signupMailFragment(t, sink.Messages()[3])
	now = now.Add(service.SignupLifetime + time.Second)
	if err := auth.VerifySignup(ctx, service.SignupVerification{Token: expired.Get("token"), Password: password}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expired token %v", err)
	}
	if err := auth.ReapSignups(ctx); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_signups"); n != 0 {
		t.Fatalf("reaper left %d rows", n)
	}
	if err := reg.Delete(ctx, service.LocalPrincipal(root), instanceReg, ""); err != nil {
		t.Fatal(err)
	}
	if err := auth.Signup(ctx, "closed@example.test", ""); err != nil {
		t.Fatal(err)
	}
	if len(sink.Messages()) != 4 {
		t.Fatalf("SMTP sends %d", len(sink.Messages()))
	}
}

func TestLocalSignupFreshOrg(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, sink := localSignupHarness(t, db)
		ctx := t.Context()
		if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingFreshOrg, Cap: 5}}, ""); err != nil {
			t.Fatal(err)
		}
		password := "FreshOrganizationPassword42!"
		if err := auth.Signup(ctx, "fresh@example.test", ""); err != nil {
			t.Fatal(err)
		}
		first := signupMailFragment(t, sink.Messages()[0])
		verify := service.SignupVerification{Token: first.Get("token"), DisplayName: "Fresh User", Password: password, OrgName: "org-a", Landing: "fresh-org"}
		// Fixture org-a collides. The explicit field refusal must roll back token consumption.
		if err := auth.VerifySignup(ctx, verify); err == nil {
			t.Fatal("org_name collision accepted")
		} else {
			wantRefusal(t, err, "org_name")
		}
		if err := tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.RegistrationSignupByEmail(ctx, "fresh@example.test")
			return err
		}); err != nil {
			t.Fatalf("collision spent token: %v", err)
		}
		verify.OrgName = "Signup Fresh Org"
		if err := auth.VerifySignup(ctx, verify); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM orgs WHERE name='Signup Fresh Org' AND origin='registration'"); n != 1 {
			t.Fatal("fresh name absent")
		}
		if err := auth.Signup(ctx, "stale@example.test", ""); err != nil {
			t.Fatal(err)
		}
		second := signupMailFragment(t, sink.Messages()[1])
		verify.Token = second.Get("token")
		verify.Landing = "none"
		verify.OrgName = "Ignored stale name"
		if err := auth.VerifySignup(ctx, verify); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM orgs WHERE origin='registration' AND name LIKE 'org-org_%'"); n != 1 {
			t.Fatalf("stale hint fallback count %d", n)
		}
	})
}

func TestLocalSignupFailedMailChargesBudget(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, _ := localSignupHarness(t, db)
		ctx := t.Context()
		sink := mailtest.NewWithOptions(t, mailtest.Options{Mode: "implicit", OnMessage: func(mailtest.Message) error { return errors.New("injected SMTP failure") }})
		bundle, err := runtimeconfig.Prepare(map[string]string{"HIKYO_MAIL_ADDR": sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.0/8"})
		if err != nil {
			t.Fatal(err)
		}
		auth.EnableLocalSignup(func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil })
		failures := 0
		auth.MailFailed = func() { failures++ }
		if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		for range service.BudgetSignupPerHour {
			if err := auth.Signup(ctx, "failed@example.test", ""); err != nil {
				t.Fatal(err)
			}
		}
		if failures != service.BudgetSignupPerHour {
			t.Fatalf("failure callbacks %d", failures)
		}
		if err := auth.Signup(ctx, "failed@example.test", ""); err == nil {
			t.Fatal("failed sends refunded budget")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.mail_outcome' AND outcome='failure'"); n != service.BudgetSignupPerHour {
			t.Fatalf("failed mail outcomes %d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_signups WHERE email='failed@example.test'"); n != 1 {
			t.Fatalf("resend rows %d", n)
		}
	})
}

func TestLocalSignupTokenRefusalsAndPolicyDelete(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, sink := localSignupHarness(t, db)
		ctx := t.Context()
		password := "TokenRefusalPassword42!"
		in := service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: orgTemplateLanding()}
		policy, err := reg.Put(ctx, service.LocalPrincipal(root), orgAReg, in, "")
		if err != nil {
			t.Fatal(err)
		}
		if err = auth.Signup(ctx, "org-user@example.test", orgA); err != nil {
			t.Fatal(err)
		}
		fragment := signupMailFragment(t, sink.Messages()[0])
		token := fragment.Get("token")
		if fragment.Get("landing") != "org" {
			t.Fatalf("mail landing hint = %q, want org", fragment.Get("landing"))
		}
		if fragment.Get("org") != string(orgA) {
			t.Fatalf("mail org scope %q", fragment.Get("org"))
		}
		// Rendering and inspecting mail is read-only: repeated row resolution never consumes.
		for range 2 {
			if err = tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
				_, e := az.RegistrationSignupByEmail(ctx, "org-user@example.test")
				return e
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err = auth.VerifySignup(ctx, service.SignupVerification{Token: token, DisplayName: "Org User", Password: password, Landing: fragment.Get("landing")}); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE principal_id IN (SELECT principal_id FROM accounts WHERE email='org-user@example.test')"); n == 0 {
			t.Fatal("org template granted nothing")
		}
		unknown, _, err := crypto.NewArtifact(crypto.ArtifactSignup)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"malformed", unknown} {
			if err = auth.VerifySignup(ctx, service.SignupVerification{Token: value, Password: password}); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("token refusal %v", err)
			}
		}
		if err = auth.VerifySignup(ctx, service.SignupVerification{Token: "malformed", Password: "short"}); !errors.Is(err, service.ErrWeakPassword) {
			t.Fatalf("password must refuse before token: %v", err)
		}
		if err = auth.Signup(ctx, "epoch@example.test", orgA); err != nil {
			t.Fatal(err)
		}
		epochToken := signupMailFragment(t, sink.Messages()[1]).Get("token")
		execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch=credential_epoch+1 WHERE id=1")
		if err = auth.VerifySignup(ctx, service.SignupVerification{Token: epochToken, Password: password}); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("epoch token %v", err)
		}
		if err = reg.Delete(ctx, service.LocalPrincipal(root), orgAReg, ""); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_signups WHERE policy_id='"+policy.ID+"'"); n != 0 {
			t.Fatalf("policy deletion left %d rows", n)
		}
		if err = auth.VerifySignup(ctx, service.SignupVerification{Token: epochToken, Password: password}); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("deleted policy token %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='registration.signup_expired'"); n != 1 {
			t.Fatalf("policy deletion expiry events %d", n)
		}
	})
}

func TestLocalSignupClosedAndFencedRequestsUncharged(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, sink := localSignupHarness(t, db)
		ctx := t.Context()
		// More than the signup budget against a closed scope cannot exhaust it.
		for range service.BudgetSignupPerHour + 1 {
			if err := auth.Signup(ctx, "closed@example.test", ""); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		auth.EnableLocalSignup(func(context.Context) (*runtimeconfig.Bundle, error) { return nil, service.ErrSelfConfigFenced })
		for range service.BudgetSignupPerHour + 1 {
			if err := auth.Signup(ctx, "fenced@example.test", ""); err != nil {
				t.Fatal(err)
			}
		}
		bundle, err := runtimeconfig.Prepare(map[string]string{"HIKYO_MAIL_ADDR": sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.0/8"})
		if err != nil {
			t.Fatal(err)
		}
		auth.EnableLocalSignup(func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil })
		failures := 0
		auth.MailFailed = func() { failures++ }
		if err := auth.Signup(ctx, "admitted@example.test", ""); err != nil {
			t.Fatalf("closed/fenced charged budget: %v", err)
		}
		if len(sink.Messages()) != 1 || failures != 0 {
			t.Fatalf("messages=%d failures=%d", len(sink.Messages()), failures)
		}
	})
}

func TestLocalSignupRecoversOrphanMailIntent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, sink := localSignupHarness(t, db)
		ctx := t.Context()
		var logs bytes.Buffer
		auth.Log = slog.New(slog.NewTextHandler(&logs, nil))
		policy, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, "")
		if err != nil {
			t.Fatal(err)
		}
		oldToken, verifier, err := crypto.NewArtifact(crypto.ArtifactSignup)
		if err != nil {
			t.Fatal(err)
		}
		intentID, err := audit.NewEventID()
		if err != nil {
			t.Fatal(err)
		}
		// Durable state left by a crash after commit and before SMTP, using the same storage/audit seams.
		err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			epoch, e := az.CredentialEpoch(ctx)
			if e != nil {
				return e
			}
			now := time.Now().UTC()
			if e = az.CreateRegistrationSignup(ctx, authz.NewRegistrationSignup{ID: "su_crash", Email: "crash@example.test", TokenVerifier: verifier, PolicyID: policy.ID, CredentialEpoch: epoch, CreatedAt: now, ExpiresAt: now.Add(service.SignupLifetime)}); e != nil {
				return e
			}
			return az.RecordAuthEvent(ctx, audit.Event{ID: intentID, Type: audit.EventRegistrationMailIntent, SchemaVersion: 1, OccurredAt: now, Origin: audit.OriginAPI, Outcome: audit.OutcomeIntent, Object: audit.Object{Type: "registration-policy", ID: policy.ID}, Payload: audit.Payload{"kind": "verification", "recipient": audit.SanitizeFreeText("crash@example.test"), "policy_id": policy.ID}})
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.mail_outcome'"); n != 0 {
			t.Fatal("crash state has outcome")
		}
		if err = auth.Signup(ctx, "crash@example.test", ""); err != nil {
			t.Fatal(err)
		}
		if len(sink.Messages()) != 1 {
			t.Fatalf("recovery sent %d mails", len(sink.Messages()))
		}
		fragment := signupMailFragment(t, sink.Messages()[0])
		if fragment.Get("token") == oldToken {
			t.Fatal("recovery reused token")
		}
		if err = tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			row, e := az.RegistrationSignupByEmail(ctx, "crash@example.test")
			if row.ID != "su_crash" {
				t.Error("recovery replaced live pending row")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.mail_intent'"); n != 2 {
			t.Fatalf("intent count %d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.mail_outcome' AND outcome='success'"); n != 1 {
			t.Fatalf("outcome count %d", n)
		}
		payload := queryString(t, db, "SELECT payload FROM audit_instance_events WHERE type='registration.mail_outcome'")
		if strings.Contains(payload, intentID) {
			t.Fatal("recovery outcome consumed orphan intent")
		}
		trail := queryStrings(t, db, "SELECT payload FROM audit_instance_events WHERE type LIKE 'registration.%'")
		for _, secret := range []string{oldToken, fragment.Get("token"), "https://hikyo.test/signup/verify#", "Verify your email and finish creating your Hikyo account"} {
			if strings.Contains(trail, secret) || strings.Contains(logs.String(), secret) {
				t.Fatal("mail body or link leaked to trail")
			}
		}
	})
}

func TestLocalSignupExpiredRowReplacementAndReaper(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, sink := localSignupHarness(t, db)
		ctx := t.Context()
		now := time.Now().UTC()
		auth.Now = func() time.Time { return now }
		if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		if err := auth.Signup(ctx, "replace@example.test", ""); err != nil {
			t.Fatal(err)
		}
		var original authz.RegistrationSignup
		if err := tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			original, e = az.RegistrationSignupByEmail(ctx, "replace@example.test")
			return e
		}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(service.SignupLifetime + time.Second)
		if err := auth.Signup(ctx, "replace@example.test", ""); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.signup_expired'"); n != 1 {
			t.Fatalf("replacement expiry events %d", n)
		}
		payload := queryString(t, db, "SELECT payload FROM audit_instance_events WHERE type='registration.signup_expired'")
		if !strings.Contains(payload, original.ID) || !strings.Contains(payload, original.PolicyID) || !strings.Contains(payload, `"expired"`) {
			t.Fatalf("replacement expiry payload %s", payload)
		}
		if len(sink.Messages()) != 2 {
			t.Fatalf("replacement sends %d", len(sink.Messages()))
		}
		if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			live, e := az.RegistrationSignupByEmail(ctx, "replace@example.test")
			if e != nil {
				return e
			}
			if live.ID == original.ID {
				t.Error("expired row reissued instead of replaced")
			}
			gone, e := az.PruneExpiredRegistrationSignup(ctx, live.ID, now)
			if gone {
				t.Error("reaper CAS deleted future-expiry row")
			}
			return e
		}); err != nil {
			t.Fatal(err)
		}
		if err := auth.ReapSignups(ctx); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_signups"); n != 1 {
			t.Fatalf("reaper pruned live row count %d", n)
		}
	})
}

func TestLocalSignupReissueRefusesDeletedPendingRow(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, _ := localSignupHarness(t, db)
		ctx := t.Context()
		if _, err := reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		if err := auth.Signup(ctx, "reissue-race@example.test", ""); err != nil {
			t.Fatal(err)
		}
		var snapshot authz.RegistrationSignup
		if err := tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			snapshot, e = az.RegistrationSignupByEmail(ctx, "reissue-race@example.test")
			return e
		}); err != nil {
			t.Fatal(err)
		}
		// Model verification winning after the request read, before UPDATE.
		if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			_, e := az.DeleteRegistrationSignup(ctx, snapshot.ID)
			return e
		}); err != nil {
			t.Fatal(err)
		}
		_, verifier, err := crypto.NewArtifact(crypto.ArtifactSignup)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.TokenVerifier = verifier
		err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.ReissueRegistrationSignup(ctx, snapshot)
		})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("missing row reissue = %v", err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM registration_signups"); n != 0 {
			t.Fatalf("missing row revived %d", n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='registration.mail_intent'"); n != 1 {
			t.Fatalf("missing-row reissue created intent: %d", n)
		}
	})
}

func TestLocalSignupReleasesAdmissionBeforeSlowSMTP(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		auth, reg, _ := localSignupHarness(t, db)
		ctx := audit.WithContext(t.Context(), audit.Context{SourceIP: "192.0.2.42", Origin: audit.OriginAPI})
		limiter, err := admission.New(admission.Config{BudgetMiB: 80, ArgonMemoryKiB: 64 * 1024})
		if err != nil {
			t.Fatal(err)
		}
		auth.Admission = limiter
		started, finish := make(chan struct{}), make(chan struct{})
		var once sync.Once
		releaseSMTP := func() { once.Do(func() { close(finish) }) }
		defer releaseSMTP()
		sink := mailtest.NewWithOptions(t, mailtest.Options{Mode: "implicit", OnMessage: func(mailtest.Message) error { close(started); <-finish; return nil }})
		bundle, err := runtimeconfig.Prepare(map[string]string{"HIKYO_MAIL_ADDR": sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.0/8"})
		if err != nil {
			t.Fatal(err)
		}
		auth.EnableLocalSignup(func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil })
		if _, err = reg.Put(ctx, service.LocalPrincipal(root), instanceReg, service.RegistrationPolicyInput{Local: &service.RegistrationLocalEntry{}, Landing: service.RegistrationLanding{Kind: service.LandingNone}}, ""); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { result <- auth.Signup(ctx, "slow@example.test", "") }()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("SMTP did not start")
		}
		if current := limiter.Snapshot(); current.InFlight != 0 {
			t.Fatalf("SMTP held %d password admission slots", current.InFlight)
		}
		// A login's expensive work can be admitted while the mail transport is blocked.
		enterCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		release, err := limiter.Enter(enterCtx, "")
		if err != nil {
			t.Fatalf("SMTP blocked password admission: %v", err)
		}
		release()
		for range admission.PerIPPerMinute - 1 {
			release, e := limiter.Enter(ctx, "192.0.2.42")
			if e != nil {
				t.Fatalf("remaining per-IP admission: %v", e)
			}
			release()
		}
		if _, e := limiter.Enter(ctx, "192.0.2.42"); !errors.Is(e, admission.ErrOverloaded) {
			t.Fatalf("SMTP release refunded per-IP charge: %v", e)
		}
		releaseSMTP()
		select {
		case err = <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("signup did not finish")
		}
		if current := limiter.Snapshot(); current.InFlight != 0 {
			t.Fatalf("signup leaked %d admission slots", current.InFlight)
		}
	})
}
