package shell

import (
	"sync"
	"sync/atomic"
	"time"
)

// limiter allows n events per window. It is a sliding window: a page that
// calls a shell route in a loop gets rate_limited, not a queue.
type limiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	events []time.Time
	now    func() time.Time
}

func newLimiter(n int, window time.Duration, now func() time.Time) *limiter {
	return &limiter{n: n, window: window, now: now}
}

func (l *limiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	keep := l.events[:0]
	for _, t := range l.events {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	l.events = keep
	if len(l.events) >= l.n {
		return false
	}
	l.events = append(l.events, now)
	return true
}

// slot is a one-at-a-time gate. Taking it returns the release function.
type slot struct{ busy atomic.Bool }

func (s *slot) take() (release func(), ok bool) {
	if !s.busy.CompareAndSwap(false, true) {
		return nil, false
	}
	return func() { s.busy.Store(false) }, true
}
