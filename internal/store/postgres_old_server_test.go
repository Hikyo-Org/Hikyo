package store

import (
	"os"
	"strings"
	"testing"
)

// The pool must refuse a PostgreSQL older than 18 at open, before serving.
func TestOpenPostgresRefusesOldServer(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_OLD_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires the PostgreSQL 17 refusal fixture")
		}
		t.Skip("HIKYO_TEST_POSTGRES_OLD_DSN not set")
	}
	db, err := openPostgres(t.Context(), dsn, 0)
	if err == nil {
		_ = db.Close()
		t.Fatal("pool opened against a PostgreSQL older than 18")
	}
	if !strings.Contains(err.Error(), "requires PostgreSQL 18 or later") {
		t.Fatalf("refusal does not name the requirement: %v", err)
	}
}
