package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/console"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

func TestWriteVersionUsesReadableBuildSummary(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldVersion, oldCommit, oldDate })
	version, commit, buildDate = "0.2.0", "abcdef12", "2026-08-24T08:00:00Z"

	var output bytes.Buffer
	writeVersion(&output)
	want := "Hikyo 0.2.0\n  Commit  abcdef12\n  Built   2026-08-24T08:00:00Z\n"
	if output.String() != want {
		t.Fatalf("version output = %q, want %q", output.String(), want)
	}
}

func TestWriteMachineVersionKeepsSingleValue(t *testing.T) {
	oldVersion := version
	t.Cleanup(func() { version = oldVersion })
	version = "0.2.0-rc.1"

	var output bytes.Buffer
	writeMachineVersion(&output)
	if got, want := output.String(), "0.2.0-rc.1\n"; got != want {
		t.Fatalf("--version output = %q, want %q", got, want)
	}
}

func TestWriteAboutAndWelcomeExposeFullArtwork(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldVersion, oldCommit, oldDate })
	version, commit, buildDate = "1.2.3", "abcdef12", "2026-08-24T08:00:00Z"

	for name, write := range map[string]func(io.Writer){
		"about": writeAbout, "welcome": writeWelcome,
	} {
		var output bytes.Buffer
		write(&output)
		if !strings.HasPrefix(output.String(), console.FullArtwork()+"\n") {
			t.Fatalf("%s output does not start with full artwork", name)
		}
		if !strings.Contains(output.String(), "1.2.3") {
			t.Fatalf("%s output does not name the version", name)
		}
	}
}

func TestServerAppURLUsesActualEphemeralPort(t *testing.T) {
	cfg := &config.Config{
		Listen:         "127.0.0.1:0",
		ExternalOrigin: "http://127.0.0.1:0",
	}
	srv := &app.Server{Addr: "127.0.0.1:54321"}
	if got, want := serverAppURL(cfg, srv), "http://127.0.0.1:54321"; got != want {
		t.Fatalf("serverAppURL() = %q, want %q", got, want)
	}
}

func TestServerAppURLPreservesConfiguredExternalOrigin(t *testing.T) {
	cfg := &config.Config{
		Listen:         "0.0.0.0:8443",
		ExternalOrigin: "https://hikyo.example.com",
	}
	srv := &app.Server{Addr: "0.0.0.0:8443"}
	if got := serverAppURL(cfg, srv); got != cfg.ExternalOrigin {
		t.Fatalf("serverAppURL() = %q, want configured origin %q", got, cfg.ExternalOrigin)
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

func TestOnlyClientCommandsMayCheckForUpdatesBeforeDispatch(t *testing.T) {
	for _, command := range []string{"run", "login"} {
		if !shouldCheckForUpdate(command) {
			t.Errorf("shouldCheckForUpdate(%q) = false, want true", command)
		}
	}
	for _, command := range []string{"server", "operator", "updater", "migrate", "admin", "backup", "restore", "unknown", "update", "version", "--version", "about", "welcome"} {
		if shouldCheckForUpdate(command) {
			t.Errorf("shouldCheckForUpdate(%q) = true, want no optional executable mutation before the command gate", command)
		}
	}
}

func TestHostCommandDevelopmentOptInIsOnlyLeadingGroupFlag(t *testing.T) {
	t.Setenv("HIKYO_DB", "sqlite:"+filepath.Join(t.TempDir(), "host.db"))
	// This unsupported environment key must not select a weaker trust domain.
	t.Setenv("HIKYO_DEV", "true")
	for _, group := range []struct{ name, verb string }{{"admin", "create"}, {"backup", "export"}, {"restore", "status"}} {
		for _, leading := range []bool{false, true} {
			label := "production"
			if leading {
				label = "explicit-development"
			}
			t.Run(group.name+"/"+label, func(t *testing.T) {
				verbArgs := []string{group.verb, "--display-name", "--dev"}
				arguments := slices.Clone(verbArgs)
				if leading {
					arguments = append([]string{"--dev"}, arguments...)
				}
				called := false
				code := runOperator(t.Context(), group.name, arguments, func(_ context.Context, cfg *config.Config, _ *slog.Logger, args []string, _ io.Writer, _ *disclose.TerminalSession, _ error) error {
					called = true
					if cfg.Dev != leading {
						t.Fatalf("development mode=%v want %v", cfg.Dev, leading)
					}
					if !slices.Equal(args, verbArgs) {
						t.Fatalf("verb data changed: %q", args)
					}
					return nil
				})
				if code != 0 || !called {
					t.Fatalf("host command dispatch failed: code=%d called=%v", code, called)
				}
			})
		}
	}
}
