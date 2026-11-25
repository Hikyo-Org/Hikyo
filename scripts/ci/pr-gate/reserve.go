package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

// groupReserve is the share of the shared token's hourly API window kept for the
// merge-queue group jobs (#1553). Merge CI takes 36 to 73 minutes, so at most two groups
// finish in one window, and each may run three attempts of about 41 reads: 2 x 3 x 41 =
// 246, rounded up to 250. Every job that does not gate a merge-queue head checks
// the free rate_limit endpoint first and defers instead of spending below it. Keep it in
// step with `reserve=250` in .github/workflows/ci-queue-retry.yml and the sweep in
// scripts/ci/discover-dev-prs.sh.
const groupReserve = 250

// nonGroupCost is the expected cost of one non-group pass (a dev publish takes about 42
// reads). A job that passes the check must also afford itself, so it defers below
// groupReserve+nonGroupCost, the same threshold the retry and wait workflows use
// (`other_cost=42` in .github/workflows/ci-queue-retry.yml) before they rerun one.
const nonGroupCost = 42

// heldBackByReserve reports the roles that must leave the reserve alone: dev admission,
// retirement, targeted queue admission and the admission and ambiguous head workers. A
// group job (plain --queue, --head-worker group), the group repair and the worker
// dispatch gate a merge-queue head and may spend it.
func heldBackByReserve(dev, discoverRetirement, retire, targeted bool, headWorker string) bool {
	return dev || discoverRetirement || retire || targeted || headWorker == "admission" || headWorker == "ambiguous"
}

// deferForGroupReserve fails a job that is not a group job when either bucket it may
// draw on (each judged by its absolute remaining, like the retry workflows) would be left
// with fewer than groupReserve requests after this job's own cost. The job writes nothing, so the
// check is left as it is; the failed Reconcile step makes ci-queue-retry.yml rerun it
// after the reset, group jobs first. A rate_limit read that fails does not block the
// job: the read is free and a broken one says nothing about the window.
func deferForGroupReserve(ctx context.Context, a api) error {
	// The retry workflow reruns nothing after the last attempt, so a deferral there would
	// leave the check with no completer. The last attempt spends instead; if the window is
	// truly spent it ends as the rate-limited final attempt, which stays pending.
	if attempt, err := strconv.Atoi(os.Getenv("GITHUB_RUN_ATTEMPT")); err == nil && attempt >= admissionMaxAttempts {
		return nil
	}
	var limits struct {
		Resources map[string]struct {
			Remaining *int `json:"remaining"`
		} `json:"resources"`
	}
	if a.read(ctx, "rate_limit", &limits) != nil {
		return nil
	}
	for _, name := range []string{"core", "graphql"} {
		if b, ok := limits.Resources[name]; ok && b.Remaining != nil && *b.Remaining-nonGroupCost < groupReserve {
			return fmt.Errorf("deferred: the %s rate limit has %d requests left, too few to leave the %d kept for merge-group jobs after this job's own %d; this job spent nothing and is rerun after the reset", name, *b.Remaining, groupReserve, nonGroupCost)
		}
	}
	return nil
}
