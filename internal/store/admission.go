package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQLiteTransaction retains host reader/writer exclusion until SQL settles.
// The native transaction cannot escape through a raw handle getter.
type SQLiteTransaction struct {
	tx    *sql.Tx
	guard *upgrade.SQLiteGuard
	// These two SQL templates contain no tenant results or retained arguments.
	// They belong to this transaction alone; database/sql closes them when the
	// transaction settles. Repository proof verification still precedes every
	// execution, including reuse with another environment's bound parameters.
	snapshotMu         sync.Mutex
	snapshotStatements [2]*sql.Stmt
}

func (t *SQLiteTransaction) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if slot, ok := sqlitegen.SnapshotInsertSlot(q); ok {
		t.snapshotMu.Lock()
		statement := t.snapshotStatements[slot]
		if statement == nil {
			// SQLC adds trailing whitespace. The SQLite driver's single-statement
			// reuse path requires no tail, so normalize only these exact queries.
			var err error
			statement, err = t.tx.PrepareContext(ctx, strings.TrimSpace(q))
			if err != nil {
				t.snapshotMu.Unlock()
				return nil, err
			}
			t.snapshotStatements[slot] = statement
		}
		t.snapshotMu.Unlock()
		return statement.ExecContext(ctx, args...)
	}
	return t.tx.ExecContext(ctx, q, args...)
}
func (t *SQLiteTransaction) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, q)
}
func (t *SQLiteTransaction) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, q, args...)
}
func (t *SQLiteTransaction) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, q, args...)
}
func (t *SQLiteTransaction) Commit() error   { return errors.Join(t.tx.Commit(), t.guard.Close()) }
func (t *SQLiteTransaction) Rollback() error { return errors.Join(t.tx.Rollback(), t.guard.Close()) }

// BeginSQLite acquires host admission before BEGIN. The write pool retains its
// BEGIN IMMEDIATE policy; WAL readers retain the shared host guard too.
func (d *DB) BeginSQLite(ctx context.Context, readonly bool) (*SQLiteTransaction, error) {
	if d == nil || d.engine != EngineSQLite || !d.admission.Valid() {
		return nil, upgrade.ErrConflict
	}
	guard, err := d.admission.LockSQLite(ctx)
	if err != nil {
		return nil, err
	}
	pool := d.sqWrite
	if readonly {
		pool = d.sqRead
	}
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: readonly})
	if err != nil {
		return nil, errors.Join(err, guard.Close())
	}
	if err := guard.Check(ctx, tx); err != nil {
		return nil, errors.Join(err, tx.Rollback(), guard.Close())
	}
	return &SQLiteTransaction{tx: tx, guard: guard}, nil
}

// PostgresTransaction exposes transactional statements, never a pooled/native
// connection that could outlive admission. Its shared control lock is owned by
// this same SQL transaction and is released only by commit or rollback.
type PostgresTransaction struct {
	tx      pgx.Tx
	release func() error
}

func (t *PostgresTransaction) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, q, args...)
}
func (t *PostgresTransaction) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, q, args...)
}
func (t *PostgresTransaction) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, q, args...)
}
func (t *PostgresTransaction) Commit(ctx context.Context) error {
	return errors.Join(t.tx.Commit(ctx), t.closeOwner())
}
func (t *PostgresTransaction) Rollback(ctx context.Context) error {
	return errors.Join(t.tx.Rollback(ctx), t.closeOwner())
}

func (t *PostgresTransaction) closeOwner() error {
	if t.release != nil {
		return t.release()
	}
	return nil
}

type postgresBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func (d *DB) BeginPostgres(ctx context.Context, options pgx.TxOptions) (*PostgresTransaction, error) {
	if d == nil {
		return nil, upgrade.ErrConflict
	}
	return d.beginPostgresOn(ctx, d.pool, options)
}

// beginPostgresOn preserves the existing named serialization-lock connection.
// Every retry independently locks and checks admission before domain SQL.
func (d *DB) beginPostgresOn(ctx context.Context, beginner postgresBeginner, options pgx.TxOptions) (*PostgresTransaction, error) {
	if d == nil || d.engine != EnginePostgres || !d.admission.Valid() || beginner == nil {
		return nil, upgrade.ErrConflict
	}
	readonly := options.AccessMode == pgx.ReadOnly
	options.AccessMode = pgx.ReadWrite
	tx, err := beginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := d.admission.GuardPostgres(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if readonly {
		if _, err := tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	return &PostgresTransaction{tx: tx}, nil
}

// CheckAdmission is a read-only readiness decision under the same transaction
// fence as tenant work. It never creates goose tables or migrates a schema.
func (d *DB) CheckAdmission(ctx context.Context) error {
	if d.engine == EnginePostgres {
		tx, err := d.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	tx, err := d.BeginSQLite(ctx, true)
	if err != nil {
		return err
	}
	return tx.Commit()
}
