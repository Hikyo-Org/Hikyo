package ci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

// maxParallelLanes reads max-parallel from the two gated publisher fan-outs (dev publishers
// and queue head workers) so the concurrency margin is derived from the workflows.
func maxParallelLanes(t *testing.T) int {
	t.Helper()
	total := 0
	for _, file := range []string{"ci-dev-gate.yml", "ci-queue-worker.yml"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", file))
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`(?m)^\s+max-parallel: (\d+)$`).FindAllStringSubmatch(string(raw), -1)
		if len(m) != 1 {
			t.Fatalf("%s must cap its publisher matrix with exactly one max-parallel, found %d", file, len(m))
		}
		n, err := strconv.Atoi(m[0][1])
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	return total
}

// TestReserveSizingIsPinnedAndDocumented pins the sizing behind the interim exception of
// 2026-11-26 (#1598): R = 480 is below the honest bound, so ungated spend can exceed it in a
// busy hour while a group is in flight and a merge group can then be dequeued and
// re-queued. Every figure is measured (M) or estimated (E); docs/development/ci.md must
// state the same numbers.
func TestReserveSizingIsPinnedAndDocumented(t *testing.T) {
	const limit = 1000
	reserve, nonGroupCost, groupCost := reserveConstants(t)
	margin := maxParallelLanes(t) * nonGroupCost // E: lanes past the check at once, now capped by max-parallel
	const (
		discovery   = 102 // M: 34 event discovers x 3 reads, incident hour 2026-11-23 21:00-22:00 UTC
		retryFirst  = 75  // M: 75 executed retry runs x at least one jobs listing
		requested   = 36  // M: requested passes of two groups, 2 x about 18 (fixtures)
		waitSweeps  = 62  // E: lower bound, 2 sweeps an hour x 31 (6 run listings + 25 jobs listings)
		attempt3    = 42  // E: one attempt-3 bypass (reserve.go) per hour, spent ungated
		inRepoOther = 60  // E: about 55 ungated in-repo runs an hour x at least 1 call
		// E: the dev-revoke first pass (pr-gate --dev-revoke): the repository, the open PR list and the dev tip
		// cost 3 once, plus one create per head. 7 gate-dev push runs in the busiest hour of
		// the last 100, 7 open dev PRs on 2026-11-26. Never deferred by the reserve, so it is ungated.
		revokeEvents = 7
		revokeFixed  = 3
		revokeHeads  = 7
	)
	revoke := revokeEvents * (revokeFixed + revokeHeads)
	const postWrite = 34        // E: the post-write revocation listing, one request per successful dev pass, at most one per event-driven discover (34 in the incident hour)
	groups := 2 * 3 * groupCost // M: two overlapping groups, three attempts each
	measured := discovery + retryFirst + groups + requested + margin
	honest := measured + waitSweeps + attempt3 + inRepoOther + revoke + postWrite
	if measured != 627 || revoke != 70 || honest != 895 {
		t.Fatalf("sizing changed: measured %d (want 627), honest lower bound %d (want 895, 791 plus the 70 of the revocation first pass and the 34 of its post-write check)", measured, honest)
	}
	if reserve >= honest {
		t.Fatalf("R=%d now covers the honest bound %d: drop the interim exception from the PR body and docs", reserve, honest)
	}
	gatedBound := limit - (reserve + nonGroupCost) + margin
	worst := gatedBound + (honest - margin)
	if gatedBound != 646 || worst != 1373 {
		t.Fatalf("pinned figures changed: gated bound %d (want 646), worst %d (want 1373)", gatedBound, worst)
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "development", "ci.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#### Interim exception to the delay-only rule (2026-11-26, ends when #1598 lands)", "https://github.com/Hikyo-Org/Hikyo/issues/1598", "R = 480", "627", "895", "dequeued and re-queued", "1,373", "dev-revoke"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("docs/development/ci.md must state the interim exception sizing: missing %q", want)
		}
	}
}
