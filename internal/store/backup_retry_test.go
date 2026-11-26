package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A guard failure after BEGIN must return the error (no nil-transaction panic)
// and roll back, leaving the pooled connection outside any transaction.
func TestBeginPostgresExportSnapshotRollsBackOnGuardFailure(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI requires HIKYO_TEST_POSTGRES_DSN")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// The zero admission is invalid, so the guard fails inside the transaction.
	tx, tables, err := beginPostgresExportSnapshot(t.Context(), conn, &DB{engine: EnginePostgres}, &Manifest{}, nil)
	if err == nil || tx != nil || tables != nil {
		t.Fatalf("tx=%v tables=%v err=%v, want a guard error and no transaction", tx, tables, err)
	}
	if status := conn.Conn().PgConn().TxStatus(); status != 'I' {
		t.Fatalf("connection tx status = %q, want idle 'I' (rolled back)", status)
	}
}

func TestRetryPostgresSerializationRetriesTransientFailure(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		calls := 0
		err := retryPostgresSerialization(t.Context(), func() error {
			calls++
			if calls == 1 {
				return fmt.Errorf("upgrade: read control: %w", &pgconn.PgError{Code: code})
			}
			return nil
		})
		if err != nil || calls != 2 {
			t.Fatalf("%s: err=%v calls=%d, want nil after 2 calls", code, err, calls)
		}
	}
}

func TestRetryPostgresSerializationGivesUpAfterBound(t *testing.T) {
	calls := 0
	err := retryPostgresSerialization(t.Context(), func() error {
		calls++
		return &pgconn.PgError{Code: "40001"}
	})
	if want := len(postgresRetryBackoff) + 1; calls != want {
		t.Fatalf("calls = %d, want %d", calls, want)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Fatalf("err = %v, want exhausted wrapping 40001", err)
	}
}

func TestRetryPostgresSerializationDoesNotRetryOtherErrors(t *testing.T) {
	calls := 0
	boom := errors.New("boom")
	if err := retryPostgresSerialization(t.Context(), func() error { calls++; return boom }); !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want boom after 1 call", err, calls)
	}
}

func TestRetryPostgresSerializationStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	err := retryPostgresSerialization(ctx, func() error {
		calls++
		cancel()
		return &pgconn.PgError{Code: "40001"}
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want canceled after 1 call", err, calls)
	}
}
