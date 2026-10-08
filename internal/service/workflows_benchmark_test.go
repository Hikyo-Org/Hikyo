package service

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// These benchmarks exercise authorized service operations against disk-backed
// SQLite and a real encrypted snapshot. They exclude boot, migrations, fixture
// writes and external adapters. Reads keep dataset size fixed across iterations.
// Nil budgets deliberately exclude rate limiting from service throughput.
type workflowBenchmark struct {
	path      string
	admission upgrade.Admission
	actor     Actor
	scope     domain.Scope
	values    *Values
	revisions *Revisions
	count     int
	pending   map[string]string
	keyIDs    map[string]string
}

func newWorkflowBenchmark(b *testing.B, count int) workflowBenchmark {
	b.Helper()
	return newWorkflowBenchmarkDrafts(b, count, true)
}

func newWorkflowBenchmarkDrafts(b *testing.B, count int, drafts bool) workflowBenchmark {
	b.Helper()
	path := filepath.Join(b.TempDir(), "benchmark.db")
	db, admission, err := openServiceFixtureAdmission(b, store.Config{Engine: store.EngineSQLite, Path: path})
	if err != nil {
		b.Fatal(err)
	}
	self, actor := selfConfigFixtureDB(b, db, map[string]string{"HIKYO_UPDATE_CHANNEL": "nightly"})
	ctx := b.Context()
	org, err := (&Orgs{DB: self.DB}).Create(ctx, actor, "benchmark", true, []byte(`{}`))
	if err != nil {
		b.Fatal(err)
	}
	project, err := (&Projects{DB: self.DB}).Create(ctx, actor, domain.OrgID(org.ID), "application")
	if err != nil {
		b.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID)}
	keys := &Keys{DB: self.DB, Keyring: self.Keyring}
	entries := make([]ImportEntry, count)
	keyIDs := make(map[string]string, count)
	// Declare before creating the environment to avoid measuring fixture fan-out.
	for i := range count {
		classification := "config"
		if i%5 == 0 {
			classification = "secret"
		}
		name := fmt.Sprintf("SETTING_%04d", i)
		key, err := keys.Create(ctx, actor, scope, KeySpec{
			Name: name, Classification: classification,
			Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
			Presence:    schema.DefaultPresenceRules(),
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
		keyIDs[name] = key.ID
		entries[i] = ImportEntry{Key: name, Value: "https://service.example.test/path?setting=" + name}
	}
	env, err := (&Environments{DB: self.DB, Keyring: self.Keyring}).Create(ctx, actor, scope, "development", nil)
	if err != nil {
		b.Fatal(err)
	}
	scope.Env = domain.EnvID(env.ID)
	values := &Values{DB: self.DB, Keyring: self.Keyring, Auth: self.Auth}
	imported, err := values.Import(ctx, actor, scope, ImportRequest{Entries: entries})
	if err != nil || len(imported.Imported) != count || len(imported.Skipped) != 0 {
		b.Fatalf("fixture import: imported=%d skipped=%d err=%v", len(imported.Imported), len(imported.Skipped), err)
	}
	revisions := &Revisions{DB: self.DB, Keyring: self.Keyring, Auth: self.Auth}
	// Live drafts on 10% of keys distinguish signals from an empty-draft query.
	pending := make(map[string]string, count/10)
	for i := 0; drafts && i < count; i += 10 {
		change, err := values.Set(ctx, actor, scope, entries[i].Key, "pending-value", nil)
		if err != nil || change.VersionID == "" {
			b.Fatalf("fixture draft: %v", err)
		}
		pending[entries[i].Key] = change.VersionID
	}
	return workflowBenchmark{path: path, admission: admission, actor: actor, scope: scope, values: values, revisions: revisions, count: count, pending: pending, keyIDs: keyIDs}
}

func benchmarkWorkflowSizes(b *testing.B, run func(*testing.B, workflowBenchmark)) {
	b.Helper()
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("keys=%d", count), func(b *testing.B) {
			fixture := newWorkflowBenchmark(b, count)
			b.ReportAllocs()
			run(b, fixture)
		})
	}
}

// Validate fixture contents outside the timed loop; the loop still checks
// errors and cardinality so a shortcut or refusal cannot produce a fast result.
func checkBenchmarkValues(b *testing.B, name, classification, value string, revealed bool, index int) {
	b.Helper()
	wantName := fmt.Sprintf("SETTING_%04d", index)
	if name != wantName {
		b.Fatalf("name = %q, want %q", name, wantName)
	}
	if index%5 == 0 {
		if classification != "secret" || revealed || value != "" {
			b.Fatalf("secret %s was not masked", name)
		}
	} else if classification != "config" || !revealed || value != "https://service.example.test/path?setting="+name {
		b.Fatalf("config %s did not preserve published plaintext", name)
	}
}

func BenchmarkValuesList(b *testing.B) {
	benchmarkWorkflowSizes(b, func(b *testing.B, f workflowBenchmark) {
		ctx := b.Context()
		cells, err := f.values.List(ctx, f.actor, f.scope, false)
		if err != nil || len(cells) != f.count {
			b.Fatalf("list: cells=%d err=%v", len(cells), err)
		}
		for i, cell := range cells {
			checkBenchmarkValues(b, cell.Name, cell.Classification, cell.Value, cell.Revealed, i)
		}
		for b.Loop() {
			cells, err := f.values.List(ctx, f.actor, f.scope, false)
			if err != nil || len(cells) != f.count {
				b.Fatalf("list: cells=%d err=%v", len(cells), err)
			}
		}
	})
}

func BenchmarkRevisionExport(b *testing.B) {
	benchmarkWorkflowSizes(b, func(b *testing.B, f workflowBenchmark) {
		ctx := b.Context()
		values, revision, err := f.revisions.ExportWithParameters(ctx, f.actor, f.scope, 0, false, nil)
		if err != nil || len(values) != f.count || revision < 1 {
			b.Fatalf("export: values=%d revision=%d err=%v", len(values), revision, err)
		}
		for i, value := range values {
			checkBenchmarkValues(b, value.Name, value.Classification, value.Value, value.Revealed, i)
		}
		for b.Loop() {
			values, gotRevision, err := f.revisions.ExportWithParameters(ctx, f.actor, f.scope, 0, false, nil)
			if err != nil || len(values) != f.count || gotRevision != revision {
				b.Fatalf("export: values=%d revision=%d err=%v", len(values), gotRevision, err)
			}
		}
	})
}

func BenchmarkEnvironmentSignals(b *testing.B) {
	benchmarkWorkflowSizes(b, func(b *testing.B, f workflowBenchmark) {
		ctx := b.Context()
		signals, err := f.revisions.Signals(ctx, f.actor, f.scope)
		if err != nil || len(signals.Cells) != f.count || signals.Revision < 1 {
			b.Fatalf("signals: cells=%d revision=%d err=%v", len(signals.Cells), signals.Revision, err)
		}
		for i, cell := range signals.Cells {
			if cell.Name != fmt.Sprintf("SETTING_%04d", i) {
				b.Fatalf("signal name = %q, want key %d", cell.Name, i)
			}
			if cell.PendingVersionID != f.pending[cell.Name] {
				b.Fatalf("pending draft for %s = %q, want %q", cell.Name, cell.PendingVersionID, f.pending[cell.Name])
			}
		}
		for b.Loop() {
			got, err := f.revisions.Signals(ctx, f.actor, f.scope)
			if err != nil || len(got.Cells) != f.count || got.Revision != signals.Revision {
				b.Fatalf("signals: cells=%d revision=%d err=%v", len(got.Cells), got.Revision, err)
			}
		}
	})
}
