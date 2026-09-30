package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
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
func GuardPGSingletonLease(ctx context.Context, transaction pggen.DBTX) error {
	lease, present := ctx.Value(singletonLeaseKey{}).(singletonLease)
	if !present {
		return nil
	}
	count, err := pggen.New(transaction).GuardSingletonLease(ctx, pggen.GuardSingletonLeaseParams{
		Name: lease.name, Owner: lease.owner, FenceToken: lease.fence,
	})
	if err != nil {
		return fmt.Errorf("store: guard singleton lease: %w", err)
	}
	if count != 1 {
		return ErrSingletonLeaseLost
	}
	return nil
}

// GuardSQLiteSingletonLease is the SQLite counterpart. BEGIN IMMEDIATE holds
// write admission through commit; SQLite's process clock is authoritative.
func GuardSQLiteSingletonLease(ctx context.Context, transaction sqlitegen.DBTX) error {
	lease, present := ctx.Value(singletonLeaseKey{}).(singletonLease)
	if !present {
		return nil
	}
	count, err := sqlitegen.New(transaction).GuardSingletonLease(ctx, sqlitegen.GuardSingletonLeaseParams{
		Name: lease.name, Owner: lease.owner, FenceToken: lease.fence, Now: fixedStamp(time.Now().UTC()),
	})
	if err != nil {
		return fmt.Errorf("store: guard singleton lease: %w", err)
	}
	if count != 1 {
		return ErrSingletonLeaseLost
	}
	return nil
}
