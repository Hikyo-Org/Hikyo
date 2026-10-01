package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

// A real SQLite uniqueness failure verifies the generated exec path and both accessors.
func TestAdapterMoveAccessorsPreserveConstraintErrorBoundary(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(t.Context(), `CREATE TABLE adapter_route_moves(id TEXT PRIMARY KEY, org_id TEXT, project_id TEXT, adapter_id TEXT, kind TEXT, pending_origin TEXT, pending_credential_ciphertext BLOB, authority_principal_id TEXT, state TEXT, keep_remote BOOLEAN, created_at TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	request := (sqliteAdoptDB{db: tx}).adapterMoveQueries()
	runtime := (sqliteAdapterTx{tx: tx}).adapterMoveQueries()
	insert := func(q adapterMoveQueries) error {
		_, err := q.insertOrigin(t.Context(), "move", "org", "project", "adapter", "https://provider.example", []byte("sealed"), "principal", "scrubbing", false, time.Now().UTC())
		return err
	}
	if err := insert(request); err != nil {
		t.Fatal(err)
	}
	if err := insert(request); !errors.Is(err, ErrConflict) {
		t.Fatalf("request error=%v want conflict", err)
	}
	var driverErr *sqlite.Error
	if err := insert(runtime); !errors.As(err, &driverErr) || errors.Is(err, ErrConflict) {
		t.Fatalf("runtime error=%v want raw SQLite constraint", err)
	}
}

type moveConstraintPGTx struct {
	pgx.Tx
	failure error
}

func (tx moveConstraintPGTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.failure
}

func TestAdapterMovePostgresAccessorsPreserveConstraintErrorBoundary(t *testing.T) {
	driverErr := &pgconn.PgError{Code: "23505", ConstraintName: "adapter_route_moves_pkey"}
	tx := moveConstraintPGTx{failure: driverErr}
	request := (pgAdoptDB{db: tx}).adapterMoveQueries()
	runtime := (pgAdapterTx{tx: tx}).adapterMoveQueries()
	for _, tc := range []struct {
		name   string
		q      adapterMoveQueries
		mapped bool
	}{{"request", request, true}, {"runtime", runtime, false}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.q.insertOrigin(t.Context(), "move", "org", "project", "adapter", "https://provider.example", []byte("sealed"), "principal", "scrubbing", false, time.Now().UTC())
			if tc.mapped {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("request error=%v want conflict", err)
				}
			} else if err != driverErr {
				t.Fatalf("runtime error=%v want original driver error", err)
			}
		})
	}
}
