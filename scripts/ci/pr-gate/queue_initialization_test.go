package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A decoder may populate authenticated identity before rejecting trailing data.
// Initialization failure must still replace a prior owned green with a blocker.
type repositoryReadErrorAPI struct{ api }

func (a repositoryReadErrorAPI) read(ctx context.Context, path string, out any) error {
	if err := a.api.read(ctx, path, out); err != nil {
		return err
	}
	if path == "repos/Hikyo-Org/Hikyo" {
		return errors.New("invalid trailing repository response data")
	}
	return nil
}

func TestAdmissionInitializationRequiresOnlyItsOwnWorkflow(t *testing.T) {
	for _, route := range []string{"head worker", "direct callback", "feedback callback", "manual callback"} {
		for _, fault := range []string{"none", "repository ID", "repository name", "repository read", "repository setting", "check name", "feedback ID", "feedback path", "feedback name", "feedback read"} {
			t.Run(route+"/"+fault, func(t *testing.T) {
				f := newQueueFixture(t)
				candidate := f.c.endpoint("actions/workflows/ci-candidate.yml")
				f.api.responses[candidate] = errors.New("irrelevant candidate workflow unavailable")
				feedback := workflow{ID: 21, Path: feedbackWorkflowPath, Name: "ci-feedback"}
				repo := f.pr.Base.Repo
				switch fault {
				case "repository ID":
					repo.ID = 0
				case "repository name":
					repo.FullName = "other/repo"
				case "repository setting":
					f.c.repoName = "invalid"
				case "check name":
					f.c.checkName = "untrusted-check"
				case "feedback ID":
					feedback.ID = 0
				case "feedback path":
					feedback.Path = ".github/workflows/other.yml"
				case "feedback name":
					feedback.Name = "other"
				}
				f.api.responses["repos/"+f.pr.Base.Repo.FullName] = repo
				if fault == "repository read" {
					f.api.responses["repos/"+f.pr.Base.Repo.FullName] = errors.New("repository unavailable")
				}
				f.api.responses[f.c.endpoint("actions/workflows/ci-feedback.yml")] = feedback
				if fault == "feedback read" {
					f.api.responses[f.c.endpoint("actions/workflows/ci-feedback.yml")] = errors.New("feedback unavailable")
				}
				var err error
				if route == "head worker" {
					err = snapshotAdmission(f)
				} else {
					name, e := "pull_request_target", targetedFixtureEvent(f)
					if route == "feedback callback" {
						name, e.Run = "workflow_run", f.feedbackRun
					} else if route == "manual callback" {
						name = "workflow_dispatch"
					}
					start := time.Now()
					err = f.q.executeTargeted(context.Background(), name, e, admissionBudget{Started: start, Deadline: start.Add(time.Minute)})
				}
				if fault == "none" {
					if err != nil {
						t.Fatalf("admission depended on legacy workflow metadata: %v", err)
					}
					if len(f.api.checks) == 0 || f.api.checks[len(f.api.checks)-1].Conclusion != "success" || f.verifyCalls != 2 {
						t.Fatal("complete initial and final admission proof was not published")
					}
				} else {
					if err == nil {
						t.Fatal("invalid admission authority accepted")
					}
					f.assertNoSuccess(t)
				}
				if f.api.reads[candidate] != 0 {
					t.Fatal("admission read the unused legacy candidate workflow")
				}
			})
		}
	}
}

// The retired workflow's file is gone, so no queue path may depend on looking it up.
func TestGroupInitializationIgnoresRetiredCandidateWorkflow(t *testing.T) {
	f := newQueueFixture(t)
	path := f.c.endpoint("actions/workflows/ci-candidate.yml")
	f.api.responses[path] = errors.New("candidate unavailable")
	if err := f.group(); err != nil {
		t.Fatal(err)
	}
	if f.api.reads[path] != 0 {
		t.Fatal("group initialization read the retired workflow")
	}
}
