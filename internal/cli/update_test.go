package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

type updateSourceFunc func(context.Context) ([]updatecheck.Release, error)

func (fn updateSourceFunc) Releases(ctx context.Context) ([]updatecheck.Release, error) {
	return fn(ctx)
}

func updateIO(t *testing.T, source updatecheck.Source) (IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	stateDir := t.TempDir()
	return IO{
		Stdout: &stdout,
		Stderr: &stderr,
		Env: Env{Getenv: func(name string) string {
			if name == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}},
		Version:          "1.0.0",
		UpdateSource:     source,
		StderrIsTerminal: func() bool { return true },
		Now:              func() time.Time { return time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC) },
	}, &stdout, &stderr
}

func TestUpdateChannelPersistsAndCheckReportsSelectedTrack(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		return []updatecheck.Release{
			{Version: "1.0.1", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.0.1"},
			{Version: "1.1.0-nightly.20260824.42.gbbbbbbbb", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.gbbbbbbbb", Prerelease: true},
		}, nil
	})
	ios, stdout, stderr := updateIO(t, source)

	if code := Run(t.Context(), ios, []string{"update", "channel", "nightly"}); code != ExitOK {
		t.Fatalf("channel exit = %d: %s", code, stderr.String())
	}
	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("check exit = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "1.1.0-nightly.20260824.42.gbbbbbbbb") ||
		!strings.Contains(stdout.String(), "nightly") {
		t.Fatalf("stdout = %q, want nightly update", stdout.String())
	}

	statePath := filepath.Join(ios.Env.Getenv("HIKYO_STATE_DIR"), "updates.json")
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("updates.json mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestNotifyUpdateUsesFreshSnapshotWithoutNetwork(t *testing.T) {
	calls := 0
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		calls++
		return []updatecheck.Release{{Version: "1.0.1", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.0.1"}}, nil
	})
	ios, _, stderr := updateIO(t, source)
	if err := refreshReleaseSnapshot(t.Context(), ios); err != nil {
		t.Fatal(err)
	}
	NotifyUpdate(t.Context(), ios)
	if calls != 1 {
		t.Fatalf("source calls = %d, want cached second check", calls)
	}
	if !strings.Contains(stderr.String(), "Update available: 1.0.1") {
		t.Fatalf("stderr = %q, want update notice", stderr.String())
	}
}

func TestNotifyUpdateIsSilentForNonTTYAndDevelopmentBuilds(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		t.Fatal("noninteractive notification reached network")
		return nil, nil
	})
	ios, _, stderr := updateIO(t, source)
	ios.StderrIsTerminal = func() bool { return false }
	NotifyUpdate(t.Context(), ios)
	ios.StderrIsTerminal = func() bool { return true }
	ios.Version = "dev"
	NotifyUpdate(t.Context(), ios)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want silence", stderr.String())
	}
}

func TestNotifyUpdateRefreshesAfterClockRollback(t *testing.T) {
	calls := 0
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		calls++
		return []updatecheck.Release{{Version: "1.0.1"}}, nil
	})
	ios, _, _ := updateIO(t, source)
	if err := refreshReleaseSnapshot(t.Context(), ios); err != nil {
		t.Fatal(err)
	}
	ios.Now = func() time.Time { return time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC) }
	NotifyUpdate(t.Context(), ios)
	if calls != 2 {
		t.Fatalf("source calls = %d, want refresh after clock rollback", calls)
	}
}

func TestNotifyUpdateThrottlesReleaseSourceOutages(t *testing.T) {
	calls := 0
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		calls++
		return nil, errors.New("offline")
	})
	ios, _, stderr := updateIO(t, source)
	NotifyUpdate(t.Context(), ios)
	NotifyUpdate(t.Context(), ios)
	if calls != 1 {
		t.Fatalf("source calls = %d, want one attempt per snapshot window", calls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want best-effort outage silence", stderr.String())
	}
}
