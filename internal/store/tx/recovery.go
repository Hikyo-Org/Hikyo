package tx

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/jackc/pgx/v5"
)

// RecoveryRead and RecoveryWrite are confined to an authenticated operator
// scratch database. Ordinary service constructors cannot accept RecoveryDB.
// Callbacks retain ordinary repository proofs and cannot escape an attempt.
// The resolver is always the historical one bound to the source schema: at the
// current schema its gates are all off and it reads exactly as authn.NewPG or
// authn.NewSQLite, so the schema thresholds live only in recovery_compat.go.
func RecoveryRead(ctx context.Context, db *store.RecoveryDB, fn ReadFn) error {
	version, err := db.SourceMigrationVersion()
	if err != nil {
		return err
	}
	return retryLoop(ctx, db.Engine(), func(ctx context.Context) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if db.Engine() == store.EnginePostgres {
			tx, err := db.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			resolver := authn.NewHistoricalRecoveryPG(tx, version)
			az := authz.NewTxAuthorizer(resolver, tok)
			outcome := fn(ctx, store.PGTxReadRepos(tx, tok), az)
			if outcome != nil {
				_ = tx.Rollback(ctx)
			} else {
				outcome = tx.Commit(ctx)
			}
			return settleRecoveryDenials(ctx, db, az, outcome)
		}
		tx, err := db.BeginSQLite(ctx, true)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		resolver := authn.NewHistoricalRecoverySQLite(tx, version)
		az := authz.NewTxAuthorizer(resolver, tok)
		outcome := fn(ctx, store.SQLiteTxReadRepos(tx, tok), az)
		if outcome != nil {
			_ = tx.Rollback()
		} else {
			outcome = tx.Commit()
		}
		return settleRecoveryDenials(ctx, db, az, outcome)
	})
}
func RecoveryWrite(ctx context.Context, db *store.RecoveryDB, fn WriteFn) error {
	version, err := db.SourceMigrationVersion()
	if err != nil {
		return err
	}
	return retryLoop(ctx, db.Engine(), func(ctx context.Context) error {
		tok := authz.NewTxToken()
		defer tok.Invalidate()
		if db.Engine() == store.EnginePostgres {
			tx, err := db.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			resolver := authn.NewHistoricalRecoveryPG(tx, version)
			az := authz.NewTxAuthorizer(resolver, tok)
			outcome := fn(ctx, store.PGTxRepos(tx, tok), az)
			if outcome != nil {
				_ = tx.Rollback(ctx)
			} else {
				outcome = tx.Commit(ctx)
			}
			return settleRecoveryDenials(ctx, db, az, outcome)
		}
		tx, err := db.BeginSQLite(ctx, false)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		resolver := authn.NewHistoricalRecoverySQLite(tx, version)
		az := authz.NewTxAuthorizer(resolver, tok)
		outcome := fn(ctx, store.SQLiteTxRepos(tx, tok), az)
		if outcome != nil {
			_ = tx.Rollback()
		} else {
			outcome = tx.Commit()
		}
		return settleRecoveryDenials(ctx, db, az, outcome)
	})
}

func settleRecoveryDenials(ctx context.Context, db *store.RecoveryDB, az *authz.TxAuthorizer, outcome error) error {
	return settleDenialResult(ctx, db.Engine(), az, outcome, func(ctx context.Context, denials []authz.Denial) error { return flushRecoveryDenials(ctx, db, denials) })
}
func flushRecoveryDenials(ctx context.Context, db *store.RecoveryDB, denials []authz.Denial) error {
	if db.Engine() == store.EnginePostgres {
		pgtx, err := db.BeginPostgres(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return err
		}
		defer pgtx.Rollback(ctx)
		w := authn.NewPG(pgtx)
		for _, d := range denials {
			if err := w.WriteDenial(ctx, d.Event, d.Trail, d.Scope); err != nil {
				_ = pgtx.Rollback(ctx)
				return err
			}
		}
		return pgtx.Commit(ctx)
	}
	sqtx, err := db.BeginSQLite(ctx, false)
	if err != nil {
		return err
	}
	defer sqtx.Rollback()
	w := authn.NewSQLite(sqtx)
	for _, d := range denials {
		if err := w.WriteDenial(ctx, d.Event, d.Trail, d.Scope); err != nil {
			_ = sqtx.Rollback()
			return err
		}
	}
	return sqtx.Commit()
}

// RecoveryReconcile preserves the named single-principal resolution boundary.
func RecoveryReconcile(ctx context.Context, db *store.RecoveryDB, fn RestoreFn) error {
	return RecoveryWrite(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error { return fn(ctx, az) })
}
