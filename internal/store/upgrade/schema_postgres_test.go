package upgrade

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

// This exercises the catalog reader used by admission, over one unchanged schema.
func TestPostgresCatalogCanonicalRendering(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	db, err := open(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), `CREATE TYPE choice AS ENUM('a'); CREATE TABLE rendering_probe (
 at timestamptz DEFAULT '1970-01-01T00:00:00Z', day date DEFAULT '2001-02-03',
 duration interval DEFAULT '1 year 2 months 3 days 04:05:06',
 precise double precision DEFAULT '1.2345678901234567'::double precision,
 payload bytea DEFAULT '\x012345'::bytea, choice choice DEFAULT 'a', s text DEFAULT E'a\\b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), `SET timezone='UTC'`); err != nil {
		t.Fatal(err)
	}
	baseline, err := inspectCatalog(t.Context(), conn, releaseidentity.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []struct{ name, value string }{
		{"TimeZone", "Europe/Amsterdam"}, {"TimeZone", "America/New_York"},
		{"DateStyle", "SQL, DMY"}, {"IntervalStyle", "iso_8601"},
		{"extra_float_digits", "-3"}, {"bytea_output", "escape"},
		{"search_path", "pg_catalog"}, {"quote_all_identifiers", "on"},
		{"standard_conforming_strings", "off"},
	} {
		t.Run(setting.name+"/"+setting.value, func(t *testing.T) {
			if _, err := conn.ExecContext(t.Context(), `SET timezone='UTC'`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(t.Context(), `SELECT set_config($1,$2,false)`, setting.name, setting.value); err != nil {
				t.Fatal(err)
			}
			defer conn.ExecContext(context.Background(), "RESET ALL")
			actual, err := inspectCatalog(t.Context(), conn, releaseidentity.Postgres)
			if err != nil {
				t.Fatal(err)
			}
			if actual.Digest() != baseline.Digest() {
				t.Fatalf("unchanged catalog differs under %s=%s: %s != %s", setting.name, setting.value, actual.Digest(), baseline.Digest())
			}
			var after string
			if err := conn.QueryRowContext(t.Context(), `SELECT current_setting($1)`, setting.name).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != setting.value {
				t.Fatalf("reader leaked setting %s: got %s want %s", setting.name, after, setting.value)
			}
		})
	}
	// Temporary types must not change public type visibility while rendering.
	if _, err := conn.ExecContext(t.Context(), `CREATE TEMP TABLE choice(id integer)`); err != nil {
		t.Fatal(err)
	}
	shadowed, err := inspectCatalog(t.Context(), conn, releaseidentity.Postgres)
	if err != nil || shadowed.Digest() != baseline.Digest() {
		t.Fatalf("temporary type changed domain catalog: %v", err)
	}
}

func TestPostgresCatalogPreservesCallerTransaction(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	db, err := open(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE transaction_probe(id integer, at timestamptz DEFAULT '1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	for _, adapter := range []string{"sql-transaction", "sql-explicit-begin"} {
		t.Run(adapter, func(t *testing.T) {
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var q queryer = conn
			exec := conn.ExecContext
			row := conn.QueryRowContext
			commit := func() error { _, err := conn.ExecContext(t.Context(), "COMMIT"); return err }
			rollback := func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }
			if adapter == "sql-transaction" {
				tx, err := conn.BeginTx(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				q, exec, row = tx, tx.ExecContext, tx.QueryRowContext
				commit, rollback = tx.Commit, func() { _ = tx.Rollback() }
			} else if _, err := exec(t.Context(), "BEGIN"); err != nil {
				t.Fatal(err)
			}
			defer rollback()
			if _, err := exec(t.Context(), `SET LOCAL timezone='Europe/Amsterdam'; INSERT INTO transaction_probe(id) VALUES(7)`); err != nil {
				t.Fatal(err)
			}
			catalog, err := inspectCatalog(t.Context(), q, releaseidentity.Postgres)
			if err != nil || len(catalog.Objects) == 0 {
				t.Fatal(err)
			}
			var zone string
			var count int
			var locked bool
			if err := row(t.Context(), `SELECT current_setting('TimeZone'), (SELECT count(*) FROM transaction_probe), EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND relation='pg_language'::regclass AND mode='AccessShareLock')`).Scan(&zone, &count, &locked); err != nil {
				t.Fatal(err)
			}
			if zone != "Europe/Amsterdam" || count != 1 || !locked {
				t.Fatalf("caller state changed: timezone=%s rows=%d catalog lock=%v", zone, count, locked)
			}
			if err := commit(); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(t.Context(), `TRUNCATE transaction_probe`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPostgresCatalogPooledConnections(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	db, err := open(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE pool_probe(at timestamptz DEFAULT '1970-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	first, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := first.ExecContext(t.Context(), `SET timezone='Europe/Amsterdam'`); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ExecContext(t.Context(), `SET timezone='America/New_York'`); err != nil {
		t.Fatal(err)
	}
	left, err := inspectCatalog(t.Context(), first, releaseidentity.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	right, err := inspectCatalog(t.Context(), second, releaseidentity.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest() != right.Digest() {
		t.Fatal("connection-dependent catalog")
	}
	var beforePID int
	if err := first.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&beforePID); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reused, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer reused.Close()
	var afterPID int
	var zone string
	if err := reused.QueryRowContext(t.Context(), `SELECT pg_backend_pid(),current_setting('TimeZone')`).Scan(&afterPID, &zone); err != nil {
		t.Fatal(err)
	}
	if beforePID != afterPID || zone != "Europe/Amsterdam" {
		t.Fatalf("pool reuse pid=%d/%d timezone=%s", beforePID, afterPID, zone)
	}
	again, err := inspectCatalog(t.Context(), reused, releaseidentity.Postgres)
	if err != nil || again.Digest() != left.Digest() {
		t.Fatalf("reused catalog: %v", err)
	}
}
