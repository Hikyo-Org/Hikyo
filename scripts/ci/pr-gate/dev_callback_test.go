package main

import (
	"context"
	"testing"
)

// ci-feedback-retarget.yml shares the name `ci-feedback` so that gate-dev's
// callback reassesses a dev PR after a base retarget. Runs 43724319349 and
// 43724317672 failed when the publisher refused its path.
func TestDevCallbackAcceptsOnlyTheTwoFeedbackWorkflows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		mutate func(*event)
		want   bool
	}{
		{"feedback", feedbackWorkflowPath, nil, true},
		{"retarget", retargetWorkflowPath, nil, true},
		{"foreign workflow", ".github/workflows/other.yml", nil, false},
		{"retarget with a ref suffix", retargetWorkflowPath + "@refs/heads/dev", nil, false},
		{"foreign event", retargetWorkflowPath, func(e *event) { e.Run.Event = "push" }, false},
		{"foreign repository", retargetWorkflowPath, func(e *event) { e.Run.Repository.ID++ }, false},
		{"other head", retargetWorkflowPath, func(e *event) { e.Run.HeadSHA = "dddddddddddddddddddddddddddddddddddddddd" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDevFixture(t)
			f.c.repo = f.pr.Base.Repo
			f.c.repo.DefaultBranch = "main"
			e := event{Repository: f.c.repo, Run: run{Event: "pull_request", Path: tc.path, Repository: f.c.repo, HeadSHA: f.pr.Head.SHA}}
			if tc.mutate != nil {
				tc.mutate(&e)
			}
			err := f.q.executeDev(context.Background(), "workflow_run", e, f.pr.Number)
			if tc.want {
				if err != nil {
					t.Fatalf("callback refused: %v", err)
				}
				requireDevSuccess(t, f)
				return
			}
			if err == nil {
				t.Fatal("foreign callback was accepted")
			}
			f.assertNoSuccess(t)
			if len(f.api.writes) != 0 {
				t.Fatalf("a refused callback wrote: %+v", f.api.writes)
			}
		})
	}
}
