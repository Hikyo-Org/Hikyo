package upgrade_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
	"github.com/jackc/pgx/v5"
)

func TestSameArchiveRestoresNewIncarnationsBeforePublication(t *testing.T) {
	both(t, func(t *testing.T, cfg upgrade.Config) {
		admission, material := gatefixture.PrepareWithMaterial(t, cfg, store.MigrationsFS, "migrations/"+string(cfg.Engine), bytes.Repeat([]byte{42}, 32))
		original, err := upgrade.InspectControl(t.Context(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := upgrade.PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := upgradebundle.Load(t.Context(), material.Directory, material.Pinned, original.Floor)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := bundle.Plan(upgradecompat.InstalledSource{Identity: original.Applied, Migrations: manifest, SchemaSHA256: original.SchemaDigest}, original.Applied.Release)
		if err != nil {
			t.Fatal(err)
		}
		source, err := store.Open(t.Context(), schemaConfig(cfg), admission)
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		var archive bytes.Buffer
		if _, err := store.Export(t.Context(), source, &archive, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		var restorations []upgrade.State
		for range 2 {
			targetCfg := testConfig(t, cfg.Engine)
			var restored upgrade.State
			if cfg.Engine == releaseidentity.SQLite {
				_, err = store.RestoreSQLite(t.Context(), bytes.NewReader(archive.Bytes()), targetCfg.Path, func(ctx context.Context, tx *sql.Tx) error {
					// F2 consumes the existing resolver's committed-in-this-tx
					// epoch result. The resolver's max-known scan is unchanged.
					if _, err := tx.ExecContext(ctx, `UPDATE auth_instance_state SET credential_epoch=99,restore_epoch=99 WHERE id=1`); err != nil {
						return err
					}
					return upgrade.ReconcileSQLiteRestoreIfPresent(ctx, tx)
				})
			} else {
				if err := migrate.Run(t.Context(), schemaConfig(targetCfg)); err != nil {
					t.Fatal(err)
				}
				// Recovery schema construction is not ledger bootstrap: imported
				// rows arrive together with the new authority domain transaction.
				prepareArchiveControlFixture(t, targetCfg, manifest, original.SchemaDigest)
				err = upgrade.WithLock(t.Context(), targetCfg, func(session *upgrade.Session) error {
					authority, err := session.ValidateDataRestoreDestination(t.Context(), plan)
					if err != nil {
						return err
					}
					destination, err := store.OpenDataRestoreDestination(t.Context(), schemaConfig(targetCfg), authority, plan)
					if err != nil {
						return err
					}
					defer destination.Close()
					_, err = destination.RestorePostgres(t.Context(), bytes.NewReader(archive.Bytes()), func(ctx context.Context, tx pgx.Tx) error {
						if _, err := tx.Exec(ctx, `UPDATE auth_instance_state SET credential_epoch=99,restore_epoch=99 WHERE id=1`); err != nil {
							return err
						}
						return upgrade.ReconcilePostgresRestoreIfPresent(ctx, tx)
					})
					return err
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			restored, err = upgrade.InspectControl(t.Context(), targetCfg)
			if err != nil {
				t.Fatal(err)
			}
			if restored.RestoreEpoch != 99 || restored.Generation != 2 || restored.RecoveryIncarnation == original.RecoveryIncarnation || !restored.Pending.Invalidated || restored.Pending.Phase != upgrade.RestoreRequired || !restored.Maintenance || restored.TrustDomain != original.TrustDomain {
				t.Fatalf("restored authority=%+v", restored)
			}
			err = upgrade.WithLock(t.Context(), targetCfg, func(s *upgrade.Session) error {
				read, err := s.Read(t.Context())
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(read, restored) {
					t.Fatal("published state differs from recovery transaction")
				}
				if _, err := s.Resume(t.Context(), original); !errors.Is(err, upgrade.ErrConflict) {
					t.Fatal("archive operation resumed")
				}
				if _, err := s.Resume(t.Context(), restored); !errors.Is(err, upgrade.ErrConflict) {
					t.Fatal("invalidated operation resumed")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			restorations = append(restorations, restored)
		}
		if restorations[0].RecoveryIncarnation == restorations[1].RecoveryIncarnation {
			t.Fatal("identical archive reused incarnation")
		}
	})
}

func TestRestoreMutationFailureDoesNotPublishSQLite(t *testing.T) {
	cfg := testConfig(t, releaseidentity.SQLite)
	admission := gatefixture.Prepare(t, cfg, store.MigrationsFS, "migrations/"+string(cfg.Engine), bytes.Repeat([]byte{42}, 32))
	source, err := store.Open(t.Context(), schemaConfig(cfg), admission)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var archive bytes.Buffer
	if _, err := store.Export(t.Context(), source, &archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "refused.db")
	_, err = store.RestoreSQLite(t.Context(), bytes.NewReader(archive.Bytes()), path, func(ctx context.Context, tx *sql.Tx) error {
		// No credential advance: a restored record alone never grants authority.
		_, err = upgrade.ReconcileSQLiteRestore(ctx, tx)
		return err
	})
	if err == nil {
		t.Fatal("restore without strongest epoch advance accepted")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("failed mutation published destination")
	}
}
