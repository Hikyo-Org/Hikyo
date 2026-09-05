package store

import (
	"context"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
	"github.com/jackc/pgx/v5"
)

type sqliteTransaction interface {
	sqlitegen.DBTX
	Commit() error
	Rollback() error
}
type postgresTransaction interface {
	pggen.DBTX
	Commit(context.Context) error
	Rollback(context.Context) error
}

// Each multiquery runtime read owns one admitted snapshot. No pool shim can
// outlive the callback or cross a maintenance boundary between its queries.
func dbRead(ctx context.Context, db *DB, fn func(adapterDB) error) error {
	var wrapped adapterDBTX
	if db.Engine() == EnginePostgres {
		tx, err := db.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		wrapped = pgAdapterTx{tx: tx}
	} else {
		tx, err := db.BeginSQLite(ctx, true)
		if err != nil {
			return err
		}
		wrapped = sqliteAdapterTx{tx: tx}
	}
	defer wrapped.Rollback(ctx)
	if err := fn(wrapped); err != nil {
		return err
	}
	return wrapped.Commit(ctx)
}
func dbReadResult[T any](ctx context.Context, db *DB, fn func(adapterDB) (T, error)) (T, error) {
	var result T
	err := dbRead(ctx, db, func(q adapterDB) error { var err error; result, err = fn(q); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
