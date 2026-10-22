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
					feedback.Path = workflowPath
				case "feedback name":
					feedback.Name = "pr-candidate"
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

func TestLegacyInitializationStillRequiresCandidateWorkflow(t *testing.T) {
	for _, fault := range []string{"valid", "missing", "unavailable", "ID", "path", "name"} {
		t.Run(fault, func(t *testing.T) {
			f := newFixture(t)
			path := f.c.endpoint("actions/workflows/ci-candidate.yml")
			w := workflow{ID: 20, Path: workflowPath, Name: "pr-candidate"}
			switch fault {
			case "ID":
				w.ID = 0
			case "path":
				w.Path = feedbackWorkflowPath
			case "name":
				w.Name = "ci-feedback"
			}
			f.api.responses[path] = w
			if fault == "missing" {
				delete(f.api.responses, path)
			} else if fault == "unavailable" {
				f.api.responses[path] = errors.New("candidate unavailable")
			}
			err := f.execute()
			if fault == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				requireConclusion(t, f, "completed", "success")
			} else if err == nil || len(f.api.writes) != 0 {
				t.Fatalf("legacy identity refusal changed: error=%v, writes=%v", err, f.api.writes)
			}
			if f.api.reads[path] != 1 {
				t.Fatal("legacy initializer did not authenticate the candidate workflow")
			}
		})
	}
}

func TestGroupInitializationRetainsLegacyLookup(t *testing.T) {
	f := newQueueFixture(t)
	path := f.c.endpoint("actions/workflows/ci-candidate.yml")
	f.api.responses[path] = errors.New("candidate unavailable")
	if err := f.group(); err == nil {
		t.Fatal("group initialization behavior changed")
	}
	if f.api.reads[path] != 1 {
		t.Fatal("group initialization no longer reads the legacy workflow")
	}
	f.assertNoSuccess(t)
}
