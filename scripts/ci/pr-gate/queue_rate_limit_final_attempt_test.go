package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// A spent API window says nothing about the PR. On the last attempt the reserve
// would publish the terminal refusal; when the failure is the rate limit it must
// leave the check blocking and pending for a later window instead (#1553).
func TestRateLimitedFinalAttemptStaysPendingNotTerminal(t *testing.T) {
	for attempt := 1; attempt <= admissionMaxAttempts; attempt++ {
		t.Run("attempt "+strconv.Itoa(attempt), func(t *testing.T) {
			t.Setenv("GITHUB_RUN_ATTEMPT", strconv.Itoa(attempt))
			f := newQueueFixture(t)
			f.c.verify = func(context.Context, int, string) (policyDecision, error) {
				return policyDecision{}, fmt.Errorf("verify: %w", &githubReadError{path: "repos", status: 403, rateLimited: true, cause: errors.New("rate limit exhausted beyond this job's remaining budget")})
			}
			b := admissionBudget{Started: time.Now(), Deadline: time.Now().Add(admissionReserve + 2*time.Second)}
			if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), b); err == nil {
				t.Fatal("a rate-limited run must still fail visibly")
			}
			f.assertNoSuccess(t)
			for _, w := range f.api.writes {
				if w["status"] == "completed" {
					t.Fatalf("attempt %d published a terminal verdict for a rate limit: %+v", attempt, w)
				}
			}
		})
	}
}

// reconcile joins the work error before the block error, and errors.As stops at the
// first *githubReadError whatever its flag: any rate-limited branch must count.
func TestRateLimitedFailureWalksEveryBranchOfAJoinedError(t *testing.T) {
	limited := &githubReadError{path: "repos", status: 403, rateLimited: true, cause: errors.New("limit")}
	ordinary := &githubReadError{path: "repos/other", status: 500, cause: errors.New("boom")}
	for name, err := range map[string]error{
		"limited after an ordinary error":      errors.Join(errors.New("work failed"), limited),
		"limited after another read error":     errors.Join(ordinary, limited),
		"limited first":                        errors.Join(limited, errors.New("block failed")),
		"limited under a wrapper in a join":    errors.Join(errors.New("work"), fmt.Errorf("verify: %w", limited)),
		"limited beside an ordinary one in %w": fmt.Errorf("%w: %w", ordinary, limited),
		"nested joins":                         errors.Join(errors.Join(ordinary, errors.New("x")), errors.Join(errors.New("y"), limited)),
	} {
		if !rateLimitedFailure(err) {
			t.Errorf("%s: a rate-limited branch was missed", name)
		}
	}
	for name, err := range map[string]error{
		"nil":                       nil,
		"ordinary only":             errors.Join(errors.New("work"), ordinary),
		"a limited-looking message": errors.Join(errors.New("rate limit"), ordinary),
	} {
		if rateLimitedFailure(err) {
			t.Errorf("%s: wrongly classified as rate-limited", name)
		}
	}
}
