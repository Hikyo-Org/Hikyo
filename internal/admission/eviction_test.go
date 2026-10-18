package admission

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWindowEvictionPreservesLivePolicy(t *testing.T) {
	for _, expired := range []int{0, 1, MaxTrackedSubjects / 4} {
		t.Run(fmt.Sprintf("expired_%d", expired), func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			l, err := New(Config{ArgonMemoryKiB: 64 * 1024, Now: fixedClock(&now)})
			if err != nil {
				t.Fatal(err)
			}
			cutoff := now.Add(-time.Minute)
			for i := range MaxTrackedSubjects {
				at := now.Add(-time.Second)
				if i < expired {
					at = cutoff
				}
				l.ipHits[fmt.Sprintf("source-%d", i)] = []time.Time{at}
			}
			// The fallback uses the oldest first hit, even when a window also has
			// newer hits. Expired reclamation must not forgive any live subject.
			l.ipHits[fmt.Sprintf("source-%d", MaxTrackedSubjects-1)] = []time.Time{cutoff.Add(time.Nanosecond), now}
			if wait := l.allowIPIn(l.ipHits, "new-source", PerIPPerMinute); wait != 0 {
				t.Fatal(wait)
			}
			want := MaxTrackedSubjects
			if expired > 0 {
				want = MaxTrackedSubjects - expired + 1
			}
			if len(l.ipHits) != want {
				t.Fatalf("buckets = %d, want %d", len(l.ipHits), want)
			}
			_, oldestKept := l.ipHits[fmt.Sprintf("source-%d", MaxTrackedSubjects-1)]
			if oldestKept != (expired > 0) {
				t.Fatalf("oldest live kept = %v, expired = %d", oldestKept, expired)
			}
			for i := expired; i < MaxTrackedSubjects-1; i++ {
				if len(l.ipHits[fmt.Sprintf("source-%d", i)]) != 1 {
					t.Fatalf("unrelated live source %d changed", i)
				}
			}
		})
	}
}

func TestAccountEvictionReclaimsZeroAndExactDeadline(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l, err := New(Config{ArgonMemoryKiB: 64 * 1024, Now: fixedClock(&now)})
	if err != nil {
		t.Fatal(err)
	}
	for i := range MaxTrackedSubjects {
		until := now.Add(time.Nanosecond)
		if i == 0 {
			until = time.Time{}
		}
		if i == 1 {
			until = now
		}
		l.accounts[bucketKey(fmt.Sprintf("account-%d", i))] = accountBackoff{failures: cappedFailures, until: until}
	}
	l.RecordFailure("new-account")
	if len(l.accounts) != MaxTrackedSubjects-1 {
		t.Fatalf("accounts = %d", len(l.accounts))
	}
	for i := range MaxTrackedSubjects {
		state, ok := l.accounts[bucketKey(fmt.Sprintf("account-%d", i))]
		if ok != (i >= 2) {
			t.Fatalf("account %d retained = %v", i, ok)
		}
		if ok && (state.failures != cappedFailures || state.until != now.Add(time.Nanosecond)) {
			t.Fatalf("live account %d changed", i)
		}
	}
	// A surviving account keeps its curve and cannot cross the audit threshold
	// again. An evicted account starts fresh, crossing exactly once at six.
	if l.RecordFailure("account-2") {
		t.Fatal("survivor crossed threshold again")
	}
	if got := l.AccountDelay("account-2"); got != MaxAccountBackoff {
		t.Fatal(got)
	}
	for i := range FailuresBeforeBackoff + 1 {
		if got := l.RecordFailure("account-0"); got != (i == FailuresBeforeBackoff) {
			t.Fatalf("threshold signal at failure %d = %v", i+1, got)
		}
	}
}

func TestConcurrentEvictionAtTrackingBound(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l, err := New(Config{ArgonMemoryKiB: 64 * 1024, Now: fixedClock(&now)})
	if err != nil {
		t.Fatal(err)
	}
	for i := range MaxTrackedSubjects {
		l.ipHits[fmt.Sprintf("seed-%d", i)] = []time.Time{now}
		l.accounts[bucketKey(fmt.Sprintf("seed-%d", i))] = accountBackoff{failures: cappedFailures, until: now.Add(time.Minute)}
	}
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 64 {
				subject := fmt.Sprintf("worker-%d-%d", worker, i)
				_ = l.allowIPIn(l.ipHits, subject, PerIPPerMinute)
				l.RecordFailure(subject)
				_ = l.AccountDelay(subject)
				if i%2 == 0 {
					l.RecordSuccess(subject)
				}
				_ = l.Snapshot()
			}
		}()
	}
	wg.Wait()
	if len(l.ipHits) > MaxTrackedSubjects || len(l.accounts) > MaxTrackedSubjects {
		t.Fatal("tracking bound exceeded")
	}
}
