package main

import "testing"

// The gates run on the `requested` and `completed` callbacks of a feedback run,
// not on `in_progress` (#1553). `requested` is what keeps a rerun of an exact-head
// feedback run from leaving the earlier attempt's success admitting a merge:
// the reconcile it starts reads the live run, which is now unfinished, and
// publishes a pending record. This pins that guarantee for every unfinished state.
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
