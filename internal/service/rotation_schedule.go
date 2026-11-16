package service

import "time"

// The rotation failure schedule (#148, S2). One anchor throughout: the publish
// time P of the delivered credential. See docs/handoff/148-credential-rotation.md,
// "Failure, retry and credential expiry".
const (
	// rotationPublishBudget is how long a started run may take to publish. A run
	// that has not published by then is rolled back.
	rotationPublishBudget = time.Hour
	// rotationSlack is the validity a delivered credential keeps beyond
	// interval + delay, the room the retries live in.
	rotationSlack = 24 * time.Hour
	// rotationRetryWindow is the longest single backoff step; the extension
	// margin is twice it.
	rotationRetryWindow  = 6 * time.Hour
	rotationExtendMargin = 2 * rotationRetryWindow
	// rotationBackoffFloor is the first retry delay; it doubles up to the window.
	rotationBackoffFloor = 5 * time.Minute
	// rotationAttemptCap bounds the failed attempts of one cycle.
	rotationAttemptCap = 12
	// rotationLeaseTerm is one worker claim; it must exceed the provider deadline.
	rotationLeaseTerm = 2 * time.Minute
)

// rotationNever parks a run that owes nothing more: far past any real clock, and
// inside every column's range on both engines.
var rotationNever = time.Date(9000, 1, 1, 0, 0, 0, 0, time.UTC)

// rotationWindow is the retry window of one cycle. D is the last instant a run
// may START: while the superseded credential still has at least
// publish_budget + delay of validity left, so a permitted retry never truncates
// the overlap. A cycle with no superseded credential (the first run ever of a
// key) has no D and is bounded by the attempt cap only.
type rotationWindow struct {
	HasD bool
	D    time.Time
	// Since is the cut after which a failed run counts as an attempt of this
	// cycle: the publish time P, or zero for a cycle that began without one.
	Since time.Time
}

// rotationCycleWindow is the window of a cycle anchored at a publish P: the next
// scheduled attempt is S = P + interval and D = S + slack - publish_budget.
func rotationCycleWindow(published time.Time, interval time.Duration) rotationWindow {
	s := published.Add(interval)
	return rotationWindow{HasD: true, D: s.Add(rotationSlack - rotationPublishBudget), Since: published}
}

// rotationInheritedWindow is the window of a cycle that began over a credential
// it did not publish itself (a resumed or recreated rotation): it is anchored at
// the cycle's own first attempt.
func rotationInheritedWindow(first time.Time) rotationWindow {
	return rotationWindow{HasD: true, D: first.Add(rotationSlack - rotationPublishBudget)}
}

// rotationBackoff is the delay after the nth failed attempt of a cycle (n >= 1):
// five minutes, doubling, capped at the retry window.
func rotationBackoff(attempt int64) time.Duration {
	delay := rotationBackoffFloor
	for i := int64(1); i < attempt && delay < rotationRetryWindow; i++ {
		delay *= 2
	}
	if delay > rotationRetryWindow {
		delay = rotationRetryWindow
	}
	return delay
}

// rotationRetry decides what follows the attempt-th failed attempt (counting the
// one that just failed) at time now: the next attempt time, or exhausted when the
// attempt cap is spent or the window has closed. A backoff step that would land
// past D is clamped to D itself, the last permitted start, while D is still ahead.
func rotationRetry(now time.Time, attempt int64, w rotationWindow) (next time.Time, exhausted bool) {
	if attempt >= rotationAttemptCap {
		return time.Time{}, true
	}
	next = now.Add(rotationBackoff(attempt))
	if !w.HasD {
		return next, false
	}
	if next.After(w.D) {
		if !now.Before(w.D) {
			return time.Time{}, true
		}
		next = w.D
	}
	return next, false
}

// rotationMayStart reports whether a run may start at now: before D, with fewer
// than the cap of failed attempts spent.
func rotationMayStart(now time.Time, failedAttempts int64, w rotationWindow) bool {
	if failedAttempts >= rotationAttemptCap {
		return false
	}
	return !w.HasD || !now.After(w.D)
}

// rotationExtendDue is when a settled run's delivered credential falls due an
// extension: the margin before its VALID UNTIL.
func rotationExtendDue(validUntil time.Time) time.Time {
	if validUntil.IsZero() {
		return rotationNever
	}
	return validUntil.Add(-rotationExtendMargin)
}
