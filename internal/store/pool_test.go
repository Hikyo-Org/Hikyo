package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSQLiteCapsConnectionPools(t *testing.T) {
	db, err := admittedStoreFixture(t, Config{Engine: EngineSQLite, Path: filepath.Join(t.TempDir(), "pool.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := db.SQLiteWrite().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("write pool maximum = %d, want 1", got)
	}
	if got := db.SQLiteRead().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("read pool maximum = %d, want 4", got)
	}
	if got := db.ConnectionPoolLimits(); got != (ConnectionPoolLimits{Primary: 1, ReadOnly: 4}) {
		t.Fatalf("ConnectionPoolLimits() = %+v, want primary 1 and read-only 4", got)
	}
}

func TestOpenPostgresRejectsNegativePoolMaximum(t *testing.T) {
	_, err := openConfigured(t.Context(), Config{
		Engine:          EnginePostgres,
		DSN:             "postgres://u:p@localhost/hikyo",
		PostgresPoolMax: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "pool maximum must be positive") {
		t.Fatalf("Open() error = %v, want pool maximum refusal", err)
	}
}
