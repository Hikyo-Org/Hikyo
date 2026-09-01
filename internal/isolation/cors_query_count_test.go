package isolation

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"

	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
)

const corsQueryDriverName = "hikyo-cors-query-counter"

var corsQueryDriver = &queryCountingDriver{delegate: &sqlite.Driver{}}

func init() {
	sql.Register(corsQueryDriverName, corsQueryDriver)
}

type queryCountingDriver struct {
	delegate driver.Driver
	queries  atomic.Int64
}

func (d *queryCountingDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.delegate.Open(name)
	if err != nil {
		return nil, err
	}
	return &queryCountingConn{Conn: conn, queries: &d.queries}, nil
}

type queryCountingConn struct {
	driver.Conn
	queries *atomic.Int64
}

func (c *queryCountingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *queryCountingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.queries.Add(1)
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *queryCountingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func TestCORSRequestsIssueNoAllowlistQueriesAfterSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cors-query-count.db")
	migrationConfig := store.Config{Engine: store.EngineSQLite, Path: path}
	if err := migrate.Run(t.Context(), migrationConfig); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), store.Config{
		Engine: store.EngineSQLite, Path: path, SQLiteDriver: corsQueryDriverName,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	workspace := &service.Workspace{DB: db}
	if err := workspace.PrimeOriginAllowlist(t.Context()); err != nil {
		t.Fatal(err)
	}
	handler := server.NewPublic(nil, &server.API{Workspace: workspace}, nil, server.PublicOptions{
		ExternalOrigin: "https://hikyo.example",
	})

	corsQueryDriver.queries.Store(0)
	for _, tc := range []struct {
		name   string
		origin string
	}{
		{name: "same origin mutation", origin: "https://hikyo.example"},
		{name: "foreign origin with empty allowlist", origin: "https://hostile.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := corsQueryDriver.queries.Load()
			req := httptest.NewRequest(http.MethodPost, "/mutation", nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if got := corsQueryDriver.queries.Load() - before; got != 0 {
				t.Fatalf("request issued %d datastore queries, want 0", got)
			}
		})
	}
}
