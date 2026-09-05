package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestUnknownSchemaRefusesWithoutControlEffects(t *testing.T) {
	for _, change := range []string{
		`CREATE TABLE surprise(id INTEGER)`,
		`ALTER TABLE orgs ADD COLUMN surprise TEXT`,
		`CREATE INDEX drift_probe ON orgs(name)`,
		`DELETE FROM goose_db_version WHERE version_id=1`,
		`INSERT INTO goose_db_version(version_id,is_applied) VALUES(9000,TRUE)`,
		`UPDATE goose_db_version SET is_applied=FALSE WHERE version_id=1`,
	} {
		t.Run(change, func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				if err := migrateFixture(t, cfg); err != nil {
					t.Fatal(err)
				}

				query(t, cfg, change)
				manifest, err := PinnedLegacyManifest(cfg.Engine)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Inspect(t.Context(), cfg, manifest); !errors.Is(err, ErrGenesis) {
					t.Fatalf("inspection accepted drift: %v", err)
				}
				err = WithLock(t.Context(), cfg, func(s *Session) error {
					before, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
					if err != nil {
						return err
					}
					_, bootstrapErr := s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), Production)
					if bootstrapErr == nil {
						t.Fatal("bootstrap accepted drift")
					}
					after, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
					if err == nil && !reflect.DeepEqual(before, after) {
						t.Fatal("refusal changed schema")
					}
					return err
				})
				// Corrupt goose history itself is rejected by the read-only inspector.
				if err != nil && !errors.Is(err, ErrGenesis) {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestBootstrapAtomicCommitBoundaries(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-commit", true: "after-commit"}[after], func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				fault := errors.New("injected process boundary")
				err := WithLock(t.Context(), cfg, func(s *Session) error {
					if after {
						s.afterCommit = func() error { return fault }
					} else {
						s.beforeCommit = func() error { return fault }
					}
					result, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine)), Production)
					if !reflect.DeepEqual(result, State{}) {
						t.Fatal("ambiguous commit returned usable result")
					}
					return err
				})
				if !errors.Is(err, fault) {
					t.Fatal(err)
				}
				err = WithLock(t.Context(), cfg, func(s *Session) error {
					if after {
						state, err := s.Read(t.Context())
						if err == nil && state.Pending.Phase != Prepared {
							t.Fatal(state)
						}
						return err
					}
					catalog, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
					if err != nil {
						return err
					}
					if controlPresent(catalog) {
						t.Fatal("partial control survived rollback")
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

func TestCASRefusesEveryStaleIdentityAndIllegalPhase(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		changes := []struct {
			name   string
			change func(*State)
		}{
			{"instance", func(s *State) { s.InstanceID += "changed" }},
			{"domain", func(s *State) { s.TrustDomain = LocalDevelopment }},
			{"epoch", func(s *State) { s.RestoreEpoch++ }},
			{"generation", func(s *State) { s.Generation++ }},
			{"incarnation", func(s *State) { s.RecoveryIncarnation[0] ^= 255 }},
			{"target", func(s *State) { s.Pending.Target = target(2) }},
			{"route", func(s *State) { s.Pending.RouteDigest = releaseidentity.Hash([]byte("other")) }},
			{"backup", func(s *State) { s.Pending.BackupID += "other" }},
		}
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			for _, change := range changes {
				candidate := state
				pending := *state.Pending
				candidate.Pending = &pending
				change.change(&candidate)
				if _, err := s.Advance(t.Context(), candidate, SchemaWriteStarted); !errors.Is(err, ErrConflict) {
					t.Errorf("%s CAS: %v", change.name, err)
				}
				if _, err := s.Resume(t.Context(), candidate); !errors.Is(err, ErrConflict) {
					t.Errorf("%s resume: %v", change.name, err)
				}
			}
			for _, phase := range []Phase{Prepared, SchemaApplied, Healthy, "unknown"} {
				if _, err := s.Advance(t.Context(), state, phase); !errors.Is(err, ErrConflict) {
					t.Errorf("illegal phase %s: %v", phase, err)
				}
			}
			read, err := s.Read(t.Context())
			if err == nil && !reflect.DeepEqual(read, state) {
				t.Fatal("refused CAS mutated state")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestMigrationExclusionCancellationAndConcurrentBootstrap(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- WithLock(t.Context(), cfg, func(s *Session) error {
				close(entered)
				<-release
				_, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine)), Production)
				return err
			})
		}()
		<-entered
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		err := WithLock(ctx, cfg, func(*Session) error { t.Error("competing lock entered"); return nil })
		cancel()
		if err == nil {
			t.Error("competing migrator did not refuse")
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			_, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine)), Production)
			return err
		})
		if !errors.Is(err, ErrGenesis) {
			t.Fatalf("duplicate bootstrap: %v", err)
		}
	})
}

func TestCorruptControlStateRefuses(t *testing.T) {
	for _, mutation := range []string{`DELETE FROM upgrade_pending`, `DROP TABLE upgrade_pending`, `UPDATE upgrade_control SET applied_json='{}'`, `UPDATE upgrade_pending SET operation_json='{}'`, `UPDATE upgrade_control SET incarnation='0000000000000000000000000000000000000000000000000000000000000000'`} {
		t.Run(mutation, func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				bootstrap(t, cfg)
				query(t, cfg, mutation)
				if err := WithLock(t.Context(), cfg, func(s *Session) error { _, err := s.Read(t.Context()); return err }); err == nil {
					t.Fatal("corrupt state admitted")
				}
			})
		})
	}
}

func TestSQLiteHardLinksAndReplacedIdentityRefuse(t *testing.T) {
	cfg := testConfig(t, releaseidentity.SQLite)
	bootstrap(t, cfg)
	alias := filepath.Join(filepath.Dir(cfg.Path), "alias.db")
	if err := os.Link(cfg.Path, alias); err != nil {
		t.Fatal(err)
	}
	if err := WithLock(t.Context(), cfg, func(*Session) error { t.Error("hard link admitted"); return nil }); err == nil {
		t.Fatal("multiply linked database accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	err := WithLock(t.Context(), cfg, func(s *Session) error {
		if err := os.Rename(cfg.Path, cfg.Path+".old"); err != nil {
			return err
		}
		if err := os.WriteFile(cfg.Path, nil, 0600); err != nil {
			return err
		}
		_, err := s.Read(t.Context())
		return err
	})
	if err == nil {
		t.Fatal("replaced database identity accepted")
	}
}
