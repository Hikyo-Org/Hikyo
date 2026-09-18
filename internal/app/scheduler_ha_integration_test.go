package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
	"github.com/jackc/pgx/v5"
)

// TestSchedulerHAThreeNodesOnePostgres boots three schedulers sharing one real
// PostgreSQL lease and asserts the multi-node invariants against the datastore:
// exactly one leader runs jobs, and losing the leader hands leadership to
// another node (automatic failover). It also exercises three concurrent
// runtime admission and guarded coordination against a real signed gate.
func TestSchedulerHAThreeNodesOnePostgres(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI run without HIKYO_TEST_POSTGRES_DSN: the HA scheduler leg must not silently skip")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	db := openSchedulerHAPostgres(t, dsn)
	coord := db.Coordination()

	// Per-node run counts, so the final assertion is per leadership term (which
	// node ran the singleton, and how often), not one cluster-wide total that a
	// duplicate on the wrong node could still satisfy.
	nodeIDs := []string{"node-a", "node-b", "node-c"}
	runs := make(map[string]*atomic.Int64, len(nodeIDs))
	cancels := make(map[string]context.CancelFunc, len(nodeIDs))
	exited := make(map[string]chan struct{}, len(nodeIDs))
	schedulers := make(map[string]*Scheduler, len(nodeIDs))
	var workers sync.WaitGroup
	for _, id := range nodeIDs {
		count := &atomic.Int64{}
		runs[id] = count
		s := &Scheduler{
			Interval:  time.Hour, // only the startup catch-up runs per leadership term
			Deadline:  time.Second,
			Log:       testLogger(),
			Lease:     coord,
			NodeID:    id,
			LeaseTTL:  400 * time.Millisecond,
			Heartbeat: 100 * time.Millisecond,
			Jobs:      []ScheduledJob{{Name: "gc", Run: func(context.Context) error { count.Add(1); return nil }}},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancels[id] = cancel
		schedulers[id] = s
		done := make(chan struct{})
		exited[id] = done
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer close(done)
			s.Run(ctx)
		}()
	}
	t.Cleanup(func() {
		for _, cancel := range cancels {
			cancel()
		}
		workers.Wait()
	})
	total := func() int64 {
		var sum int64
		for _, count := range runs {
			sum += count.Load()
		}
		return sum
	}

	leader := waitForSingleLeader(t, schedulers)
	// Wait for the startup catch-up to land instead of sleeping a fixed span the
	// -race build can outrun. The singleton guarantee, that only one node runs
	// it, rests on waitForSingleLeader above (exactly one leader) plus the hour
	// Interval, which stops anything but the startup catch-up from firing; the
	// per-node counts after every worker has been joined (below) are the final
	// word, this early check only fails fast.
	waitFor(t, "cluster startup job", func() bool { return total() >= 1 })
	if got := total(); got != 1 {
		t.Fatalf("startup job ran %d times across the cluster, want exactly 1", got)
	}
	// Exactly one lease row in the datastore, owned by the elected leader.
	owner, _, live, err := coord.LeaseHolder(context.Background(), "scheduler", time.Now().UTC())
	if err != nil || !live {
		t.Fatalf("lease holder: owner=%q live=%v err=%v", owner, live, err)
	}
	if owner != leader {
		t.Fatalf("datastore lease owner %q disagrees with the elected leader %q", owner, leader)
	}

	// Kill the leader and JOIN it: Run returns only once its term goroutine has
	// drained (or the drain bound elapsed and the lease was left to expire), so
	// nothing the old leader ran can land after this point and the handover
	// below observes a node that has fully stopped, not one still winding down.
	cancels[leader]()
	<-exited[leader]
	delete(schedulers, leader)
	newLeader := waitForSingleLeader(t, schedulers)
	if newLeader == leader {
		t.Fatalf("leadership did not move off the terminated node %q", leader)
	}
	// The new leader ran its own startup catch-up exactly once more: takeover
	// executes the singleton one additional time, never once per surviving node.
	waitFor(t, "failover startup job", func() bool { return total() >= 2 })

	// Stop every node and join every worker BEFORE the final counts, so a
	// delayed extra startup execution cannot slip in after the assertion: the
	// counts below are the complete history of the cluster.
	for _, cancel := range cancels {
		cancel()
	}
	workers.Wait()
	for _, id := range nodeIDs {
		var want int64
		if id == leader || id == newLeader {
			want = 1
		}
		if got := runs[id].Load(); got != want {
			t.Errorf("node %q ran the startup job %d times, want %d (first leader %q, failover leader %q)", id, got, want, leader, newLeader)
		}
	}
	if got := total(); got != 2 {
		t.Errorf("startup job ran %d times across both terms, want exactly 2", got)
	}
}

// waitForSingleLeader waits until exactly one of the live schedulers reports
// leadership and returns its node id.
func waitForSingleLeader(t *testing.T, schedulers map[string]*Scheduler) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var leaders []string
		for id, s := range schedulers {
			if s.IsLeader() {
				leaders = append(leaders, id)
			}
		}
		if len(leaders) == 1 {
			return leaders[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for exactly one leader")
	return ""
}

func openSchedulerHAPostgres(t *testing.T, dsn string) *store.DB {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	base := strings.TrimPrefix(parsed.Path, "/")
	database := fmt.Sprintf("%s_ha_sched_%d", base, time.Now().UnixNano())
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	if _, err := admin.Exec(t.Context(), `CREATE DATABASE "`+strings.ReplaceAll(database, `"`, ``)+`"`); err != nil {
		admin.Close(context.Background())
		t.Fatalf("create database: %v", err)
	}
	parsed.Path = "/" + database
	cfg := store.Config{Engine: store.EnginePostgres, DSN: parsed.String()}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+strings.ReplaceAll(database, `"`, ``)+`" WITH (FORCE)`)
		admin.Close(context.Background())
	})
	root, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(root)
	admission := testfixture.Prepare(t, upgrade.Config{Engine: "postgres", DSN: cfg.DSN}, store.MigrationsFS, "migrations/postgres", root)
	db, err := store.Open(t.Context(), cfg, admission)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
