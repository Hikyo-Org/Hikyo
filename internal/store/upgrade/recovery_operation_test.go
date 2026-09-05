package upgrade

import (
	"crypto/rand"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestSameReleaseRecoveryRequiresNewProofAndNeverRunsMigrations(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		manifest, err := PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		op := legacyOperation(t, cfg, manifest)
		op.TargetSchemaDigest = op.SourceSchemaDigest
		op.TargetMigrationDigest = op.SourceMigrationDigest
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			state, err := s.Bootstrap(t.Context(), manifest, op, Production)
			if err != nil {
				return err
			}
			state = healthy(t, s, state)
			var restored State
			err = s.transaction(t.Context(), func() error {
				if _, err := s.conn.ExecContext(t.Context(), `UPDATE auth_instance_state SET credential_epoch=99,restore_epoch=99 WHERE id=1`); err != nil {
					return err
				}
				var err error
				restored, err = reconcileRestore(t.Context(), func(q string, a ...any) scanner { return s.conn.QueryRowContext(t.Context(), q, a...) }, func(q string, a ...any) (int64, error) {
					r, err := s.conn.ExecContext(t.Context(), q, a...)
					if err != nil {
						return 0, err
					}
					return r.RowsAffected()
				}, rand.Reader)
				return err
			})
			if err != nil {
				return err
			}
			recovery := nextOperation(restored)
			recovery.Kind = RecoveryOperation
			recovery.Source = restored.Applied
			recovery.RouteSource = restored.Applied
			recovery.Target = restored.Applied.Release
			recovery.TargetMigrationDigest = restored.MigrationDigest
			recovery.TargetSchemaDigest = restored.SchemaDigest
			recovery.BackupID = "new-restored-incarnation-backup"
			recovery.RouteDigest = releaseidentity.Hash([]byte("authenticated zero-migration same-source plan"))
			recovery.Acceptance.Attestation = fixtureAttestation(restored, recovery)
			oldProof := recovery
			oldProof.BackupID = restored.Pending.BackupID
			if _, err := s.PrepareRecovery(t.Context(), restored, oldProof); err == nil {
				t.Fatal("old backup proof reused")
			}
			downgrade := recovery
			downgrade.Target = target(recovery.Target.Sequence + 1)
			if _, err := s.PrepareRecovery(t.Context(), restored, downgrade); err == nil {
				t.Fatal("recovery changed release identity")
			}
			fault := errors.New("recovery commit fault")
			s.beforeCommit = func() error { return fault }
			if _, err := s.PrepareRecovery(t.Context(), restored, recovery); !errors.Is(err, fault) {
				t.Fatalf("fault lost: %v", err)
			}
			s.beforeCommit = nil
			prepared, err := s.PrepareRecovery(t.Context(), restored, recovery)
			if err != nil {
				return err
			}
			if _, err := s.Advance(t.Context(), prepared, SchemaWriteStarted); err == nil {
				t.Fatal("recovery entered migration write phase")
			}
			if err := s.ApplyMigrations(t.Context(), prepared, sessionMigrations, "testdata/session-success"); err == nil {
				t.Fatal("recovery executed migration engine")
			}
			checked, err := s.ValidateRecoverySchema(t.Context(), prepared)
			if err != nil {
				return err
			}
			ready, err := s.Advance(t.Context(), checked, Healthy)
			if err != nil {
				return err
			}
			if ready.Maintenance || ready.Applied != state.Applied || ready.RecoveryIncarnation != restored.RecoveryIncarnation || ready.Generation != restored.Generation+1 {
				t.Fatal("recovery lost exact source or restored authority")
			}
			if _, err := s.PrepareRecovery(t.Context(), restored, recovery); err == nil {
				t.Fatal("same proof replayed")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
