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
	if reserve != 250 || nonGroupCost != 42 || groupCost != 41 {
		t.Fatalf("the documented figures changed (250, 42, 41), update docs/development/ci.md and the PR derivation: %d %d %d", reserve, nonGroupCost, groupCost)
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
	if gatedDemand <= gatedBound {
		t.Fatalf("incident demand %d fits under the gated bound %d, the throttle would not be what holds it", gatedDemand, gatedBound)
	}
	if spent != 1149 || gatedBound != 876 || ungated != 232 {
		t.Fatalf("pinned figures changed: spent %d (want 1149), gated bound %d (want 876), ungated %d (want 232)", spent, gatedBound, ungated)
	}
	// The owner accepts about 25% over the limit as delay only.
	if over := spent - limit; over > limit/4 {
		t.Fatalf("the throttled hour overshoots by %d, more than the accepted quarter", over)
	}
	// Where the excess lands: once gated work stops, what is left for group passes is the limit
	// less everything spent. Ungated work plus one group pass exceeds the reserve by 23, and
	// with 4 jobs in flight at the margin nothing is left (-149): a group pass can be
	// rate-limited, which delays that merge group to the reset (it is not dropped).
	if left := limit - spent; left >= 0 || ungated+group-reserve != 23 {
		t.Fatalf("the reserve now covers ungated work plus a group pass (left %d, shortfall %d); update the PR body and ci.md", left, ungated+group-reserve)
	}
}
