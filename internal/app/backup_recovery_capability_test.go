package app

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3/lock"
)

func authenticateDrillFixture(t *testing.T, f upgradeDrillFixture) *backupreceipt.AuthenticatedArchive {
	t.Helper()
	archive, err := backupreceipt.AuthenticateArchive(t.Context(), f.request.Ciphertext, f.request.Receipt, f.request.Plan, f.request.Unlock, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { archive.Close() })
	return archive
}

func TestRecoveryCapabilityExpiresWithOwnerAndArchive(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, expiry := range []string{"owner", "archive"} {
			t.Run(string(engine)+"/"+expiry, func(t *testing.T) {
				f := newUpgradeDrillFixture(t, engine, true, true)
				if _, err := DrillUpgrade(t.Context(), f.request); err != nil {
					t.Fatal(err)
				}
				archive := authenticateDrillFixture(t, f)
				cfg := f.request.Scratch
				gateCfg := upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}
				other := upgradeDrillDatabase(t, engine)
				var recovery *store.RecoveryDB
				err := upgrade.WithLock(t.Context(), gateCfg, func(session *upgrade.Session) error {
					authority, err := session.ScratchAdmission(t.Context(), archive, f.request.Plan)
					if err != nil {
						return err
					}
					if db, err := store.OpenRecovery(t.Context(), other, authority); err == nil {
						db.Close()
						t.Fatal("scratch authority admitted another physical database")
					}
					recovery, err = store.OpenRecovery(t.Context(), cfg, authority)
					if err != nil {
						return err
					}
					status, err := (&service.Recovery{DB: recovery}).Status(t.Context())
					if err != nil {
						return err
					}
					if !status.State.Restored() {
						t.Fatal("positive recovery read did not observe restore")
					}
					if err := (&keyring.RecoveryStore{DB: recovery}).CreateHierarchy(t.Context(), crypto.WrappedKey{}, nil); err == nil {
						t.Fatal("recovery minted missing hierarchy")
					}
					if expiry == "archive" {
						if err := archive.Close(); err != nil {
							return err
						}
						assertExpiredRecovery(t, recovery)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				defer recovery.Close()
				assertExpiredRecovery(t, recovery)
				if db, err := store.OpenRecovery(t.Context(), cfg, upgrade.RecoveryAdmission{}); err == nil {
					db.Close()
					t.Fatal("zero recovery authority admitted database")
				}
			})
		}
	}
}
func assertExpiredRecovery(t *testing.T, db *store.RecoveryDB) {
	t.Helper()
	called := false
	err := tx.RecoveryRead(t.Context(), db, func(context.Context, store.ReadRepos, *authz.TxAuthorizer) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("expired recovery read reached callback")
	}
	err = tx.RecoveryWrite(t.Context(), db, func(context.Context, store.Repos, *authz.TxAuthorizer) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("expired recovery write reached callback")
	}
}

func TestPostgresRestoreDestinationRefusesExpiredOwnerAndOccupiedTarget(t *testing.T) {
	for _, scenario := range []string{"expired owner", "occupied target", "expired archive", "wrong schema"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUpgradeDrillFixture(t, store.EnginePostgres, false, true)
			archive := authenticateDrillFixture(t, f)
			plain, err := archive.Open()
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := store.ReadManifest(plain)
			if err != nil {
				t.Fatal(err)
			}
			cfg := f.request.Scratch
			if err := migrate.RunUpTo(t.Context(), cfg, manifest.SchemaVersion); err != nil {
				t.Fatal(err)
			}
			native, err := pgx.Connect(t.Context(), cfg.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer native.Close(t.Context())
			if scenario == "wrong schema" {
				if _, err := native.Exec(t.Context(), "CREATE TABLE unexpected_restore_object(id INTEGER)"); err != nil {
					t.Fatal(err)
				}
			}
			var destination *store.RestoreDestination
			err = upgrade.WithLock(t.Context(), upgrade.Config{Engine: releaseidentity.Postgres, DSN: cfg.DSN}, func(session *upgrade.Session) error {
				authority, err := session.ValidateRestoreDestination(t.Context(), archive, f.request.Plan)
				if scenario == "wrong schema" {
					if err == nil {
						t.Fatal("destination admitted unsigned catalog")
					}
					return nil
				}
				if err != nil {
					return err
				}
				destination, err = store.OpenRestoreDestination(t.Context(), cfg, authority, archive, f.request.Plan)
				if err != nil {
					return err
				}
				if scenario == "expired owner" {
					return nil
				}
				if scenario == "occupied target" {
					if _, err := native.Exec(t.Context(), "INSERT INTO principals(id,kind,created_at) VALUES ('usr_keep','human',now())"); err != nil {
						return err
					}
				} else if err := archive.Close(); err != nil {
					return err
				}
				called := false
				_, err = tx.RestoreUpgradeDestinationPostgres(t.Context(), destination, func(context.Context, *authz.TxAuthorizer) error { called = true; return nil })
				if err == nil || called {
					t.Fatal("refused destination reached invalidation callback")
				}
				if scenario == "occupied target" {
					if !errors.Is(err, store.ErrTargetNotEmpty) {
						t.Fatal(err)
					}
					var count int
					if err := native.QueryRow(t.Context(), "SELECT count(*) FROM principals WHERE id='usr_keep'").Scan(&count); err != nil || count != 1 {
						t.Fatal("occupied target changed", err)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if destination != nil {
				defer destination.Close()
			}
			if scenario == "expired owner" {
				called := false
				_, err := tx.RestoreUpgradeDestinationPostgres(t.Context(), destination, func(context.Context, *authz.TxAuthorizer) error { called = true; return nil })
				if err == nil || called {
					t.Fatal("expired destination retained import authority")
				}
			}
		})
	}
}

func TestRestoreSchemaRechecksPreflightBeforeAnyGooseWrite(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EnginePostgres, false, true)
	archive := authenticateDrillFixture(t, f)
	plain, err := archive.Open()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := store.ReadManifest(plain)
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.request.Scratch
	if err := checkRestorable(t.Context(), cfg, manifest); err != nil {
		t.Fatal(err)
	}
	// A competing owner populated an older schema after that successful preflight.
	// Migration44 would remove retired fields; the refused source initializer must
	// leave both old schema and occupied identity untouched.
	if err := migrate.RunUpTo(t.Context(), cfg, 43); err != nil {
		t.Fatal(err)
	}
	native, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close(t.Context())
	if _, err := native.Exec(t.Context(), "INSERT INTO principals(id,kind,created_at) VALUES ('usr_keep','human',now())"); err != nil {
		t.Fatal(err)
	}
	before, err := upgrade.DomainCatalogPostgres(t.Context(), native)
	if err != nil {
		t.Fatal(err)
	}
	err = upgrade.WithLock(t.Context(), backupGateConfig(cfg), func(session *upgrade.Session) error {
		return session.ApplyRestoreSchema(t.Context(), f.request.Plan, store.MigrationsFS, "migrations/postgres")
	})
	if err == nil {
		t.Fatal("occupied target received restore schema migrations")
	}
	after, err := upgrade.DomainCatalogPostgres(t.Context(), native)
	if err != nil || before.Digest() != after.Digest() {
		t.Fatal("refused initialization changed exact schema/goose inventory", err)
	}
	var count int
	if err := native.QueryRow(t.Context(), "SELECT count(*) FROM principals WHERE id='usr_keep'").Scan(&count); err != nil || count != 1 {
		t.Fatal("refused initialization changed existing identity", err)
	}
}

func terminateRecoveryOwner(t *testing.T, native *pgx.Conn) {
	t.Helper()
	var killed bool
	err := native.QueryRow(t.Context(), `SELECT pg_terminate_backend(pid) FROM pg_locks
  WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
  AND classid=($1::bigint >> 32)::oid AND objid=($1::bigint & 4294967295)::oid
  AND objsubid=1 AND mode='ExclusiveLock' AND granted`, lock.DefaultLockID).Scan(&killed)
	if err != nil || !killed {
		t.Fatalf("terminate retained owner: %v", err)
	}
}

func TestRecoveryPostgresRollsBackWhenMigrationOwnerDies(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EnginePostgres, true, true)
	if _, err := DrillUpgrade(t.Context(), f.request); err != nil {
		t.Fatal(err)
	}
	archive := authenticateDrillFixture(t, f)
	cfg := f.request.Scratch
	native, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close(context.Background())
	reached := false
	err = upgrade.WithLock(t.Context(), backupGateConfig(cfg), func(session *upgrade.Session) error {
		authority, err := session.ScratchAdmission(t.Context(), archive, f.request.Plan)
		if err != nil {
			return err
		}
		db, err := store.OpenRecovery(t.Context(), cfg, authority)
		if err != nil {
			return err
		}
		defer db.Close()
		transaction, err := db.BeginPostgres(t.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return err
		}
		defer transaction.Rollback(context.Background())
		if _, err := transaction.Exec(t.Context(), "INSERT INTO principals(id,kind,created_at) VALUES ('usr_owner_died','human',now())"); err != nil {
			return err
		}
		terminateRecoveryOwner(t, native)
		if err := transaction.Commit(t.Context()); err == nil {
			t.Fatal("recovery committed after owner termination")
		}
		reached = true
		return nil
	})
	if err == nil || !reached {
		t.Fatalf("owner termination regression: reached=%v err=%v", reached, err)
	}
	var count int
	if err := native.QueryRow(t.Context(), "SELECT count(*) FROM principals WHERE id='usr_owner_died'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired recovery mutation persisted: count=%d err=%v", count, err)
	}
}

func TestRestorePostgresRollsBackWhenMigrationOwnerDies(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EnginePostgres, false, true)
	archive := authenticateDrillFixture(t, f)
	cfg := f.request.Scratch
	native, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close(context.Background())
	reached := false
	err = upgrade.WithLock(t.Context(), backupGateConfig(cfg), func(session *upgrade.Session) error {
		if err := session.ApplyRestoreSchema(t.Context(), f.request.Plan, store.MigrationsFS, "migrations/postgres"); err != nil {
			return err
		}
		authority, err := session.ValidateRestoreDestination(t.Context(), archive, f.request.Plan)
		if err != nil {
			return err
		}
		destination, err := store.OpenRestoreDestination(t.Context(), cfg, authority, archive, f.request.Plan)
		if err != nil {
			return err
		}
		defer destination.Close()
		_, err = tx.RestoreUpgradeDestinationPostgres(t.Context(), destination, func(context.Context, *authz.TxAuthorizer) error {
			terminateRecoveryOwner(t, native)
			reached = true
			return nil
		})
		if err == nil {
			t.Fatal("restore committed after owner termination")
		}
		return nil
	})
	if err == nil || !reached {
		t.Fatalf("owner termination regression: reached=%v err=%v", reached, err)
	}
	var count int
	if err := native.QueryRow(t.Context(), "SELECT count(*) FROM principals").Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired restore persisted principals: count=%d err=%v", count, err)
	}
}
