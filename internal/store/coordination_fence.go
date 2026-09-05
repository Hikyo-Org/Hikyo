package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSingletonLeaseLost refuses work from an expired or superseded leadership
// term, even if the scheduler has not yet observed its failed heartbeat.
var ErrSingletonLeaseLost = errors.New("store: singleton lease lost")

type singletonLeaseKey struct{}

type singletonLease struct {
	name  string
	owner string
	fence int64
}

// WithSingletonLease binds scheduler work to one leadership term. tx.Write
// checks this identity inside every attempt, using the same transaction as the
// job's writes. It is infrastructure identity, never tenant authorization.
// Ordinary requests and single-node jobs do not carry a singleton lease.
func WithSingletonLease(ctx context.Context, name, owner string, fence int64) context.Context {
	return context.WithValue(ctx, singletonLeaseKey{}, singletonLease{name: name, owner: owner, fence: fence})
}

// GuardPGSingletonLease locks a live matching lease through transaction commit.
// A takeover cannot pass the lock while admitted work is committing. After a
// takeover, an old SERIALIZABLE snapshot retries and checks the new fence.
// clock_timestamp, rather than the transaction's start time, also refuses a
// term that expired while this transaction waited for the lease row.
func GuardPGSingletonLease(ctx context.Context, transaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	lease, present := ctx.Value(singletonLeaseKey{}).(singletonLease)
	if !present {
		return nil
	}
	result, err := transaction.Exec(ctx, `UPDATE singleton_leases SET fence_token = fence_token
		WHERE name = $1 AND owner = $2 AND fence_token = $3 AND expires_at > clock_timestamp()`,
		lease.name, lease.owner, lease.fence)
	if err != nil {
		return fmt.Errorf("store: guard singleton lease: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrSingletonLeaseLost
	}
	return nil
}

// GuardSQLiteSingletonLease is the SQLite counterpart. BEGIN IMMEDIATE holds
// write admission through commit; SQLite's process clock is authoritative.
func GuardSQLiteSingletonLease(ctx context.Context, transaction interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}) error {
	lease, present := ctx.Value(singletonLeaseKey{}).(singletonLease)
	if !present {
		return nil
	}
	result, err := transaction.ExecContext(ctx, `UPDATE singleton_leases SET fence_token = fence_token
		WHERE name = ? AND owner = ? AND fence_token = ? AND expires_at > ?`,
		lease.name, lease.owner, lease.fence, fixedStamp(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("store: guard singleton lease: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: guard singleton lease result: %w", err)
	}
	if count != 1 {
		return ErrSingletonLeaseLost
	}
	return nil
}
