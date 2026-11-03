package main

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// exhaustedBudget reproduces setup consuming its original budget: the work
// context ends after 25ms and the reserve runs on the remaining fifteen seconds.
func exhaustedBudget() admissionBudget {
	return admissionBudget{Started: time.Now().Add(-45 * time.Second), Deadline: time.Now().Add(admissionReserve + 25*time.Millisecond)}
}

func hangingVerifier(f *queueFixture) {
	f.c.verify = func(ctx context.Context, _ int, _ string) (policyDecision, error) {
		<-ctx.Done()
		return policyDecision{}, ctx.Err()
	}
}

// A budget overrun is a flake, not a verdict. While a rerun with a new budget
// will follow, the record must keep blocking without being terminal; only the
// last attempt may publish the refusal.
func TestBudgetExhaustionPublishesNonterminalBlockWhileRerunsRemain(t *testing.T) {
	for attempt := 1; attempt <= admissionMaxAttempts; attempt++ {
		t.Run("attempt "+strconv.Itoa(attempt), func(t *testing.T) {
			t.Setenv("GITHUB_RUN_ATTEMPT", strconv.Itoa(attempt))
			f := newQueueFixture(t)
			hangingVerifier(f)
			err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), exhaustedBudget())
			if err == nil || !strings.Contains(err.Error(), "budget exhausted") {
				t.Fatalf("the run must still fail visibly: %v", err)
			}
			f.assertNoSuccess(t)
			got := f.api.writes[len(f.api.writes)-1]
			if attempt < admissionMaxAttempts {
				if got["status"] == "completed" || got["conclusion"] != nil && got["conclusion"] != "" {
					t.Fatalf("attempt %d published a terminal verdict although a rerun follows: %+v", attempt, got)
				}
				output, _ := got["output"].(map[string]any)
				if summary, _ := output["summary"].(string); !strings.Contains(summary, "rerun") {
					t.Fatalf("the blocking record does not say a rerun follows: %+v", got)
				}
				return
			}
			if got["status"] != "completed" || got["conclusion"] != "failure" {
				t.Fatalf("the last attempt must publish the terminal refusal: %+v", got)
			}
		})
	}
}

func TestBudgetExhaustionWithoutAnAttemptNumberStaysTerminal(t *testing.T) {
	for _, attempt := range []string{"", "0", "-1", "x", "4"} {
		t.Run("attempt "+attempt, func(t *testing.T) {
			t.Setenv("GITHUB_RUN_ATTEMPT", attempt)
			f := newQueueFixture(t)
			hangingVerifier(f)
			if err := f.q.executeTargeted(context.Background(), "pull_request_target", targetedFixtureEvent(f), exhaustedBudget()); err == nil {
				t.Fatal("exhausted budget passed")
			}
			if got := f.api.writes[len(f.api.writes)-1]; got["status"] != "completed" || got["conclusion"] != "failure" {
				t.Fatalf("nothing promises a rerun, so the refusal must be terminal: %+v", got)
			}
		})
	}
}

// The work clock can end just after the pass published its verdict. That
// verdict is fresher than anything the reserve could write.
func TestBudgetEndingAfterPublishedVerdictKeepsTheVerdict(t *testing.T) {
	t.Setenv("GITHUB_RUN_ATTEMPT", "3")
	f := newQueueFixture(t)
	e := targetedFixtureEvent(f)
	b := admissionBudget{Started: time.Now(), Deadline: time.Now().Add(admissionReserve + 300*time.Millisecond)}
	err := f.q.withAdmissionBudget(context.Background(), b, func(work context.Context) error {
		if err := f.q.execute(work, "pull_request_target", e); err != nil {
			return err
		}
		<-work.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("a published verdict was reported as a budget failure: %v", err)
	}
	last := f.api.writes[len(f.api.writes)-1]
	if last["conclusion"] != "success" {
		t.Fatalf("the published verdict was replaced: %+v", last)
	}
}

// The final authority checks are independent reads of one phase. Run serially
// they cost the setup-inclusive budget about as much as everything before them;
// this holds the policy verifier until the DCO check and the final feedback read
// are in flight beside it, which only a concurrent final phase can satisfy.
func TestFinalAdmissionChecksRunTogether(t *testing.T) {
	f := newQueueFixture(t)
	var verifies, dcos atomic.Int32
	dcoStarted, feedbackStarted := make(chan struct{}), make(chan struct{})
	f.q.dco = func(context.Context, pull) (int, error) {
		if dcos.Add(1) == 2 {
			close(dcoStarted)
		}
		return 0, nil
	}
	f.api.readHook = func(path string, n int) {
		if strings.Contains(path, "actions/runs/101/attempts/1/jobs?") && n == 2 {
			close(feedbackStarted)
		}
	}
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		if verifies.Add(1) == 2 {
			for name, started := range map[string]chan struct{}{"DCO": dcoStarted, "feedback": feedbackStarted} {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Errorf("the final %s check had not started while the policy verifier ran: the final phase is serial", name)
				}
			}
		}
		return policyDecision{}, nil
	}
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if got := f.api.writes[len(f.api.writes)-1]["conclusion"]; got != "success" {
		t.Fatalf("concurrent final checks changed the verdict: %v", got)
	}
}
