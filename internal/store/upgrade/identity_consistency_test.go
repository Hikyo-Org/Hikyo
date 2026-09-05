package upgrade

import (
	"errors"
	"testing"
)

func TestSourceInspectionChecksActualIdentityAndStrongestEpoch(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		query(t, cfg, `UPDATE auth_instance_state SET credential_epoch=7 WHERE id=1`)
		manifest, err := PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			_, err := s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), Production)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := InspectInstalled(t.Context(), cfg, manifest)
		if err != nil || initial.RestoreEpoch != 7 {
			t.Fatalf("credential-only rotation refused: %v", err)
		}
		query(t, cfg, `UPDATE instance_identity SET identity='ins_changed' WHERE id=1`)
		if _, err := InspectInstalled(t.Context(), cfg, manifest); !errors.Is(err, ErrConflict) {
			t.Fatalf("changed actual identity accepted: %v", err)
		}
		query(t, cfg, `UPDATE instance_identity SET identity=$1 WHERE id=1`, initial.InstanceID)
		query(t, cfg, `UPDATE auth_instance_state SET credential_epoch=8 WHERE id=1`)
		if _, err := InspectInstalled(t.Context(), cfg, manifest); err != nil {
			t.Fatalf("post-bootstrap credential-only advance refused: %v", err)
		}
		query(t, cfg, `UPDATE auth_instance_state SET credential_epoch=9,restore_epoch=9 WHERE id=1`)
		if _, err := InspectInstalled(t.Context(), cfg, manifest); !errors.Is(err, ErrConflict) {
			t.Fatalf("unreconciled restore accepted: %v", err)
		}
	})
}

func TestFreshReconciliationOnlyBeforePopulation(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		manifest := emptyManifest(cfg.Engine)
		op := operation(Source{Genesis: FreshGenesis}, manifest)
		targetManifest, err := PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		op.TargetMigrationDigest, err = targetManifest.Digest()
		if err != nil {
			t.Fatal(err)
		}
		op.TargetSchemaDigest, err = PinnedLegacySchemaDigest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		var state State
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			var err error
			state, err = s.Bootstrap(t.Context(), manifest, op, Production)
			if err != nil {
				return err
			}
			state, err = s.Advance(t.Context(), state, SchemaWriteStarted)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			if err := s.ReconcileFreshInstance(t.Context(), state); !errors.Is(err, ErrConflict) {
				t.Fatalf("write-started rewrite accepted: %v", err)
			}
			var err error
			state, err = s.Advance(t.Context(), state, SchemaApplied)
			if err != nil {
				return err
			}
			if err := s.ReconcileFreshInstance(t.Context(), state); err != nil {
				return err
			}
			if err := s.ReconcileFreshInstance(t.Context(), state); err != nil {
				return err
			}
			var actual string
			if err := s.conn.QueryRowContext(t.Context(), `SELECT identity FROM instance_identity WHERE id=1`).Scan(&actual); err != nil {
				return err
			}
			if actual != state.InstanceID {
				t.Fatal("fresh migration identity not reconciled")
			}
			_, err = s.conn.ExecContext(t.Context(), `INSERT INTO orgs(id,name,active,metadata,created_at) VALUES('org_existing','Existing',$1,'{}','2026-01-01T00:00:00Z')`, true)
			if err != nil {
				return err
			}
			if err := s.ReconcileFreshInstance(t.Context(), state); !errors.Is(err, ErrConflict) {
				t.Fatalf("populated source rewrite accepted: %v", err)
			}
			if _, err := s.conn.ExecContext(t.Context(), `DELETE FROM orgs WHERE id='org_existing'`); err != nil {
				return err
			}
			state, err = s.Advance(t.Context(), state, Healthy)
			if err != nil {
				return err
			}
			if err := s.ReconcileFreshInstance(t.Context(), state); !errors.Is(err, ErrConflict) {
				t.Fatalf("healthy restart rewrite accepted: %v", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		installed, err := InspectInstalled(t.Context(), cfg, targetManifest)
		if err != nil || installed.InstanceID != state.InstanceID {
			t.Fatalf("healthy installed identity: %v", err)
		}
	})
}
