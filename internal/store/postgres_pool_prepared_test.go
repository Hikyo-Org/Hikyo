package store

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func poolReplacementFixture(t *testing.T, maximum int32, dsnMaximum string) *DB {
	t.Helper()
	cfg := ownedAdmissionConfig(t, EnginePostgres)
	cfg.PostgresPoolMax = maximum
	db, err := admittedStoreFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if dsnMaximum != "" {
		u, err := url.Parse(cfg.DSN)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("pool_max_conns", dsnMaximum)
		u.RawQuery = q.Encode()
		cfg.DSN = u.String()
		admission := db.admission
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = Open(t.Context(), cfg, admission)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func preparePoolReplacement(t *testing.T, db *DB, maximum int32) *PreparedPostgresPool {
	t.Helper()
	prepared, err := db.PreparePostgresPool(t.Context(), maximum)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prepared.Close() })
	return prepared
}

func TestPreparedPostgresPoolPreservesTransactionsAndCoordination(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	coordination := db.Coordination()
	instance, incarnation, err := db.RecoveryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	before := db.pool.current
	tx, err := db.BeginPostgres(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	prepared := preparePoolReplacement(t, db, 2)
	if db.PG() != before.pool || db.ConnectionPoolLimits().Primary != 4 {
		t.Fatal("preparation changed the active pool")
	}
	if _, err := coordination.BumpWindow(t.Context(), IPBucket, "pool-swap", accountWindow); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if db.PG() == before.pool || db.ConnectionPoolLimits().Primary != 2 {
		t.Fatal("activation did not replace the effective pool")
	}
	select {
	case <-before.done:
		t.Fatal("old pool closed with an unsettled transaction")
	default:
	}
	var one int
	if err := tx.QueryRow(t.Context(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("old transaction lost its connection: %d %v", one, err)
	}
	if count, err := coordination.BumpWindow(t.Context(), IPBucket, "pool-swap", accountWindow); err != nil || count != 2 {
		t.Fatalf("stable coordination lost counter: %d %v", count, err)
	}
	if gotInstance, gotIncarnation, err := db.RecoveryIdentity(); err != nil || gotInstance != instance || gotIncarnation != incarnation {
		t.Fatalf("admission identity changed: %v", err)
	}
	// The driver's real acquisition limit changes too, including while an old
	// transaction remains borrowed from the retiring generation.
	first, err := db.pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := db.pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if extra, err := db.pool.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		if extra != nil {
			extra.Release()
		}
		t.Fatalf("new pool did not enforce two connections: %v", err)
	}
	first.Release()
	second.Release()
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-before.done:
	case <-time.After(5 * time.Second):
		t.Fatal("retired pool did not close after transaction settlement")
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckAdmission(t.Context()); err != nil {
		t.Fatalf("closing activated handle closed DB: %v", err)
	}
}

func TestPreparedPostgresPoolDefaultsAndStaleCandidates(t *testing.T) {
	for _, dsnMaximum := range []string{"", "6"} {
		t.Run("dsnMaximum="+dsnMaximum, func(t *testing.T) {
			db := poolReplacementFixture(t, 4, dsnMaximum)
			before := db.PG()
			unchanged := preparePoolReplacement(t, db, 4)
			if unchanged.candidate != nil {
				t.Fatal("unchanged effective limit opened another pool")
			}
			if err := unchanged.Activate(t.Context()); err != nil {
				t.Fatal(err)
			}
			if db.PG() != before {
				t.Fatal("no-op replaced the pool")
			}
			stale := preparePoolReplacement(t, db, 3)
			reset := preparePoolReplacement(t, db, 0)
			if err := reset.Activate(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := 10
			if dsnMaximum != "" {
				want = 6
			}
			if got := db.ConnectionPoolLimits().Primary; got != want {
				t.Fatalf("default limit=%d want=%d", got, want)
			}
			if err := stale.Activate(t.Context()); err == nil {
				t.Fatal("stale preparation overwrote newer generation")
			}
			if db.ConnectionPoolLimits().Primary != want {
				t.Fatal("stale attempt changed limit")
			}
			if err := reset.Activate(t.Context()); err != nil {
				t.Fatalf("activation not idempotent: %v", err)
			}
		})
	}
}

func TestPreparedPostgresPoolFailuresDoNotPublish(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	before := db.PG()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.PreparePostgresPool(ctx, 2); err == nil {
		t.Fatal("canceled preparation succeeded")
	}
	if _, err := db.PreparePostgresPool(t.Context(), -1); err == nil {
		t.Fatal("negative limit accepted")
	}
	prepared := preparePoolReplacement(t, db, 2)
	if err := prepared.Activate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled activation: %v", err)
	}
	if db.PG() != before {
		t.Fatal("canceled activation changed pool")
	}
	candidate := prepared.candidate
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if candidate.Ping(t.Context()) == nil {
		t.Fatal("aborted candidate still open")
	}
	if prepared.Activate(t.Context()) == nil {
		t.Fatal("closed candidate activated")
	}
	if db.PG() != before || db.Ping(t.Context()) != nil {
		t.Fatal("candidate disposal damaged active pool")
	}

	failed := preparePoolReplacement(t, db, 2)
	failed.candidate.Close()
	if failed.Activate(t.Context()) == nil {
		t.Fatal("unavailable candidate activated")
	}
	if db.PG() != before {
		t.Fatal("failed validation changed pool")
	}
	pending := preparePoolReplacement(t, db, 3)
	pendingPool := pending.candidate
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if pendingPool.Ping(t.Context()) == nil {
		t.Fatal("DB close leaked prepared pool")
	}
	if pending.Activate(t.Context()) == nil {
		t.Fatal("candidate activated after database close")
	}
	if _, err := db.PreparePostgresPool(t.Context(), 3); err == nil {
		t.Fatal("closed database prepared replacement")
	}
}

func TestPreparedPostgresPoolRefusesChangedAdmission(t *testing.T) {
	for _, mutation := range []string{
		"UPDATE upgrade_control SET maintenance=1",
		"UPDATE upgrade_control SET generation=generation+1",
		"UPDATE upgrade_control SET instance_id='another-owner'",
		"UPDATE upgrade_control SET incarnation=repeat('0',64)",
		"UPDATE upgrade_control SET schema_digest=repeat('0',64)",
	} {
		t.Run(mutation, func(t *testing.T) {
			db := poolReplacementFixture(t, 4, "")
			before := db.PG()
			prepared := preparePoolReplacement(t, db, 2)
			if _, err := before.Exec(t.Context(), mutation); err != nil {
				t.Fatal(err)
			}
			if err := prepared.Activate(t.Context()); err == nil {
				t.Fatalf("changed admission accepted: %v", err)
			}
			if db.PG() != before {
				t.Fatal("refused candidate replaced active pool")
			}
			if _, err := db.PreparePostgresPool(t.Context(), 3); err == nil {
				t.Fatal("preparation accepted invalid admission")
			}
		})
	}
}

func TestPreparedPostgresPoolConcurrentCoordination(t *testing.T) {
	db := poolReplacementFixture(t, 4, "")
	coordination := db.Coordination()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	ready := make(chan struct{}, 2)
	results := make(chan error, 2)
	for range 2 {
		workers.Go(func() {
			if _, err := coordination.Now(ctx); err != nil {
				ready <- struct{}{}
				results <- err
				return
			}
			ready <- struct{}{}
			for {
				if ctx.Err() != nil {
					results <- nil
					return
				}
				if _, err := coordination.Now(ctx); err != nil {
					if ctx.Err() != nil {
						results <- nil
					} else {
						results <- err
					}
					return
				}
				_ = db.ConnectionPoolLimits()
			}
		})
	}
	t.Cleanup(func() { cancel(); workers.Wait() })
	<-ready
	<-ready
	for _, maximum := range []int32{2, 5, 3, 4} {
		prepared, err := db.PreparePostgresPool(ctx, maximum)
		if err != nil {
			t.Fatal(err)
		}
		if err := prepared.Activate(ctx); err != nil {
			_ = prepared.Close()
			t.Fatal(err)
		}
		_ = prepared.Close()
	}
	cancel()
	workers.Wait()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("coordination interrupted by pool swap: %v", err)
		}
	}
}

func TestPreparedPostgresPoolSQLitePolicy(t *testing.T) {
	db, err := admittedStoreFixture(t, ownedAdmissionConfig(t, EngineSQLite))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.PreparePostgresPool(t.Context(), 2); err == nil {
		t.Fatal("sqlite accepted postgres capacity")
	}
	prepared := preparePoolReplacement(t, db, 0)
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if db.ConnectionPoolLimits() != (ConnectionPoolLimits{Primary: 1, ReadOnly: 4}) {
		t.Fatal("sqlite pool limits changed")
	}
}
