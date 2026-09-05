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
	"github.com/jackc/pgx/v5"
)

func TestSameArchiveRestoresNewIncarnationsBeforePublication(t *testing.T) {
	both(t, func(t *testing.T, cfg upgrade.Config) {
		if err := migrate.Run(t.Context(), schemaConfig(cfg)); err != nil {
			t.Fatal(err)
		}
		manifest, err := upgrade.PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		var original upgrade.State
		err = upgrade.WithLock(t.Context(), cfg, func(s *upgrade.Session) error {
			var err error
			original, err = s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), upgrade.Production)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		source, err := store.Open(t.Context(), schemaConfig(cfg))
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
				_, err = store.RestoreSQLite(t.Context(), bytes.NewReader(archive.Bytes()), targetCfg.Path, func(ctx context.Context, db *store.DB) error {
					tx, err := db.SQLiteWrite().BeginTx(ctx, nil)
					if err != nil {
						return err
					}
					defer tx.Rollback()
					// F2 consumes the existing resolver's committed-in-this-tx
					// epoch result. The resolver's max-known scan is unchanged.
					if _, err := tx.ExecContext(ctx, `UPDATE auth_instance_state SET credential_epoch=99,restore_epoch=99 WHERE id=1`); err != nil {
						return err
					}
					restored, err = upgrade.ReconcileSQLiteRestore(ctx, tx)
					if err != nil {
						return err
					}
					return tx.Commit()
				})
			} else {
				if err := migrate.Run(t.Context(), schemaConfig(targetCfg)); err != nil {
					t.Fatal(err)
				}
				// Recovery schema construction is not ledger bootstrap: imported
				// rows arrive together with the new authority domain transaction.
				prepareArchiveControlFixture(t, targetCfg, manifest, original.SchemaDigest)
				destination, openErr := store.Open(t.Context(), schemaConfig(targetCfg))
				if openErr != nil {
					t.Fatal(openErr)
				}
				_, err = store.RestorePostgres(t.Context(), destination, bytes.NewReader(archive.Bytes()), func(ctx context.Context, tx pgx.Tx) error {
					if _, err := tx.Exec(ctx, `UPDATE auth_instance_state SET credential_epoch=99,restore_epoch=99 WHERE id=1`); err != nil {
						return err
					}
					var err error
					restored, err = upgrade.ReconcilePostgresRestore(ctx, tx)
					return err
				})
				destination.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if restored.RestoreEpoch != 99 || restored.Generation != 2 || restored.RecoveryIncarnation == original.RecoveryIncarnation || !restored.Pending.Invalidated || restored.Pending.Phase != upgrade.RestoreRequired || !restored.Maintenance || restored.TrustDomain != upgrade.Production {
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
	if err := migrate.Run(t.Context(), schemaConfig(cfg)); err != nil {
		t.Fatal(err)
	}
	manifest, err := upgrade.PinnedLegacyManifest(cfg.Engine)
	if err != nil {
		t.Fatal(err)
	}
	err = upgrade.WithLock(t.Context(), cfg, func(s *upgrade.Session) error {
		_, err := s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), upgrade.Production)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.Open(t.Context(), schemaConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var archive bytes.Buffer
	if _, err := store.Export(t.Context(), source, &archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "refused.db")
	_, err = store.RestoreSQLite(t.Context(), bytes.NewReader(archive.Bytes()), path, func(ctx context.Context, db *store.DB) error {
		tx, err := db.SQLiteWrite().BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			return err
		}
		defer tx.Rollback()
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
