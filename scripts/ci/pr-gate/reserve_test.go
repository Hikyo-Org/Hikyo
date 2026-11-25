package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func reserveWindow(t *testing.T, core, graphql int) {
	t.Helper()
	fakeGH(t, fmt.Sprintf(`echo '{"resources":{"core":{"remaining":%d,"limit":1000,"reset":1},"graphql":{"remaining":%d,"limit":1000,"reset":1}}}'`, core, graphql))
}

// A job that does not gate a merge-queue head defers below the reserve and runs at it;
// either bucket can hold it back, and what it prints names no response text.
func TestNonGroupJobDefersBelowTheReserveAndRunsAtIt(t *testing.T) {
	for _, tc := range []struct {
		name          string
		core, graphql int
		deferred      bool
	}{
		// The job must afford itself: it runs only while its own cost still leaves the reserve.
		{"core one short of reserve plus cost", groupReserve + nonGroupCost - 1, 900, true},
		{"core at reserve plus cost", groupReserve + nonGroupCost, 900, false},
		{"core between the reserve and reserve plus cost", groupReserve + 20, 900, true},
		{"graphql one short of reserve plus cost", 900, groupReserve + nonGroupCost - 1, true},
		{"graphql at reserve plus cost", 900, groupReserve + nonGroupCost, false},
		{"both healthy", 900, 900, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reserveWindow(t, tc.core, tc.graphql)
			err := deferForGroupReserve(t.Context(), ghAPI{})
			if (err != nil) != tc.deferred {
				t.Fatalf("core=%d graphql=%d: err=%v, want deferred=%t", tc.core, tc.graphql, err, tc.deferred)
			}
			if err != nil && !strings.Contains(err.Error(), "spent nothing") {
				t.Fatalf("the deferral must say it spent nothing: %v", err)
			}
		})
	}
}

// The free rate_limit read failing says nothing about the window: the job still runs.
func TestReserveCheckDoesNotBlockOnAFailedRateLimitRead(t *testing.T) {
	fakeGH(t, `echo 'gh: HTTP 502' >&2; exit 1`)
	if err := deferForGroupReserve(t.Context(), ghAPI{}); err != nil {
		t.Fatalf("an unreadable rate_limit must not stop a job: %v", err)
	}
}

// main applies the check through enforceReserve, called once for dev admission and once for
// every other role before any work starts; the behaviour per role is tested below.
func TestMainCallsTheReserveDecision(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if strings.Count(text, "enforceReserve(") != 2 || strings.Contains(text, "deferForGroupReserve(") || strings.Contains(text, "heldBackByReserve(") {
		t.Fatal("main must reach the reserve only through enforceReserve")
	}
	if strings.Index(text, "enforceReserve(") > strings.Index(text, "err := runDevCommand(") {
		t.Fatal("dev admission must check the reserve before it runs")
	}
}

// With the window spent, every held-back role is refused and every group role runs.
func TestEnforceReserveRefusesEachHeldBackRoleOnASpentWindow(t *testing.T) {
	reserveWindow(t, 10, 10)
	for _, tc := range []struct {
		name                                  string
		dev, discoverRetirement, retire, targ bool
		headWorker                            string
		refused                               bool
	}{
		{"dev admission", true, false, false, false, "", true},
		{"retirement discovery", false, true, false, false, "", true},
		{"retirement", false, false, true, false, "", true},
		{"targeted queue admission", false, false, false, true, "", true},
		{"admission head worker", false, false, false, false, "admission", true},
		{"ambiguous head worker", false, false, false, false, "ambiguous", true},
		{"group job (plain --queue)", false, false, false, false, "", false},
		{"group head worker", false, false, false, false, "group", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := enforceReserve(t.Context(), ghAPI{}, tc.dev, tc.discoverRetirement, tc.retire, tc.targ, tc.headWorker)
			if (err != nil) != tc.refused {
				t.Fatalf("enforceReserve err=%v, want refused=%t", err, tc.refused)
			}
		})
	}
}

// Group jobs gate a merge-queue head and spend into the reserve; every other role defers.
func TestOnlyGroupRolesMaySpendTheReserve(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		dev, discoverRetirement, retire, targ bool
		headWorker                            string
		held                                  bool
	}{
		{"dev admission", true, false, false, false, "", true},
		{"retirement discovery", false, true, false, false, "", true},
		{"retirement", false, false, true, false, "", true},
		{"targeted queue admission", false, false, false, true, "", true},
		{"admission head worker", false, false, false, false, "admission", true},
		{"ambiguous head worker", false, false, false, false, "ambiguous", true},
		{"group job (plain --queue)", false, false, false, false, "", false},
		{"group head worker", false, false, false, false, "group", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := heldBackByReserve(tc.dev, tc.discoverRetirement, tc.retire, tc.targ, tc.headWorker); got != tc.held {
				t.Fatalf("heldBackByReserve = %t, want %t", got, tc.held)
			}
		})
	}
}

// A deferral costs a run attempt, and nothing reruns after the last one, so the last
// attempt spends instead of deferring.
func TestTheLastAttemptSpendsInsteadOfDeferring(t *testing.T) {
	reserveWindow(t, 10, 900)
	for attempt, deferred := range map[string]bool{"1": true, "2": true, "3": false} {
		t.Setenv("GITHUB_RUN_ATTEMPT", attempt)
		if err := deferForGroupReserve(t.Context(), ghAPI{}); (err != nil) != deferred {
			t.Fatalf("attempt %s: err=%v, want deferred=%t", attempt, err, deferred)
		}
	}
}
