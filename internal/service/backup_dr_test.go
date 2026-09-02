package service

import (
	"errors"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestPlanPruneKeepsNewestAndDeletesOldestFirst(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	day := func(d int) string { return now.AddDate(0, 0, -d).Format("20060102T150405Z") }
	names := []string{
		"hikyo-sqlite-" + day(0) + ".age",       // newest, kept
		"hikyo-sqlite-" + day(1) + ".age",       // kept by count
		"hikyo-sqlite-" + day(2) + ".age",       // kept by count
		"hikyo-sqlite-" + day(5) + ".age",       // outside count, young: kept by age
		"hikyo-sqlite-" + day(40) + ".age",      // old: deleted
		"hikyo-sqlite-" + day(41) + "-2.age",    // old, same-second suffix: deleted
		"hikyo-sqlite-" + day(400) + ".age",     // oldest: deleted FIRST
		"hikyo-postgres-" + day(400) + ".age",   // other engine: never touched
		".hikyo-export-123.partial",             // staging: never touched
		"hikyo-sqlite-" + day(400) + ".age.bak", // foreign: never touched
		"notes.txt",
	}
	got, kept := planPrune(names, store.EngineSQLite, now, PrunePolicy{RetainCount: 3, RetainDays: 30}, "")
	want := []string{
		"hikyo-sqlite-" + day(400) + ".age",
		"hikyo-sqlite-" + day(41) + "-2.age",
		"hikyo-sqlite-" + day(40) + ".age",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delete = %v, want %v", got, want)
	}
	if kept != 4 {
		t.Fatalf("kept = %d, want 4", kept)
	}
}

func TestPlanPruneProtectsPersistedNewestAfterClockRollback(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	// A wall-clock rollback: the newest SUCCESSFUL export ("recovered") has an
	// older filename timestamp than an archive taken before the rollback.
	newest := "hikyo-sqlite-" + now.AddDate(0, 0, -1).Format("20060102T150405Z") + ".age"
	older := "hikyo-sqlite-" + now.AddDate(0, 0, -200).Format("20060102T150405Z") + ".age"
	recovered := "hikyo-sqlite-" + now.AddDate(0, 0, -400).Format("20060102T150405Z") + ".age"
	names := []string{newest, older, recovered}
	// RetainCount 1 keeps `newest` by name; both `older` and the even older
	// `recovered` are aged deletion candidates. Without protection `recovered`
	// (the persisted last success after a clock rollback) would be deleted;
	// protecting its name keeps it and leaves only `older` to go.
	got, _ := planPrune(names, store.EngineSQLite, now, PrunePolicy{RetainCount: 1, RetainDays: 180}, recovered)
	for _, name := range got {
		if name == recovered {
			t.Fatalf("prune deleted the persisted newest-successful archive %s", recovered)
		}
	}
	if len(got) != 1 || got[0] != older {
		t.Fatalf("delete = %v, want [%s]", got, older)
	}
}

func TestPlanPruneNeverDeletesTheNewestWhateverItsAge(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	ancient := "hikyo-postgres-" + now.AddDate(-2, 0, 0).Format("20060102T150405Z") + ".age"
	got, kept := planPrune([]string{ancient}, store.EnginePostgres, now, PrunePolicy{RetainCount: 1, RetainDays: 1}, "")
	if len(got) != 0 || kept != 1 {
		t.Fatalf("the only archive was pruned: delete=%v kept=%d", got, kept)
	}
	// A retain count of zero cannot be configured, and even if it reached
	// here the newest survives.
	got, _ = planPrune([]string{ancient}, store.EnginePostgres, now, PrunePolicy{RetainCount: 0, RetainDays: 1}, "")
	if len(got) != 0 {
		t.Fatalf("retain count 0 deleted the newest archive: %v", got)
	}
}

func TestPlanPruneOrdersByNameNotListing(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	stamp := func(d int) string { return now.AddDate(0, 0, -d).Format("20060102T150405Z") }
	// Listed youngest-last on purpose: the plan must sort by the timestamp
	// in the name, not trust directory order or mtime.
	names := []string{
		"hikyo-sqlite-" + stamp(200) + ".age",
		"hikyo-sqlite-" + stamp(1) + ".age",
		"hikyo-sqlite-" + stamp(190) + ".age",
	}
	got, _ := planPrune(names, store.EngineSQLite, now, PrunePolicy{RetainCount: 1, RetainDays: 180}, "")
	want := []string{"hikyo-sqlite-" + stamp(200) + ".age", "hikyo-sqlite-" + stamp(190) + ".age"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delete = %v, want %v", got, want)
	}
}

// pruneDB is a migrated sqlite instance for exercising the real Prune loop.
func pruneDB(t *testing.T) *store.DB {
	t.Helper()
	cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "prune.db")}
	if err := migrate.Run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestPruneLoopStopsAfterFirstFailedUnlink drives the real deletion loop with a
// remove seam that fails on its first call, and proves the loop stops there:
// exactly one unlink is attempted (the oldest), the later candidates and the
// newest are never touched, and no prune success is recorded. A regression that
// kept going after an error would attempt more than one unlink and fail here.
func TestPruneLoopStopsAfterFirstFailedUnlink(t *testing.T) {
	db := pruneDB(t)
	dir := t.TempDir()
	now := time.Now().UTC()
	name := func(days int) string {
		return "hikyo-sqlite-" + now.AddDate(0, 0, -days).Format("20060102T150405Z") + ".age"
	}
	newest, middle, oldest := name(1), name(300), name(400)
	for _, n := range []string{newest, middle, oldest} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("a"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var attempts []string
	svc := &Backup{DB: db, removeFile: func(path string) error {
		attempts = append(attempts, filepath.Base(path))
		return errors.New("unlink refused")
	}}
	_, err := svc.Prune(t.Context(), dir, PrunePolicy{RetainCount: 1, RetainDays: 180})
	if err == nil {
		t.Fatal("a prune whose unlink failed returned no error")
	}
	// Oldest-first, and stopped after the first failure.
	if len(attempts) != 1 || attempts[0] != oldest {
		t.Fatalf("unlink attempts = %v, want exactly [%s]", attempts, oldest)
	}
	// Nothing was actually removed (the seam refused), and the newest was never
	// even a candidate.
	for _, n := range []string{newest, middle, oldest} {
		if _, statErr := os.Stat(filepath.Join(dir, n)); statErr != nil {
			t.Fatalf("%s missing after a prune that only attempted a refused unlink", n)
		}
	}
	st, err := svc.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !st.LastPruneAt.IsZero() {
		t.Fatal("a failed prune recorded a success timestamp")
	}
}

func TestBackupHealthVerdicts(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	scheduled := BackupPolicy{Scheduled: true, RPO: 26 * time.Hour}
	fresh := store.BackupState{LastSuccessAt: now.Add(-2 * time.Hour), LastDrillAt: now.Add(-24 * time.Hour), LastDrillOK: true}
	h := backupHealth(now, scheduled, fresh)
	if h.RPOExceeded || h.DrillStale || h.ArtifactAge != 2*time.Hour {
		t.Fatalf("fresh state = %+v", h)
	}
	late := backupHealth(now, scheduled, store.BackupState{LastSuccessAt: now.Add(-27 * time.Hour)})
	if !late.RPOExceeded {
		t.Fatal("an artifact older than the RPO did not exceed it")
	}
	never := backupHealth(now, scheduled, store.BackupState{})
	if !never.RPOExceeded || !never.DrillStale || never.ArtifactAge != 0 {
		t.Fatalf("never-exported state = %+v", never)
	}
	// No policy: nothing is scheduled, so no RPO can be exceeded; the drill
	// cadence is still owed.
	unscheduled := backupHealth(now, BackupPolicy{}, store.BackupState{})
	if unscheduled.RPOExceeded || unscheduled.Scheduled || !unscheduled.DrillStale {
		t.Fatalf("unscheduled state = %+v", unscheduled)
	}
	// A failed drill is not a recent drill.
	failed := backupHealth(now, scheduled, store.BackupState{LastSuccessAt: now, LastDrillAt: now, LastDrillOK: false})
	if !failed.DrillStale {
		t.Fatal("a failed drill counted as a fresh one")
	}
	old := backupHealth(now, scheduled, store.BackupState{LastSuccessAt: now, LastDrillAt: now.Add(-91 * 24 * time.Hour), LastDrillOK: true})
	if !old.DrillStale {
		t.Fatal("a 91-day-old drill was not stale")
	}
}
