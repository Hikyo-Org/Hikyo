package store

import (
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
	"testing"
)

func admittedStoreFixture(t testing.TB, cfg Config) (*DB, error) {
	t.Helper()
	root, err := crypto.GenerateRootKey()
	if err != nil {
		return nil, err
	}
	admission := gatefixture.Prepare(t, upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, MigrationsFS, "migrations/"+string(cfg.Engine), root)
	return Open(t.Context(), cfg, admission)
}
