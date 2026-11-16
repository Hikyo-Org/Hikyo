package service

import (
	"testing"
	"time"
)

// The schedule proof of docs/handoff/148-credential-rotation.md: over the whole
// configurable grid, the retry window D leaves the superseded credential valid
// for at least a publish budget plus the revocation delay, and two full retry
// windows fit between S and D.
func TestRotationScheduleGrid(t *testing.T) {
	intervals := []time.Duration{time.Hour, 24 * time.Hour, 90 * 24 * time.Hour}
	delays := []time.Duration{5 * time.Minute, 7 * 24 * time.Hour}
	for _, interval := range intervals {
		for _, delay := range append(delays, interval-time.Second) {
			if delay >= interval || delay < 5*time.Minute || delay > 7*24*time.Hour {
				continue
			}
			published := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			w := rotationCycleWindow(published, interval)
			s := published.Add(interval)
			if w.D.Sub(s) < 2*rotationRetryWindow {
				t.Errorf("interval %s delay %s: D-S = %s, want >= 2*retry_window", interval, delay, w.D.Sub(s))
			}
			// The superseded credential, minted at its own publish, is valid until
			// published + budget + interval + delay + slack at least.
			validUntil := published.Add(rotationPublishBudget + interval + delay + rotationSlack)
			if validUntil.Sub(w.D.Add(rotationPublishBudget+delay)) < 0 {
				t.Errorf("interval %s delay %s: superseded expires before D + budget + delay", interval, delay)
			}
			if !rotationMayStart(w.D, 0, w) {
				t.Errorf("interval %s: a start exactly at D must be allowed", interval)
			}
			if rotationMayStart(w.D.Add(time.Second), 0, w) {
				t.Errorf("interval %s: a start at D+1s must be refused", interval)
			}
		}
	}
}

func TestRotationBackoffAndRetry(t *testing.T) {
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 80 * time.Minute, 160 * time.Minute, 320 * time.Minute, 6 * time.Hour, 6 * time.Hour}
	for i, w := range want {
		if got := rotationBackoff(int64(i + 1)); got != w {
			t.Errorf("backoff(%d) = %s, want %s", i+1, got, w)
		}
	}
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	w := rotationCycleWindow(base, 24*time.Hour)
	// A step landing past D is clamped to D while D is still ahead...
	near := w.D.Add(-time.Minute)
	if next, exhausted := rotationRetry(near, 3, w); exhausted || !next.Equal(w.D) {
		t.Errorf("retry near D = %s %v, want D", next, exhausted)
	}
	// ...and exhausts once D has been reached.
	if _, exhausted := rotationRetry(w.D, 3, w); !exhausted {
		t.Error("retry at D must be exhausted")
	}
	if _, exhausted := rotationRetry(base, rotationAttemptCap, w); !exhausted {
		t.Error("the attempt cap must exhaust")
	}
	// A cycle with no superseded credential has no window, only the cap.
	if next, exhausted := rotationRetry(base, 4, rotationWindow{}); exhausted || !next.Equal(base.Add(40*time.Minute)) {
		t.Errorf("no-window retry = %s %v", next, exhausted)
	}
	if rotationMayStart(base, rotationAttemptCap, rotationWindow{}) {
		t.Error("no start once the cap is spent")
	}
}

func TestRotationExtendDue(t *testing.T) {
	vu := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	if got := rotationExtendDue(vu); !got.Equal(vu.Add(-12 * time.Hour)) {
		t.Errorf("extend due = %s", got)
	}
	if got := rotationExtendDue(time.Time{}); !got.Equal(rotationNever) {
		t.Errorf("zero valid until must park at never, got %s", got)
	}
}
