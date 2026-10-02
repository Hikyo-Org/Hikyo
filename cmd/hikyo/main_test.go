package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/cli"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/console"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/disclose"
	"github.com/Hikyo-Org/hikyo/internal/multicall"
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
	for _, command := range []string{"server", "operator", "updater", "migrate", "admin", "backup", "restore", "unknown", "update", "scan", "version", "--version", "about", "welcome"} {
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

func TestEscrowDispatchPreservesServerRootIdentity(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "server-root")
	if err := os.WriteFile(primary, []byte(crypto.EncodeRootKey(bytes.Repeat([]byte{85}, 32))), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIKYO_DB", "sqlite:"+filepath.Join(t.TempDir(), "must-not-open.db"))
	t.Setenv("HIKYO_ROOT_KEY_FILE", primary)
	t.Setenv("HIKYO_ROOT_KEY", "")
	for _, path := range []string{primary, filepath.Join(t.TempDir(), "alias")} {
		if path != primary {
			if err := os.Link(primary, path); err != nil {
				t.Fatal(err)
			}
		}
		called := false
		code := runOperator(t.Context(), "escrow", []string{"verify", "--root-key-file", path, "--assert-separate-custody"}, func(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string, out io.Writer, term *disclose.TerminalSession, termErr error) error {
			called = true
			if cfg.RootKeyFile != primary {
				t.Fatal("actual dispatcher lost configured root identity")
			}
			err := app.RunEscrow(ctx, cfg, log, args, out, term, termErr)
			if err == nil || !strings.Contains(err.Error(), "server root source") {
				t.Fatalf("same-file refusal: %v", err)
			}
			return err
		})
		if !called || code == 0 {
			t.Fatal("actual dispatch accepted same-file custody")
		}
	}
}

func TestUsageCoversEveryMode(t *testing.T) {
	// The mode switch in run() and the multicall help must not drift apart:
	// every dispatched mode has a synopsis line, and no line names a mode
	// that does not dispatch. Client verbs are appended from cli.Verbs.
	var out bytes.Buffer
	usage(&out)
	text := out.String()
	if strings.Contains(text, "—") {
		t.Error("usage contains an em-dash")
	}
	for _, entry := range multicall.Modes() {
		if entry.Hidden {
			continue
		}
		mode := entry.Name
		if !strings.Contains(text, "  hikyo "+mode) && !strings.Contains(text, "  sudo hikyo "+mode) {
			t.Errorf("usage has no synopsis for mode %q", mode)
		}
		if !cli.HelpFromText(io.Discard, usageText, []string{mode}) {
			t.Errorf("hikyo %s --help would find no section", mode)
		}
	}
	for _, verb := range cli.Verbs {
		if !strings.Contains(text, verb) {
			t.Errorf("usage does not list client verb %q", verb)
		}
	}
	if cli.HelpFromText(io.Discard, usageText, []string{"teleport"}) {
		t.Error("usage answers help for an unknown mode")
	}
}

func TestRunHelpRewritesTheHelpWord(t *testing.T) {
	// `hikyo help values set` is `hikyo values set --help`, handed on to the
	// client dispatcher untouched otherwise; host groups answer in place.
	invocation, handled, code := runHelp([]string{"help", "values", "set"}, io.Discard, io.Discard)
	if handled || code != 0 || strings.Join(invocation, " ") != "values set --help" {
		t.Errorf("runHelp(help values set) = %q, %v, %d", invocation, handled, code)
	}
	invocation, handled, _ = runHelp([]string{"login", "--local"}, io.Discard, io.Discard)
	if handled || strings.Join(invocation, " ") != "login --local" {
		t.Errorf("runHelp(login --local) = %q, %v", invocation, handled)
	}
	for _, args := range [][]string{{"help"}, {"-h"}, {"help", "backup", "export"}, {"admin", "create", "--help"}, {"escrow", "--help"}, {"help", "client"}, {"updater", "--help"}, {"upgrade", "operator", "--help"}, {"upgrade", "operator", "rotate", "-h"}} {
		if _, handled, code := runHelp(args, io.Discard, io.Discard); !handled || code != 0 {
			t.Errorf("runHelp(%q) = %v, %d, want handled with success", args, handled, code)
		}
	}
	// Modes with their own flag sets answer --help through the flag package.
	for _, args := range [][]string{{"server", "--help"}, {"migrate", "-h"}, {"upgrade", "--help"}, {"config-rollout", "--help"}} {
		if _, handled, _ := runHelp(args, io.Discard, io.Discard); handled {
			t.Errorf("runHelp(%q) handled, want the mode's own flag usage", args)
		}
	}
	if _, handled, code := runHelp([]string{"help", "teleport"}, io.Discard, io.Discard); !handled || code != 2 {
		t.Errorf("runHelp(help teleport) = %v, %d, want usage exit", handled, code)
	}
}

func TestConfigRolloutHelpExitsZero(t *testing.T) {
	if code := runConfigRollout(context.Background(), []string{"--help"}, io.Discard); code != 0 {
		t.Errorf("config-rollout --help exited %d, want 0", code)
	}
}
