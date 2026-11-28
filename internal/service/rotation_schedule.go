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
// it did not publish itself (a resumed or recreated rotation). D is derived from
// that delivered credential, exactly as a published cycle derives it: the last
// instant a run may start while the credential still has publish_budget + delay of
// validity left. A credential already past its VALID UNTIL has nothing left to
// protect, so the cycle has no D. The cycle's very first run (firstRun) is always
// allowed: refusing the only chance to restore a nearly lapsed credential would
// guarantee the outage the window exists to prevent; its retries are bound by D.
func rotationInheritedWindow(now, delivered time.Time, delay time.Duration, firstRun bool) rotationWindow {
	if !now.Before(delivered) {
		return rotationWindow{}
	}
	d := delivered.Add(-(rotationPublishBudget + delay))
	if firstRun && !now.Before(d) {
		return rotationWindow{}
	}
	return rotationWindow{HasD: true, D: d}
}

// rotationCandidateValidUntil is the VALID UNTIL a candidate is minted with:
// mint + publish_budget + interval + delay + slack, so a publish anywhere inside
// the budget leaves at least interval + delay + slack of validity.
func rotationCandidateValidUntil(mintedAt time.Time, interval, delay time.Duration) time.Time {
	return mintedAt.Add(rotationPublishBudget + interval + delay + rotationSlack)
}

// rotationResumeNext is when a resumed rotation's first run is due: a full
// interval away, unless the delivered credential cannot wait that long. It is
// then due at the earlier of its last permitted start (publish_budget + delay of
// validity left) and its extension margin, and immediately when that has passed
// (a lapsed credential is restored now, not an interval from now).
func rotationResumeNext(now time.Time, interval, delay time.Duration, delivered time.Time) time.Time {
	next := now.Add(interval)
	if delivered.IsZero() {
		return next
	}
	deadline := delivered.Add(-(rotationPublishBudget + delay))
	if due := rotationExtendDue(delivered); due.Before(deadline) {
		deadline = due
	}
	if deadline.Before(next) {
		next = deadline
	}
	if next.Before(now) {
		next = now
	}
	return next
}

// rotationOverlapWake is when a run waiting out its overlap is next visited: its
// revoke deadline, or the delivered credential's extension margin when that comes
// first (an extension after VALID UNTIL is impossible, and a recovered run can be
// inside the margin already). A visit before the deadline extends and parks again;
// the deadline itself lives in the run's RevokeNotBefore, never only in this wake.
// A zero validUntil (nothing to extend) leaves the deadline.
func rotationOverlapWake(now, deadline, validUntil time.Time) time.Time {
	wake := deadline
	if !validUntil.IsZero() {
		if due := rotationExtendDue(validUntil); due.Before(wake) {
			wake = due
		}
	}
	if wake.Before(now) {
		wake = now
	}
	return wake
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

// rotationInfraNext is the next start after the (priorFaults+1)th infrastructure
// fault of a cycle (a definite failure in between does not reset the count): the same capped exponential backoff as any
// retry (5 minutes doubling to the 6 hour cap), clamped to D while D is ahead so a
// fault near the end of the window still gets its last try. It never exhausts: an
// infrastructure fault is no attempt, and a window that closes is caught by the
// start check, not here.
func rotationInfraNext(now time.Time, priorFaults int64, w rotationWindow) time.Time {
	next := now.Add(rotationBackoff(priorFaults + 1))
	if w.HasD && next.After(w.D) && now.Before(w.D) {
		return w.D
	}
	return next
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
