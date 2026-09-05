package app

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
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

	var jobRuns atomic.Int64
	nodeIDs := []string{"node-a", "node-b", "node-c"}
	cancels := make(map[string]context.CancelFunc, len(nodeIDs))
	schedulers := make(map[string]*Scheduler, len(nodeIDs))
	for _, id := range nodeIDs {
		s := &Scheduler{
			Interval:  time.Hour, // only the startup catch-up runs per leadership term
			Deadline:  time.Second,
			Log:       testLogger(),
			Lease:     coord,
			NodeID:    id,
			LeaseTTL:  400 * time.Millisecond,
			Heartbeat: 100 * time.Millisecond,
			Jobs:      []ScheduledJob{{Name: "gc", Run: func(context.Context) error { jobRuns.Add(1); return nil }}},
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancels[id] = cancel
		schedulers[id] = s
		go s.Run(ctx)
	}
	t.Cleanup(func() {
		for _, cancel := range cancels {
			cancel()
		}
	})

	leader := waitForSingleLeader(t, schedulers)
	// Exactly one node ran the startup catch-up: the singleton executed at most
	// once across the cluster (Interval is an hour, so nothing else can fire).
	time.Sleep(300 * time.Millisecond)
	if got := jobRuns.Load(); got != 1 {
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

	// Kill the leader: another node must take over within a bounded time.
	cancels[leader]()
	delete(schedulers, leader)
	newLeader := waitForSingleLeader(t, schedulers)
	if newLeader == leader {
		t.Fatalf("leadership did not move off the terminated node %q", leader)
	}
	// The new leader ran its own startup catch-up exactly once more: takeover
	// executes the singleton one additional time, never once per surviving node.
	time.Sleep(300 * time.Millisecond)
	if got := jobRuns.Load(); got != 2 {
		t.Fatalf("startup job ran %d times after failover, want exactly 2", got)
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
