package isolation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/jackc/pgx/v5"
)

// Emitter inventory uses durable fixture staging, not a promise that every
// runtime accounting attempt succeeds inside its bounded accounting budget.
func TestAuditRefusalEmitterWaitsForStagingWriter(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		var release func()
		var sqliteWaits int64
		var blockerPID int
		if db.Engine() == store.EngineSQLite {
			held, err := db.BeginSQLite(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			release = func() { _ = held.Rollback() }
			sqliteWaits = db.SQLiteWrite().Stats().WaitCount
		} else {
			held, err := db.BeginPostgres(t.Context(), pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := held.Exec(t.Context(), "SELECT id FROM auth_refusal_window WHERE id=1 FOR UPDATE"); err != nil {
				_ = held.Rollback(t.Context())
				t.Fatal(err)
			}
			if err := held.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&blockerPID); err != nil {
				_ = held.Rollback(t.Context())
				t.Fatal(err)
			}
			release = func() { _ = held.Rollback(t.Context()) }
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		lifecycleDone := make(chan struct{})
		result := make(chan error, 1)
		go func() {
			defer close(done)
			err := waitForRefusalStagingContention(ctx, db, sqliteWaits, blockerPID)
			if err == nil {
				t.Log("staging contention observed; retaining writer beyond runtime accounting budget")
				timer := time.NewTimer(2 * service.RefusalDeadline)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-lifecycleDone:
					err = fmt.Errorf("emitter lifecycle completed while staging writer was held")
				case <-ctx.Done():
					err = ctx.Err()
				}
			}
			release()
			result <- err
		}()
		t.Cleanup(func() { cancel(); <-done })
		runRefusalAuditLifecycle(t, db)
		close(lifecycleDone)
		if err := <-result; err != nil {
			t.Fatalf("staging contention precondition: %v", err)
		}
		if got := sealedRefusalCount(t, db); got != 1 {
			t.Fatalf("emitter lost or duplicated the fixture sample: %d", got)
		}
	})
}

// Wait for the staging attempt itself, so helper prework or scheduling cannot
// consume the hold interval before the writer is actually contended.
func waitForRefusalStagingContention(ctx context.Context, db *store.DB, sqliteWaits int64, blockerPID int) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if db.Engine() == store.EngineSQLite {
			if db.SQLiteWrite().Stats().WaitCount > sqliteWaits {
				return nil
			}
		} else {
			var waiting bool
			if err := db.PG().QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname=current_database() AND wait_event_type='Lock'
				AND query LIKE '%ReadAuthRefusalWindow%'
				AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting); err != nil {
				return fmt.Errorf("observe staging writer contention: %w", err)
			}
			if waiting {
				return nil
			}
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return fmt.Errorf("staging never visibly contended: %w", ctx.Err())
		}
	}
}
