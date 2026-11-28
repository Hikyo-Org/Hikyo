package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
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
	afterClaim  func()
	afterRenew  func()
	dbSkew      atomic.Int64 // datastore clock offset in ns
	lateOnce    atomic.Int64 // offset applied to the next Now read only
	nowErr      atomic.Bool
}

func (f *fakeLease) set(fn func(*fakeLease)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeLease) Now(context.Context) (time.Time, error) {
	if f.nowErr.Load() {
		return time.Time{}, errors.New("clock down")
	}
	return time.Now().UTC().Add(time.Duration(f.dbSkew.Load() + f.lateOnce.Swap(0))), nil
}

func (f *fakeLease) ClaimLease(_ context.Context, _, _ string, _, _ time.Time) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	if !f.claimHeld {
		return 0, false, nil
	}
	f.fence++
	if f.afterClaim != nil {
		f.afterClaim()
	}
	return f.fence, true, nil
}

func (f *fakeLease) RenewLease(ctx context.Context, _, _ string, _ int64, _, _ time.Time) (bool, error) {
	f.mu.Lock()
	blocks, err, held, after := f.renewBlocks, f.renewErr, f.renewHeld, f.afterRenew
	f.renews++
	f.mu.Unlock()
	if after != nil {
		after()
	}
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

type cancelledClaimLease struct {
	fakeLease
	cancel            context.CancelFunc
	releasedFence     int64
	releaseContextErr error
}

func (l *cancelledClaimLease) ClaimLease(ctx context.Context, name, owner string, now, expires time.Time) (int64, bool, error) {
	fence, _, err := l.fakeLease.ClaimLease(ctx, name, owner, now, expires)
	if err != nil {
		return 0, false, err
	}
	l.cancel()
	return fence, false, context.Canceled
}

func (l *cancelledClaimLease) ReleaseLease(ctx context.Context, name, owner string, fence int64) error {
	l.releasedFence = fence
	l.releaseContextErr = ctx.Err()
	return l.fakeLease.ReleaseLease(ctx, name, owner, fence)
}

func TestSchedulerHAReleasesProvisionalClaimOnStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	lease := &cancelledClaimLease{fakeLease: fakeLease{claimHeld: true, renewHeld: true}, cancel: cancel}
	var runs atomic.Int64
	scheduler := &Scheduler{Lease: lease, NodeID: "provisional-node", Heartbeat: 10 * time.Millisecond, LeaseTTL: time.Minute, Log: testLogger(), Jobs: []ScheduledJob{{Name: "must-not-run", Run: func(context.Context) error { runs.Add(1); return nil }}}}
	done := make(chan struct{})
	go func() { defer close(done); scheduler.Run(ctx) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after canceled claim")
	}
	if runs.Load() != 0 {
		t.Fatal("unconfirmed claim ran a job")
	}
	if lease.releasedFence != 1 || lease.releaseContextErr != nil {
		t.Fatalf("provisional cleanup: fence=%d context=%v", lease.releasedFence, lease.releaseContextErr)
	}
	if lease.claims != 1 {
		t.Fatalf("shutdown attempted %d claims, want1", lease.claims)
	}
}

func haScheduler(lease *fakeLease, now func() time.Time, runs *atomic.Int64) *Scheduler {
	return &Scheduler{
		Interval: time.Hour, Deadline: time.Second, Log: testLogger(), Now: now,
		Lease: lease, NodeID: "node-a", LeaseTTL: 40 * time.Millisecond, Heartbeat: 10 * time.Millisecond,
		Jobs: []ScheduledJob{{Name: "gc", Run: func(context.Context) error { runs.Add(1); return nil }}},
	}
}

func claims(l *fakeLease) int { l.mu.Lock(); defer l.mu.Unlock(); return l.claims }

func TestSchedulerHAClaimLandingAfterExpiryStartsNoTerm(t *testing.T) {
	var runs atomic.Int64
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	lease.afterClaim = func() { lease.lateOnce.Store(int64(time.Hour)); lease.claimHeld, lease.renewHeld = false, false }
	s := haScheduler(lease, nil, &runs)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "claim", func() bool { return claims(lease) >= 1 })
	time.Sleep(80 * time.Millisecond)
	if s.IsLeader() || runs.Load() != 0 {
		t.Fatalf("expired claim started a term: leader=%v runs=%d", s.IsLeader(), runs.Load())
	}
	cancel()
	<-done
}

func TestSchedulerHARenewLandingAfterExpiryDropsLeadership(t *testing.T) {
	var runs atomic.Int64
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	s := haScheduler(lease, nil, &runs)
	var logged syncBuf
	s.Log = slog.New(slog.NewTextHandler(&logged, nil))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "leadership", s.IsLeader)
	var once atomic.Bool
	lease.set(func(f *fakeLease) {
		f.afterRenew = func() {
			if once.CompareAndSwap(false, true) {
				f.dbSkew.Add(int64(time.Hour))
			}
		}
	})
	waitFor(t, "expired renewal to be rejected in-tick", func() bool { return strings.Contains(logged.String(), "lapsed before its grant") })
	cancel()
	<-done
}

func TestSchedulerHAClockErrorAfterClaimStartsNoTerm(t *testing.T) {
	var runs atomic.Int64
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	lease.afterClaim = func() { lease.nowErr.Store(true) }
	s := haScheduler(lease, nil, &runs)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "claim", func() bool { return claims(lease) >= 1 })
	time.Sleep(80 * time.Millisecond)
	if s.IsLeader() || runs.Load() != 0 {
		t.Fatalf("claim with unreadable clock started a term: leader=%v runs=%d", s.IsLeader(), runs.Load())
	}
	cancel()
	<-done
}

func TestSchedulerHALocalClockSkewDoesNotBlockLeadership(t *testing.T) {
	var runs atomic.Int64
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	s := haScheduler(lease, func() time.Time { return time.Now().Add(24 * time.Hour) }, &runs)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "leadership under local clock skew", s.IsLeader)
	cancel()
	<-done
}

func TestGateWaitsForRuntimeConfigPublication(t *testing.T) {
	pending := fmt.Errorf("window: %w", service.ErrSelfConfigPublicationPending)
	for name, transient := range map[string]error{
		"not ready":           service.ErrSelfConfigUnavailable,
		"previous generation": pending,
	} {
		var calls, runs, refusals atomic.Int64
		job := gateOnRuntimeConfig(ScheduledJob{
			Name:      "gc",
			Run:       func(context.Context) error { runs.Add(1); return nil },
			OnRefused: func(_ context.Context, err error) error { refusals.Add(1); return err },
		}, 300*time.Millisecond, func(context.Context) error {
			if calls.Add(1) < 3 {
				return transient
			}
			return nil
		})
		if err := job.Run(t.Context()); err != nil || runs.Load() != 1 || refusals.Load() != 0 {
			t.Fatalf("%s: err=%v runs=%d refusals=%d", name, err, runs.Load(), refusals.Load())
		}
	}
}

func TestGateNeverRetriesFencedAndRefusesOnceAfterGrace(t *testing.T) {
	for name, refusal := range map[string]error{
		"fenced":  fmt.Errorf("wrapped: %w", service.ErrSelfConfigFenced),
		"timeout": service.ErrSelfConfigUnavailable,
		"pending": service.ErrSelfConfigPublicationPending,
	} {
		var calls, refusals atomic.Int64
		job := gateOnRuntimeConfig(ScheduledJob{
			Name:      "gc",
			Run:       func(context.Context) error { t.Fatal("refused job ran"); return nil },
			OnRefused: func(_ context.Context, err error) error { refusals.Add(1); return err },
		}, 30*time.Millisecond, func(context.Context) error { calls.Add(1); return refusal })
		if err := job.Run(t.Context()); !errors.Is(err, refusal) || refusals.Load() != 1 {
			t.Fatalf("%s: err=%v refusals=%d", name, err, refusals.Load())
		}
		if name == "fenced" && calls.Load() != 1 {
			t.Fatalf("fenced capture retried %d times", calls.Load())
		}
		if name != "fenced" && calls.Load() < 2 {
			t.Fatalf("%s capture not retried: %d", name, calls.Load())
		}
	}
}

func TestSchedulerHARenewClockErrorReconfirmsWithoutWaitingATTL(t *testing.T) {
	var runs atomic.Int64
	// A TTL far beyond the test: leadership must return by reconfirmation.
	lease := &fakeLease{claimHeld: true, renewHeld: true}
	s := haScheduler(lease, nil, &runs)
	s.LeaseTTL = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitFor(t, "leadership", s.IsLeader)
	var once atomic.Bool
	lease.set(func(f *fakeLease) {
		f.afterRenew = func() {
			if once.CompareAndSwap(false, true) {
				f.nowErr.Store(true)
				go func() { time.Sleep(15 * time.Millisecond); f.nowErr.Store(false) }()
			}
		}
	})
	waitFor(t, "term restarted after clock error", func() bool { return runs.Load() >= 2 })
	waitFor(t, "leadership again", s.IsLeader)
	lease.mu.Lock()
	claims := lease.claims
	lease.mu.Unlock()
	if claims != 1 {
		t.Fatalf("node reclaimed (%d claims) instead of reconfirming its live lease", claims)
	}
	cancel()
	<-done
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
