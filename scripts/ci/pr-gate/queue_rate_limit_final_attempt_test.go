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
