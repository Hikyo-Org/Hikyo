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
// 2026-11-26 (#1598). The pinned worst case is 1,401 requests an hour (40.1% over
// the window, above the 25% of Q2). Marc accepted 1,396 (39.6%) for the #1598 interim; on 2026-11-26
// the coordinator accepted figures up to a ceiling of about 1,420 an hour (42%). The exception ends when #1598 lands.
// R = 480 is below the honest bound, so ungated spend can exceed it in a
// busy hour while a group is in flight and a merge group can then be dequeued and
// re-queued. Every figure is measured (M) or estimated (E); docs/development/ci.md must
// state the same numbers.
func TestReserveSizingIsPinnedAndDocumented(t *testing.T) {
	const limit = 1000
	reserve, nonGroupCost, groupCost := reserveConstants(t)
	// E: a dev pass that creates its record lists the head's checks again after its success write
	// (suite-aware listing, 3 reads), so a dev pass costs the gate's 42 plus 3. It runs inside the pass,
	// after enforceReserve, so it is gated spend and belongs in the margin, not in the ungated rows.
	const postWriteReads = 3
	// E: the one ancestry read (compare main...head) of the backmerge exception in devPublisherContract,
	// spent only where dev's copy of the publisher differs from main's. A pass that takes it costs 42 + 3 + 1.
	const backmergeReads = 1
	// E: dev lists a second feedback source (ci-feedback-retarget), 4 more reads per pass than main.
	// The owner accepted ~1,425/h (42.5%) on dev on 2026-11-27 as an interim exception, ceiling ~1,430/h;
	// the extra 4 reads per pass come from dev's retarget feedback source; follow-up #1612; removed by #1598.
	const retargetFeedbackReads = 4
	devPassCost := nonGroupCost + postWriteReads + backmergeReads + retargetFeedbackReads
	margin := maxParallelLanes(t) * devPassCost // E: lanes past the check at once, capped by max-parallel
	const (
		discovery   = 102 // M: 34 event discovers x 3 reads, incident hour 2026-11-23 21:00-22:00 UTC
		retryFirst  = 75  // M: 75 executed retry runs x at least one jobs listing
		requested   = 36  // M: requested passes of two groups, 2 x about 18 (fixtures)
		waitSweeps  = 62  // E: lower bound, 2 sweeps an hour x 31 (6 run listings + 25 jobs listings)
		inRepoOther = 60  // E: about 55 ungated in-repo runs an hour x at least 1 call
		// E: the dev-revoke first pass (pr-gate --dev-revoke): the repository, the open PR list and the dev tip
		// cost 3 once, plus one create per head. 7 gate-dev push runs in the busiest hour of
		// the last 100, 7 open dev PRs on 2026-11-26. Never deferred by the reserve, so it is ungated.
		revokeEvents = 7
		revokeFixed  = 3
		revokeHeads  = 7
		// M: one pass per hour, spent ungated at the final attempt, whose confirmation listing never
		// catches up. Counted on the dev fixture by TestDevAttempt3UnconfirmedPassCostIsPinnedForTheSizing:
		// 95 requests, 90 reads and 5 writes on dev (87 and 82 on main) = the pass 38 + 2, the confirmation listing 12, the
		// fail-closed POST with its fail-safe re-list and re-POST 6 + 2, and the admission-reserve
		// refusal phase (withAdmissionBudget, refuseAdmission) 26 + 1. It costs this on every
		// attempt; attempts 1 and 2 are gated by the reserve like any pass. A created-record
		// pass that confirms costs 41 + 2, within the 45 of the margin. A pass that takes the backmerge
		// exception reads the head's ancestry once more: 96 on dev (88 on main), counted by
		// TestDevBackmergeAttempt3UnconfirmedPassCostIsPinnedForTheSizing, which is the figure pinned here.
		attempt3 = 96
	)
	revoke := revokeEvents * (revokeFixed + revokeHeads)
	groups := 2 * 3 * groupCost // M: two overlapping groups, three attempts each
	measured := discovery + retryFirst + groups + requested + margin
	honest := measured + waitSweeps + attempt3 + inRepoOther + revoke
	if measured != 659 || revoke != 70 || attempt3 != 96 || honest != 947 {
		t.Fatalf("sizing changed: measured %d (want 659), honest lower bound %d (want 947: 659 plus 62, 96 (attempt-3 bypass of a backmerge pass with a lagging confirmation), 60 and the 70 of the revocation first pass)", measured, honest)
	}
	if reserve >= honest {
		t.Fatalf("R=%d now covers the honest bound %d: drop the interim exception from the PR body and docs", reserve, honest)
	}
	gatedBound := limit - (reserve + nonGroupCost) + margin
	worst := gatedBound + (honest - margin)
	if gatedBound != 678 || worst != 1425 {
		t.Fatalf("pinned figures changed: gated bound %d (want 678), worst %d (want 1425)", gatedBound, worst)
	}
	measuredIn, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "ci", "pr-gate", "dev_revoke_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(measuredIn), "const attempt3PassRequests = 95") {
		t.Fatal("the attempt-3 cost pinned here must be the one scripts/ci/pr-gate/dev_revoke_test.go counts")
	}
	backmergeIn, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "ci", "pr-gate", "dev_backmerge_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(backmergeIn), "attempt3BackmergePassRequests = 96") || !strings.Contains(string(backmergeIn), "backmergePassReads            = 46") {
		t.Fatal("the backmerge pass cost pinned here must be the one scripts/ci/pr-gate/dev_backmerge_test.go counts")
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "development", "ci.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#### Interim exception to the delay-only rule (2026-11-26, ends when #1598 lands)", "https://github.com/Hikyo-Org/Hikyo/issues/1598", "R = 480", "659", "947", "dequeued and re-queued", "1,425", "42.5%", "accepted this figure on 2026-11-26", "the owner accepted ~1,425/h (42.5%) on dev on 2026-11-27 as an interim exception, ceiling ~1,430/h; the extra 4 reads per pass come from dev's retarget feedback source; follow-up #1612; removed by #1598", "dev-revoke"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("docs/development/ci.md must state the interim exception sizing: missing %q", want)
		}
	}
}
