package service

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func mailTestSeed(sink *mailtest.Sink) map[string]string {
	return map[string]string{"HIKYO_MAIL_ADDR": sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "Hikyo <hikyo@example.com>", "HIKYO_MAIL_USER": "operator", "HIKYO_MAIL_PASSWORD": " password\n", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.1/32"}
}

func TestSelfConfigTestMailRecordsOutcomeAfterActorRevocation(t *testing.T) {
	t.Parallel()
	owner := make(chan struct {
		s     *SelfConfig
		local Actor
	}, 1)
	revoked := make(chan error, 1)
	sink := mailtest.NewWithOptions(t, mailtest.Options{Mode: "implicit", OnMessage: func(mailtest.Message) error {
		fixture := <-owner
		grants := &Grants{DB: fixture.s.DB}
		err := grants.Revoke(t.Context(), fixture.local, GrantSpec{Target: fixture.local.principal, Capability: domain.CapInstanceConfig})
		revoked <- err
		return err
	}})
	s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "mail-outcome.db")}, mailTestSeed(sink))
	owner <- struct {
		s     *SelfConfig
		local Actor
	}{s, local}
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	runSelfConfig(t, s)
	actor, sessionID := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "mail-test", OwnerInstanceID: status.OwnerInstanceID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1, To: "recipient@example.com"})
	result, err := s.TestMail(t.Context(), actor, SelfConfigMailTestRequest{Revision: 1, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, To: "recipient@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
	if !result.Sent || len(sink.Messages()) != 1 {
		t.Fatal("completed mail was not durably acknowledged")
	}
	if _, err := s.Status(t.Context(), actor); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("initiating actor remained valid after revocation: %v", err)
	}
}

func TestSelfConfigTestMailChargesFivePerPrincipalPerHour(t *testing.T) {
	t.Parallel()
	sink := mailtest.New(t, "implicit")
	s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "mail-budget.db")}, mailTestSeed(sink))
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	runSelfConfig(t, s)
	actor, sessionID := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 6; attempt++ {
		selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "mail-test", OwnerInstanceID: status.OwnerInstanceID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1, To: "recipient@example.com"})
		_, err := s.TestMail(t.Context(), actor, SelfConfigMailTestRequest{Revision: 1, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, To: "recipient@example.com"})
		if attempt <= 5 && err != nil {
			t.Fatal(err)
		}
		if attempt == 6 && !errors.Is(err, ErrSelfConfigMailLimited) {
			t.Fatalf("sixth test got %v, want rate refusal", err)
		}
	}
	if len(sink.Messages()) != 5 {
		t.Fatalf("relay received %d messages", len(sink.Messages()))
	}
}
