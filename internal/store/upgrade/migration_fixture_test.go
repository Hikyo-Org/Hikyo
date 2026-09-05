package upgrade

import (
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)

// migrateFixture constructs the sole documented pre-gate source in an owned
// test database. Reading unchanged repository SQL here avoids a package cycle
// when runtime store and archive export import the upgrade authority boundary.
// Production migration bytes remain embedded, immutable gate input.
func migrateFixture(t *testing.T, cfg Config) error {
	t.Helper()
	db, err := open(cfg, false)
	if err != nil {
		return err
	}
	defer db.Close()
	files, err := fs.Sub(os.DirFS(".."), "migrations/"+string(cfg.Engine))
	if err != nil {
		return err
	}
	dialect := database.DialectSQLite3
	opts := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true)}
	if cfg.Engine == releaseidentity.Postgres {
		dialect = database.DialectPostgres
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return err
		}
		opts = append(opts, goose.WithSessionLocker(locker))
	}
	provider, err := goose.NewProvider(dialect, db, files, opts...)
	if err != nil {
		return err
	}
	// This helper models the immutable pre-gate installation, not whatever
	// migrations a later candidate appends. Verify its prefix bytes before use.
	legacy, err := PinnedLegacyManifest(cfg.Engine)
	if err != nil {
		return err
	}
	current, err := releaseidentity.BuildMigrationManifest(os.DirFS(".."), "migrations/"+string(cfg.Engine), cfg.Engine)
	if err != nil {
		return err
	}
	if len(current.Entries) < len(legacy.Entries) || len(legacy.Entries) == 0 {
		return fmt.Errorf("legacy fixture migration prefix missing")
	}
	for i, expected := range legacy.Entries {
		if current.Entries[i] != expected {
			return fmt.Errorf("legacy fixture migration prefix changed at %d", i)
		}
	}
	_, err = provider.UpTo(t.Context(), int64(legacy.Entries[len(legacy.Entries)-1].Version))
	return err
}
