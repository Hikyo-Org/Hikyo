package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

// The real native-hook tests cover SQLite cancellation and host exclusion.
// This small driver covers database/sql's independent discard/error paths,
// which SQLite cannot reliably be forced to return on demand.
func TestSQLiteSettlementPreservesDriverErrors(t *testing.T) {
	ordinary := errors.New("native settlement failed")
	for _, nativeErr := range []error{ordinary, driver.ErrBadConn} {
		for _, commit := range []bool{false, true} {
			name := "rollback/" + nativeErr.Error()
			if commit {
				name = "commit/" + nativeErr.Error()
			}
			t.Run(name, func(t *testing.T) {
				connector := &settlementConnector{nativeErr: nativeErr, closed: make(chan struct{})}
				pool := sql.OpenDB(connector)
				defer pool.Close()
				conn, err := pool.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				native, err := conn.BeginTx(t.Context(), nil)
				if err != nil {
					_ = conn.Close()
					t.Fatal(err)
				}
				owner := &SQLiteTransaction{tx: native, conn: conn}
				if commit {
					err = owner.Commit()
				} else {
					err = owner.Rollback()
				}
				if !errors.Is(err, nativeErr) || errors.Is(err, sql.ErrConnDone) {
					t.Fatalf("settlement lost native error or added closed-connection error: %v", err)
				}
				if pool.Stats().InUse != 0 {
					t.Fatal("native settlement error leaked a pool connection")
				}
				if err := owner.Rollback(); !errors.Is(err, sql.ErrTxDone) || errors.Is(err, sql.ErrConnDone) {
					t.Fatalf("repeat settlement after native error: %v", err)
				}
			})
		}
	}
}

func TestSQLiteSettlementAfterAutomaticDiscard(t *testing.T) {
	connector := &settlementConnector{closed: make(chan struct{})}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	native, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		t.Fatal(err)
	}
	owner := &SQLiteTransaction{tx: native, conn: conn}
	cancel()
	awaitSettlementSignal(t, connector.closed)
	// Automatic discard has claimed Conn.Close. The transaction is still done,
	// and its wrapper must not invent an additional ErrConnDone failure.
	if err := owner.Rollback(); !errors.Is(err, sql.ErrTxDone) || errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("settlement after automatic discard: %v", err)
	}
}

type settlementConnector struct {
	nativeErr error
	closed    chan struct{}
}

func (c *settlementConnector) Connect(context.Context) (driver.Conn, error) {
	return &settlementDriverConn{connector: c}, nil
}
func (c *settlementConnector) Driver() driver.Driver { return settlementDriver{} }

type settlementDriver struct{}

func (settlementDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use the settlement connector")
}

type settlementDriverConn struct{ connector *settlementConnector }

func (c *settlementDriverConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("settlement fixture does not execute statements")
}
func (c *settlementDriverConn) Close() error { close(c.connector.closed); return nil }
func (c *settlementDriverConn) Begin() (driver.Tx, error) {
	return settlementDriverTx{err: c.connector.nativeErr}, nil
}

type settlementDriverTx struct{ err error }

func (t settlementDriverTx) Commit() error   { return t.err }
func (t settlementDriverTx) Rollback() error { return t.err }
