package upgrade

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
)

// Match the narrow store admission/recovery wrapper capability: no Conn/Begin.
type catalogTestTransaction struct{ postgresCatalogTransaction }

func TestPostgresCatalogNativeTransactionsAndErrors(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(t.Context(), `CREATE TABLE native_probe(id integer, at timestamptz DEFAULT '1970-01-01T00:00:00Z'); SET timezone='UTC'`); err != nil {
		t.Fatal(err)
	}
	baseline, err := DomainCatalogPostgres(t.Context(), conn)
	if err != nil {
		t.Fatal(err)
	}
	for _, wrapped := range []bool{false, true} {
		tx, err := conn.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(t.Context(), `SET LOCAL timezone='Europe/Amsterdam'; SET LOCAL DateStyle='SQL, DMY'; SET LOCAL IntervalStyle='iso_8601'; SET LOCAL extra_float_digits=-3; SET LOCAL bytea_output='escape'; SET LOCAL search_path=pg_catalog; SET LOCAL quote_all_identifiers=on; SET LOCAL standard_conforming_strings=off; INSERT INTO public.native_probe(id) VALUES(1)`); err != nil {
			t.Fatal(err)
		}
		var q PGSnapshotQueries = tx
		if wrapped {
			q = catalogTestTransaction{tx}
		}
		settingsBefore := postgresRenderingSettings(t, q)
		catalog, err := DomainCatalogPostgres(t.Context(), q)
		if err != nil || catalog.Digest() != baseline.Digest() {
			t.Fatalf("wrapper=%v catalog=%s: %v", wrapped, catalog.Digest(), err)
		}
		if after := postgresRenderingSettings(t, q); after != settingsBefore {
			t.Fatalf("settings changed: before=%v after=%v", settingsBefore, after)
		}
		query := func(ctx context.Context, statement string, args ...any) (catalogRows, error) {
			return q.Query(ctx, statement, args...)
		}
		for _, failure := range []string{"sql-error", "cancelled-context"} {
			ctx, cancel := context.WithCancel(t.Context())
			_, err := withPostgresCatalogScope(ctx, query, true, tx.Rollback, func(ctx context.Context, query catalogQuery, engine releaseidentity.Engine) (Catalog, error) {
				actual, err := inspectCatalogWith(ctx, query, engine)
				if err != nil || actual.Digest() != baseline.Digest() {
					return Catalog{}, errors.New("scope failed to canonicalize before error")
				}
				if failure == "cancelled-context" {
					cancel()
					return Catalog{}, ctx.Err()
				}
				rows, err := query(ctx, `SELECT 1/0`)
				if err == nil {
					for rows.Next() {
					}
					err = rows.Err()
					rows.Close()
				}
				return Catalog{}, err
			})
			cancel()
			if err == nil {
				t.Fatalf("%s was ignored", failure)
			}
			if failure == "cancelled-context" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if after := postgresRenderingSettings(t, q); after != settingsBefore {
				t.Fatalf("%s changed settings: before=%v after=%v", failure, settingsBefore, after)
			}
			var zone string
			var count int
			var locked bool
			if err := tx.QueryRow(t.Context(), `SELECT current_setting('TimeZone'),(SELECT count(*) FROM public.native_probe),EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND relation='pg_language'::regclass AND mode='AccessShareLock')`).Scan(&zone, &count, &locked); err != nil {
				t.Fatal(err)
			}
			if zone != "Europe/Amsterdam" || count != 1 || !locked {
				t.Fatalf("error changed caller transaction zone=%s count=%d locked=%v", zone, count, locked)
			}
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(t.Context(), `TRUNCATE native_probe`); err != nil {
			t.Fatal(err)
		}
		if _, err := DomainCatalogPostgres(t.Context(), tx); !errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("closed transaction accepted: %v", err)
		}
	}
}

func TestPostgresCatalogCleanupFailureInvalidates(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	for _, failure := range []string{"restore-and-rollback", "release"} {
		t.Run(failure, func(t *testing.T) {
			conn, err := pgx.Connect(t.Context(), cfg.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(context.Background())
			before := postgresRenderingSettings(t, conn)
			tx, err := conn.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			query := func(ctx context.Context, statement string, args ...any) (catalogRows, error) {
				if failure == "restore-and-rollback" && (statement == postgresCatalogRestoreSettings || strings.HasPrefix(statement, "ROLLBACK TO")) || failure == "release" && strings.HasPrefix(statement, "RELEASE") {
					return nil, errors.New("injected cleanup failure")
				}
				return tx.Query(ctx, statement, args...)
			}
			invalidated := false
			catalog, err := withPostgresCatalogScope(t.Context(), query, true, func(ctx context.Context) error { invalidated = true; return tx.Rollback(ctx) }, inspectCatalogWith)
			if err == nil || !strings.Contains(err.Error(), "injected cleanup failure") || !invalidated || catalog.Format != "" {
				t.Fatalf("uncertain catalog escaped: %+v %v invalidated=%v", catalog, err, invalidated)
			}
			if _, err := tx.Exec(t.Context(), `SELECT 1`); !errors.Is(err, pgx.ErrTxClosed) {
				t.Fatalf("failed cleanup transaction survived: %v", err)
			}
			if after := postgresRenderingSettings(t, conn); after != before {
				t.Fatalf("settings leaked: %v != %v", after, before)
			}
		})
	}
}

func TestPostgresCatalogInFlightCancellation(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	connection, err := pgx.ParseConfig(cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise recoverable cancellation. The default transport may instead close
	// the connection, in which case preservation of its transaction is impossible.
	connection.BuildContextWatcherHandler = func(conn *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: conn, DeadlineDelay: time.Second}
	}
	conn, err := pgx.ConnectConfig(t.Context(), connection)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	before := postgresRenderingSettings(t, tx)
	query := func(ctx context.Context, statement string, args ...any) (catalogRows, error) {
		return tx.Query(ctx, statement, args...)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	catalog, err := withPostgresCatalogScope(ctx, query, true, tx.Rollback, func(ctx context.Context, query catalogQuery, _ releaseidentity.Engine) (Catalog, error) {
		timer := time.AfterFunc(50*time.Millisecond, cancel)
		defer timer.Stop()
		rows, err := query(ctx, `SELECT pg_sleep(10)`)
		if err == nil {
			for rows.Next() {
			}
			rows.Close()
			err = rows.Err()
		}
		return Catalog{}, err
	})
	if err == nil || ctx.Err() == nil || catalog.Format != "" {
		t.Fatalf("cancelled query accepted: %v", err)
	}
	if after := postgresRenderingSettings(t, tx); after != before {
		t.Fatalf("cancellation leaked settings: %v != %v", after, before)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("recoverable cancellation aborted caller: %v", err)
	}
}

type catalogCloseErrorRows struct {
	catalogRows
	closed bool
}

func (r *catalogCloseErrorRows) Close() { r.catalogRows.Close(); r.closed = true }
func (r *catalogCloseErrorRows) Err() error {
	if r.closed {
		return errors.New("injected rows close error")
	}
	return r.catalogRows.Err()
}

func TestPostgresCatalogSettingsRowsCloseErrors(t *testing.T) {
	cfg := testConfig(t, releaseidentity.Postgres)
	conn, err := pgx.Connect(t.Context(), cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	for _, statement := range []string{postgresCatalogOriginalSettings, postgresCatalogSettings} {
		tx, err := conn.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		before := postgresRenderingSettings(t, tx)
		query := func(ctx context.Context, sql string, args ...any) (catalogRows, error) {
			rows, err := tx.Query(ctx, sql, args...)
			if err != nil {
				return nil, err
			}
			if sql == statement {
				return &catalogCloseErrorRows{catalogRows: rows}, nil
			}
			return rows, nil
		}
		catalog, err := withPostgresCatalogScope(t.Context(), query, true, tx.Rollback, inspectCatalogWith)
		if err == nil || !strings.Contains(err.Error(), "injected rows close error") || catalog.Format != "" {
			t.Fatalf("rows error ignored: %v", err)
		}
		if after := postgresRenderingSettings(t, tx); after != before {
			t.Fatalf("rows failure leaked settings: %v != %v", after, before)
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}

func postgresRenderingSettings(t *testing.T, q PGSnapshotQueries) [8]string {
	t.Helper()
	var settings [8]string
	var args [8]any
	for i := range settings {
		args[i] = &settings[i]
	}
	if err := q.QueryRow(t.Context(), postgresCatalogOriginalSettings).Scan(args[:]...); err != nil {
		t.Fatal(err)
	}
	return settings
}
