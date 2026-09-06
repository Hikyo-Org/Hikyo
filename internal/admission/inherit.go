package admission

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"time"
)

var (
	ErrNotQuiescent         = errors.New("admission: generation still has active operations")
	ErrCountersNotEmpty     = errors.New("admission: candidate already has admission counters")
	ErrSharedBackendChanged = errors.New("admission: generation handoff cannot change shared counter backend")
)

// InheritCounters carries abuse history into an unused replacement limiter.
// The caller must stop admitting old-generation requests and drain its workers
// before calling, then activate only the replacement. The old limiter must not
// serve again after a successful handoff. Its backend must remain alive for the
// replacement's lifetime. A candidate may preconfigure that same backend, or
// leave it unset to inherit the existing one.
//
// Limits, clock, queue and semaphore remain those derived for the candidate.
// No counter expires or resets during this operation. Active admissions or
// counter operations refuse the transfer so the caller can finish draining.
func (l *Limiter) InheritCounters(previous *Limiter) error {
	if l == nil || previous == nil || l == previous {
		return errors.New("admission: generation handoff requires distinct initialized limiters")
	}
	// Neither acquisition waits: concurrent reverse handoffs cannot deadlock,
	// and no writer blocks a queued admission from finishing its drain.
	if !previous.lifecycle.TryLock() {
		return ErrNotQuiescent
	}
	defer previous.lifecycle.Unlock()
	if !l.lifecycle.TryLock() {
		return ErrNotQuiescent
	}
	defer l.lifecycle.Unlock()
	previous.mu.Lock()
	defer previous.mu.Unlock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous.slots == nil || l.slots == nil {
		return errors.New("admission: generation handoff requires initialized limiters")
	}
	if previous.waiting != 0 || l.waiting != 0 || len(previous.slots) != previous.concurrency || len(l.slots) != l.concurrency {
		return ErrNotQuiescent
	}
	if len(l.ipHits) != 0 || len(l.metaHits) != 0 || len(l.issuerRefreshes) != 0 || len(l.accounts) != 0 {
		return ErrCountersNotEmpty
	}
	if l.shared != nil && (!reflect.TypeOf(l.shared).Comparable() || l.shared != previous.shared) {
		return ErrSharedBackendChanged
	}
	l.ipHits = cloneWindows(previous.ipHits)
	l.metaHits = cloneWindows(previous.metaHits)
	l.issuerRefreshes = cloneWindows(previous.issuerRefreshes)
	l.accounts = maps.Clone(previous.accounts)
	l.shared = previous.shared
	if l.log == nil {
		l.log = previous.log
	}
	return nil
}

func cloneWindows(previous map[string][]time.Time) map[string][]time.Time {
	out := make(map[string][]time.Time, len(previous))
	for subject, hits := range previous {
		out[subject] = slices.Clone(hits)
	}
	return out
}
