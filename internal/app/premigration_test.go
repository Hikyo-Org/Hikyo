package app

// Upgrade admission replaces the retired best-effort export-and-skip path.
// Missing signed proof must refuse before DDL, even when recipients exist.

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// legacySchema creates actual pre-ledger domain tables, never runtime admission.
func legacySchema(t *testing.T, f *storeFixture) {
	t.Helper()
	if err := migrate.Run(t.Context(), f.sc); err != nil {
		t.Fatal(err)
	}
}

type storeFixture struct {
	sc  store.Config
	dir string
}

func newStoreFixture(t *testing.T) *storeFixture {
	t.Helper()
	dir := t.TempDir()
	return &storeFixture{
		sc:  store.Config{Engine: store.EngineSQLite, Path: filepath.Join(dir, "hikyo.db")},
		dir: dir,
	}
}

func countInstanceEvents(t *testing.T, sc store.Config, typ string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", store.SQLiteDSN(sc.Path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM audit_instance_events WHERE type = ?", typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func archiveCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".age") {
			n++
		}
	}
	return n
}

func TestMigrationRefusesUnverifiedExistingSchemaBeforeWrites(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			fixture := newStoreFixture(t)
			legacySchema(t, fixture)
			cfg := preMigrationConfig(fixture, filepath.Join(fixture.dir, "backups"), nil)
			if configured {
				cfg.BackupRecipients = []string{"configured-public-recipient"}
			}
			db, err := sql.Open("sqlite", store.SQLiteDSN(fixture.sc.Path))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			before, err := upgrade.DomainCatalogSQLite(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			if err := RunMigrate(t.Context(), cfg, quietLogger()); err == nil {
				t.Fatal("existing schema migrated without authenticated release trust and restore proof")
			}
			after, err := upgrade.DomainCatalogSQLite(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			if before.Digest() != after.Digest() {
				t.Fatal("refused migration changed domain schema")
			}
			if n := archiveCount(t, cfg.BackupDir); n != 0 {
				t.Fatalf("refusal created %d archives", n)
			}
			if n := countInstanceEvents(t, fixture.sc, "backup.export_skipped"); n != 0 {
				t.Fatal("refusal recorded a permitted backup skip")
			}
			if _, err := upgrade.InspectControl(t.Context(), upgrade.Config{Engine: "sqlite", Path: fixture.sc.Path}); !errors.Is(err, upgrade.ErrAbsent) {
				t.Fatalf("refusal wrote upgrade control: %v", err)
			}
		})
	}
}

func TestVerifiedMigrateSkipsBackupOnHealthyRestart(t *testing.T) {
	cfg := devConfig(t)
	srv, err := Boot(t.Context(), cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	cfg.BackupDir = filepath.Join(filepath.Dir(cfg.Store.Path), "backups")
	if err := RunMigrate(t.Context(), cfg, quietLogger()); err != nil {
		t.Fatal(err)
	}
	if n := archiveCount(t, cfg.BackupDir); n != 0 {
		t.Fatalf("idle migrate created %d archives", n)
	}
}

func quietLogger() *slog.Logger { return testLogger() }

// preMigrationConfig is the operator configuration the hook reads: a datastore,
// a destination, and a recipient set that may deliberately be empty.
func preMigrationConfig(f *storeFixture, backupDir string, recipients []string) *config.Config {
	return &config.Config{
		Store:            config.Datastore{Engine: config.EngineSQLite, Path: f.sc.Path},
		AutoMigrate:      true,
		BackupDir:        backupDir,
		BackupRecipients: recipients,
	}
}

// MinRestoreSchemaVersion is a hand-written pin on a migration number, and
// this repo renumbers migrations when parallel tickets land. A desync fails
// in the WRONG direction — a too-low pin admits archives missing the restore
// state — so the pin is asserted against the migration files themselves.
func TestMinRestoreSchemaVersionMatchesTheMigration(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		entries, err := store.MigrationsFS.ReadDir("migrations/" + engine)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), "_restore_reconciliation.sql") {
				continue
			}
			found = true
			var version int64
			if _, err := fmt.Sscanf(e.Name(), "%d_", &version); err != nil {
				t.Fatalf("%s/%s: unparseable migration number: %v", engine, e.Name(), err)
			}
			if version != MinRestoreSchemaVersion {
				t.Errorf("%s restore_reconciliation migration is %05d but MinRestoreSchemaVersion = %d — renumbered without re-pinning",
					engine, version, MinRestoreSchemaVersion)
			}
		}
		if !found {
			t.Errorf("%s has no restore_reconciliation migration to pin against", engine)
		}
	}
}
