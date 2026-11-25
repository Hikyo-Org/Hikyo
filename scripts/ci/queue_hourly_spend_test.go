package ci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// reserveConstants parses the reserve and the per-pass costs out of every source that
// states them and fails if two sources disagree, so a change to one place cannot leave the
// others behind: groupReserve and nonGroupCost in pr-gate/reserve.go, reserve=, other_cost=
// and group_cost= in ci-queue-retry.yml (every occurrence), reserve= and cost= in
// discover-dev-prs.sh. The group cost has one source: the retry workflow.
func reserveConstants(t *testing.T) (reserve, nonGroupCost, groupCost int) {
	t.Helper()
	root := repositoryRoot(t)
	read := func(parts ...string) string {
		raw, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	all := func(source, pattern string) []int {
		var found []int
		for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(source, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatal(err)
			}
			found = append(found, n)
		}
		return found
	}
	agree := func(what string, want int, got []int, atLeast int) {
		t.Helper()
		if len(got) < atLeast {
			t.Fatalf("%s: expected at least %d statements, found %d", what, atLeast, len(got))
		}
		for _, n := range got {
			if n != want {
				t.Fatalf("%s says %d, the Go gate says %d: the sources drifted apart", what, n, want)
			}
		}
	}
	goSource := read("scripts", "ci", "pr-gate", "reserve.go")
	retry := read(".github", "workflows", "ci-queue-retry.yml")
	discover := read("scripts", "ci", "discover-dev-prs.sh")
	one := func(source, pattern string) int {
		got := all(source, pattern)
		if len(got) != 1 {
			t.Fatalf("expected one match for %s, found %d", pattern, len(got))
		}
		return got[0]
	}
	reserve = one(goSource, `(?m)^const groupReserve = (\d+)$`)
	nonGroupCost = one(goSource, `(?m)^const nonGroupCost = (\d+)$`)
	groupCost = one(retry, `(?m)^\s+group_cost=(\d+)$`)
	agree("ci-queue-retry.yml reserve", reserve, all(retry, `(?m)^\s+reserve=(\d+)$`), 2)
	agree("ci-queue-retry.yml other_cost", nonGroupCost, all(retry, `(?m)^\s+other_cost=(\d+)$`), 2)
	agree("discover-dev-prs.sh reserve", reserve, all(discover, `(?m)^reserve=(\d+)$`), 1)
	agree("discover-dev-prs.sh cost", nonGroupCost, all(discover, `(?m)^cost=(\d+)$`), 1)
	return reserve, nonGroupCost, groupCost
}

func TestReserveConstantsAgreeAcrossGoAndShell(t *testing.T) {
	reserve, nonGroupCost, groupCost := reserveConstants(t)
	if reserve != 480 || nonGroupCost != 42 || groupCost != 41 {
		t.Fatalf("the documented figures changed (480, 42, 41), update docs/development/ci.md and the PR derivation: %d %d %d", reserve, nonGroupCost, groupCost)
	}
}

// TestIncidentHourSpendIsPinned builds the incident-hour spend from per-call costs and pins
// the throttled total. The reserve and costs come from the sources (reserveConstants).
//
// Measured: per-pass costs counted in the pr-gate fixtures (dev publish 42, queue publish 33,
// a group pass 41, a dev discover 3) and the incident hour's run counts from the run listing
// (2026-11-23 21:00-22:00 UTC: 42 dev publishes, 17 queue publishes, 1 group, 34 discovers,
// 75 retry runs). Estimated: the counts after coalescing (16 dev publishes, 10 queue
// publishes), 30 retry listings, 100 for workflows this repository does not control, and 4
// gated jobs passing the check at the same instant.
//
// Gated spenders (publishers, queue publishers, the sweep's routing target) defer when
// remaining - 42 < reserve. Ungated spenders cannot defer: dev discovery on events (gating it
// would drop the event or hold a 5-minute job for up to an hour), the retry listings that
// complete group jobs, and foreign workflows. Group passes may spend the reserve.
func TestIncidentHourSpendIsPinned(t *testing.T) {
	const (
		limit       = 1000
		concurrency = 4 // estimated
	)
	reserve, nonGroupCost, groupCost := reserveConstants(t)
	const (
		discoverCost = 3   // measured
		otherWork    = 100 // estimated, foreign workflows
		retryLists   = 30  // estimated
	)
	gatedDemand := 16*nonGroupCost + 10*33
	ungated := 34*discoverCost + retryLists + otherWork
	group := groupCost
	// Gated work starts only while remaining >= reserve + cost, then up to `concurrency`
	// jobs started at the margin still finish.
	gatedBound := limit - (reserve + nonGroupCost) + concurrency*nonGroupCost
	spent := min(gatedDemand, gatedBound) + ungated + group
	// The worst case of the reserve's own derivation: two overlapping groups of three attempts.
	groupWorst := 2 * 3 * groupCost
	worst := min(gatedDemand, gatedBound) + ungated + groupWorst
	if gatedDemand <= gatedBound {
		t.Fatalf("incident demand %d fits under the gated bound %d, the throttle would not be what holds it", gatedDemand, gatedBound)
	}
	// The reserve must cover what nothing defers plus the groups: otherwise a merge group
	// can be starved by this repository's own spend and delayed past its timeout.
	if ungated+group > reserve || ungated+groupWorst > reserve {
		t.Fatalf("the reserve %d does not cover ungated %d plus a group pass %d (worst %d)", reserve, ungated, group, groupWorst)
	}
	// The owner accepts about 25% over the limit, as delay only.
	if worst > limit+limit/4 {
		t.Fatalf("the worst case %d overshoots the accepted quarter", worst)
	}
	if spent != 919 || worst != 1124 || gatedBound != 646 || ungated != 232 {
		t.Fatalf("pinned figures changed: spent %d (want 919), worst %d (want 1124), gated bound %d (want 646), ungated %d (want 232)", spent, worst, gatedBound, ungated)
	}
}
