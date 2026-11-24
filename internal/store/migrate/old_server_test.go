package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Migrating a PostgreSQL older than 18 must be refused before any table exists.
func TestRunRefusesOldPostgresWithoutWriting(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_OLD_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires the PostgreSQL 17 refusal fixture")
		}
		t.Skip("HIKYO_TEST_POSTGRES_OLD_DSN not set")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("old_refusal_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(t.Context(), `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.WithoutCancel(t.Context()), `DROP DATABASE "`+name+`" WITH (FORCE)`)
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name

	err = Run(t.Context(), store.Config{Engine: store.EnginePostgres, DSN: u.String()})
	if err == nil || !strings.Contains(err.Error(), "requires PostgreSQL 18 or later") {
		t.Fatalf("migration not refused with the version message: %v", err)
	}
	target, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	var tables int
	if err := target.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("refused migration wrote %d tables", tables)
	}
}
