package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
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
		if attempt == 6 {
			// The hourly window names its own reopening instead of the fixed guess.
			limited, ok := errors.AsType[*admission.RateLimitedError](err)
			if !ok || limited.Wait <= 0 || limited.Wait > time.Hour {
				t.Fatalf("sixth test refusal %v carries no wait within its hourly window", err)
			}
		}
	}
	if len(sink.Messages()) != 5 {
		t.Fatalf("relay received %d messages", len(sink.Messages()))
	}
}

// schema-model.md trims every write path; the SMTP password has no exemption (#1492).
func TestSelfConfigTrimsAStagedMailPassword(t *testing.T) {
	t.Parallel()
	sink := mailtest.New(t, "implicit")
	s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "mail-trim.db")}, mailTestSeed(sink))
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, sessionID := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	staged, err := values.Set(t.Context(), local, scope, "HIKYO_MAIL_PASSWORD", "\t rotated secret \r\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatal(err)
	}
	runSelfConfig(t, s)
	req := SelfConfigApplyRequest{Revision: 2, ExpectedGeneration: 1, SchemaVersion: runtimeconfig.SchemaVersion, IdempotencyKey: "trim-apply"}
	selfConfigReauthenticate(t, s, sessionID, SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1})
	if _, err := s.Apply(t.Context(), actor, req); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err := s.Capture(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Send(t.Context(), "owner@example.com", "trim", "body"); err != nil {
		t.Fatal(err)
	}
	messages := sink.Messages()
	if len(messages) != 1 || messages[0].Password != "rotated secret" {
		t.Fatalf("SMTP AUTH saw %d messages, or an untrimmed password", len(messages))
	}
}

// A draft staged before the trim was uniform can hold a padded password. Publish
// must seal the snapshot and the live cell from the same trimmed value.
func TestPublishTrimsAPaddedPendingMailPassword(t *testing.T) {
	t.Parallel()
	sink := mailtest.New(t, "implicit")
	s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "mail-padded.db")}, mailTestSeed(sink))
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, _ := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	const name = "HIKYO_MAIL_PASSWORD"
	keyTarget := authz.KeyByName(name)
	sealer, err := sealerForKey(t.Context(), s.DB, s.Keyring, local, authz.OpValueStage, scope, &keyTarget)
	if err != nil {
		t.Fatal(err)
	}
	var versionID string
	if err := tx.Write(t.Context(), s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := local.resolve(ctx, az, time.Now().UTC())
		if err != nil {
			return err
		}
		p, err := az.AuthorizeKey(ctx, caller, authz.OpValueStage, scope, keyTarget)
		if err != nil {
			return err
		}
		key, err := findKey(ctx, r.Catalogue(), p, name)
		if err != nil {
			return err
		}
		revision, err := currentRevision(ctx, r, p)
		if err != nil {
			return err
		}
		entry, err := r.Values().Get(ctx, p, key.ID)
		if err != nil {
			return err
		}
		if versionID, err = newID("pcv"); err != nil {
			return err
		}
		sealed, err := sealer.SealField(pendingAAD(string(scope.Org), string(scope.Project), string(scope.Env), key.ID, versionID), []byte(" \t padded secret \r\n"))
		if err != nil {
			return err
		}
		if err := fenceProject(ctx, r, p, sealer, scope); err != nil {
			return err
		}
		return r.Pending().Stage(ctx, p, store.NewPendingChange{
			ID: versionID, KeyID: key.ID, OwnerID: string(caller.Principal), Operation: store.PendingSet, Ciphertext: sealed,
			StagedFromRevision: revision, StagedFromEntry: entry.ID, CreatedAt: store.CanonTime(time.Now()),
			Source: store.PendingSourceValues, Secret: true, MaterialSecret: true,
		})
	}); err != nil {
		t.Fatal(err)
	}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{versionID}}); err != nil {
		t.Fatal(err)
	}
	var live, snapshot string
	if err := tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := local.resolve(ctx, az, time.Now().UTC())
		if err != nil {
			return err
		}
		p, err := az.AuthorizeKey(ctx, caller, authz.OpValueStage, scope, keyTarget)
		if err != nil {
			return err
		}
		key, err := findKey(ctx, r.Catalogue(), p, name)
		if err != nil {
			return err
		}
		entry, err := r.Values().Get(ctx, p, key.ID)
		if err != nil {
			return err
		}
		if live, err = openCell(sealer, entry); err != nil {
			return err
		}
		pp, err := az.AuthorizeKey(ctx, caller, authz.OpValuePublish, scope, keyTarget)
		if err != nil {
			return err
		}
		latest, err := r.Snapshots().Latest(ctx, pp)
		if err != nil {
			return err
		}
		entries, err := r.Snapshots().Entries(ctx, pp, latest)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.KeyID != key.ID {
				continue
			}
			plain, err := sealer.OpenField(snapshotAAD(e.OrgID, e.ProjectID, e.EnvironmentID, e.KeyID, e.SnapshotID, e.ID), e.Ciphertext)
			if err != nil {
				return err
			}
			snapshot = string(plain)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if live != "padded secret" || snapshot != "padded secret" {
		t.Fatalf("live %q and snapshot %q must both be trimmed", live, snapshot)
	}
}
