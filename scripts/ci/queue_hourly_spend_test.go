package ci

import "testing"

// TestIncidentHourSpendStaysSlightlyOverTheBudget builds the incident-hour spend from
// per-call costs and fails if the throttled total exceeds the stated bound. Measured inputs
// come from counting calls in the pr-gate fixtures (a dev publish 42, a queue publish 33, a
// completed group pass 41) and from the incident run listing (2026-11-23 21:00-22:00 UTC:
// 42 dev publishes, 17 queue publishes, 1 group, 34 dev discovers, 75 retry runs). Estimated
// inputs: the other workflows (under 100) and the share of events coalescing removes.
func TestIncidentHourSpendStaysSlightlyOverTheBudget(t *testing.T) {
	const (
		limit        = 1000
		reserve      = 250 // kept for merge-queue group jobs
		nonGroupCost = 42  // measured, the largest non-group pass
		groupCost    = 41  // measured
		otherCeiling = 100 // estimated, other workflows
		concurrency  = 4   // estimated: non-group jobs that can pass the gate at the same instant
	)
	// Unthrottled incident hour, measured counts x measured costs.
	before := 42*42 + 17*33 + 1*groupCost + 34*3 + 75*2 + otherCeiling
	// With this PR: coalescing leaves about 16 dev publishes (estimated from the incident's
	// event mix: 21 sweep + 15 workflow_run + 6 pull_request_target, sweep capped at 5, one
	// publish per head), the sweep reads one count, and a retry run lists jobs once.
	after := 16*42 + 10*33 + 1*groupCost + 34*3 + 30*1 + otherCeiling
	if before < 2*limit {
		t.Fatalf("the unthrottled incident hour must exceed twice the budget to be the incident, got %d", before)
	}
	// Non-group work stops when remaining-42 < 250, so it can spend at most limit-reserve+42,
	// plus what jobs already past the gate still spend: concurrency x cost.
	nonGroupBound := limit - reserve + nonGroupCost + (concurrency-1)*nonGroupCost
	// Group jobs may spend the reserve; one pass is 41 and a rate-limited pass is retried once.
	groupBound := reserve + groupCost
	bound := nonGroupBound + groupBound
	// The throttle admits non-group demand only up to its bound; the group reserve is extra.
	spent := min(after-groupCost, nonGroupBound) + groupCost
	if spent > limit {
		t.Fatalf("the incident hour spends %d of %d after the throttle", spent, limit)
	}
	// Worst case: every concurrent job passes the gate just above the threshold and a group
	// job spends the whole reserve. Stated bound: at most five non-group passes (210) over.
	if over := bound - limit; over > 5*nonGroupCost {
		t.Fatalf("the worst case %d overshoots the budget by %d, more than five non-group passes", bound, over)
	}
	if after-groupCost <= nonGroupBound {
		t.Fatalf("the estimated demand %d fits under the throttle bound %d, so the throttle is not what holds the budget", after-groupCost, nonGroupBound)
	}
}
