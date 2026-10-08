package service

import (
	"bytes"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

var serviceFixtureRoots sync.Map

// openServiceFixture admits a fresh database through the real signed gate.
// The gate initializes its hierarchy, so later keyring loads must use this
// exact root. The map stores test custody only, never admission authority.
func openServiceFixture(t testing.TB, cfg store.Config) (*store.DB, error) {
	t.Helper()
	db, _, err := openServiceFixtureAdmission(t, cfg)
	return db, err
}

// Benchmarks that restore a closed fixture must reuse its original signed
// admission, including the installation trust domain and release root.
func openServiceFixtureAdmission(t testing.TB, cfg store.Config) (*store.DB, upgrade.Admission, error) {
	t.Helper()
	root, err := crypto.GenerateRootKey()
	if err != nil {
		return nil, upgrade.Admission{}, err
	}
	admission := gatefixture.Prepare(t, upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, store.MigrationsFS, "migrations/"+string(cfg.Engine), bytes.Clone(root))
	db, err := openAdmittedServiceFixture(t, cfg, root, admission)
	return db, admission, err
}

func openAdmittedServiceFixture(t testing.TB, cfg store.Config, root []byte, admission upgrade.Admission) (*store.DB, error) {
	t.Helper()
	db, err := store.Open(t.Context(), cfg, admission)
	if err != nil {
		crypto.Zero(root)
		return nil, err
	}
	serviceFixtureRoots.Store(db, root)
	t.Cleanup(func() { serviceFixtureRoots.Delete(db); crypto.Zero(root) })
	return db, nil
}

func serviceFixtureRoot(t testing.TB, db *store.DB) []byte {
	t.Helper()
	value, ok := serviceFixtureRoots.Load(db)
	if !ok {
		t.Fatal("service keyring requires root from admitted fixture")
	}
	root, ok := value.([]byte)
	if !ok {
		t.Fatal("invalid service fixture custody")
	}
	return bytes.Clone(root)
}
