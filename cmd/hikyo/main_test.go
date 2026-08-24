package main

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

func TestVersionStringUsesReleaseMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldVersion, oldCommit, oldDate })

	version = "0.2.0-rc.1"
	commit = "0123456789abcdef"
	buildDate = "2026-08-07T07:00:00Z"

	want := "hikyo 0.2.0-rc.1 (0123456789abcdef, 2026-08-07T07:00:00Z)"
	if got := versionString(); got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
}

func TestVersionStringMarksDevelopmentBuild(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldVersion, oldCommit, oldDate })

	version, commit, buildDate = "dev", "unknown", "unknown"
	if got := versionString(); got != "hikyo dev" {
		t.Fatalf("versionString() = %q, want %q", got, "hikyo dev")
	}
}

func TestBuiltUpdateChannelDefaultsOffAndRejectsInvalidMetadata(t *testing.T) {
	oldChannel := updateChannel
	t.Cleanup(func() { updateChannel = oldChannel })

	updateChannel = "off"
	channel, err := builtUpdateChannel()
	if err != nil {
		t.Fatal(err)
	}
	if channel != updatecheck.ChannelOff {
		t.Fatalf("built channel = %q, want off", channel)
	}

	updateChannel = "preview"
	if _, err := builtUpdateChannel(); err == nil {
		t.Fatal("invalid linker-stamped update channel was accepted")
	}
}

func TestEveryDispatchedCommandChecksForUpdatesExceptUpdateManagement(t *testing.T) {
	for _, command := range []string{"version", "server", "operator", "updater", "migrate", "admin", "backup", "restore", "run", "unknown"} {
		if !shouldCheckForUpdate(command) {
			t.Errorf("shouldCheckForUpdate(%q) = false, want true", command)
		}
	}
	if shouldCheckForUpdate("update") {
		t.Fatal("update management recursively triggered the automatic update check")
	}
}
