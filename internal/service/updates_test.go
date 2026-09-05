package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
)

type updateSourceFunc func(context.Context) ([]updatecheck.Release, error)

func (f updateSourceFunc) Releases(ctx context.Context) ([]updatecheck.Release, error) { return f(ctx) }

type updateControlStub struct {
	submitted    []updater.Request
	job          updater.Job
	beforeSubmit func(updater.Request) error
	acknowledged int
	ackErr       error
}

func (s *updateControlStub) Capability(context.Context) (updater.Capability, error) {
	return updater.Capability{Backend: updater.BackendFlux}, nil
}
func (s *updateControlStub) Submit(_ context.Context, req updater.Request) (updater.Job, error) {
	if s.beforeSubmit != nil {
		if err := s.beforeSubmit(req); err != nil {
			return updater.Job{}, err
		}
	}
	s.submitted = append(s.submitted, req)
	s.job = updater.Job{
		ID: req.ID, Backend: updater.BackendFlux, Version: req.Version,
		RequestedBy: req.RequestedBy, State: updater.StateQueued, Phase: "queued", RequestedAt: time.Now().UTC(),
	}
	return s.job, nil
}
func (s *updateControlStub) Job(context.Context, string) (updater.Job, error) { return s.job, nil }
func (s *updateControlStub) AcknowledgeOutcome(context.Context, string) error {
	s.acknowledged++
	if s.ackErr != nil {
		err := s.ackErr
		s.ackErr = nil
		return err
	}
	s.job.OutcomeReported = true
	return nil
}
func (s *updateControlStub) PendingOutcomes(context.Context) ([]updater.Job, error) {
	if s.job.State.Terminal() && !s.job.OutcomeReported {
		return []updater.Job{s.job}, nil
	}
	return nil, nil
}

func TestUpdateRequestRequiresRecentHumanAuthenticationBeforeHelperContact(t *testing.T) {
	now := time.Date(2026, 8, 24, 4, 0, 0, 0, time.UTC)
	db, artifact := updateServiceDB(t, now.Add(-updateRecentAuthentication-time.Second))
	control := &updateControlStub{}
	control.beforeSubmit = func(req updater.Request) error {
		var count int
		if err := db.SQLiteRead().QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM audit_instance_events WHERE type = 'system.update_requested' AND correlation_id = ?`, req.ID,
		).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("intent audit count at helper boundary = %d, want 1", count)
		}
		return nil
	}
	updates := fixtureUpdates(db, control, now)

	_, err := updates.Request(t.Context(), Bearer(artifact), "1.1.0")
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("error = %v, want fresh-auth refusal", err)
	}
	if len(control.submitted) != 0 {
		t.Fatalf("helper received stale-auth request: %#v", control.submitted)
	}
}

func TestUpdateRequestRefusesAndAuditsWithoutHelperContact(t *testing.T) {
	now := time.Date(2026, 8, 24, 4, 0, 0, 0, time.UTC)
	db, artifact := updateServiceDB(t, now)
	control := &updateControlStub{}
	updates := fixtureUpdates(db, control, now)
	status, err := updates.GetStatus(t.Context(), Bearer(artifact))
	if err != nil || !status.Available || status.LatestVersion != "1.1.0" || status.ApplySupported || status.ApplyError != updater.RemoteApplyDisabledReason {
		t.Fatalf("release metadata or disabled capability: status=%+v err=%v", status, err)
	}
	for _, configured := range []updater.Control{nil, control} {
		updates.Control = configured
		_, err := updates.Request(t.Context(), Bearer(artifact), "1.1.0")
		if !errors.Is(err, updater.ErrRemoteApplyDisabled) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("apply error=%v, want disabled conflict", err)
		}
	}
	if len(control.submitted) != 0 {
		t.Fatalf("retired helper received requests: %+v", control.submitted)
	}
	for _, event := range []string{"system.update_requested", "system.update_outcome"} {
		var count int
		if err := db.SQLiteRead().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM audit_instance_events WHERE type = ?", event).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("%s count=%d, want 2 refused attempts", event, count)
		}
	}
}

func TestUpdateOutcomeReconcilesWithoutRequestingSession(t *testing.T) {
	now := time.Date(2026, 8, 24, 4, 0, 0, 0, time.UTC)
	db, _ := updateServiceDB(t, now)
	control := &updateControlStub{job: updater.Job{
		ID: "upd_0198aa00-0000-7000-8000-000000000001", Backend: updater.BackendFlux,
		Version: "1.1.0", RequestedBy: "usr_update", State: updater.StateSucceeded,
		Phase: updater.PhaseComplete,
	}}
	updates := fixtureUpdates(db, control, now)

	if err := updates.ReconcileOutcomes(t.Context()); err != nil {
		t.Fatal(err)
	}
	if control.acknowledged != 1 {
		t.Fatalf("outcome acknowledgements = %d, want 1", control.acknowledged)
	}
	var count int
	if err := db.SQLiteRead().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM audit_instance_events WHERE type = 'system.update_outcome' AND correlation_id = ?`, control.job.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("outcome audit count = %d, want 1", count)
	}
	if err := updates.ReconcileOutcomes(t.Context()); err != nil {
		t.Fatal(err)
	}
	if control.acknowledged != 1 {
		t.Fatalf("second reconciliation acknowledged outcome again: %d", control.acknowledged)
	}
}

func TestUpdateOutcomeRetryAfterAcknowledgementFailureIsIdempotent(t *testing.T) {
	now := time.Date(2026, 8, 24, 4, 0, 0, 0, time.UTC)
	db, _ := updateServiceDB(t, now)
	control := &updateControlStub{
		ackErr: errors.New("socket closed after audit commit"),
		job: updater.Job{
			ID: "upd_0198aa00-0000-7000-8000-000000000002", Backend: updater.BackendCompose,
			Version: "1.1.0", RequestedBy: "usr_update", State: updater.StateSucceeded,
			Phase: updater.PhaseComplete,
		},
	}
	updates := fixtureUpdates(db, control, now)
	if err := updates.ReconcileOutcomes(t.Context()); err == nil {
		t.Fatal("lost helper acknowledgement was not reported")
	}
	if err := updates.ReconcileOutcomes(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.SQLiteRead().QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM audit_instance_events WHERE type = 'system.update_outcome' AND correlation_id = ?`, control.job.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("outcome audit count after retry = %d, want 1", count)
	}
}

func fixtureUpdates(db *store.DB, control updater.Control, now time.Time) *Updates {
	return &Updates{
		DB: db, Version: "1.0.0", Channel: updatecheck.ChannelStable, Control: control, Now: func() time.Time { return now },
		Source: updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
			return []updatecheck.Release{{
				Version: "1.1.0", URL: "https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.1.0", PublishedAt: now,
			}}, nil
		}),
	}
}

func updateServiceDB(t *testing.T, authenticatedAt time.Time) (*store.DB, string) {
	t.Helper()
	cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "updates.db")}
	if err := migrate.Run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_update','human','2026-08-24T00:00:00Z')`,
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('gr_update','usr_update','instance-config',NULL,NULL,NULL,'2026-08-24T00:00:00Z')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactBrowserSession)
	if err != nil {
		t.Fatal(err)
	}
	err = storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return az.MintSession(ctx, authz.NewSession{
			ID: "ses_update", PrincipalID: "usr_update", Verifier: verifier, Artifact: "browser",
			SessionGeneration: 1, CredentialEpoch: 1, AuthMethod: "local-passkey", Factors: `["webauthn","mfa"]`,
			AuthenticatedAt: authenticatedAt, CreatedAt: authenticatedAt, IdleExpiresAt: authenticatedAt.Add(time.Hour),
			AbsoluteExpiresAt: authenticatedAt.Add(24 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "test",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return db, artifact
}
