package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestQueueFeedbackWorkflowAndRunListReadsOverlap(t *testing.T) {
	f := newQueueFixture(t)
	release := make(chan struct{})
	a := &delayedReadAPI{api: f.api, started: make(chan string, 2), release: release}
	f.c.api = a
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type result struct {
		run *run
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := f.q.feedback(ctx, f.pr)
		done <- result{r, err}
	}()
	paths := map[string]bool{}
	for range 2 {
		select {
		case path := <-a.started:
			paths[path] = true
		case <-ctx.Done():
			<-done
			t.Fatal("feedback run listing waited for the workflow definition read")
		}
	}
	wantWorkflow := f.c.endpoint("actions/workflows/ci-feedback.yml")
	wantRuns := f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))
	if len(paths) != 2 || !paths[wantWorkflow] || !paths[wantRuns] {
		cancel()
		<-done
		t.Fatalf("unexpected independent feedback reads: %v", paths)
	}
	close(release)
	got := <-done
	if got.err != nil || got.run == nil || got.run.ID != f.feedbackRun.ID || got.run.Conclusion != "success" {
		t.Fatalf("feedback identity and job proof failed after overlapping reads: run=%+v err=%v", got.run, got.err)
	}
	if a.count.Load() != 3 || a.maximum.Load() != 2 || a.active.Load() != 0 {
		t.Fatalf("feedback did not join its inputs before checking jobs: reads=%d maximum=%d active=%d", a.count.Load(), a.maximum.Load(), a.active.Load())
	}
}

func TestQueueFeedbackReadFailureCancelsAndJoinsItsSibling(t *testing.T) {
	for _, endpoint := range []string{"actions/workflows/ci-feedback.yml", "runs"} {
		t.Run(endpoint, func(t *testing.T) {
			f := newQueueFixture(t)
			path := f.c.endpoint(endpoint)
			if endpoint == "runs" {
				path = f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))
			}
			failure := errors.New("deliberate feedback identity read failure")
			a := &failingPairedReadAPI{api: f.api, failingPath: path, failure: failure, ready: make(chan struct{})}
			f.c.api = a
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			r, err := f.q.feedback(ctx, f.pr)
			if r != nil || !errors.Is(err, failure) || ctx.Err() != nil || a.cancelled.Load() != 1 || a.active.Load() != 0 {
				t.Fatalf("feedback failure did not cancel and join its sibling: run=%+v err=%v parent=%v cancelled=%d active=%d", r, err, ctx.Err(), a.cancelled.Load(), a.active.Load())
			}
			if len(f.api.reads) != 0 || len(f.api.writes) != 0 {
				t.Fatal("failed feedback identity read reached jobs or publication")
			}
		})
	}
}

func TestQueueFeedbackJoinsAndValidatesBothInputsBeforeJobs(t *testing.T) {
	for _, change := range []string{"workflow identity", "run identity", "run list"} {
		t.Run(change, func(t *testing.T) {
			f := newQueueFixture(t)
			switch change {
			case "workflow identity":
				f.api.responses[f.c.endpoint("actions/workflows/ci-feedback.yml")] = workflow{ID: 99, Path: feedbackWorkflowPath, Name: "ci-feedback"}
			case "run identity":
				f.feedbackRun.WorkflowID = 99
				f.feedbackRefresh()
			case "run list":
				path := f.c.endpoint(fmt.Sprintf("actions/workflows/ci-feedback.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))
				f.api.responses[path] = map[string]any{"total_count": 1, "workflow_runs": nil}
			}
			r, err := f.q.feedback(context.Background(), f.pr)
			if r != nil || err == nil {
				t.Fatalf("contradictory feedback identity accepted: run=%+v err=%v", r, err)
			}
			total := 0
			for _, count := range f.api.reads {
				total += count
			}
			if total != 2 || len(f.api.writes) != 0 {
				t.Fatalf("invalid feedback identity reached dependent jobs or publication: reads=%v writes=%v", f.api.reads, f.api.writes)
			}
		})
	}
}

func TestQueueHealthyAdmissionPreservesBudgetSpentOnSetup(t *testing.T) {
	f := newQueueFixture(t)
	snapshotPriorFailure(f)
	started := time.Now().Add(-26 * time.Second)
	budget := admissionBudget{Started: started, Deadline: started.Add(time.Minute)}
	workDeadline := budget.Deadline.Add(-admissionReserve)
	assertWorkDeadline := func(ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Equal(workDeadline) {
			t.Errorf("proof reset the setup-inclusive work deadline: got=%v present=%t want=%v", deadline, ok, workDeadline)
		}
	}
	verifications, dcoChecks := 0, 0
	f.c.verify = func(ctx context.Context, _ int, _ string) (policyDecision, error) {
		assertWorkDeadline(ctx)
		verifications++
		return policyDecision{}, nil
	}
	f.q.dco = func(ctx context.Context, _ pull) (int, error) {
		assertWorkDeadline(ctx)
		dcoChecks++
		return 0, nil
	}
	f.c.api = deadlineAPI{api: f.api, readHook: func(ctx context.Context, _ string) error {
		assertWorkDeadline(ctx)
		return nil
	}}
	e := f.event
	e.Inputs.Work = fmt.Sprintf(`[{"head":%q,"kind":"admission"}]`, f.pr.Head.SHA)
	if err := f.q.withAdmissionBudget(context.Background(), budget, func(ctx context.Context) error {
		return f.q.runHead(ctx, "workflow_dispatch", e, "admission")
	}); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, count := range f.api.reads {
		total += count
	}
	if total != 22 || verifications != 2 || dcoChecks != 2 || newestQueueCheck(f.api.checks).Conclusion != "success" {
		t.Fatalf("setup-aged admission did not retain its complete proof: reads=%d authority=%d DCO=%d", total, verifications, dcoChecks)
	}
}
