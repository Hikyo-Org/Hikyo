package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLease is a deterministic LeaseManager for the HA leadership tests: no
// datastore, behaviour flipped from the test goroutine.
type fakeLease struct {
	mu          sync.Mutex
	claimHeld   bool
	renewHeld   bool
	renewErr    error
	renewBlocks bool
	fence       int64
	claims      int
	renews      int
	releases    int
}

func (f *fakeLease) set(fn func(*fakeLease)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeLease) ClaimLease(_ context.Context, _, _ string, _, _ time.Time) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	if !f.claimHeld {
		return 0, false, nil
	}
	f.fence++
	return f.fence, true, nil
}

func (f *fakeLease) RenewLease(ctx context.Context, _, _ string, _ int64, _ time.Time) (bool, error) {
	f.mu.Lock()
	blocks, err, held := f.renewBlocks, f.renewErr, f.renewHeld
	f.renews++
	f.mu.Unlock()
	if blocks {
		<-ctx.Done()
		return false, ctx.Err()
	}
	return held, err
}

func (f *fakeLease) ReleaseLease(context.Context, string, string, int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return nil
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSchedulerHAOnlyLeaderRunsJobs(t *testing.T) {
	var runs atomic.Int64
	lease := &fakeLease{claimHeld: false} // this node never wins the lease
	s := &Scheduler{
		Interval:  time.Hour,
		Deadline:  time.Second,
		Log:       testLogger(),
		Lease:     lease,
		NodeID:    "node-a",
		LeaseTTL:  40 * time.Millisecond,
		Heartbeat: 10 * time.Millisecond,
		Jobs:      []ScheduledJob{{Name: "gc", Run: func(context.Context) error { runs.Add(1); return nil }}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	// Give the loop several heartbeats to prove a non-leader stays idle.
	time.Sleep(60 * time.Millisecond)
	if runs.Load() != 0 {
		t.Fatalf("non-leader ran %d jobs, want 0", runs.Load())
	}
	if s.IsLeader() {
		t.Fatal("non-leader reports IsLeader true")
	}
	cancel()
	<-done
}

func TestSchedulerHALeaderRunsStartupAndReleasesOnStop(t *testing.T) {
	var runs atomic.Int64
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	s := &Scheduler{
		Interval:  time.Hour, // only the startup catch-up runs
		Deadline:  time.Second,
		Log:       testLogger(),
		Lease:     lease,
		NodeID:    "node-a",
		LeaseTTL:  40 * time.Millisecond,
		Heartbeat: 10 * time.Millisecond,
		Jobs:      []ScheduledJob{{Name: "gc", Run: func(context.Context) error { runs.Add(1); return nil }}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "leadership", s.IsLeader)
	waitFor(t, "startup job", func() bool { return runs.Load() >= 1 })
	cancel()
	<-done
	lease.mu.Lock()
	releases := lease.releases
	lease.mu.Unlock()
	if releases == 0 {
		t.Fatal("leader did not release the lease on shutdown")
	}
}

func TestSchedulerHADropsLeadershipWhenRenewLoses(t *testing.T) {
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	s := &Scheduler{
		Interval:  time.Hour,
		Deadline:  time.Second,
		Log:       testLogger(),
		Lease:     lease,
		NodeID:    "node-a",
		LeaseTTL:  40 * time.Millisecond,
		Heartbeat: 10 * time.Millisecond,
		Jobs:      []ScheduledJob{{Name: "gc", Run: func(context.Context) error { return nil }}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "leadership", s.IsLeader)
	// Another node takes over: our renew now reports we no longer hold it.
	lease.set(func(f *fakeLease) { f.renewHeld = false })
	waitFor(t, "leadership drop on lost renew", func() bool { return !s.IsLeader() })
	cancel()
	<-done
}

func TestSchedulerHADropsLeadershipWhenRenewBlocks(t *testing.T) {
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	s := &Scheduler{
		Interval:  time.Hour,
		Deadline:  time.Second,
		Log:       testLogger(),
		Lease:     lease,
		NodeID:    "node-a",
		LeaseTTL:  40 * time.Millisecond,
		Heartbeat: 10 * time.Millisecond,
		Jobs:      []ScheduledJob{{Name: "gc", Run: func(context.Context) error { return nil }}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "leadership", s.IsLeader)
	// The datastore stops responding: renew blocks. Coordination is lost, so
	// leadership must drop (fail closed) rather than hang past the TTL.
	lease.set(func(f *fakeLease) { f.renewBlocks = true })
	waitFor(t, "leadership drop on blocked renew", func() bool { return !s.IsLeader() })
	cancel()
	<-done
}

func TestSchedulerRunsAtStartupAndOnTicker(t *testing.T) {
	var runs atomic.Int64
	reached := make(chan struct{}, 1)
	s := &Scheduler{
		Interval: 10 * time.Millisecond,
		Deadline: time.Second,
		Log:      testLogger(),
		Jobs: []ScheduledJob{{
			Name: "payload_gc",
			Run: func(context.Context) error {
				if runs.Add(1) >= 2 {
					select {
					case reached <- struct{}{}:
					default:
					}
				}
				return nil
			},
			LastSuccess: func(context.Context) (time.Time, bool, error) {
				return time.Now().UTC(), true, nil
			},
		}},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	select {
	case <-reached:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("scheduler did not run at startup and on its ticker")
	}
	<-done
}

func TestSchedulerDeadlineAndStaleSuccessAreLoudOpsLogs(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))
	s := &Scheduler{
		Deadline: 10 * time.Millisecond,
		Log:      log,
		Now:      func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) },
		Jobs: []ScheduledJob{{
			Name: "payload_gc",
			Run: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			LastSuccess: func(context.Context) (time.Time, bool, error) {
				return time.Date(2026, 8, 14, 11, 59, 0, 0, time.UTC), true, nil
			},
		}},
	}
	s.runOnce(t.Context(), "startup")
	text := logged.String()
	for _, want := range []string{"scheduler job failed", "payload_gc", "last_prune_success is stale"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ops log %q does not contain %q", text, want)
		}
	}
}

func TestSchedulerExposesLastPruneSuccessOnOpsLog(t *testing.T) {
	var logged bytes.Buffer
	at := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	s := &Scheduler{
		Log: slog.New(slog.NewTextHandler(&logged, nil)),
		Now: func() time.Time { return at.Add(time.Hour) },
	}
	s.checkHealth(t.Context(), ScheduledJob{
		Name: "payload_gc",
		LastSuccess: func(context.Context) (time.Time, bool, error) {
			return at, true, nil
		},
	})
	text := logged.String()
	for _, want := range []string{"scheduler job health", "payload_gc", "last_prune_success=2026-08-15T11:00:00.000Z"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ops log %q does not contain %q", text, want)
		}
	}
}
