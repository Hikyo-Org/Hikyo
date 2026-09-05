package isolation

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

var isolationAdmissions sync.Map
var isolationCustody sync.Map

// openIsolationFixture builds a fresh instance through the signed development
// gate. Its hierarchy and every later keyring load share the same test custody.
func openIsolationFixture(t *testing.T, cfg store.Config) (*store.DB, error) {
	t.Helper()
	root, err := crypto.GenerateRootKey()
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(root)
	return openIsolationFixtureWithRoot(t, cfg, root)
}

func openIsolationFixtureWithRoot(t *testing.T, cfg store.Config, root []byte) (*store.DB, error) {
	t.Helper()
	authority, material := gatefixture.PrepareWithMaterial(t, upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, store.MigrationsFS, "migrations/"+string(cfg.Engine), bytes.Clone(root))
	db, err := store.Open(t.Context(), cfg, authority)
	if err != nil {
		return nil, err
	}
	isolationAdmissions.Store(db, authority)
	isolationCustody.Store(db, material)
	t.Cleanup(func() { isolationAdmissions.Delete(db); isolationCustody.Delete(db) })
	loadAndRegisterKeyring(t, db, bytes.Clone(root))
	return db, nil
}

func isolationAdmission(t *testing.T, db *store.DB) upgrade.Admission {
	t.Helper()
	value, ok := isolationAdmissions.Load(db)
	if !ok {
		t.Fatal("peer requires admission from the original signed gate")
	}
	authority, ok := value.(upgrade.Admission)
	if !ok {
		t.Fatal("invalid isolation admission registry entry")
	}
	return authority
}

// openBootedIsolationFixture repeats authenticated admission using the server's
// persisted custody. It cannot substitute a new trust root for a populated DB.
func openBootedIsolationFixture(t *testing.T, cfg *config.Config) (*store.DB, error) {
	t.Helper()
	root, err := crypto.ReadRootKey(cfg.RootKeyFile, "")
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(root)
	stateDir := cfg.Upgrade.StateDirectory
	if stateDir == "" {
		t.Fatal("boot fixture must explicitly name its development custody directory")
	}
	material, err := devupgrade.Open(t.Context(), stateDir)
	if err != nil {
		return nil, err
	}
	result, err := upgradegate.RunDevelopment(t.Context(), upgradegate.Request{
		Store:           upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN},
		BundleDirectory: material.Directory, Pinned: material.Pinned,
		Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(cfg.Store.Engine),
		Mode: upgradegate.Boot, AllowMigrations: cfg.AutoMigrate, RootKey: root,
	})
	if err != nil {
		return nil, err
	}
	db, err := store.Open(t.Context(), retentionStoreConfig(cfg), result.Admission)
	if err != nil {
		return nil, err
	}
	isolationAdmissions.Store(db, result.Admission)
	t.Cleanup(func() { isolationAdmissions.Delete(db) })
	return db, nil
}

func isolationCustodyDirectory(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
