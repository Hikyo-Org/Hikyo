package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RecoveryDB exposes only the operator scratch transaction boundary. It has no
// runtime DB, driver, coordination, adapter or export accessor.
type RecoveryDB struct {
	db        *DB
	authority upgrade.RecoveryAdmission
	config    Config
}

func OpenRecovery(ctx context.Context, cfg Config, authority upgrade.RecoveryAdmission) (*RecoveryDB, error) {
	if err := authority.Check(ctx, upgradeConfig(cfg)); err != nil {
		return nil, err
	}
	db, err := openConfigured(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &RecoveryDB{db: db, authority: authority, config: cfg}, nil
}
func (r *RecoveryDB) Close() error   { return r.db.Close() }
func (r *RecoveryDB) Engine() Engine { return r.db.engine }
func (r *RecoveryDB) BeginSQLite(ctx context.Context, readonly bool) (*RecoverySQLiteTransaction, error) {
	if r == nil || r.Engine() != EngineSQLite {
		return nil, upgrade.ErrConflict
	}
	if err := r.authority.Check(ctx, upgradeConfig(r.config)); err != nil {
		return nil, err
	}
	pool := r.db.sqWrite
	if readonly {
		pool = r.db.sqRead
	}
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: readonly})
	if err != nil {
		return nil, err
	}
	if err := r.authority.GuardSQLite(ctx, tx); err != nil {
		return nil, errors.Join(err, tx.Rollback())
	}
	return &RecoverySQLiteTransaction{tx: tx, authority: r.authority}, nil
}
func (r *RecoveryDB) BeginPostgres(ctx context.Context, options pgx.TxOptions) (*RecoveryPostgresTransaction, error) {
	if r == nil || r.Engine() != EnginePostgres {
		return nil, upgrade.ErrConflict
	}
	if err := r.authority.Check(ctx, upgradeConfig(r.config)); err != nil {
		return nil, err
	}
	readonly := options.AccessMode == pgx.ReadOnly
	options.AccessMode = pgx.ReadWrite
	tx, err := r.db.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := r.authority.GuardPostgres(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if readonly {
		if _, err := tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	return &RecoveryPostgresTransaction{tx: tx, authority: r.authority}, nil
}

// Recovery transactions remain distinct from runtime transactions and refuse a
// commit after either piece of operator custody has expired.
type RecoverySQLiteTransaction struct {
	tx        *sql.Tx
	authority upgrade.RecoveryAdmission
}

func (t *RecoverySQLiteTransaction) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, q, args...)
}
func (t *RecoverySQLiteTransaction) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, q)
}
func (t *RecoverySQLiteTransaction) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, q, args...)
}
func (t *RecoverySQLiteTransaction) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, q, args...)
}
func (t *RecoverySQLiteTransaction) Commit() error {
	if err := t.authority.CheckOwner(); err != nil {
		return errors.Join(err, t.tx.Rollback())
	}
	return t.tx.Commit()
}
func (t *RecoverySQLiteTransaction) Rollback() error { return t.tx.Rollback() }

type RecoveryPostgresTransaction struct {
	tx        pgx.Tx
	authority upgrade.RecoveryAdmission
}

func (t *RecoveryPostgresTransaction) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, q, args...)
}
func (t *RecoveryPostgresTransaction) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, q, args...)
}
func (t *RecoveryPostgresTransaction) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, q, args...)
}
func (t *RecoveryPostgresTransaction) Commit(ctx context.Context) error {
	if err := t.authority.CheckPostgresOwner(ctx); err != nil {
		return errors.Join(err, t.tx.Rollback(ctx))
	}
	return t.tx.Commit(ctx)
}
func (t *RecoveryPostgresTransaction) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// SourceMigrationVersion is derived from the verified recovery plan, not from
// request flags or a best-effort live schema probe.
func (r *RecoveryDB) SourceMigrationVersion() (uint64, error) {
	return r.authority.SourceMigrationVersion()
}
