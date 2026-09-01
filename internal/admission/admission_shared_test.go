package admission

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakeShared is an in-memory SharedStore for the HA admission tests: two
// limiters pointed at one fakeShared model two nodes hitting one datastore.
type fakeShared struct {
	mu          sync.Mutex
	windows     map[string]int64
	failures    map[string]int64
	lastFailure map[string]time.Time
	err         error
}

func newFakeShared() *fakeShared {
	return &fakeShared{windows: map[string]int64{}, failures: map[string]int64{}, lastFailure: map[string]time.Time{}}
}

func (f *fakeShared) setErr(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }

func (f *fakeShared) BumpWindow(_ context.Context, bucket, subject string, window time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	k := bucket + "|" + subject + "|" + window.Format(time.RFC3339)
	f.windows[k]++
	return f.windows[k], nil
}

func (f *fakeShared) AccountFailureState(_ context.Context, subject string) (int64, time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, time.Time{}, false, f.err
	}
	n, ok := f.failures[subject]
	if !ok {
		return 0, time.Time{}, false, nil
	}
	return n, f.lastFailure[subject], true, nil
}

func (f *fakeShared) RecordAccountFailure(_ context.Context, subject string, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.failures[subject]++
	f.lastFailure[subject] = now
	return f.failures[subject], nil
}

func (f *fakeShared) ClearAccount(_ context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	delete(f.failures, subject)
	delete(f.lastFailure, subject)
	return nil
}

func sharedLimiter(t *testing.T, store SharedStore, now *time.Time) *Limiter {
	t.Helper()
	l, err := New(Config{BudgetMiB: DefaultBudgetMiB, ArgonMemoryKiB: 64 * 1024, Now: fixedClock(now)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.UseShared(store, testLogger())
	return l
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSharedIPBudgetIsInstanceWideAcrossNodes(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 30, 0, time.UTC)
	store := newFakeShared()
	nodeA := sharedLimiter(t, store, &now)
	nodeB := sharedLimiter(t, store, &now)

	// The per-IP allowance is installation-wide: split across two nodes it is
	// still one budget. PerIPPerMinute attempts succeed, the next is refused
	// even though it lands on the other node.
	allowed := 0
	for i := 0; i < PerIPPerMinute; i++ {
		l := nodeA
		if i%2 == 1 {
			l = nodeB
		}
		if l.allowIP("1.2.3.4") {
			allowed++
		}
	}
	if allowed != PerIPPerMinute {
		t.Fatalf("allowed %d of the first %d, want all", allowed, PerIPPerMinute)
	}
	if nodeB.allowIP("1.2.3.4") {
		t.Fatal("node B admitted an attempt past the shared per-IP budget (node hopping bypassed the limit)")
	}
	// A different window resets the budget.
	now = now.Add(time.Minute)
	if !nodeA.allowIP("1.2.3.4") {
		t.Fatal("new window did not reset the shared per-IP budget")
	}
}

func TestSharedAccountBackoffIsInstanceWide(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	store := newFakeShared()
	nodeA := sharedLimiter(t, store, &now)
	nodeB := sharedLimiter(t, store, &now)

	// Failures accumulate across nodes; the threshold crossing is reported once.
	crossed := 0
	for i := 0; i < FailuresBeforeBackoff+1; i++ {
		l := nodeA
		if i%2 == 1 {
			l = nodeB
		}
		if l.RecordFailure("alice") {
			crossed++
		}
	}
	if crossed != 1 {
		t.Fatalf("threshold crossed %d times, want exactly 1", crossed)
	}
	// The backoff a node A failure set is visible to node B.
	if d := nodeB.AccountDelay("alice"); d <= 0 {
		t.Fatalf("node B delay = %v, want a live shared backoff", d)
	}
	// A success on either node clears it installation-wide.
	nodeA.RecordSuccess("alice")
	if d := nodeB.AccountDelay("alice"); d != 0 {
		t.Fatalf("node B delay after shared success = %v, want 0", d)
	}
}

func TestSharedCounterErrorsFailClosed(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	store := newFakeShared()
	l := sharedLimiter(t, store, &now)
	store.setErr(errors.New("datastore unreachable"))

	if l.allowIP("1.2.3.4") {
		t.Fatal("allowIP admitted while the shared counter was unreachable (must fail closed)")
	}
	if l.AllowDiscovery("1.2.3.4") {
		t.Fatal("AllowDiscovery admitted while the shared counter was unreachable")
	}
	if l.AllowIssuerRefresh("https://issuer.example") {
		t.Fatal("AllowIssuerRefresh admitted while the shared counter was unreachable")
	}
	if d := l.AccountDelay("alice"); d <= 0 {
		t.Fatalf("AccountDelay = %v while unreachable, want a positive hold-off", d)
	}
}
