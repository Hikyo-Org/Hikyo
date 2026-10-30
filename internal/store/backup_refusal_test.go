package store

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRestoreOnlyReplacesUntouchedRefusalSeed(t *testing.T) {
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
	for _, change := range []string{"", "window_start=60", `state='{"counts":[1,0,0,0,0],"samples":[]}'`, `state='{"counts":[0,0,0,0,0],"samples":[{}]}'`} {
		t.Run(change, func(t *testing.T) {
			transaction, err := conn.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.Rollback(t.Context())
			// Shadow only this test's table, without touching installation data.
			_, err = transaction.Exec(t.Context(), `CREATE TEMP TABLE auth_refusal_window(id INTEGER PRIMARY KEY,window_start BIGINT NOT NULL,state TEXT NOT NULL) ON COMMIT DROP; INSERT INTO auth_refusal_window VALUES(1,0,'{"counts":[0,0,0,0,0],"samples":[]}')`)
			if err != nil {
				t.Fatal(err)
			}
			if change != "" {
				if _, err := transaction.Exec(t.Context(), "UPDATE auth_refusal_window SET "+change); err != nil {
					t.Fatal(err)
				}
			}
			err = assertOnlyMigrationSeeds(t.Context(), transaction, []string{"auth_refusal_window"})
			if change == "" && err != nil {
				t.Fatal(err)
			}
			if change != "" && !errors.Is(err, ErrTargetNotEmpty) {
				t.Fatalf("refusal evidence accepted for overwrite: %v", err)
			}
		})
	}
}
