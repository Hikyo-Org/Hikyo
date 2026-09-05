package upgrade

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBootstrapCrashChild(t *testing.T) {
	path := os.Getenv("HIKYO_LEDGER_TEST_CRASH_CONFIG")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	err = WithLock(t.Context(), cfg, func(s *Session) error {
		crash := func() error { os.Exit(73); return nil }
		if os.Getenv("HIKYO_LEDGER_TEST_CRASH_PHASE") == "after" {
			s.afterCommit = crash
		} else {
			s.beforeCommit = crash
		}
		_, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine)), Production)
		return err
	})
	t.Fatalf("crash point not reached: %v", err)
}

func TestAbruptProcessExitLeavesAbsentOrCompleteBootstrap(t *testing.T) {
	for _, phase := range []string{"before", "after"} {
		t.Run(phase, func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				// This test-only child has no deferred rollback/connection cleanup. A
				// separate process inspects the durable result after its abrupt exit.
				raw, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "owned-test-config.json")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBootstrapCrashChild$")
				command.Env = append(os.Environ(), "HIKYO_LEDGER_TEST_CRASH_CONFIG="+path, "HIKYO_LEDGER_TEST_CRASH_PHASE="+phase)
				output, err := command.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 73 {
					t.Fatalf("child did not hit abrupt boundary: %v %s", err, output)
				}
				err = WithLock(t.Context(), cfg, func(s *Session) error {
					if phase == "after" {
						state, err := s.Read(t.Context())
						if err != nil {
							return err
						}
						if state.Pending.Phase != Prepared {
							t.Fatal("committed phase lost")
						}
						return nil
					}
					catalog, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
					if err != nil {
						return err
					}
					if controlPresent(catalog) {
						t.Fatal("partial control survived abrupt exit")
					}
					_, err = validateGenesis(catalog, emptyManifest(cfg.Engine))
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
