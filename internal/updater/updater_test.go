package updater

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutorRefusesEveryLegacyBackend(t *testing.T) {
	for _, backend := range []Backend{BackendFlux, BackendCompose, BackendSystemd} {
		for _, version := range []string{"1.2.3", "1.2.3-nightly.1", "dev"} {
			t.Run(string(backend)+"/"+version, func(t *testing.T) {
				executor := Executor{Config: Config{Backend: backend}}
				job, err := executor.Execute(t.Context(), Request{ID: "upd_legacy", Version: version})
				if !errors.Is(err, ErrRemoteApplyDisabled) || job.State != StateFailed || job.FailureCode != "remote-apply-disabled" {
					t.Fatalf("job=%+v error=%v, want disabled failure", job, err)
				}
				if job.Backend != backend {
					t.Fatalf("job backend=%q, want %q", job.Backend, backend)
				}
			})
		}
	}
}

func TestDirectLegacyCommandRunnerRefusesWithoutStartingProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "deployment-mutated")
	err := (CommandRunner{}).Run(t.Context(), Command{Name: "/usr/bin/touch", Argv: []string{marker}}, Request{})
	if !errors.Is(err, ErrRemoteApplyDisabled) {
		t.Fatalf("direct phase error=%v, want disabled", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy phase touched deployment: %v", err)
	}
}

func TestJournalPersistsOneActiveJobAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater-state.json")
	journal := Journal{Path: path}
	job := Job{ID: "upd_1", Backend: BackendFlux, Version: "1.2.3", State: StateRunning}
	if err := journal.Create(job); err != nil {
		t.Fatal(err)
	}
	reopened := Journal{Path: path}
	if err := reopened.Create(Job{ID: "upd_2", Backend: BackendFlux, Version: "1.2.4", State: StateQueued}); !errors.Is(err, ErrUpdateActive) {
		t.Fatalf("second active job error = %v, want ErrUpdateActive", err)
	}
	got, err := reopened.Get("upd_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.2.3" || got.State != StateRunning {
		t.Fatalf("reopened job = %#v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode = %o, want 600", info.Mode().Perm())
	}
}

func TestJournalMarksInterruptedJobFailedOnHelperRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater-state.json")
	journal := Journal{Path: path}
	if err := journal.Create(Job{ID: "upd_1", State: StateRunning, Phase: "apply"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 5, 0, 0, 0, time.UTC)
	if err := journal.RecoverInterrupted(now); err != nil {
		t.Fatal(err)
	}
	job, err := journal.Get("upd_1")
	if err != nil {
		t.Fatal(err)
	}
	if job.State != StateFailed || job.Phase != "recovery" || job.FailureCode != "helper-restarted" || !job.FinishedAt.Equal(now) {
		t.Fatalf("recovered job = %#v", job)
	}
}
