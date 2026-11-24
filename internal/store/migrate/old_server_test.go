package migrate

import (
	"strings"
	"testing"
)

// Migrating a PostgreSQL older than 18 must be refused before any table exists.
func TestRunRefusesOldPostgresWithoutWriting(t *testing.T) {
	cfg := postgresTestConfigFrom(t, "HIKYO_TEST_POSTGRES_OLD_DSN", "old_refusal")
	err := Run(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), "requires PostgreSQL 18 or later") {
		t.Fatalf("migration not refused with the version message: %v", err)
	}
	if tables := countUpgrade(t, cfg, `SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema')`); tables != 0 {
		t.Fatalf("refused migration wrote %d tables", tables)
	}
}
