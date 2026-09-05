package updater

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingRunner struct {
	calls  []string
	failAt string
}

func (r *recordingRunner) Run(_ context.Context, command Command, _ Request) error {
	name := filepath.Base(command.Name)
	r.calls = append(r.calls, name)
	if name == r.failAt {
		return errors.New("fixture failure")
	}
	return nil
}

func TestExecutorRefusesEveryLegacyBackendWithoutCommandsOrJournalMutation(t *testing.T) {
	for _, backend := range []Backend{BackendFlux, BackendCompose, BackendSystemd} {
		for _, version := range []string{"1.2.3", "1.2.3-nightly.1", "dev"} {
			t.Run(string(backend)+"/"+version, func(t *testing.T) {
				runner := &recordingRunner{}
				progress := false
				executor := Executor{Config: fixtureConfig(t, backend), Runner: runner,
					Progress: func(Job) error { progress = true; return nil },
				}
				job, err := executor.Execute(t.Context(), Request{ID: "upd_legacy", Version: version})
				if !errors.Is(err, ErrRemoteApplyDisabled) || job.State != StateFailed || job.FailureCode != "remote-apply-disabled" {
					t.Fatalf("job=%+v error=%v, want disabled failure", job, err)
				}
				if len(runner.calls) != 0 || progress {
					t.Fatalf("retired job ran commands=%v or changed journal=%v", runner.calls, progress)
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

func TestLoadConfigRefusesWritableOrAmbiguousPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updater.json")
	encoded := `{"backend":"flux","socket":"/run/hikyo/updater.sock","state_file":"/var/lib/hikyo-updater/jobs.json","commands":{"plan":{"name":"plan","timeout_seconds":1},"backup":{"name":"backup","timeout_seconds":1},"verify":{"name":"verify","timeout_seconds":1},"apply":{"name":"apply","timeout_seconds":1},"health":{"name":"health","timeout_seconds":1},"rollback":{"name":"rollback","timeout_seconds":1}}}`
	if err := os.WriteFile(path, []byte(encoded), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "group/world writable") {
		t.Fatalf("writable config error = %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(encoded+` {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("multiple value error = %v", err)
	}
}

func fixtureConfig(t *testing.T, backend Backend) Config {
	t.Helper()
	config := Config{
		Backend: backend,
		Commands: PhaseCommands{
			Plan:     Command{Name: "/commands/plan", TimeoutSeconds: 5},
			Backup:   Command{Name: "/commands/backup", TimeoutSeconds: 5},
			Verify:   Command{Name: "/commands/verify", TimeoutSeconds: 5},
			Apply:    Command{Name: "/commands/apply", TimeoutSeconds: 5},
			Health:   Command{Name: "/commands/health", TimeoutSeconds: 5},
			Rollback: Command{Name: "/commands/rollback", TimeoutSeconds: 5},
		},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	return config
}
