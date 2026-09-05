package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// Coordination SQL can only be reached through an admitted transaction. Nested
// coordination helpers reuse it; no pool-backed autocommit writer remains.
type coordinationTx struct{ db *coordinationQueries }
type coordinationQueries struct {
	engine          Engine
	pool            *PostgresTransaction
	sqWrite, sqRead *SQLiteTransaction
}

func (c *Coordination) transaction(ctx context.Context, readonly bool, fn func(*coordinationTx) error) error {
	q := &coordinationQueries{engine: c.db.engine}
	if c.db.engine == EnginePostgres {
		options := pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
		if readonly {
			options.IsoLevel = pgx.RepeatableRead
			options.AccessMode = pgx.ReadOnly
		}
		tx, err := c.db.BeginPostgres(ctx, options)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		q.pool = tx
		if err := fn(&coordinationTx{db: q}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	tx, err := c.db.BeginSQLite(ctx, readonly)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q.sqRead, q.sqWrite = tx, tx
	if err := fn(&coordinationTx{db: q}); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Coordination) ClaimLease(ctx context.Context, name, owner string, now, expires time.Time) (fence int64, held bool, err error) {
	err = c.transaction(ctx, false, func(q *coordinationTx) error {
		var e error
		fence, held, e = q.ClaimLease(ctx, name, owner, now, expires)
		return e
	})
	if err != nil {
		return 0, false, err
	}
	return fence, held, nil
}

func (c *Coordination) RenewLease(ctx context.Context, name, owner string, fence int64, now, expires time.Time) (held bool, err error) {
	err = c.transaction(ctx, false, func(q *coordinationTx) error {
		var e error
		held, e = q.RenewLease(ctx, name, owner, fence, now, expires)
		return e
	})
	if err != nil {
		return false, err
	}
	return held, nil
}

func (c *Coordination) ReleaseLease(ctx context.Context, name, owner string, fence int64) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.ReleaseLease(ctx, name, owner, fence) })
}

func (c *Coordination) Now(ctx context.Context) (result time.Time, err error) {
	err = c.transaction(ctx, true, func(q *coordinationTx) error { var e error; result, e = q.Now(ctx); return e })
	if err != nil {
		return time.Time{}, err
	}
	return result, nil
}

func (c *Coordination) LeaseHolder(ctx context.Context, name string, now time.Time) (owner string, acquiredAt time.Time, live bool, err error) {
	err = c.transaction(ctx, true, func(q *coordinationTx) error {
		var e error
		owner, acquiredAt, live, e = q.LeaseHolder(ctx, name, now)
		return e
	})
	if err != nil {
		return "", time.Time{}, false, err
	}
	return owner, acquiredAt, live, nil
}

func (c *Coordination) UpsertNode(ctx context.Context, n HANode) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.UpsertNode(ctx, n) })
}

func (c *Coordination) RegisterNodeChecked(ctx context.Context, n HANode, since time.Time) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.RegisterNodeChecked(ctx, n, since) })
}

func (c *Coordination) CountLiveNodes(ctx context.Context, since time.Time) (result int, err error) {
	err = c.transaction(ctx, true, func(q *coordinationTx) error { var e error; result, e = q.CountLiveNodes(ctx, since); return e })
	if err != nil {
		return 0, err
	}
	return result, nil
}

func (c *Coordination) PruneNodes(ctx context.Context, cutoff time.Time) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.PruneNodes(ctx, cutoff) })
}

func (c *Coordination) ForeignRootKeyFingerprints(ctx context.Context, nodeID, fingerprint string, since time.Time) (result []string, err error) {
	err = c.transaction(ctx, true, func(q *coordinationTx) error {
		var e error
		result, e = q.ForeignRootKeyFingerprints(ctx, nodeID, fingerprint, since)
		return e
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Coordination) BumpWindow(ctx context.Context, bucket, subject string, windowStart time.Time) (result int64, err error) {
	err = c.transaction(ctx, false, func(q *coordinationTx) error {
		var e error
		result, e = q.BumpWindow(ctx, bucket, subject, windowStart)
		return e
	})
	if err != nil {
		return 0, err
	}
	return result, nil
}

func (c *Coordination) AcquireMCP(ctx context.Context, callID, principalID, orgID string, ttl time.Duration) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.AcquireMCP(ctx, callID, principalID, orgID, ttl) })
}

func (c *Coordination) ReleaseMCP(ctx context.Context, callID string) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.ReleaseMCP(ctx, callID) })
}

func (c *Coordination) AccountFailureState(ctx context.Context, subject string) (failures int64, lastFailure, dbNow time.Time, ok bool, err error) {
	err = c.transaction(ctx, true, func(q *coordinationTx) error {
		var e error
		failures, lastFailure, dbNow, ok, e = q.AccountFailureState(ctx, subject)
		return e
	})
	if err != nil {
		return 0, time.Time{}, time.Time{}, false, err
	}
	return failures, lastFailure, dbNow, ok, nil
}

func (c *Coordination) RecordAccountFailure(ctx context.Context, subject string, now time.Time) (result int64, err error) {
	err = c.transaction(ctx, false, func(q *coordinationTx) error {
		var e error
		result, e = q.RecordAccountFailure(ctx, subject, now)
		return e
	})
	if err != nil {
		return 0, err
	}
	return result, nil
}

func (c *Coordination) PruneAccountBackoff(ctx context.Context, cutoff time.Time) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.PruneAccountBackoff(ctx, cutoff) })
}

func (c *Coordination) ClearAccount(ctx context.Context, subject string) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.ClearAccount(ctx, subject) })
}

func (c *Coordination) PruneAdmissionWindows(ctx context.Context, cutoff time.Time) error {
	return c.transaction(ctx, false, func(q *coordinationTx) error { return q.PruneAdmissionWindows(ctx, cutoff) })
}
