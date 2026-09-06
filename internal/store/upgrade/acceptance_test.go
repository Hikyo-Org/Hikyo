package upgrade

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func healthy(t *testing.T, s *Session, state State) State {
	t.Helper()
	for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
		next, err := s.Advance(t.Context(), state, phase)
		if err != nil {
			t.Fatal(err)
		}
		state = next
	}
	return state
}

func TestNightlySequenceDoesNotRequireAStableRelease(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		for _, profile := range []releaseidentity.Profile{releaseidentity.StableV1, releaseidentity.NightlyV1} {
			err := WithLock(t.Context(), cfg, func(s *Session) error {
				op := operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine))
				op.Acceptance.Floor.HighestReleaseSequence = 0
				if profile == releaseidentity.NightlyV1 {
					op.Target.Profile, op.Target.Version = profile, "1.1.0-nightly.1"
				}
				_, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), op, Production)
				return err
			})
			if (err == nil) != (profile == releaseidentity.NightlyV1) {
				t.Fatalf("profile %s with no stable release: %v", profile, err)
			}
		}
	})
}

func nextOperation(state State) Operation {
	op := operation(state.Applied, emptyManifest(releaseidentity.SQLite))
	op.SourceMigrationDigest = state.MigrationDigest
	op.SourceSchemaDigest = state.SchemaDigest
	op.TargetMigrationDigest = state.MigrationDigest
	op.TargetSchemaDigest = state.SchemaDigest
	op.Target = target(state.Applied.Release.Sequence + 1)
	op.Generation = state.Generation + 1
	op.RecoveryIncarnation = state.RecoveryIncarnation
	op.Acceptance.Attestation = fixtureAttestation(state, op)
	return op
}

func TestNonceAndTrustFloorsCommitWithPending(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state = healthy(t, s, state)
			op := nextOperation(state)
			fault := errors.New("before commit")
			s.beforeCommit = func() error { return fault }
			if _, err := s.Prepare(t.Context(), state, op); !errors.Is(err, fault) {
				t.Fatal(err)
			}
			s.beforeCommit = nil
			var count int
			if err := s.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM upgrade_nonces`).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatal("nonce survived failed pending transaction")
			}
			read, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(read, state) {
				t.Fatal("failed prepare changed state")
			}
			state, err = s.Prepare(t.Context(), state, op)
			if err != nil {
				return err
			}
			state = healthy(t, s, state)
			replay := nextOperation(state)
			replay.Acceptance.Attestation.Nonce = op.Acceptance.Attestation.Nonce
			if _, err := s.Prepare(t.Context(), state, replay); err == nil {
				t.Fatal("same incarnation/epoch nonce reused")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestLegacyProposalNonceBootstrapIsAtomic(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				if err := migrateFixture(t, cfg); err != nil {
					t.Fatal(err)
				}
				manifest, err := PinnedLegacyManifest(cfg.Engine)
				if err != nil {
					t.Fatal(err)
				}
				op := legacyOperation(t, cfg, manifest)
				fault := errors.New("bootstrap commit failure")
				err = WithLock(t.Context(), cfg, func(s *Session) error {
					if after {
						s.afterCommit = func() error { return fault }
					} else {
						s.beforeCommit = func() error { return fault }
					}
					result, err := s.Bootstrap(t.Context(), manifest, op, Production)
					if !reflect.DeepEqual(result, State{}) {
						t.Fatal("uncertain result escaped")
					}
					return err
				})
				if !errors.Is(err, fault) {
					t.Fatal(err)
				}
				err = WithLock(t.Context(), cfg, func(s *Session) error {
					if after {
						state, err := s.Read(t.Context())
						if err != nil {
							return err
						}
						if state.RecoveryIncarnation != op.RecoveryIncarnation {
							t.Fatal("proposal replaced")
						}
						var count int
						if err := s.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM upgrade_nonces`).Scan(&count); err != nil {
							return err
						}
						if count != 1 {
							t.Fatal("complete pending lacks consumed proposal")
						}
						return nil
					}
					catalog, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
					if err != nil {
						return err
					}
					if controlPresent(catalog) {
						t.Fatal("partial nonce/control schema survived")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestInvalidAcceptanceCannotPrepare(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state = healthy(t, s, state)
			for _, change := range []func(*Operation){
				func(op *Operation) { op.Acceptance.Attestation = nil },
				func(op *Operation) { op.Acceptance.Floor.MetadataSHA256 = releaseidentity.Hash([]byte("equivocation")) },
				func(op *Operation) { op.Acceptance.Floor.HighestReleaseSequence-- },
				func(op *Operation) { op.Acceptance.ReleaseRootDigest = releaseidentity.Hash([]byte("other root")) },
				func(op *Operation) {
					op.Acceptance.Attestation.ExpiresAt = time.Now().UTC().Add(-time.Minute)
					op.Acceptance.Attestation.IssuedAt = op.Acceptance.Attestation.ExpiresAt.Add(-time.Hour)
				},
				func(op *Operation) {
					op.Acceptance.Attestation.IssuedAt = time.Now().UTC().Add(time.Hour)
					op.Acceptance.Attestation.ExpiresAt = op.Acceptance.Attestation.IssuedAt.Add(time.Hour)
				},
				func(op *Operation) { op.Acceptance.Attestation.RecoveryIncarnation[0] ^= 1 },
				func(op *Operation) { op.Acceptance.Attestation.RouteGeneration++ },
			} {
				op := nextOperation(state)
				change(&op)
				if _, err := s.Prepare(t.Context(), state, op); err == nil {
					t.Fatal("invalid acceptance prepared")
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestHealthyRestartRefreshesTrustWithoutBackupOrGeneration(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state = healthy(t, s, state)
			floor := state.Floor
			floor.CatalogSequence++
			floor.CatalogSHA256 = releaseidentity.Hash([]byte("new catalog"))
			next, err := s.RefreshTrust(t.Context(), state, floor, state.ReleaseRootDigest)
			if err != nil {
				return err
			}
			if next.Generation != state.Generation || next.Applied != state.Applied || next.Maintenance != state.Maintenance || next.Pending.Phase != Healthy {
				t.Fatal("trust refresh changed runtime identity")
			}
			if _, err := s.RefreshTrust(t.Context(), next, state.Floor, state.ReleaseRootDigest); err == nil {
				t.Fatal("trust rollback accepted")
			}
			read, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if read.Floor != floor {
				t.Fatal("new trust floor not durable")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestCASUsesDurableTimeRepresentation(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state = healthy(t, s, state)
			op := nextOperation(state)
			op.Acceptance.Attestation.IssuedAt = time.Now().Add(-time.Second)
			op.Acceptance.Attestation.ExpiresAt = op.Acceptance.Attestation.IssuedAt.Add(time.Hour)
			next, err := s.Prepare(t.Context(), state, op)
			if err != nil {
				return err
			}
			_, err = s.Resume(t.Context(), next)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
