package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

type updateSourceFunc func(context.Context) ([]updatecheck.Release, error)

func (fn updateSourceFunc) Releases(ctx context.Context) ([]updatecheck.Release, error) {
	return fn(ctx)
}

type binaryUpdaterFunc func(context.Context, updatecheck.Status) error

func (fn binaryUpdaterFunc) Apply(ctx context.Context, status updatecheck.Status) error {
	return fn(ctx, status)
}

type updateTTY struct {
	in  io.Reader
	out bytes.Buffer
}

func (t *updateTTY) Read(p []byte) (int, error)  { return t.in.Read(p) }
func (t *updateTTY) Write(p []byte) (int, error) { return t.out.Write(p) }
func (t *updateTTY) Close() error                { return nil }

func updateTerminal(t *testing.T, answer string) (*disclose.TerminalSession, *updateTTY) {
	t.Helper()
	tty := &updateTTY{in: strings.NewReader(answer)}
	session, err := disclose.NewTerminalSession(tty)
	if err != nil {
		t.Fatal(err)
	}
	return session, tty
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
		Version:              "1.0.0",
		DefaultUpdateChannel: updatecheck.ChannelStable,
		UpdateSource:         source,
		StderrIsTerminal:     func() bool { return true },
		Now:                  func() time.Time { return time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC) },
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
		!strings.Contains(stdout.String(), "Hikyo update available") {
		t.Fatalf("stdout = %q, want nightly update", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Checking for updates on nightly...") {
		t.Fatalf("stderr = %q, want update-check progress", stderr.String())
	}
	if got := strings.Count(stdout.String()+stderr.String(), "Hikyo update available"); got != 1 {
		t.Fatalf("combined output announces available update %d times, want once: stdout=%q stderr=%q", got, stdout.String(), stderr.String())
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

func TestUpdateCheckUsesTheBuildChannelUntilTheUserOverridesIt(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		return []updatecheck.Release{
			{Version: "1.0.1", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.0.1"},
			{Version: "1.1.0-nightly.20260824.42.gbbbbbbbb", URL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.1.0-nightly.20260824.42.gbbbbbbbb", Prerelease: true},
		}, nil
	})
	ios, stdout, stderr := updateIO(t, source)
	ios.DefaultUpdateChannel = updatecheck.ChannelNightly

	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("nightly default check exit = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "1.1.0-nightly.20260824.42.gbbbbbbbb") {
		t.Fatalf("stdout = %q, want build-default nightly update", stdout.String())
	}

	stdout.Reset()
	if code := Run(t.Context(), ios, []string{"update", "channel", "stable"}); code != ExitOK {
		t.Fatalf("stable override exit = %d: %s", code, stderr.String())
	}
	stdout.Reset()
	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("stable override check exit = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Hikyo update available") ||
		!strings.Contains(stdout.String(), "Channel    stable") ||
		!strings.Contains(stdout.String(), "Latest     1.0.1") {
		t.Fatalf("stdout = %q, want persisted stable override", stdout.String())
	}
}

func TestUpdateCheckDefaultsOffForSourceBuilds(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		t.Fatal("source build reached release network")
		return nil, nil
	})
	ios, stdout, stderr := updateIO(t, source)
	ios.DefaultUpdateChannel = updatecheck.ChannelOff

	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("source-build check exit = %d: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "Update checks are off.\n" {
		t.Fatalf("stdout = %q, want source-build checks off", got)
	}
}

func TestSourceBuildCannotEnableAnUpdaterItCannotTrust(t *testing.T) {
	ios, _, stderr := updateIO(t, nil)
	ios.DefaultUpdateChannel = updatecheck.ChannelOff

	if code := Run(t.Context(), ios, []string{"update", "channel", "stable"}); code != ExitUsage {
		t.Fatalf("channel exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "source builds keep update checks off") {
		t.Fatalf("stderr = %q, want source-build refusal", stderr.String())
	}
}

func TestImplicitSnapshotFollowsAnArtifactChannelChange(t *testing.T) {
	stateDir := t.TempDir()
	state := &State{dir: stateDir}
	if err := state.putUpdatesUnlocked(updateState{
		Channel:   updatecheck.ChannelStable,
		CheckedAt: time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC),
		Releases:  []updatecheck.Release{{Version: "1.0.1"}},
	}); err != nil {
		t.Fatal(err)
	}

	current, err := state.updates(updatecheck.ChannelNightly)
	if err != nil {
		t.Fatal(err)
	}
	if current.Channel != updatecheck.ChannelNightly || !current.CheckedAt.IsZero() || len(current.Releases) != 0 {
		t.Fatalf("migrated state = %+v, want fresh implicit nightly state", current)
	}
}

func TestUpdateCheckAsksBeforeApplyingTheAvailableUpdate(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		return []updatecheck.Release{{
			Version: "1.0.1",
			URL:     "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.0.1",
		}}, nil
	})
	ios, _, stderr := updateIO(t, source)
	ios.TerminalSession, _ = updateTerminal(t, "yes\n")
	var applied updatecheck.Status
	ios.BinaryUpdater = binaryUpdaterFunc(func(_ context.Context, status updatecheck.Status) error {
		applied = status
		return nil
	})

	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("check exit = %d: %s", code, stderr.String())
	}
	if applied.LatestVersion != "1.0.1" {
		t.Fatalf("applied status = %+v, want 1.0.1", applied)
	}
	if !strings.Contains(stderr.String(), "Hikyo 1.0.1 is verified and updated in place") {
		t.Fatalf("stderr = %q, want successful update", stderr.String())
	}
}

func TestUpdateCheckLeavesTheBinaryUntouchedWhenDeclined(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		return []updatecheck.Release{{Version: "1.0.1"}}, nil
	})
	ios, _, stderr := updateIO(t, source)
	var tty *updateTTY
	ios.TerminalSession, tty = updateTerminal(t, "no\n")
	ios.BinaryUpdater = binaryUpdaterFunc(func(context.Context, updatecheck.Status) error {
		t.Fatal("declined update reached binary replacement")
		return nil
	})

	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("check exit = %d: %s", code, stderr.String())
	}
	if !strings.Contains(tty.out.String(), "Update Hikyo to 1.0.1 now? [y/N]") {
		t.Fatalf("terminal = %q, want update confirmation", tty.out.String())
	}
}

func TestExplicitUpdateCheckFailsWhenAcceptedInstallFails(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		return []updatecheck.Release{{Version: "1.0.1"}}, nil
	})
	ios, _, stderr := updateIO(t, source)
	ios.TerminalSession, _ = updateTerminal(t, "yes\n")
	ios.BinaryUpdater = binaryUpdaterFunc(func(context.Context, updatecheck.Status) error {
		return errors.New("read-only install directory")
	})

	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitUnavailable {
		t.Fatalf("check exit = %d, want %d: %s", code, ExitUnavailable, stderr.String())
	}
	if !strings.Contains(stderr.String(), "update failed: read-only install directory") {
		t.Fatalf("stderr = %q, want install failure", stderr.String())
	}
}

func TestUpdateCheckClearlyReportsCurrentVersion(t *testing.T) {
	source := updateSourceFunc(func(context.Context) ([]updatecheck.Release, error) {
		return []updatecheck.Release{{Version: "1.0.0"}}, nil
	})
	ios, stdout, stderr := updateIO(t, source)

	if code := Run(t.Context(), ios, []string{"update", "check"}); code != ExitOK {
		t.Fatalf("check exit = %d: %s", code, stderr.String())
	}
	want := "Hikyo is up to date\n" +
		"  Version  1.0.0\n" +
		"  Channel  stable\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.String() != "Checking for updates on stable...\n" {
		t.Fatalf("stderr = %q, want update-check progress", stderr.String())
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
	if !strings.Contains(stderr.String(), "Hikyo update available") ||
		!strings.Contains(stderr.String(), "Installed  1.0.0") ||
		!strings.Contains(stderr.String(), "Latest     1.0.1") {
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
	ios.DefaultUpdateChannel = updatecheck.ChannelOff
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
