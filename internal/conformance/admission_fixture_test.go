package conformance

import (
	"bytes"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

// admitConformanceFixture creates actual signed development authority and
// preserves its initialized hierarchy root for the complete scenario corpus.
func admitConformanceFixture(t *testing.T, cfg store.Config) (*store.DB, error) {
	t.Helper()
	root := newRoot(t)
	admission := gatefixture.Prepare(t, upgrade.Config{Engine: releaseidentity.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}, store.MigrationsFS, "migrations/"+string(cfg.Engine), bytes.Clone(root))
	db, err := store.Open(t.Context(), cfg, admission)
	if err != nil {
		crypto.Zero(root)
		return nil, err
	}
	rootMu.Lock()
	rootBytes[db] = root
	rootMu.Unlock()
	t.Cleanup(func() { rootMu.Lock(); delete(rootBytes, db); rootMu.Unlock(); crypto.Zero(root) })
	return db, nil
}
