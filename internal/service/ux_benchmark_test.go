package service

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
)

// resetWorkflow restores an exact, closed SQLite fixture before each write.
// Copying, reopening, loading keys and warming the read path are untimed. The
// measured operation still uses production transactions and durable commits.
// It cannot drift into a no-op or grow revisions/audit rows across samples.
func resetWorkflow(b *testing.B, f *workflowBenchmark) func() {
	b.Helper()
	root := serviceFixtureRoot(b, f.values.DB)
	b.Cleanup(func() { crypto.Zero(root) })
	cfg := store.Config{Engine: store.EngineSQLite, Path: f.path}
	var busy, logFrames, checkpointed int
	if err := f.values.DB.SQLiteWrite().QueryRowContext(b.Context(), "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil || busy != 0 {
		b.Fatalf("checkpoint fixture: busy=%d frames=%d checkpointed=%d err=%v", busy, logFrames, checkpointed, err)
	}
	if err := f.values.DB.Close(); err != nil {
		b.Fatal(err)
	}
	baseline, err := os.ReadFile(f.path)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = f.values.DB.Close() })
	return func() {
		b.Helper()
		if err := f.values.DB.Close(); err != nil {
			b.Fatal(err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.Remove(f.path + suffix); err != nil && !os.IsNotExist(err) {
				b.Fatal(err)
			}
		}
		if err := os.WriteFile(f.path, baseline, 0600); err != nil {
			b.Fatal(err)
		}
		db, err := store.Open(b.Context(), cfg, f.admission)
		if err != nil {
			b.Fatal(err)
		}
		kr, err := crypto.LoadKeyring(b.Context(), &keyring.Store{DB: db}, bytes.Clone(root))
		if err != nil {
			_ = db.Close()
			b.Fatal(err)
		}
		f.values.DB, f.revisions.DB = db, db
		f.values.Keyring, f.revisions.Keyring = kr, kr
		f.values.Auth.DB, f.values.Auth.Keyring = db, kr
		f.values.Auth.SelfConfig.DB, f.values.Auth.SelfConfig.Keyring = db, kr
		if _, err := kr.ForProject(b.Context(), string(f.scope.Org), string(f.scope.Project)); err != nil {
			b.Fatal(err)
		}
		if cells, err := f.values.List(b.Context(), f.actor, f.scope, false); err != nil || len(cells) != f.count {
			b.Fatalf("warm fixture: cells=%d err=%v", len(cells), err)
		}
	}
}

func BenchmarkPublishSingleDraftLargeProject(b *testing.B) {
	f := newWorkflowBenchmarkDrafts(b, 1000, false)
	projectScope := f.scope
	projectScope.Env = ""
	group, err := (&KeyGroups{DB: f.values.DB, Keyring: f.values.Keyring}).Create(b.Context(), f.actor, projectScope, "linked-settings", nil)
	if err != nil {
		b.Fatal(err)
	}
	keys := &Keys{DB: f.values.DB, Keyring: f.values.Keyring}
	for _, name := range []string{"SETTING_0001", "SETTING_0002"} {
		if _, err := keys.SetGroup(b.Context(), f.actor, projectScope, f.keyIDs[name], group.ID); err != nil {
			b.Fatal(err)
		}
	}
	// Five populated environments make whole-project work visible, even though
	// the user selects one draft in one environment.
	for i := 1; i < 5; i++ {
		scope := f.scope
		scope.Env = ""
		env, err := (&Environments{DB: f.values.DB, Keyring: f.values.Keyring}).Create(b.Context(), f.actor, scope, fmt.Sprintf("environment-%d", i), nil)
		if err != nil {
			b.Fatal(err)
		}
		scope.Env = domain.EnvID(env.ID)
		entries := make([]ImportEntry, f.count)
		for j := range entries {
			entries[j] = ImportEntry{Key: fmt.Sprintf("SETTING_%04d", j), Value: "published-value"}
		}
		if result, err := f.values.Import(b.Context(), f.actor, scope, ImportRequest{Entries: entries}); err != nil || len(result.Imported) != f.count {
			b.Fatalf("populate environment: %v", err)
		}
	}
	change, err := f.values.Set(b.Context(), f.actor, f.scope, "SETTING_0001", "changed-value", nil)
	if err != nil {
		b.Fatal(err)
	}
	before, err := f.revisions.Show(b.Context(), f.actor, f.scope, 0)
	if err != nil {
		b.Fatal(err)
	}
	reset := resetWorkflow(b, &f)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		reset()
		b.StartTimer()
		result, err := f.revisions.PublishPlanned(b.Context(), f.actor, f.scope, PublishRequest{VersionIDs: []string{change.VersionID}})
		if err != nil || len(result.Published) != 1 || len(result.Environments) != 1 || result.Environments[0].Revision != before.Revision+1 {
			b.Fatalf("publish: %+v err=%v", result, err)
		}
		b.StopTimer()
		cell, err := f.values.Get(b.Context(), f.actor, f.scope, "SETTING_0001", false)
		if err != nil || cell.Value != "changed-value" {
			b.Fatalf("published value: %+v err=%v", cell, err)
		}
		b.StartTimer()
	}
}

func BenchmarkImportMixedExisting(b *testing.B) {
	f := newWorkflowBenchmarkDrafts(b, 1000, false)
	rules, err := scanning.Load()
	if err != nil {
		b.Fatal(err)
	}
	f.values.Scan = rules
	entries := make([]ImportEntry, f.count)
	overwrite := make([]string, 0, 200)
	for i := range entries {
		name := fmt.Sprintf("SETTING_%04d", i)
		value := "replacement-value"
		// A synthetic, non-live AWS key candidate exercises warn-not-block scanning.
		if i == 1 {
			value = "AKIAIOSFODNN7EXAMPLE"
		}
		entries[i] = ImportEntry{Key: name, Value: value}
		if i < 200 {
			overwrite = append(overwrite, name)
		}
	}
	reset := resetWorkflow(b, &f)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		reset()
		b.StartTimer()
		result, err := f.values.Import(b.Context(), f.actor, f.scope, ImportRequest{Entries: entries, Overwrite: overwrite})
		if err != nil || len(result.Imported) != 200 || len(result.Skipped) != 800 || len(result.Findings) == 0 {
			b.Fatalf("import: imported=%d skipped=%d findings=%d err=%v", len(result.Imported), len(result.Skipped), len(result.Findings), err)
		}
		b.StopTimer()
		cell, err := f.values.Get(b.Context(), f.actor, f.scope, "SETTING_0006", false)
		if err != nil || cell.Value != "replacement-value" {
			b.Fatalf("imported value: %+v err=%v", cell, err)
		}
		b.StartTimer()
	}
}

func BenchmarkHistoryPageLongLived(b *testing.B) {
	f := newWorkflowBenchmarkDrafts(b, 16, false)
	// Real publishes retain lineage and snapshot payloads; no direct SQL seeding.
	for i := range 1024 {
		change, err := f.values.Set(b.Context(), f.actor, f.scope, "SETTING_0001", fmt.Sprintf("revision-%04d", i), nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := f.revisions.PublishPlanned(b.Context(), f.actor, f.scope, PublishRequest{VersionIDs: []string{change.VersionID}}); err != nil {
			b.Fatal(err)
		}
	}
	latest, err := f.revisions.Show(b.Context(), f.actor, f.scope, 0)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		page, err := f.revisions.History(b.Context(), f.actor, f.scope, HistoryFromNewest, 20)
		if err != nil || len(page) != 20 || page[0].Revision != latest.Revision || page[19].Revision != latest.Revision-19 {
			b.Fatalf("history page: count=%d err=%v", len(page), err)
		}
	}
}

func BenchmarkRevisionDiffSparseLargeSnapshot(b *testing.B) {
	f := newWorkflowBenchmarkDrafts(b, 1000, false)
	left, err := f.revisions.Show(b.Context(), f.actor, f.scope, 0)
	if err != nil {
		b.Fatal(err)
	}
	change, err := f.values.Set(b.Context(), f.actor, f.scope, "SETTING_0001", "changed-value", nil)
	if err != nil {
		b.Fatal(err)
	}
	result, err := f.revisions.PublishPlanned(b.Context(), f.actor, f.scope, PublishRequest{VersionIDs: []string{change.VersionID}})
	if err != nil || len(result.Environments) != 1 {
		b.Fatalf("fixture publish: %v", err)
	}
	right := result.Environments[0].Revision
	check := func(diff RevisionDiff, err error) {
		b.Helper()
		if err != nil || len(diff.Items) != 1000 {
			b.Fatalf("diff: count=%d err=%v", len(diff.Items), err)
		}
		changed := 0
		for _, row := range diff.Items {
			if row.Classification == "config" && row.Status == "changed" {
				changed++
				if row.Name != "SETTING_0001" || row.Before == nil || row.After == nil || *row.After != "changed-value" {
					b.Fatalf("unexpected config diff: %+v", row)
				}
			}
		}
		if changed != 1 {
			b.Fatalf("changed config keys=%d, want 1", changed)
		}
	}
	check(f.revisions.Diff(b.Context(), f.actor, f.scope, left.Revision, right, ""))
	b.ReportAllocs()
	for b.Loop() {
		check(f.revisions.Diff(b.Context(), f.actor, f.scope, left.Revision, right, ""))
	}
}
