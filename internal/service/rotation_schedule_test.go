package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
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
			// The superseded credential, minted at the executor's own formula, is valid
			// at least until published + interval + delay + slack even when its mint
			// was a whole publish budget before the publish. Dropping the budget term
			// from the formula makes this fail.
			validUntil := rotationCandidateValidUntil(published.Add(-rotationPublishBudget), interval, delay)
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

// A resumed or recreated rotation derives its window from the delivered
// credential, exactly as a published cycle does.
func TestRotationInheritedWindow(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	delay := time.Hour
	delivered := now.Add(30 * time.Hour)
	w := rotationInheritedWindow(now, delivered, delay, false)
	if !w.HasD || !w.D.Equal(delivered.Add(-(rotationPublishBudget + delay))) {
		t.Fatalf("window = %+v, want D = delivered - (budget + delay)", w)
	}
	if got := rotationInheritedWindow(delivered, delivered, delay, true); got.HasD {
		t.Error("a credential already past its VALID UNTIL has nothing to protect")
	}
	late := delivered.Add(-time.Hour)
	if got := rotationInheritedWindow(late, delivered, delay, true); got.HasD {
		t.Error("the cycle's first run is always allowed, even past D")
	}
	if got := rotationInheritedWindow(late, delivered, delay, false); !got.HasD || rotationMayStart(late, 0, got) {
		t.Error("a retry past D must be refused")
	}
}

// Resume schedules the first run so the delivered credential cannot lapse first.
func TestRotationResumeNext(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	interval, delay := 24*time.Hour, time.Hour
	if got := rotationResumeNext(now, interval, delay, time.Time{}); !got.Equal(now.Add(interval)) {
		t.Errorf("no delivered credential: next = %s, want one interval away", got)
	}
	if got := rotationResumeNext(now, interval, delay, now.Add(10*24*time.Hour)); !got.Equal(now.Add(interval)) {
		t.Errorf("a long-lived credential: next = %s, want one interval away", got)
	}
	delivered := now.Add(26 * time.Hour)
	if got := rotationResumeNext(now, interval, delay, delivered); !got.Equal(rotationExtendDue(delivered)) {
		t.Errorf("a credential due before the interval: next = %s, want its extension margin %s", got, rotationExtendDue(delivered))
	}
	if got := rotationResumeNext(now, interval, delay, now.Add(-time.Hour)); !got.Equal(now) {
		t.Errorf("a lapsed credential is restored now, got %s", got)
	}
}

// An infrastructure fault is no attempt, but it backs off like any retry: five
// minutes doubling to the six hour cap, never sitting at the floor, never past
// the cap, and clamped to D so the last try inside the window still happens.
func TestRotationInfraBackoffGrowsAndCaps(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var last time.Duration
	for prior := int64(0); prior < 40; prior++ {
		got := rotationInfraNext(now, prior, rotationWindow{}).Sub(now)
		if got < last {
			t.Fatalf("backoff shrank after %d prior faults: %s after %s", prior, got, last)
		}
		if got > rotationRetryWindow {
			t.Fatalf("backoff %s after %d prior faults exceeds the %s cap", got, prior, rotationRetryWindow)
		}
		last = got
	}
	if got := rotationInfraNext(now, 0, rotationWindow{}).Sub(now); got != rotationBackoffFloor {
		t.Fatalf("first infrastructure fault backs off %s, want the %s floor", got, rotationBackoffFloor)
	}
	if got := rotationInfraNext(now, 1, rotationWindow{}).Sub(now); got != 2*rotationBackoffFloor {
		t.Fatalf("second infrastructure fault backs off %s, want %s", got, 2*rotationBackoffFloor)
	}
	if last != rotationRetryWindow {
		t.Fatalf("a long outage settles at %s, want the %s cap", last, rotationRetryWindow)
	}
	w := rotationCycleWindow(now, 24*time.Hour)
	near := w.D.Add(-time.Minute)
	if got := rotationInfraNext(near, 10, w); !got.Equal(w.D) {
		t.Fatalf("a backoff past D lands at %s, want D %s", got, w.D)
	}
	if got := rotationInfraNext(w.D.Add(time.Minute), 10, w); got.Before(w.D) {
		t.Fatalf("a fault already past D never schedules backwards: %s", got)
	}
}

// A credential that cannot be decrypted is a definite fault that counts as an
// attempt; only the datastore and the keyring are infrastructure.
func TestProviderOpenFaultClassification(t *testing.T) {
	cause := errors.New("boom")
	for stage, infra := range map[string]bool{"material": true, "keyring": true, "decrypt": false, "kind": false, "factory": false} {
		if got := providerInfraFault(&providerOpenError{stage: stage, err: cause}); got != infra {
			t.Errorf("stage %q infra = %v, want %v", stage, got, infra)
		}
	}
	if providerInfraFault(&providerOpenError{stage: "material", err: store.ErrNoProviderCredential}) {
		t.Error("a provider without a credential is the operator's state, not infrastructure")
	}
	if providerInfraFault(&providerOpenError{stage: "material", err: store.ErrNotFound}) {
		t.Error("a missing provider is not infrastructure")
	}
	if providerInfraFault(cause) {
		t.Error("an error that did not come from opening a provider is not infrastructure")
	}
}

// An expiry exit needs VALID UNTIL to be past by the clock-skew margin: the
// role's expiry is judged by the database server's clock, not this process's.
func TestRoleExpiredNeedsTheSkewMargin(t *testing.T) {
	until := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	edge := until.Add(rotationExpirySkew)
	cases := []struct {
		name      string
		now       time.Time
		uncertain bool
		valid     time.Time
		want      bool
	}{
		{"before VALID UNTIL", until.Add(-time.Second), false, until, false},
		{"just past VALID UNTIL", until.Add(time.Second), false, until, false},
		{"exactly at the margin", edge, false, until, false},
		{"one second past the margin", edge.Add(time.Second), false, until, true},
		{"uncertain extend never expires", edge.Add(time.Hour), true, until, false},
		{"unrecorded VALID UNTIL never expires", edge.Add(time.Hour), false, time.Time{}, false},
	}
	for _, c := range cases {
		if got := roleExpired(c.valid, c.uncertain, c.now); got != c.want {
			t.Errorf("%s: expired = %v, want %v", c.name, got, c.want)
		}
	}
}
