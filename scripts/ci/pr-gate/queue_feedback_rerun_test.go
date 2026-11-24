package main

import "testing"

// The gates reconcile at the start of every feedback attempt. GitHub sends requested,
// in_progress and completed for a first attempt, but a rerun sends no requested, so a
// rerun's only start callback is in_progress (the workflow jobs take in_progress only
// for run_attempt > 1; scripts/ci/feedback_event_sequence_test.go pins that sequence).
// The reconcile that callback starts reads the live run, which is now an unfinished
// attempt 2, and publishes a pending record over attempt 1's success. This pins that
// guarantee for every unfinished state the live run can show.
func TestFeedbackRerunNeverLeavesAStaleSuccess(t *testing.T) {
	for _, status := range []string{"requested", "queued", "waiting", "pending", "in_progress"} {
		for _, name := range []string{"queue", "dev"} {
			t.Run(name+"/"+status, func(t *testing.T) {
				f := newQueueFixture(t)
				if name == "dev" {
					f = newDevFixture(t)
				}
				// The same run, rerun: attempt 2 is unfinished and attempt 1's success is gone.
				f.feedbackRun.Attempt = 2
				f.feedbackRun.Status = status
				f.feedbackRun.Conclusion = ""
				f.feedbackRefresh()
				if err := f.admit(); err != nil {
					t.Fatalf("admission of a head with an unfinished feedback rerun failed: %v", err)
				}
				f.assertNoSuccess(t)
				if len(f.api.writes) == 0 {
					t.Fatal("an unfinished rerun must publish a pending record over the earlier success")
				}
				if last := f.api.writes[len(f.api.writes)-1]; last["conclusion"] == "success" || last["status"] == "completed" {
					t.Fatalf("last record is not pending: %+v", last)
				}
			})
		}
	}
}
