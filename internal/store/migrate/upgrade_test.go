package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The upgrade path old→new as one cross-engine E2E (mvp-boundary O1): a database
// one migration behind fails the schema check (a server would refuse to serve,
// fail-closed), data seeded on the old schema survives the upgrade, and the
// check passes afterwards (the server would serve). The fail-closed boot
// sequence itself is proven in internal/app; this pins the migrate-level
// mechanics on both engines, where app's sqlite-only boot tests are blind.

func TestUpgradeOldToNewSQLite(t *testing.T) {
	runUpgradeOldToNew(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "upgrade.db")})
}

func TestUpgradeOldToNewPostgres(t *testing.T) {
	// A dedicated scratch database so the partial-schema (N-1) state never
	// collides with other postgres legs sharing the server.
	runUpgradeOldToNew(t, postgresTestConfig(t, "upgrade"))
}

func TestMCPAuditOriginMigrationRollsBackSQLite(t *testing.T) {
	runMCPAuditOriginRollback(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "rollback.db")})
}

func TestMCPAuditOriginMigrationRollsBackPostgres(t *testing.T) {
	runMCPAuditOriginRollback(t, postgresTestConfig(t, "mcp_origin_rollback"))
}

func runMCPAuditOriginRollback(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 42); err != nil {
		t.Fatal(err)
	}
	if err := withProvider(ctx, cfg, func(provider *goose.Provider, _ *sql.DB) error {
		// Roll back through migration 42 specifically. Later migrations must not
		// turn this targeted compatibility proof into a rollback of the latest
		// unrelated schema change.
		_, err := provider.DownTo(ctx, 41)
		return err
	}); err != nil {
		t.Fatalf("rollback MCP audit origin migration: %v", err)
	}
	if err := insertMCPAuditOrigin(t, cfg, "evt_mcp_rollback_refused"); err == nil {
		t.Fatal("rolled-back schema still accepted the mcp audit origin")
	}
	if err := RunUpTo(ctx, cfg, 42); err != nil {
		t.Fatalf("reapply MCP audit origin migration: %v", err)
	}
	if err := insertMCPAuditOrigin(t, cfg, "evt_mcp_rollback_reapplied"); err != nil {
		t.Fatalf("reapplied schema refused the mcp audit origin: %v", err)
	}
}

// postgresTestConfig provisions a throwaway postgres database for a migration
// leg: it fails loud under CI when the DSN is unset, skips otherwise, creates a
// uniquely named scratch database, drops it on cleanup, and returns the config
// pointed at it (migration tests run migrate.Run against the config themselves).
func postgresTestConfig(t *testing.T, label string) store.Config {
	t.Helper()
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI run without HIKYO_TEST_POSTGRES_DSN: the postgres migration leg must not silently skip in CI")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimPrefix(parsed.Path, "/")
	database := fmt.Sprintf("%s_%s_%d", base, label, time.Now().UnixNano())
	admin, err := store.Open(t.Context(), store.Config{Engine: store.EnginePostgres, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.PG().Exec(context.Background(), `DROP DATABASE IF EXISTS "`+strings.ReplaceAll(database, `"`, ``)+`" WITH (FORCE)`)
		admin.Close()
	})
	if _, err := admin.PG().Exec(t.Context(), `CREATE DATABASE "`+strings.ReplaceAll(database, `"`, ``)+`"`); err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + database
	return store.Config{Engine: store.EnginePostgres, DSN: parsed.String()}
}

func runUpgradeOldToNew(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()

	max, err := MaxVersion(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunUpTo(ctx, cfg, max-1); err != nil {
		t.Fatalf("migrate to the old (N-1) schema: %v", err)
	}

	// Fail-closed: one migration behind, the schema check refuses. This is the
	// state a server boots into with auto-migrate off, and it must not serve.
	if pending, err := HasPending(ctx, cfg); err != nil || !pending {
		t.Fatalf("N-1 schema should report a pending migration: pending=%v err=%v", pending, err)
	}
	if err := Check(ctx, cfg); err == nil {
		t.Fatal("a database one migration behind must fail the schema check (fail-closed serving)")
	}

	// Seed a row on the OLD schema. orgs exists from the earliest migrations, so
	// it is present at N-1 and must survive the upgrade untouched.
	seed := "org_upgrade_survivor"
	execUpgrade(t, cfg, "INSERT INTO orgs (id, name, active, metadata, created_at) "+
		"VALUES ('"+seed+"', 'survivor', TRUE, '{}', '2026-01-01T00:00:00Z')")

	// Upgrade to the new schema.
	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("upgrade old→new: %v", err)
	}
	// Serves: exact schema match now.
	if err := Check(ctx, cfg); err != nil {
		t.Fatalf("after the upgrade the schema check must pass: %v", err)
	}
	// The new closed origin member is accepted on both audit trails' shared
	// envelope shape. This catches a Go-only vocabulary change with no matching
	// engine migration.
	execUpgrade(t, cfg, "INSERT INTO audit_instance_events "+
		"(id, type, schema_version, occurred_at, occurred_asserted, recorded_at, actor_class, outcome, origin, payload) "+
		"VALUES ('evt_mcp_upgrade', 'grant.denied', 1, '2026-01-01T00:00:00Z', FALSE, '2026-01-01T00:00:00Z', 'unauthenticated', 'denied', 'mcp', '{}')")
	// Data survived old→new.
	if n := countUpgrade(t, cfg, "SELECT COUNT(*) FROM orgs WHERE id = '"+seed+"'"); n != 1 {
		t.Fatalf("the org seeded on the old schema did not survive the upgrade: count=%d", n)
	}
}

func execUpgrade(t *testing.T, cfg store.Config, stmt string) {
	t.Helper()
	db, err := store.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Engine() == store.EnginePostgres {
		if _, err := db.PG().Exec(t.Context(), stmt); err != nil {
			t.Fatalf("seed exec: %v", err)
		}
		return
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), stmt); err != nil {
		t.Fatalf("seed exec: %v", err)
	}
}

func countUpgrade(t *testing.T, cfg store.Config, query string) int {
	t.Helper()
	db, err := store.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if db.Engine() == store.EnginePostgres {
		if err := db.PG().QueryRow(t.Context(), query).Scan(&n); err != nil {
			t.Fatalf("count query: %v", err)
		}
		return n
	}
	if err := db.SQLiteWrite().QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}

func insertMCPAuditOrigin(t *testing.T, cfg store.Config, id string) error {
	t.Helper()
	db, err := store.Open(t.Context(), cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	query := "INSERT INTO audit_instance_events " +
		"(id, type, schema_version, occurred_at, occurred_asserted, recorded_at, actor_class, outcome, origin, payload) " +
		"VALUES ('" + id + "', 'grant.denied', 1, '2026-01-01T00:00:00Z', FALSE, '2026-01-01T00:00:00Z', 'unauthenticated', 'denied', 'mcp', '{}')"
	if db.Engine() == store.EnginePostgres {
		_, err = db.PG().Exec(t.Context(), query)
		return err
	}
	_, err = db.SQLiteWrite().ExecContext(t.Context(), query)
	return err
}
