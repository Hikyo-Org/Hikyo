package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"

	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The registration mailer predicate (#606, mailer-seam 7.3): only a known
// fenced configuration reads "unconfigured"; any other capture failure is a
// fault that must reach the caller, never a silent false.
func TestRegistrationMailPredicate(t *testing.T) {
	configured, err := runtimeconfig.Prepare(map[string]string{
		"HIKYO_MAIL_ADDR": "relay.invalid:465", "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	unconfigured, err := runtimeconfig.Prepare(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	fault := errors.New("metadata read failed")
	for _, tc := range []struct {
		name    string
		bundle  *runtimeconfig.Bundle
		capture error
		want    bool
		wantErr error
	}{
		{name: "configured", bundle: configured, want: true},
		{name: "unconfigured", bundle: unconfigured, want: false},
		{name: "fenced", capture: ErrSelfConfigFenced, want: false},
		{name: "not ready", capture: ErrSelfConfigUnavailable, wantErr: ErrSelfConfigUnavailable},
		{name: "fault", capture: fault, wantErr: fault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			predicate := mailPredicate(func(context.Context) (*runtimeconfig.Bundle, error) {
				return tc.bundle, tc.capture
			})
			got, err := predicate(t.Context())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("predicate error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("predicate = %t, %v; want %t", got, err, tc.want)
			}
		})
	}
	if _, err := SelfConfigMailConfigured(nil); err == nil {
		t.Fatal("a nil runtime configuration built a predicate")
	}
	if _, err := NewRegistration(RegistrationConfig{DB: nil}); err == nil {
		t.Fatal("a registration without a datastore was built")
	}
	if _, err := NewRegistration(RegistrationConfig{DB: &store.DB{}}); err == nil {
		t.Fatal("a registration without a mailer predicate was built")
	}
}

func instanceMailFixture(t *testing.T, engine store.Engine, options mailtest.Options) (*InstanceMail, *SelfConfig, Actor, *mailtest.Sink) {
	t.Helper()
	cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "instance-mail.db")}
	if engine == store.EnginePostgres {
		cfg = selfConfigPostgres(t)
	}
	sink := mailtest.NewWithOptions(t, options)
	seed := mailTestSeed(sink)
	seed["HIKYO_MAIL_TLS"] = options.Mode
	s, local := selfConfigFixtureConfig(t, cfg, seed)
	if _, err := (&Grants{DB: s.DB}).Create(t.Context(), local, GrantSpec{Target: local.principal, Capability: domain.CapAuditRead}); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	runSelfConfig(t, s)
	return &InstanceMail{DB: s.DB, Auth: s.Auth, Capture: s.Capture}, s, local, sink
}

func assertInstanceMailAuditPair(t *testing.T, s *SelfConfig, local Actor, want string) {
	t.Helper()
	page, err := (&Audits{DB: s.DB}).InstanceQuery(t.Context(), local.principal, store.AuditFilter{Type: "registration.mail_*", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var intent, outcome *store.AuditEvent
	for i := range page.Events {
		event := &page.Events[i]
		switch event.Type {
		case audit.EventRegistrationMailIntent:
			intent = event
		case audit.EventRegistrationMailOutcome:
			outcome = event
		}
	}
	if intent == nil || outcome == nil || intent.Seq >= outcome.Seq {
		t.Fatalf("missing ordered intent/outcome pair: %v", page.Events)
	}
	var sent struct {
		Kind      string `json:"kind"`
		Recipient string `json:"recipient"`
	}
	var delivered struct {
		IntentID string `json:"intent_id"`
	}
	if err := json.Unmarshal([]byte(intent.RawPayload), &sent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(outcome.RawPayload), &delivered); err != nil {
		t.Fatal(err)
	}
	if sent.Kind != "test" || sent.Recipient != "recipient@example.com" || delivered.IntentID != intent.ID || string(outcome.Outcome) != want {
		t.Fatalf("incorrect test delivery pair: intent=%s outcome=%s", intent.RawPayload, outcome.RawPayload)
	}
	if strings.Contains(intent.RawPayload+outcome.RawPayload, "This test email") {
		t.Fatal("mail body leaked into trail")
	}
}

func TestInstanceMailTestUsesTLSSinkAndDurableOutcomeBothEngines(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			for _, mode := range []string{"implicit", "starttls"} {
				for _, failure := range []bool{false, true} {
					name := mode + "/" + map[bool]string{false: "success", true: "failure"}[failure]
					t.Run(name, func(t *testing.T) {
						options := mailtest.Options{Mode: mode}
						if failure {
							options.OnMessage = func(mailtest.Message) error { return errors.New("confidential SMTP refusal") }
						}
						m, s, local, sink := instanceMailFixture(t, engine, options)
						if configured, err := m.Configured(t.Context(), local); err != nil || !configured {
							t.Fatalf("configured = %v, %v", configured, err)
						}
						if len(sink.Messages()) != 0 {
							t.Fatal("configured status probed SMTP")
						}
						err := m.Test(t.Context(), local, "recipient@example.com", "")
						if failure && !errors.Is(err, mail.ErrDelivery) {
							t.Fatalf("failed delivery returned %v", err)
						}
						if !failure && err != nil {
							t.Fatal(err)
						}
						messages := sink.Messages()
						if len(messages) != 1 || messages[0].Username != "operator" {
							t.Fatalf("SMTP capture = %v", messages)
						}
						assertInstanceMailAuditPair(t, s, local, map[bool]string{false: "success", true: "failure"}[failure])
					})
				}
			}
		})
	}
}

func TestInstanceMailTestRequiresProofAndBoundsRateBothEngines(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			m, s, local, sink := instanceMailFixture(t, engine, mailtest.Options{Mode: "implicit"})
			actor, _ := selfConfigSession(t, s, local)
			if err := m.Test(t.Context(), actor, "recipient@example.com", ""); !errors.Is(err, ErrReauthProofRequired) {
				t.Fatalf("missing proof returned %v", err)
			}
			if len(sink.Messages()) != 0 {
				t.Fatal("proof refusal sent mail")
			}
			for attempt := 1; attempt <= 6; attempt++ {
				err := m.Test(t.Context(), local, "recipient@example.com", "")
				if attempt <= 5 && err != nil {
					t.Fatal(err)
				}
				if attempt == 6 {
					limited, ok := errors.AsType[*admission.RateLimitedError](err)
					if !ok || !errors.Is(err, ErrSelfConfigMailLimited) || limited.Wait <= 0 || limited.Wait > time.Hour {
						t.Fatalf("sixth send lacks hourly refusal: %v", err)
					}
				}
			}
			if len(sink.Messages()) != 5 {
				t.Fatalf("relay received %d messages", len(sink.Messages()))
			}
		})
	}
}

func TestInstanceMailTestConcurrentRefusalBothEngines(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			m, _, local, sink := instanceMailFixture(t, engine, mailtest.Options{Mode: "implicit", OnMessage: func(mailtest.Message) error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-time.After(3 * time.Second):
					return errors.New("fixture was not released")
				}
			}})
			done := make(chan error, 1)
			go func() { done <- m.Test(t.Context(), local, "recipient@example.com", "") }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("first send never reached relay")
			}
			if err := m.Test(t.Context(), local, "recipient@example.com", ""); !errors.Is(err, ErrSelfConfigMailLimited) {
				t.Fatalf("concurrent send returned %v", err)
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("first send did not complete")
			}
			if len(sink.Messages()) != 1 {
				t.Fatal("concurrent refusal still dialed relay")
			}
		})
	}
}

func TestInstanceMailOutcomeSurvivesActorRevocationBothEngines(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			var s *SelfConfig
			var local Actor
			m, fixture, actor, _ := instanceMailFixture(t, engine, mailtest.Options{Mode: "implicit", OnMessage: func(mailtest.Message) error {
				return (&Grants{DB: s.DB}).Revoke(t.Context(), local, GrantSpec{Target: local.principal, Capability: domain.CapInstanceConfig})
			}})
			s, local = fixture, actor
			if err := m.Test(t.Context(), local, "recipient@example.com", ""); err != nil {
				t.Fatal(err)
			}
			assertInstanceMailAuditPair(t, s, local, "success")
		})
	}
}
