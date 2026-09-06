package admission

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

func replacementLimiter(t *testing.T, now func() time.Time, memory uint32) *Limiter {
	t.Helper()
	l, err := New(Config{BudgetMiB: DefaultBudgetMiB, ArgonMemoryKiB: memory, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestMaximumKDFMemoryDoesNotOverflowConcurrency(t *testing.T) {
	if _, err := New(Config{BudgetMiB: DefaultBudgetMiB, ArgonMemoryKiB: math.MaxUint32}); err == nil {
		t.Fatal("maximum memory fit inside default budget")
	}
	l, err := New(Config{BudgetMiB: 4*1024*1024 + HeadroomMiB, ArgonMemoryKiB: math.MaxUint32})
	if err != nil || l.Concurrency() != 1 {
		t.Fatalf("exact large budget: limiter=%v error=%v", l, err)
	}
}

func TestInheritCountersKeepsAllAbuseStateAndNewConcurrency(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	previous := replacementLimiter(t, fixedClock(&now), 64*1024)
	next := replacementLimiter(t, fixedClock(&now), 128*1024)
	for range PerIPPerMinute {
		release, err := previous.Enter(t.Context(), "source")
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	for range MetaPerIPPerMinute {
		previous.AllowDiscovery("source")
	}
	for range IssuerRefreshPerMinute {
		previous.AllowIssuerRefresh("issuer")
	}
	for range FailuresBeforeBackoff + 2 {
		previous.RecordFailure("account")
	}
	// A below-threshold account must retain its failure count too.
	for range FailuresBeforeBackoff {
		previous.RecordFailure("near-threshold")
	}
	if err := next.InheritCounters(previous); err != nil {
		t.Fatal(err)
	}
	if next.Concurrency() != 2 || next.Snapshot().InFlight != 0 {
		t.Fatal("inherited old semaphore instead of new sizing")
	}
	if _, err := next.Enter(t.Context(), "source"); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("IP allowance reset: %v", err)
	}
	if next.AllowDiscovery("source") || next.AllowIssuerRefresh("issuer") {
		t.Fatal("metadata or issuer allowance reset")
	}
	if next.AccountDelay("account") != 2*time.Second {
		t.Fatal("active backoff lost")
	}
	if !next.RecordFailure("near-threshold") {
		t.Fatal("below-threshold failures lost")
	}
	// Maps and slice backing arrays belong to separate generations.
	next.RecordSuccess("account")
	if previous.AccountDelay("account") != 2*time.Second {
		t.Fatal("account map aliases previous generation")
	}
	next.ipHits["source"][0] = time.Time{}
	if previous.ipHits["source"][0].IsZero() {
		t.Fatal("window slice aliases previous generation")
	}
}

func TestInheritCountersRejectsActiveAdmissionsAndDirtyCandidate(t *testing.T) {
	previous := replacementLimiter(t, time.Now, 64*1024)
	next := replacementLimiter(t, time.Now, 128*1024)
	for _, active := range []*Limiter{previous, next} {
		release, err := active.Enter(t.Context(), "")
		if err != nil {
			t.Fatal(err)
		}
		if err := next.InheritCounters(previous); !errors.Is(err, ErrNotQuiescent) {
			t.Fatalf("active admission transfer: %v", err)
		}
		release()
	}
	next.RecordFailure("candidate-counter")
	if err := next.InheritCounters(previous); !errors.Is(err, ErrCountersNotEmpty) {
		t.Fatalf("candidate counter overwritten: %v", err)
	}
	if len(next.accounts) != 1 {
		t.Fatal("failed transfer mutated candidate")
	}
}

func TestInheritCountersKeepsSharedBackendAndFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	backend := newFakeShared()
	previous := sharedLimiter(t, backend, &now)
	next := replacementLimiter(t, fixedClock(&now), 128*1024)
	for range PerIPPerMinute {
		previous.allowIP("source")
	}
	for range MetaPerIPPerMinute {
		previous.AllowDiscovery("source")
	}
	for range IssuerRefreshPerMinute {
		previous.AllowIssuerRefresh("issuer")
	}
	for range FailuresBeforeBackoff + 1 {
		previous.RecordFailure("account")
	}
	if err := next.InheritCounters(previous); err != nil {
		t.Fatal(err)
	}
	if next.shared != backend || next.Concurrency() != 2 {
		t.Fatal("shared backend or new sizing lost")
	}
	configured := replacementLimiter(t, fixedClock(&now), 128*1024)
	configured.UseShared(backend, testLogger())
	if err := configured.InheritCounters(previous); err != nil {
		t.Fatalf("same preconfigured backend refused: %v", err)
	}
	if next.allowIP("source") || next.AllowDiscovery("source") || next.AllowIssuerRefresh("issuer") || next.AccountDelay("account") <= 0 {
		t.Fatal("shared counters reset")
	}
	backend.setErr(errors.New("offline"))
	if next.AllowDiscovery("other") || next.AccountDelay("other") <= 0 {
		t.Fatal("shared backend failure did not fail closed")
	}
	other := replacementLimiter(t, fixedClock(&now), 128*1024)
	other.UseShared(newFakeShared(), testLogger())
	if err := other.InheritCounters(previous); !errors.Is(err, ErrSharedBackendChanged) {
		t.Fatalf("backend replacement accepted: %v", err)
	}
}

type blockedWindowStore struct {
	SharedStore
	started chan struct{}
	finish  chan struct{}
}

func (s *blockedWindowStore) BumpWindow(ctx context.Context, bucket, subject string, window time.Time) (int64, error) {
	close(s.started)
	<-s.finish
	return s.SharedStore.BumpWindow(ctx, bucket, subject, window)
}

func TestInheritCountersRejectsActiveSharedCounterCall(t *testing.T) {
	previous := replacementLimiter(t, time.Now, 64*1024)
	next := replacementLimiter(t, time.Now, 128*1024)
	backend := &blockedWindowStore{SharedStore: newFakeShared(), started: make(chan struct{}), finish: make(chan struct{})}
	previous.UseShared(backend, testLogger())
	done := make(chan struct{})
	go func() {
		defer close(done)
		previous.AllowDiscovery("source")
	}()
	<-backend.started
	err := next.InheritCounters(previous)
	close(backend.finish)
	<-done
	if !errors.Is(err, ErrNotQuiescent) {
		t.Fatalf("active shared counter transfer = %v", err)
	}
	if err := next.InheritCounters(previous); err != nil {
		t.Fatalf("drained shared transfer = %v", err)
	}
}

func TestInheritCountersConcurrentOperationsStayRaceFree(t *testing.T) {
	previous := replacementLimiter(t, time.Now, 64*1024)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 100 {
				previous.RecordFailure("account")
				previous.AllowDiscovery("source")
				if release, err := previous.Enter(context.Background(), ""); err == nil {
					release()
				}
			}
		})
	}
	for range 100 {
		next := replacementLimiter(t, time.Now, 128*1024)
		if err := next.InheritCounters(previous); err != nil && !errors.Is(err, ErrNotQuiescent) {
			t.Fatal(err)
		}
	}
	workers.Wait()
	next := replacementLimiter(t, time.Now, 128*1024)
	if err := next.InheritCounters(previous); err != nil {
		t.Fatal(err)
	}
	if next.AccountDelay("account") <= 0 || next.AllowDiscovery("source") {
		t.Fatal("drained handoff lost accumulated abuse state")
	}
}
