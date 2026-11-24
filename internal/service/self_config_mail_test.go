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

// stageLegacyPaddedDraft inserts a pending draft the way a pre-#1492 write path
// left it: padded and untrimmed. Restore and Set now trim, so only a direct
// insert can reproduce one.
func stageLegacyPaddedDraft(t *testing.T, s *SelfConfig, local Actor, scope domain.Scope, name, padded string, secret bool) string {
	t.Helper()
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
		sealed, err := sealer.SealField(pendingAAD(string(scope.Org), string(scope.Project), string(scope.Env), key.ID, versionID), []byte(padded))
		if err != nil {
			return err
		}
		if err := fenceProject(ctx, r, p, sealer, scope); err != nil {
			return err
		}
		return r.Pending().Stage(ctx, p, store.NewPendingChange{
			ID: versionID, KeyID: key.ID, OwnerID: string(caller.Principal), Operation: store.PendingSet, Ciphertext: sealed,
			StagedFromRevision: revision, StagedFromEntry: entry.ID, CreatedAt: store.CanonTime(time.Now()),
			Source: store.PendingSourceValues, Secret: secret, MaterialSecret: secret,
		})
	}); err != nil {
		t.Fatal(err)
	}
	return versionID
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
	versionID := stageLegacyPaddedDraft(t, s, local, scope, name, " \t padded secret \r\n", true)
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{versionID}}); err != nil {
		t.Fatal(err)
	}
	live, snapshot := liveAndSnapshot(t, s, local, scope, name)
	if live != "padded secret" || snapshot != "padded secret" {
		t.Fatalf("live %q and snapshot %q must both be trimmed", live, snapshot)
	}
}

// liveAndSnapshot opens a key's live cell and its value in the latest snapshot.
func liveAndSnapshot(t *testing.T, s *SelfConfig, local Actor, scope domain.Scope, name string) (live, snapshot string) {
	t.Helper()
	keyTarget := authz.KeyByName(name)
	sealer, err := sealerForKey(t.Context(), s.DB, s.Keyring, local, authz.OpValueStage, scope, &keyTarget)
	if err != nil {
		t.Fatal(err)
	}
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
	return live, snapshot
}

// An older revision written before the trim was uniform can hold a padded value.
// Restore stages it verbatim; the impact preview and publish must both show and
// store the trimmed value.
func TestRestoreOfAPaddedRevisionIsTrimmed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rotated string
		secret        bool
	}{
		{"HIKYO_MAIL_PASSWORD", "rotated secret", true},
		{"HIKYO_MAIL_USER", "other-operator", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink := mailtest.New(t, "implicit")
			s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "restore-padded.db")}, mailTestSeed(sink))
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, _ := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
			values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
			revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
			staged, err := values.Set(t.Context(), local, scope, tc.name, tc.rotated, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
				t.Fatal(err)
			}
			// Rewrite revision 1's entry as an older, padded write would have left it.
			keyTarget := authz.KeyByName(tc.name)
			sealer, err := sealerForKey(t.Context(), s.DB, s.Keyring, local, authz.OpValueStage, scope, &keyTarget)
			if err != nil {
				t.Fatal(err)
			}
			var org, project, env, keyID, snapshotID, entryID string
			if err := s.DB.SQLiteWrite().QueryRowContext(t.Context(), `SELECT e.org_id, e.project_id, e.environment_id, e.key_id, e.snapshot_id, e.id
				FROM snapshot_entries e JOIN snapshots s ON s.id = e.snapshot_id WHERE s.revision = 1 AND e.key_name = ?`, tc.name).
				Scan(&org, &project, &env, &keyID, &snapshotID, &entryID); err != nil {
				t.Fatal(err)
			}
			padded, err := sealer.SealField(snapshotAAD(org, project, env, keyID, snapshotID, entryID), []byte(" \t padded \r\n"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.SQLiteWrite().ExecContext(t.Context(), `UPDATE snapshot_entries SET ciphertext = ? WHERE id = ?`, padded, entryID); err != nil {
				t.Fatal(err)
			}
			restored, err := revisions.Restore(t.Context(), local, scope, 1, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			if len(restored.Changes) != 1 {
				t.Fatalf("restore staged %d changes, want 1", len(restored.Changes))
			}
			if got := pendingPlain(t, s, local, scope, tc.name); got != "padded" {
				t.Fatalf("staged draft = %q, want the trimmed value", got)
			}
			if !tc.secret {
				after := restored.Preview.Environments[0].Changes[0].After
				if after == nil || *after != "padded" {
					t.Fatalf("impact preview After = %v, want the trimmed value", after)
				}
			}
			if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{restored.Changes[0].VersionID}, PreviewToken: restored.Preview.Token}); err != nil {
				t.Fatal(err)
			}
			if live, snapshot := liveAndSnapshot(t, s, local, scope, tc.name); live != "padded" || snapshot != "padded" {
				t.Fatalf("live %q and snapshot %q must both be trimmed", live, snapshot)
			}
		})
	}
}

// pendingPlain opens the caller's staged draft for one key exactly as stored.
func pendingPlain(t *testing.T, s *SelfConfig, local Actor, scope domain.Scope, name string) string {
	t.Helper()
	keyTarget := authz.KeyByName(name)
	sealer, err := sealerForKey(t.Context(), s.DB, s.Keyring, local, authz.OpValuePendingList, scope, &keyTarget)
	if err != nil {
		t.Fatal(err)
	}
	var out string
	if err := tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, err := local.resolve(ctx, az, time.Now().UTC())
		if err != nil {
			return err
		}
		p, err := az.AuthorizeKey(ctx, caller, authz.OpValuePendingList, scope, keyTarget)
		if err != nil {
			return err
		}
		key, err := findKey(ctx, r.Catalogue(), p, name)
		if err != nil {
			return err
		}
		changes, err := r.Pending().ListForOwnerInEnvironment(ctx, p, string(caller.Principal))
		if err != nil {
			return err
		}
		for _, c := range changes {
			if c.KeyID != key.ID {
				continue
			}
			plain, err := sealer.OpenField(pendingAAD(c.OrgID, c.ProjectID, c.EnvironmentID, c.KeyID, c.ID), c.Ciphertext)
			if err != nil {
				return err
			}
			out = string(plain)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// Restoring a padded older revision whose trimmed value is already live is not
// a change, so it must stage nothing.
func TestRestoreOfAPaddedRevisionMatchingLiveStagesNothing(t *testing.T) {
	t.Parallel()
	sink := mailtest.New(t, "implicit")
	s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "restore-same.db")}, mailTestSeed(sink))
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, _ := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	values := &Values{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	const name = "HIKYO_MAIL_USER"
	keyTarget := authz.KeyByName(name)
	sealer, err := sealerForKey(t.Context(), s.DB, s.Keyring, local, authz.OpValueStage, scope, &keyTarget)
	if err != nil {
		t.Fatal(err)
	}
	var org, project, env, keyID, snapshotID, entryID string
	if err := s.DB.SQLiteWrite().QueryRowContext(t.Context(), `SELECT e.org_id, e.project_id, e.environment_id, e.key_id, e.snapshot_id, e.id
		FROM snapshot_entries e JOIN snapshots s ON s.id = e.snapshot_id WHERE s.revision = 1 AND e.key_name = ?`, name).
		Scan(&org, &project, &env, &keyID, &snapshotID, &entryID); err != nil {
		t.Fatal(err)
	}
	padded, err := sealer.SealField(snapshotAAD(org, project, env, keyID, snapshotID, entryID), []byte(" \t padded \r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.SQLiteWrite().ExecContext(t.Context(), `UPDATE snapshot_entries SET ciphertext = ? WHERE id = ?`, padded, entryID); err != nil {
		t.Fatal(err)
	}
	staged, err := values.Set(t.Context(), local, scope, name, "padded", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatal(err)
	}
	restored, err := revisions.Restore(t.Context(), local, scope, 1, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Changes) != 0 {
		t.Fatalf("restore staged %d changes, want none", len(restored.Changes))
	}
}

// A draft staged before the trim was uniform can hold padding that a rule
// rejects. The pending list must show the value publish would store.
func TestPendingDraftViewEvaluatesTheTrimmedValue(t *testing.T) {
	t.Parallel()
	sink := mailtest.New(t, "implicit")
	s, local := selfConfigFixtureConfig(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "pending-padded.db")}, mailTestSeed(sink))
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, _ := selfConfigSession(t, s, local)
	status, err := s.Status(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(status.Binding.OrgID), Project: domain.ProjectID(status.Binding.ProjectID), Env: domain.EnvID(status.Binding.EnvironmentID)}
	const name = "HIKYO_MAIL_TLS"
	stageLegacyPaddedDraft(t, s, local, scope, name, " implicit ", false)
	revisions := &Revisions{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth}
	drafts, err := revisions.PendingDrafts(t.Context(), local, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drafts {
		if d.Name == name {
			if !d.Valid || d.Value != "implicit" {
				t.Fatalf("draft Valid=%v Value=%q, want valid and trimmed", d.Valid, d.Value)
			}
			return
		}
	}
	t.Fatalf("no pending draft for %s in %d drafts", name, len(drafts))
}
