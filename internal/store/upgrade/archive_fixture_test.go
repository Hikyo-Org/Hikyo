package upgrade_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
)

func testConfig(t *testing.T, engine releaseidentity.Engine) upgrade.Config {
	t.Helper()
	if engine == releaseidentity.SQLite {
		return upgrade.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "ledger.db")}
	}
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires PostgreSQL ledger acceptance")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ledger_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(t.Context(), `CREATE DATABASE "`+name+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := admin.ExecContext(context.Background(), `DROP DATABASE "`+name+`" WITH (FORCE)`)
		if err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	u.Path = "/" + name
	return upgrade.Config{Engine: engine, DSN: u.String()}
}

func both(t *testing.T, fn func(*testing.T, upgrade.Config)) {
	t.Helper()
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) { fn(t, testConfig(t, engine)) })
	}
}

func schemaConfig(cfg upgrade.Config) store.Config {
	return store.Config{Engine: store.Engine(cfg.Engine), Path: cfg.Path, DSN: cfg.DSN}
}

// The current root restore API checks table inventory before invoking its
// mutation callback. This fixture creates empty control tables first so the
// actual row import and authority reconciliation can be tested. F5 integration
// must move this same primitive into the owned restore transaction before load.
func prepareArchiveControlFixture(t *testing.T, cfg upgrade.Config, manifest releaseidentity.MigrationManifest, schema releaseidentity.Digest) {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if err := upgrade.PreparePostgresRestoreControlSchema(t.Context(), tx, manifest, schema); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}
