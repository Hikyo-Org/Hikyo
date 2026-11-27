package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

type rateLimitCase struct {
	core, graphql     int           // remaining requests per bucket
	resetIn, budget   time.Duration // reset distance and the job's remaining budget
	failures          int           // repos/ reads that fail before one succeeds
	resetOverride     int64         // unix reset when nonzero
	budgetEnvDisabled bool
}

// rateLimitFixture fakes gh the way the real one answers a rate-limited call:
// the JSON body with its status on stdout and the wording on stderr (HTTP 403).
func rateLimitFixture(t *testing.T, c rateLimitCase) (attempts func() int, waits *[]time.Duration, now time.Time) {
	t.Helper()
	now = time.Unix(1_800_000_000, 0)
	old := rateLimitNow
	rateLimitNow = func() time.Time { return now }
	t.Cleanup(func() { rateLimitNow = old })
	if c.budgetEnvDisabled {
		t.Setenv("PR_GATE_DEADLINE", "")
	} else {
		t.Setenv("PR_GATE_DEADLINE", strconv.FormatInt(now.Add(c.budget).Unix(), 10))
	}
	reset := now.Add(c.resetIn).Unix()
	if c.resetOverride != 0 {
		reset = c.resetOverride
	}
	waits = recordReadWaits(t)
	attempts = fakeGH(t, fmt.Sprintf(`if [ "$2" = rate_limit ]; then echo '{"resources":{"core":{"remaining":%d,"reset":%d},"graphql":{"remaining":%d,"reset":%d}}}'; exit 0; fi
if [ "$n" -le %d ]; then echo '{"message":"API rate limit exceeded for installation.","status":"403"}'; echo 'gh: API rate limit exceeded for installation. (HTTP 403)' >&2; exit 1; fi
echo '{"ok":true}'`, c.core, reset, c.graphql, reset, c.failures+1))
	return attempts, waits, now
}

func TestGitHubReadWaitsForRateLimitResetWhenItFitsTheBudget(t *testing.T) {
	// n counts every gh call, so the rate_limit read is call 2 and the retry is call 3.
	attempts, waits, _ := rateLimitFixture(t, rateLimitCase{resetIn: 120 * time.Second, budget: 10 * time.Minute, failures: 1})
	var result struct {
		OK bool `json:"ok"`
	}
	if err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result); err != nil || !result.OK {
		t.Fatalf("read did not recover after the reset: %v %+v", err, result)
	}
	if want := 120*time.Second + rateLimitSkew; len(*waits) != 1 || (*waits)[0] != want || attempts() != 3 {
		t.Fatalf("waits=%v attempts=%d, want one wait of %v then the retried read", *waits, attempts(), want)
	}
}

func TestGitHubReadFailsVisiblyWhenRateLimitResetExceedsTheBudget(t *testing.T) {
	attempts, waits, now := rateLimitFixture(t, rateLimitCase{resetIn: 20 * time.Minute, budget: 2 * time.Minute, failures: 5})
	var result map[string]any
	err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result)
	reset := now.Add(20 * time.Minute).UTC().Format(time.RFC3339)
	var failure *githubReadError
	if err == nil || !strings.Contains(err.Error(), "rate limit exhausted until "+reset) || !errors.As(err, &failure) || failure.status != 403 {
		t.Fatalf("failure must name the rate limit and its reset time and keep the status: %v", err)
	}
	if strings.Contains(err.Error(), "installation") {
		t.Fatalf("diagnostic exposed gh stderr text: %v", err)
	}
	if len(*waits) != 0 || attempts() != 2 {
		t.Fatalf("waits=%v attempts=%d, want no wait and no retry", *waits, attempts())
	}
}

func TestGitHubReadDoesNotWaitWithoutAKnownBudget(t *testing.T) {
	_, waits, _ := rateLimitFixture(t, rateLimitCase{resetIn: time.Second, failures: 5, budgetEnvDisabled: true})
	var result map[string]any
	if err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result); err == nil || !strings.Contains(err.Error(), "rate limit exhausted until") || len(*waits) != 0 {
		t.Fatalf("an unbounded job must not sleep on a reset: err=%v waits=%v", err, *waits)
	}
}

func TestGitHubReadDoesNotWaitOnASecondaryLimit(t *testing.T) {
	// The core budget has requests left, so a reset wait would not help.
	attempts, waits, _ := rateLimitFixture(t, rateLimitCase{core: 4000, graphql: 4000, resetIn: time.Minute, budget: time.Hour, failures: 5})
	var result map[string]any
	err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result)
	if err == nil || strings.Contains(err.Error(), "exhausted") || len(*waits) != 0 || attempts() != 2 {
		t.Fatalf("a 403 with core budget left must stay terminal: err=%v waits=%v attempts=%d", err, *waits, attempts())
	}
}

func TestGitHubReadWaitsForTheResetOnlyOnce(t *testing.T) {
	// Still limited after the wait: report it instead of sleeping again.
	attempts, waits, _ := rateLimitFixture(t, rateLimitCase{resetIn: 30 * time.Second, budget: 10 * time.Minute, failures: 5})
	var result map[string]any
	if err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result); err == nil || len(*waits) != 1 || attempts() != 3 {
		t.Fatalf("err=%v waits=%v attempts=%d, want one wait then a reported failure", err, *waits, attempts())
	}
}

func TestGitHubGraphQLReadChecksTheGraphQLBucket(t *testing.T) {
	// Core is spent but GraphQL is not: waiting for the core reset would not help a GraphQL read.
	attempts, waits, _ := rateLimitFixture(t, rateLimitCase{core: 0, graphql: 4000, resetIn: time.Minute, budget: time.Hour, failures: 5})
	var result map[string]any
	if err := retryingGH.read(t.Context(), "graphql:query{repository{name}}", &result); err == nil || len(*waits) != 0 || attempts() != 2 {
		t.Fatalf("err=%v waits=%v attempts=%d, want no wait on a spent core bucket", err, *waits, attempts())
	}
}

// A cancelled reset wait (a sibling read failed, or the deadline raced the wait) is still
// the rate limit: dropping the flag would publish a terminal refusal on the final attempt.
func TestCancelledRateLimitWaitKeepsTheRateLimitClassification(t *testing.T) {
	rateLimitFixture(t, rateLimitCase{resetIn: 30 * time.Second, budget: 10 * time.Minute, failures: 5})
	old := readRetryWait
	readRetryWait = func(context.Context, time.Duration) error { return context.Canceled }
	t.Cleanup(func() { readRetryWait = old })
	var result map[string]any
	err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result)
	if err == nil || !rateLimitedFailure(err) {
		t.Fatalf("a cancelled reset wait lost the rate-limit classification: %v", err)
	}
}

func TestGitHubReadRefusesAnAbsurdReset(t *testing.T) {
	_, waits, now := rateLimitFixture(t, rateLimitCase{budget: time.Hour, failures: 5, resetOverride: 1<<62 - 1})
	_ = now
	var result map[string]any
	if err := retryingGH.read(t.Context(), "repos/Hikyo-Org/Hikyo", &result); err == nil || len(*waits) != 0 {
		t.Fatalf("an out-of-range reset must not become a wait: err=%v waits=%v", err, *waits)
	}
}
