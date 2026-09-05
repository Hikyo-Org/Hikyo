package store

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRestoreOnlyReplacesUntouchedDiagnosticsSeed(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires PostgreSQL restore seed acceptance")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	for _, change := range []string{"", "escrow_verified_at=now()", "escrow_instance_id='recorded'", "escrow_incarnation='recorded'", "escrow_root_epoch=1", "last_reencrypt_success=now()"} {
		t.Run(change, func(t *testing.T) {
			transaction, err := conn.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.Rollback(t.Context())
			// A transaction-local temporary table shadows public state without touching
			// any real installation rows in the caller's exclusively owned test DB.
			_, err = transaction.Exec(t.Context(), `CREATE TEMP TABLE ops_diagnostics(singleton INTEGER PRIMARY KEY,escrow_verified_at TIMESTAMPTZ,escrow_instance_id TEXT NOT NULL DEFAULT '',escrow_incarnation TEXT NOT NULL DEFAULT '',escrow_root_epoch BIGINT NOT NULL DEFAULT 0,last_reencrypt_success TIMESTAMPTZ) ON COMMIT DROP; INSERT INTO ops_diagnostics(singleton) VALUES(1)`)
			if err != nil {
				t.Fatal(err)
			}
			if change != "" {
				if _, err := transaction.Exec(t.Context(), "UPDATE ops_diagnostics SET "+change); err != nil {
					t.Fatal(err)
				}
			}
			err = assertOnlyMigrationSeeds(t.Context(), transaction, []string{"ops_diagnostics"})
			if change == "" && err != nil {
				t.Fatal(err)
			}
			if change != "" && !errors.Is(err, ErrTargetNotEmpty) {
				t.Fatalf("non-seed diagnostics accepted: %v", err)
			}
		})
	}
}
