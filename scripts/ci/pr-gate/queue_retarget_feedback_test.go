package main

import (
	"context"
	"fmt"
	"testing"
)

// Observed on #1374: a body edit started a skipped ci-feedback run on the
// admitted head, and its newer suite hid the ci-dev-required record filed in
// ci-feedback's older suite. Edits now run ci-feedback-retarget, which calls
// ci-feedback only for a base retarget, so the gate reads both workflows.
func retargetRun(f *queueFixture, id int64) run {
	r := f.feedbackRun
	r.ID, r.WorkflowID, r.Path = id, 23, retargetWorkflowPath
	return r
}

func setJobs(f *queueFixture, r run, jobs ...job) {
	for i := range jobs {
		jobs[i].ID, jobs[i].RunID, jobs[i].Attempt = int64(400+i), r.ID, r.Attempt
		if jobs[i].HeadSHA == "" {
			jobs[i].HeadSHA = r.HeadSHA
		}
	}
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": len(jobs), "jobs": jobs}
}

func boundRun(t *testing.T, f *queueFixture) int64 {
	t.Helper()
	last := f.api.writes[len(f.api.writes)-1]
	b, err := decodeBinding(last["external_id"].(string))
	if err != nil || last["conclusion"] != "success" {
		t.Fatalf("admission did not succeed: %+v, %v", last, err)
	}
	return b.Run
}

func TestQueueRetargetFeedbackSelection(t *testing.T) {
	ok := func(name string) job { return job{Name: name, Status: "completed", Conclusion: "success"} }
	for _, tc := range []struct {
		name     string
		run      func(*queueFixture) run
		jobs     []job
		wantRun  int64 // 0 means admission must not succeed
		original bool  // the older ci-feedback run is the expected binding
	}{
		{name: "metadata edit falls through to the opened run", jobs: []job{{Name: retargetJobName, Status: "completed", Conclusion: "skipped"}}, original: true},
		{name: "retarget feedback supersedes the opened run", jobs: []job{ok("retarget / feedback (go)"), ok("retarget / feedback (frontend)")}, wantRun: 105},
		{name: "newer opened run supersedes an older retarget", run: func(f *queueFixture) run { return retargetRun(f, 99) }, jobs: []job{ok("retarget / feedback (go)"), ok("retarget / feedback (frontend)")}, original: true},
		{name: "failed retarget lane", jobs: []job{ok("retarget / feedback (go)"), {Name: "retarget / feedback (frontend)", Status: "completed", Conclusion: "failure"}}},
		{name: "missing retarget lane", jobs: []job{ok("retarget / feedback (go)")}},
		{name: "unprefixed lanes", jobs: []job{ok("feedback (go)"), ok("feedback (frontend)")}},
		{name: "unknown skipped job", jobs: []job{{Name: "other", Status: "completed", Conclusion: "skipped"}}},
		{name: "old ci-feedback skip spelling is not a retarget skip", jobs: []job{{Name: skippedFeedbackName, Status: "completed", Conclusion: "skipped"}}},
		{name: "retarget metadata lanes are not ignorable", jobs: []job{{Name: "retarget / metadata (ignored) (go)", Status: "completed", Conclusion: "skipped"}, {Name: "retarget / metadata (ignored) (frontend)", Status: "completed", Conclusion: "skipped"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newQueueFixture(t)
			r := retargetRun(f, 105)
			if tc.run != nil {
				r = tc.run(f)
			}
			if tc.jobs[0].Conclusion == "skipped" {
				r.Conclusion = "skipped"
			}
			f.retargetRefresh(r)
			setJobs(f, r, tc.jobs...)
			err := f.admit()
			if tc.wantRun == 0 && !tc.original {
				f.assertNoSuccess(t)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := tc.wantRun
			if tc.original {
				want = f.feedbackRun.ID
			}
			if got := boundRun(t, f); got != want {
				t.Fatalf("admission bound run %d, want %d", got, want)
			}
		})
	}
}

func TestDevRetargetCallbackAdmitsFromRetargetFeedback(t *testing.T) {
	f := newDevFixture(t)
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f.c.repo = f.pr.Base.Repo
	f.c.repo.DefaultBranch = "main"
	f.c.expectedHead = f.pr.Head.SHA
	r := retargetRun(f, 105)
	f.retargetRefresh(r)
	setJobs(f, r, job{Name: "retarget / feedback (go)", Status: "completed", Conclusion: "success"}, job{Name: "retarget / feedback (frontend)", Status: "completed", Conclusion: "success"}, job{Name: "retarget / feedback (tooling)", Status: "completed", Conclusion: "success"})
	e := event{Repository: f.c.repo, Run: r}
	if err := f.q.executeDev(context.Background(), "workflow_run", e, f.pr.Number); err != nil {
		t.Fatal(err)
	}
	if got := boundRun(t, f); got != r.ID {
		t.Fatalf("dev admission bound run %d, want the retarget run %d", got, r.ID)
	}
	e.Run.Path = ".github/workflows/other.yml"
	if err := f.q.executeDev(context.Background(), "workflow_run", e, f.pr.Number); err == nil {
		t.Fatal("dev callback accepted an unrelated workflow")
	}
}

// A metadata edit after a retarget skips its newer run, so the older retarget
// run with real feedback stays bound.
func TestQueueSkippedEditFallsThroughToEarlierRetarget(t *testing.T) {
	f := newQueueFixture(t)
	earlier, edit := retargetRun(f, 105), retargetRun(f, 106)
	edit.Conclusion = "skipped"
	f.retargetRefresh(edit, earlier)
	setJobs(f, earlier, job{Name: "retarget / feedback (go)", Status: "completed", Conclusion: "success"}, job{Name: "retarget / feedback (frontend)", Status: "completed", Conclusion: "success"})
	setJobs(f, edit, job{Name: retargetJobName, Status: "completed", Conclusion: "skipped"})
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	if got := boundRun(t, f); got != earlier.ID {
		t.Fatalf("admission bound run %d, want the earlier retarget run %d", got, earlier.ID)
	}
}

// GitHub can complete a skipped run before its job exists; that must not fall
// through to the older successful ci-feedback run.
func TestQueueSkippedRetargetWithoutJobsStaysPending(t *testing.T) {
	f := newQueueFixture(t)
	r := retargetRun(f, 105)
	r.Conclusion = "skipped"
	f.retargetRefresh(r)
	setJobs(f, r)
	_ = f.admit()
	f.assertNoSuccess(t)
}
