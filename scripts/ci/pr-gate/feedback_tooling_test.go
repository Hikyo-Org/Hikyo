package main

import (
	"fmt"
	"testing"
)

// The default-branch publisher already binds admission to whole-workflow
// success. A third mandatory lane must remain compatible without making its
// failure an admission success while the workflow change lands.
func TestQueueAdmissionWithIndependentToolingFeedback(t *testing.T) {
	for _, result := range []string{"success", "failure", "cancelled", "timed_out", "in_progress"} {
		t.Run(result, func(t *testing.T) {
			f := newQueueFixture(t)
			f.feedbackRun.Conclusion = result
			if result == "in_progress" {
				f.feedbackRun.Status, f.feedbackRun.Conclusion = result, ""
			}
			f.feedbackRefresh()
			r := f.feedbackRun
			jobs := []job{
				{ID: 201, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"},
				{ID: 202, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (frontend)", Status: "completed", Conclusion: "success"},
				{ID: 203, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (tooling)", Status: "completed", Conclusion: result},
			}
			if result == "in_progress" {
				jobs[2].Status, jobs[2].Conclusion = result, ""
			}
			f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": len(jobs), "jobs": jobs}
			if err := f.admit(); err != nil {
				t.Fatal(err)
			}
			if result == "success" {
				if len(f.api.writes) == 0 || f.api.writes[len(f.api.writes)-1]["conclusion"] != "success" {
					t.Fatal("complete three-lane feedback was not admitted")
				}
			} else {
				f.assertNoSuccess(t)
			}
		})
	}
}
