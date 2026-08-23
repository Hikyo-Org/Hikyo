package store_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

var errRollbackKeyTest = errors.New("rollback key-store test fixture")

func TestKeyRotationInvariantsSQLite(t *testing.T) {
	cfg := store.Config{
		Engine: store.EngineSQLite,
		Path:   filepath.Join(t.TempDir(), "keys.db"),
	}
	runKeyRotationInvariants(t, openKeyTestDB(t, cfg))
}

func TestKeyRotationInvariantsPostgres(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI run without HIKYO_TEST_POSTGRES_DSN: the postgres key-store leg must not silently skip")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}

	admin, cfg := postgresKeyTestConfig(t, dsn)
	t.Cleanup(func() {
		_, _ = admin.PG().Exec(context.Background(), `DROP DATABASE IF EXISTS "`+
			strings.ReplaceAll(strings.TrimPrefix(mustParseURL(t, cfg.DSN).Path, "/"), `"`, ``)+`" WITH (FORCE)`)
		admin.Close()
	})
	runKeyRotationInvariants(t, openKeyTestDB(t, cfg))
}

func runKeyRotationInvariants(t *testing.T, db *store.DB) {
	t.Helper()
	seedKeyTestOperator(t, db)

	for _, purpose := range []crypto.Purpose{crypto.PurposeToken, crypto.PurposeScanning} {
		purpose := purpose
		t.Run("stale_"+string(purpose)+"_predecessor", func(t *testing.T) {
			withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
				mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1)}, []crypto.WrappedKey{tier3Key(purpose, 1, 1)})
				stale := tier3Key(purpose, 3, 1)
				var err error
				if purpose == crypto.PurposeToken {
					err = keys.RotateTokenKey(ctx, proofs.token, stale)
				} else {
					err = keys.RotateScanningKey(ctx, proofs.scanning, stale)
				}
				if !errors.Is(err, store.ErrRotationSuperseded) {
					t.Fatalf("stale %s rotation error = %v, want ErrRotationSuperseded", purpose, err)
				}
			})
		})
	}

	t.Run("stale_dek_predecessor", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1)}, []crypto.WrappedKey{projectKey(1, 1)})
			if err := keys.RotateDEK(ctx, proofs.dek, projectKey(3, 1)); !errors.Is(err, store.ErrRotationSuperseded) {
				t.Fatalf("stale DEK rotation error = %v, want ErrRotationSuperseded", err)
			}
		})
	})

	t.Run("master_rotation_refuses_dual_wrap", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1), masterKey(1, 2)}, nil)
			if err := keys.RotateMasterKey(ctx, proofs.master, masterKey(2, 2), nil); !errors.Is(err, crypto.ErrMasterRotationBlocked) {
				t.Fatalf("dual-wrapped master rotation error = %v, want ErrMasterRotationBlocked", err)
			}
		})
	})

	t.Run("root_prepare_refuses_master_version_mismatch", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1)}, nil)
			if err := keys.RootKeyRotatePrepare(ctx, proofs.root, masterKey(2, 2)); !errors.Is(err, crypto.ErrRootRotationBlocked) {
				t.Fatalf("mismatched root prepare error = %v, want ErrRootRotationBlocked", err)
			}
		})
	})

	t.Run("root_finalize_requires_dual_wrap", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1)}, nil)
			if _, err := keys.RootKeyRotateFinalize(ctx, proofs.root); !errors.Is(err, crypto.ErrNotDualWrapped) {
				t.Fatalf("single-wrapper finalize error = %v, want ErrNotDualWrapped", err)
			}
		})
	})

	t.Run("root_finalize_keeps_newest_epoch", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1), masterKey(1, 2)}, nil)
			epoch, err := keys.RootKeyRotateFinalize(ctx, proofs.root)
			if err != nil {
				t.Fatalf("finalize dual-wrapped root: %v", err)
			}
			if epoch != 2 {
				t.Fatalf("finalized root epoch = %d, want 2", epoch)
			}
			wrappers, err := keys.ActiveMasterWrappers(ctx, proofs.boot)
			if err != nil {
				t.Fatalf("read finalized master wrappers: %v", err)
			}
			if len(wrappers) != 1 || wrappers[0].RootKeyEpoch != 2 {
				t.Fatalf("active wrappers after finalize = %+v, want only epoch 2", wrappers)
			}
		})
	})

	t.Run("master_rotation_refuses_stranded_tier3", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1)}, []crypto.WrappedKey{projectKey(1, 1)})
			if err := keys.RotateMasterKey(ctx, proofs.master, masterKey(2, 1), nil); !errors.Is(err, store.ErrRotationSuperseded) {
				t.Fatalf("stranded tier-3 rotation error = %v, want ErrRotationSuperseded", err)
			}
		})
	})

	t.Run("duplicate_master_maps_to_key_exists", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			key := masterKey(1, 1)
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{key}, nil)
			if err := keys.InsertMaster(ctx, proofs.boot, key); !errors.Is(err, crypto.ErrKeyExists) {
				t.Fatalf("duplicate master error = %v, want ErrKeyExists", err)
			}
		})
	})

	t.Run("duplicate_tier3_maps_to_key_exists", func(t *testing.T) {
		withKeyRepo(t, db, func(ctx context.Context, keys store.KeyRepo, proofs keyTestProofs) {
			key := tier3Key(crypto.PurposeToken, 1, 1)
			mustInsertKeyFixture(t, ctx, keys, proofs.boot, []crypto.WrappedKey{masterKey(1, 1)}, []crypto.WrappedKey{key})
			if err := keys.InsertTier3(ctx, proofs.boot, key); !errors.Is(err, crypto.ErrKeyExists) {
				t.Fatalf("duplicate tier-3 error = %v, want ErrKeyExists", err)
			}
		})
	})
}

type keyTestProofs struct {
	boot     authz.Proof
	token    authz.Proof
	scanning authz.Proof
	dek      authz.Proof
	master   authz.Proof
	root     authz.Proof
}

func withKeyRepo(t *testing.T, db *store.DB, test func(context.Context, store.KeyRepo, keyTestProofs)) {
	t.Helper()
	err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
		proofs, err := mintKeyTestProofs(ctx, az)
		if err != nil {
			return err
		}
		test(ctx, repos.Keys(), proofs)
		return errRollbackKeyTest
	})
	if !errors.Is(err, errRollbackKeyTest) {
		t.Fatalf("key-store test transaction: %v", err)
	}
}

func mintKeyTestProofs(ctx context.Context, az *authz.TxAuthorizer) (keyTestProofs, error) {
	boot, err := authz.SystemAuthority(authz.SiteBoot, az.Token())
	if err != nil {
		return keyTestProofs{}, err
	}
	authorize := func(op authz.Operation) (authz.Proof, error) {
		return az.Authorize(ctx, authz.Identity{Principal: "usr_keys"}, op, domain.Scope{})
	}
	token, err := authorize(authz.OpRotateTokenKey)
	if err != nil {
		return keyTestProofs{}, err
	}
	scanning, err := authorize(authz.OpRotateScanningKey)
	if err != nil {
		return keyTestProofs{}, err
	}
	dek, err := authorize(authz.OpRotateDEK)
	if err != nil {
		return keyTestProofs{}, err
	}
	master, err := authorize(authz.OpRotateMasterKey)
	if err != nil {
		return keyTestProofs{}, err
	}
	root, err := authorize(authz.OpRotateRootKey)
	if err != nil {
		return keyTestProofs{}, err
	}
	return keyTestProofs{boot: boot, token: token, scanning: scanning, dek: dek, master: master, root: root}, nil
}

func seedKeyTestOperator(t *testing.T, db *store.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO principals (id, kind, created_at) VALUES ('usr_keys', 'human', '2026-08-23T12:00:00Z')`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
		 VALUES ('grt_keys_dek', 'usr_keys', 'rotate-dek', NULL, NULL, NULL, '2026-08-23T12:00:00Z')`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
		 VALUES ('grt_keys_master', 'usr_keys', 'rotate-master-key', NULL, NULL, NULL, '2026-08-23T12:00:00Z')`,
		`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at)
		 VALUES ('grt_keys_root', 'usr_keys', 'rotate-root-key', NULL, NULL, NULL, '2026-08-23T12:00:00Z')`,
	}
	for _, statement := range statements {
		var err error
		if db.Engine() == store.EnginePostgres {
			_, err = db.PG().Exec(t.Context(), statement)
		} else {
			_, err = db.SQLiteWrite().ExecContext(t.Context(), statement)
		}
		if err != nil {
			t.Fatalf("seed key-store operator: %v", err)
		}
	}
}

func mustInsertKeyFixture(t *testing.T, ctx context.Context, keys store.KeyRepo, proof authz.Proof, masters, tier3 []crypto.WrappedKey) {
	t.Helper()
	if err := keys.AcquireHierarchyGeneration(ctx, proof); err != nil {
		t.Fatalf("acquire hierarchy fixture fence: %v", err)
	}
	for _, key := range masters {
		if err := keys.InsertMaster(ctx, proof, key); err != nil {
			t.Fatalf("insert master fixture: %v", err)
		}
	}
	for _, key := range tier3 {
		if err := keys.InsertTier3(ctx, proof, key); err != nil {
			t.Fatalf("insert tier-3 fixture: %v", err)
		}
		if err := keys.InsertScopeGeneration(ctx, proof, key.Purpose, key.OrgID, key.ProjectID); err != nil {
			t.Fatalf("insert scope-generation fixture: %v", err)
		}
	}
}

func masterKey(version, epoch uint32) crypto.WrappedKey {
	return crypto.WrappedKey{
		Version: version, RootKeyEpoch: epoch,
		Blob: []byte(fmt.Sprintf("master-v%d-e%d", version, epoch)), CreatedAt: keyTestTime(),
	}
}

func tier3Key(purpose crypto.Purpose, version, masterVersion uint32) crypto.WrappedKey {
	return crypto.WrappedKey{
		ID: fmt.Sprintf("key-%s-v%d", purpose, version), Purpose: purpose,
		Version: version, MasterKeyVersion: masterVersion,
		Blob: []byte(fmt.Sprintf("%s-v%d", purpose, version)), CreatedAt: keyTestTime(),
	}
}

func projectKey(version, masterVersion uint32) crypto.WrappedKey {
	key := tier3Key(crypto.PurposeProject, version, masterVersion)
	key.OrgID = "org_keys"
	key.ProjectID = "prj_keys"
	return key
}

func keyTestTime() time.Time {
	return time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
}

func openKeyTestDB(t *testing.T, cfg store.Config) *store.DB {
	t.Helper()
	if err := migrate.Run(t.Context(), cfg); err != nil {
		t.Fatalf("migrate key-store database: %v", err)
	}
	db, err := store.Open(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open key-store database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func postgresKeyTestConfig(t *testing.T, dsn string) (*store.DB, store.Config) {
	t.Helper()
	parsed := mustParseURL(t, dsn)
	base := strings.TrimPrefix(parsed.Path, "/")
	if base == "" {
		t.Fatal("postgres test DSN has no database name")
	}
	database := fmt.Sprintf("%s_store_keys_%d", base, time.Now().UnixNano())
	admin, err := store.Open(t.Context(), store.Config{Engine: store.EnginePostgres, DSN: dsn})
	if err != nil {
		t.Fatalf("open postgres admin database: %v", err)
	}
	if _, err := admin.PG().Exec(t.Context(), `CREATE DATABASE "`+strings.ReplaceAll(database, `"`, ``)+`"`); err != nil {
		admin.Close()
		t.Fatalf("create postgres key-store database: %v", err)
	}
	parsed.Path = "/" + database
	return admin, store.Config{Engine: store.EnginePostgres, DSN: parsed.String()}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse postgres DSN: %v", err)
	}
	return parsed
}
