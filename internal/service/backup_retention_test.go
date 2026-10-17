package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func recordPruneArtifact(t *testing.T, svc *Backup, dir string, at time.Time) ExportResult {
	t.Helper()
	oldNow := svc.Now
	svc.Now = func() time.Time { return at }
	defer func() { svc.Now = oldNow }()
	_, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	svc.Options = backup.Options{Recipients: []string{recipient}}
	result, err := svc.Export(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordExport(t.Context(), TriggerManual, result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBackupRetentionCeilingAndRecovery(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			db, err := openServiceFixture(t, preparationDatabase(t, engine))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			svc := &Backup{DB: db, Now: func() time.Time { return now }}
			dir := t.TempDir()
			first := recordPruneArtifact(t, svc, dir, now)
			now = now.Add(24 * time.Hour)
			protected := recordPruneArtifact(t, svc, dir, now)
			now = now.Add(181 * 24 * time.Hour)
			result, err := svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 7, RetainDays: 180})
			if err == nil || result.Kept != 1 || result.Expired != 1 || len(result.Deleted) != 1 || result.Deleted[0] != filepath.Base(first.Path) {
				t.Fatalf("expired under-count policy: %+v %v", result, err)
			}
			if _, err := os.Stat(protected.Path); err != nil {
				t.Fatal("last recovery copy removed", err)
			}
			st, err := svc.State(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !st.LastPruneAt.IsZero() || st.PruneFailureReason != "protected_expired" || st.RetainedExpiredArchives != 1 || !st.LastPruneAttemptAt.Equal(now) {
				t.Fatalf("breach not durably reported: %+v", st)
			}
			if h := backupHealth(now, BackupPolicy{Scheduled: true}, st); !h.RetentionBreach {
				t.Fatal("health hid expired recovery copy")
			}
			// With no new success, another run must keep the breach and last copy.
			if _, err := svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 7, RetainDays: 180}); err == nil {
				t.Fatal("repeat exception became success")
			}
			fresh := recordPruneArtifact(t, svc, dir, now)
			result, err = svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 7, RetainDays: 180})
			if err != nil || result.Kept != 1 || result.Expired != 0 {
				t.Fatalf("fresh replacement did not permit expiry: %+v %v", result, err)
			}
			if _, err := os.Stat(protected.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("expired previous copy survived replacement")
			}
			if _, err := os.Stat(fresh.Path); err != nil {
				t.Fatal(err)
			}
			st, err = svc.State(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !st.LastPruneAt.Equal(now) || st.PruneFailureReason != "" || st.RetainedExpiredArchives != 0 || backupHealth(now, BackupPolicy{Scheduled: true}, st).RetentionBreach {
				t.Fatalf("successful recovery did not clear breach: %+v", st)
			}
		})
	}
}

func TestBackupRetentionUncertaintyPreservesCandidates(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, scenario := range []string{"never_recorded", "missing_recorded_file", "same_size_corruption", "clock_rollback", "legacy_digest", "future_attempt"} {
			t.Run(string(engine)+"/"+scenario, func(t *testing.T) {
				db, err := openServiceFixture(t, preparationDatabase(t, engine))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
				svc := &Backup{DB: db, Now: func() time.Time { return now }}
				dir := t.TempDir()
				candidate := filepath.Join(dir, "hikyo-"+string(engine)+"-20200101T000000Z.age")
				if err := os.WriteFile(candidate, []byte("candidate"), 0600); err != nil {
					t.Fatal(err)
				}
				if scenario != "never_recorded" {
					result := recordPruneArtifact(t, svc, dir, now)
					switch scenario {
					case "missing_recorded_file":
						if err := os.Remove(result.Path); err != nil {
							t.Fatal(err)
						}
					case "same_size_corruption":
						raw, err := os.ReadFile(result.Path)
						if err != nil {
							t.Fatal(err)
						}
						raw[len(raw)-1] ^= 1
						if err := os.WriteFile(result.Path, raw, 0600); err != nil {
							t.Fatal(err)
						}
					case "clock_rollback":
						now = now.Add(-24 * time.Hour)
					case "legacy_digest", "future_attempt":
						err := tx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
							proof, err := authz.SystemAuthority(authz.SiteScheduler, az.Token())
							if err != nil {
								return err
							}
							if scenario == "future_attempt" {
								return r.BackupState().SetPruneResult(ctx, proof, now.Add(time.Hour), "previous-future-attempt", "in_progress", 0)
							}
							return r.BackupState().SetExportSuccess(ctx, proof, now, filepath.Base(result.Path), result.Bytes, "")
						})
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				result, err := svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 1, RetainDays: 1})
				if err == nil || len(result.Deleted) != 0 {
					t.Fatalf("uncertainty allowed destructive prune: %+v %v", result, err)
				}
				if _, err := os.Stat(candidate); err != nil {
					t.Fatal("candidate removed", err)
				}
				st, err := svc.State(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if st.PruneFailureReason != "recovery_uncertain" || !st.LastPruneAt.IsZero() {
					t.Fatalf("uncertainty not recorded: %+v", st)
				}
				if scenario == "future_attempt" {
					if !st.LastPruneAttemptAt.After(now) {
						t.Fatal("clock evidence overwritten")
					}
					if repeated, err := svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 1, RetainDays: 1}); err == nil || len(repeated.Deleted) != 0 {
						t.Fatalf("second attempt forgot clock rollback: %+v %v", repeated, err)
					}
				}
			})
		}
	}
}

func TestPlanPruneCeilingBoundaryAndEarlierThreshold(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	name := func(age time.Duration) string {
		return "hikyo-sqlite-" + now.Add(-age).Format("20060102T150405Z") + ".age"
	}
	fresh, soft, boundary, expired := name(0), name(40*24*time.Hour), name(BackupRetentionCeiling), name(BackupRetentionCeiling+time.Second)
	deleted, kept := planPrune([]string{fresh, soft, boundary, expired}, store.EngineSQLite, now, PrunePolicy{RetainCount: 7, RetainDays: 30}, fresh)
	if len(deleted) != 1 || deleted[0] != expired || kept != 3 {
		t.Fatalf("ceiling/count boundary: %v kept=%d", deleted, kept)
	}
	deleted, kept = planPrune([]string{fresh, boundary}, store.EngineSQLite, now, PrunePolicy{RetainCount: 1, RetainDays: 30}, fresh)
	if len(deleted) != 1 || deleted[0] != boundary || kept != 1 {
		t.Fatalf("exact hard ceiling hid earlier soft cutoff: %v kept=%d", deleted, kept)
	}
	deleted, kept = planPrune([]string{fresh, soft}, store.EngineSQLite, now, PrunePolicy{RetainCount: 1, RetainDays: 30}, fresh)
	if len(deleted) != 1 || deleted[0] != soft || kept != 1 {
		t.Fatalf("earlier threshold lost: %v kept=%d", deleted, kept)
	}
}

func TestRecordBackupRefusesChangedCiphertext(t *testing.T) {
	db := pruneDB(t)
	svc := &Backup{DB: db}
	dir := t.TempDir()
	previous := recordPruneArtifact(t, svc, dir, time.Now().UTC().Add(-time.Hour))
	next, err := svc.Export(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(next.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(next.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordExport(t.Context(), TriggerManual, next); err == nil {
		t.Fatal("same-size replacement certified as durable success")
	}
	st, err := svc.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.LastArtifactName != filepath.Base(previous.Path) {
		t.Fatal("failed record displaced last recovery copy")
	}
}

func TestBackupRetentionFailuresPersist(t *testing.T) {
	for _, kind := range []string{"unlink_failed", "directory_sync_failed", "directory_unavailable"} {
		t.Run(kind, func(t *testing.T) {
			db := pruneDB(t)
			now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
			svc := &Backup{DB: db, Now: func() time.Time { return now }}
			dir := t.TempDir()
			recordPruneArtifact(t, svc, dir, now)
			policy := PrunePolicy{RetainCount: 1, RetainDays: 180}
			if _, err := svc.Prune(t.Context(), dir, policy); err != nil {
				t.Fatal(err)
			}
			success := now
			now = now.Add(time.Hour)
			old := filepath.Join(dir, "hikyo-sqlite-20200101T000000Z.age")
			if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unlink_failed":
				svc.removeFile = func(string) error { return errors.New("injected unlink failure") }
			case "directory_sync_failed":
				svc.syncDirectory = func(string) error { return errors.New("injected directory sync failure") }
			case "directory_unavailable":
				dir = filepath.Join(dir, "missing")
			}
			if _, err := svc.Prune(t.Context(), dir, policy); err == nil {
				t.Fatal("failed prune reported success")
			}
			st, err := svc.State(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !st.LastPruneAt.Equal(success) || st.PruneFailureReason != kind || !backupHealth(now, BackupPolicy{Scheduled: true}, st).RetentionBreach {
				t.Fatalf("failure concealed: %+v", st)
			}
			if kind == "directory_sync_failed" {
				// The first unlink is already visible. A no-op retry must still fsync,
				// and must not turn that unconfirmed deletion into a success.
				if _, err := svc.Prune(t.Context(), dir, policy); err == nil {
					t.Fatal("no-op retry hid failed durability")
				}
				st, err = svc.State(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if !st.LastPruneAt.Equal(success) || st.PruneFailureReason != kind {
					t.Fatalf("retry cleared durability failure: %+v", st)
				}
				svc.syncDirectory = nil
				if _, err := svc.Prune(t.Context(), dir, policy); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestBackupRetentionHealthCrossesCeilingWithoutPrune(t *testing.T) {
	published := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	boundary := published.Add(BackupRetentionCeiling)
	st := store.BackupState{LastSuccessAt: published, LastArtifactName: "hikyo-sqlite-20260101T000000Z.age", LastArtifactBytes: 1024, LastArtifactSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LastPruneAttemptAt: boundary, LastPruneAt: boundary}
	policy := BackupPolicy{Scheduled: true}
	if backupHealth(boundary, policy, st).RetentionBreach {
		t.Fatal("exact ceiling prematurely breached")
	}
	if h := backupHealth(boundary.Add(time.Second), policy, st); !h.RetentionBreach || h.RetentionReason != "protected_expired" {
		t.Fatalf("aging became silently compliant: %+v", h)
	}
	st.LastArtifactName = "hikyo-sqlite-20260630T000000Z.age"
	st.LastSuccessAt = boundary
	if h := backupHealth(boundary.Add(25*time.Hour), policy, st); !h.RetentionBreach || h.RetentionReason != "stale" {
		t.Fatalf("stale observation became compliance: %+v", h)
	}
}

func TestBackupPruneCancellationHoldsDirectoryUntilUnlinkReturns(t *testing.T) {
	db := pruneDB(t)
	dir := t.TempDir()
	svc := &Backup{DB: db}
	recordPruneArtifact(t, svc, dir, time.Now().UTC())
	pending, err := svc.Export(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "hikyo-sqlite-20200101T000000Z.age")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	svc.removeFile = func(path string) error { close(entered); <-release; return os.Remove(path) }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pruned := make(chan error, 1)
	go func() { _, err := svc.Prune(ctx, dir, PrunePolicy{RetainCount: 7, RetainDays: 180}); pruned <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("prune did not enter unlink")
	}
	cancel()
	// Transaction cancellation must not release filesystem ownership while the
	// unlink is still executing. A different service instance cannot record.
	other := &Backup{DB: db}
	recordCtx, recordCancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	err = other.RecordExport(recordCtx, TriggerManual, pending)
	recordCancel()
	close(release)
	if err == nil {
		t.Fatal("record advanced while canceled prune still owned unlink")
	}
	if err := <-pruned; err == nil {
		t.Fatal("canceled prune reported success")
	}
	st, err := svc.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.PruneFailureReason != "in_progress" || !st.LastPruneAt.IsZero() {
		t.Fatalf("interrupted attempt lost durable marker: %+v", st)
	}
	if err := other.RecordExport(t.Context(), TriggerManual, pending); err != nil {
		t.Fatal("directory ownership did not release", err)
	}
}

type cancelingBackupReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelingBackupReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads > 1 {
		return 0, errors.New("read continued after cancellation")
	}
	p[0] = 1
	r.cancel()
	return 1, nil
}
func TestBackupCiphertextVerificationStopsBetweenReads(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := &cancelingBackupReader{cancel: cancel}
	err := verifyBackupCiphertext(ctx, source, 1024, "")
	if !errors.Is(err, context.Canceled) || source.reads != 1 {
		t.Fatalf("canceled verification continued: reads=%d error=%v", source.reads, err)
	}
}

func TestBackupRetentionSchedulingDoesNotHideObservedState(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	fresh := store.BackupState{
		LastSuccessAt: now.Add(-time.Hour), LastArtifactName: "hikyo-sqlite-20261015T230000Z.age", LastArtifactBytes: 1024,
		LastArtifactSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LastPruneAt:        now.Add(-time.Minute), LastPruneAttemptAt: now.Add(-time.Minute), LastPruneAttemptID: "verified-attempt",
	}
	expired := fresh
	expired.LastArtifactName = "hikyo-sqlite-20260101T000000Z.age"
	failed := fresh
	failed.PruneFailureReason = "unlink_failed"
	incomplete := fresh
	incomplete.PruneFailureReason = "in_progress"
	observed := fresh
	observed.RetainedExpiredArchives = 2
	uncertain := fresh
	uncertain.LastPruneAttemptAt = now.Add(time.Hour)
	legacy := fresh
	legacy.LastArtifactSHA256 = ""
	partial := fresh
	partial.LastSuccessAt = time.Time{}
	cases := []struct {
		name   string
		state  store.BackupState
		reason string
		breach bool
	}{
		{"no managed state", store.BackupState{}, "not_scheduled", false},
		{"fresh manually verified", fresh, "not_scheduled", false},
		{"expired after disabling", expired, "protected_expired", true},
		{"failed after disabling", failed, "unlink_failed", true},
		{"incomplete after disabling", incomplete, "in_progress", true},
		{"observed expired after disabling", observed, "protected_expired", true},
		{"clock uncertain after disabling", uncertain, "clock_uncertain", true},
		{"legacy identity after disabling", legacy, "recovery_uncertain", true},
		{"partial identity after disabling", partial, "recovery_uncertain", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := backupHealth(now, BackupPolicy{}, tc.state)
			if h.RetentionReason != tc.reason || h.RetentionBreach != tc.breach || !h.LastPruneAt.Equal(tc.state.LastPruneAt) {
				t.Fatalf("schedule erased or invented retention evidence: %+v", h)
			}
		})
	}
}
