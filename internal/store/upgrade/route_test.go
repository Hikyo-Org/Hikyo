package upgrade

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestIntermediateHealthyKeepsWholeRouteMaintenance(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			op := operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine))
			op.RouteLength = 2
			state, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), op, Production)
			if err != nil {
				return err
			}
			for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
				state, err = s.Advance(t.Context(), state, phase)
				if err != nil {
					return err
				}
			}
			if !state.Maintenance || state.Generation != 1 {
				t.Fatal("intermediate hop enabled runtime")
			}
			op.Source = state.Applied
			op.SourceMigrationDigest = state.MigrationDigest
			op.SourceSchemaDigest = state.SchemaDigest
			op.Target = target(2)
			op.Hop = 1
			op.RecoveryIncarnation = state.RecoveryIncarnation
			changed := op
			changed.RouteDigest = releaseidentity.Hash([]byte("substituted"))
			if _, err := s.Prepare(t.Context(), state, changed); !errors.Is(err, ErrConflict) {
				t.Fatal("intermediate route changed")
			}
			state, err = s.Prepare(t.Context(), state, op)
			if err != nil {
				return err
			}
			if state.Generation != 1 || !state.Maintenance {
				t.Fatal("route generation/maintenance changed")
			}
			for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
				state, err = s.Advance(t.Context(), state, phase)
				if err != nil {
					return err
				}
			}
			if state.Maintenance || state.Applied.Release != target(2) {
				t.Fatal("final route did not finish")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPhaseCommitUncertaintyReconstructsWriteBoundary(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		fault := errors.New("lost commit acknowledgement")
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			s.afterCommit = func() error { return fault }
			result, err := s.Advance(t.Context(), state, SchemaWriteStarted)
			if !reflect.DeepEqual(result, State{}) {
				t.Fatal("uncertain commit returned authority")
			}
			return err
		})
		if !errors.Is(err, fault) {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			read, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if read.Pending.Phase != SchemaWriteStarted || !read.Maintenance {
				t.Fatal("write boundary lost")
			}
			if _, err := s.Resume(t.Context(), state); !errors.Is(err, ErrConflict) {
				t.Fatal("pre-write state resumed")
			}
			_, err = s.Resume(t.Context(), read)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestDowngradeAndGenerationOverflowRefuse(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		state := bootstrap(t, cfg)
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			var err error
			for _, phase := range []Phase{SchemaWriteStarted, SchemaApplied, Healthy} {
				state, err = s.Advance(t.Context(), state, phase)
				if err != nil {
					return err
				}
			}
			op := operation(state.Applied, emptyManifest(cfg.Engine))
			op.Target = state.Applied.Release
			op.SourceMigrationDigest = state.MigrationDigest
			op.SourceSchemaDigest = state.SchemaDigest
			op.Generation = 2
			op.RecoveryIncarnation = state.RecoveryIncarnation
			if _, err := s.Prepare(t.Context(), state, op); err == nil {
				t.Fatal("same/descending release upgrade accepted")
			}
			state.Generation = math.MaxInt64
			state.Pending.Generation = math.MaxInt64
			if _, err := s.Prepare(t.Context(), state, op); !errors.Is(err, ErrConflict) {
				t.Fatal("generation wrapped")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestTrustDomainIsExplicitAndDurable(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			op := operation(Source{Genesis: FreshGenesis}, emptyManifest(cfg.Engine))
			if _, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), op, ""); !errors.Is(err, ErrCorrupt) {
				t.Fatal("missing domain accepted")
			}
			state, err := s.Bootstrap(t.Context(), emptyManifest(cfg.Engine), op, LocalDevelopment)
			if err != nil {
				return err
			}
			if state.TrustDomain != LocalDevelopment {
				t.Fatal("development domain lost")
			}
			changed := state
			changed.TrustDomain = Production
			if _, err := s.Resume(t.Context(), changed); !errors.Is(err, ErrConflict) {
				t.Fatal("development database relabeled")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
